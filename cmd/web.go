package cmd

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/jacobfgrant/emu-sync/internal/config"
	"github.com/jacobfgrant/emu-sync/internal/manifest"
	"github.com/jacobfgrant/emu-sync/internal/progress"
	"github.com/jacobfgrant/emu-sync/internal/ratelimit"
	"github.com/jacobfgrant/emu-sync/internal/storage"
	intsync "github.com/jacobfgrant/emu-sync/internal/sync"
	"github.com/spf13/cobra"
)

//go:embed web_assets/index.html
var webAssets embed.FS

// eventLog captures JSON progress lines and fans them out to SSE clients.
// Implements io.Writer so it can be passed to progress.NewReporterWriter.
type eventLog struct {
	mu     sync.Mutex
	lines  []string
	done   bool
	notify chan struct{} // buffered(1), signaled on new event or finish
}

func newEventLog() *eventLog {
	return &eventLog{notify: make(chan struct{}, 1)}
}

func (el *eventLog) Write(p []byte) (int, error) {
	line := strings.TrimRight(string(p), "\n")
	el.mu.Lock()
	el.lines = append(el.lines, line)
	el.mu.Unlock()
	select {
	case el.notify <- struct{}{}:
	default:
	}
	return len(p), nil
}

func (el *eventLog) finish() {
	el.mu.Lock()
	el.done = true
	el.mu.Unlock()
	select {
	case el.notify <- struct{}{}:
	default:
	}
}

func (el *eventLog) read(from int) ([]string, bool) {
	el.mu.Lock()
	defer el.mu.Unlock()
	if from >= len(el.lines) {
		return nil, el.done
	}
	return el.lines[from:], el.done
}

type webServer struct {
	groups            []*systemGroup
	cfg               *config.Config
	cfgPath           string
	localManifestPath string       // overrides default; used by tests
	server            *http.Server
	done              chan struct{} // closed when Save & Exit is clicked
	shutdown          chan struct{} // closed just before server.Shutdown in all exit paths
	exitOnce          sync.Once

	client     storage.Backend   // for sync operations
	syncMu     sync.Mutex       // guards sync state below
	syncLog    *eventLog        // nil when idle
	syncDone   chan struct{}     // closed when sync goroutine finishes
	syncResult *intsync.Result  // set when sync finishes
}

type systemJSON struct {
	Dir                string     `json:"dir"`
	TotalSize          int64      `json:"totalSize"`
	TotalSizeFormatted string     `json:"totalSizeFormatted"`
	State              string     `json:"state"`
	SelectedCount      int        `json:"selectedCount"`
	FileCount          int        `json:"fileCount"`
	Files              []fileJSON `json:"files"`
}

type fileJSON struct {
	Key           string `json:"key"`
	Name          string `json:"name"`
	Size          int64  `json:"size"`
	SizeFormatted string `json:"sizeFormatted"`
	Selected      bool   `json:"selected"`
}

type systemsResponse struct {
	Systems               []systemJSON `json:"systems"`
	TotalSize             int64        `json:"totalSize"`
	TotalSizeFormatted    string       `json:"totalSizeFormatted"`
	SelectedSize          int64        `json:"selectedSize"`
	SelectedSizeFormatted string       `json:"selectedSizeFormatted"`
	Delete                bool         `json:"delete"`
}

type saveRequest struct {
	Selections map[string]bool `json:"selections"`
	Exit       bool            `json:"exit"`
	Delete     *bool           `json:"delete,omitempty"`
}

type saveResponse struct {
	OK         bool   `json:"ok"`
	ConfigPath string `json:"configPath,omitempty"`
	Error      string `json:"error,omitempty"`
}

func (ws *webServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	data, _ := webAssets.ReadFile("web_assets/index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func (ws *webServer) handleSystems(w http.ResponseWriter, r *http.Request) {
	var totalSize, selectedSize int64
	sysList := make([]systemJSON, 0, len(ws.groups))

	for _, g := range ws.groups {
		files := make([]fileJSON, 0, len(g.Files))
		for _, f := range g.Files {
			files = append(files, fileJSON{
				Key:           f.Key,
				Name:          f.Name,
				Size:          f.Size,
				SizeFormatted: formatSize(f.Size),
				Selected:      f.Selected,
			})
		}
		totalSize += g.TotalSize
		selectedSize += g.selectedSize()
		sysList = append(sysList, systemJSON{
			Dir:                g.Dir,
			TotalSize:          g.TotalSize,
			TotalSizeFormatted: formatSize(g.TotalSize),
			State:              g.groupState(),
			SelectedCount:      g.selectedCount(),
			FileCount:          len(g.Files),
			Files:              files,
		})
	}

	resp := systemsResponse{
		Systems:               sysList,
		TotalSize:             totalSize,
		TotalSizeFormatted:    formatSize(totalSize),
		SelectedSize:          selectedSize,
		SelectedSizeFormatted: formatSize(selectedSize),
		Delete:                ws.cfg.Sync.Delete,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (ws *webServer) handleSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req saveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(saveResponse{Error: "invalid request body"})
		return
	}

	ws.applySelections(req.Selections)
	if req.Delete != nil {
		ws.cfg.Sync.Delete = *req.Delete
	}

	if err := config.Write(ws.cfg, ws.cfgPath); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(saveResponse{Error: err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(saveResponse{OK: true, ConfigPath: ws.cfgPath})

	if req.Exit {
		ws.exitOnce.Do(func() { close(ws.done) })
	}
}

func (ws *webServer) handleWait(w http.ResponseWriter, r *http.Request) {
	select {
	case <-ws.shutdown:
	case <-r.Context().Done():
	}
	w.WriteHeader(http.StatusOK)
}

func (ws *webServer) applySelections(selections map[string]bool) {
	for _, g := range ws.groups {
		for i := range g.Files {
			if sel, ok := selections[g.Files[i].Key]; ok {
				g.Files[i].Selected = sel
			}
		}
	}
	syncDirs, syncExclude := encodeSelections(ws.groups)
	ws.cfg.Sync.SyncDirs = syncDirs
	ws.cfg.Sync.SyncExclude = syncExclude
}

func (ws *webServer) runSync() {
	log := ws.syncLog
	defer func() {
		log.finish()
		close(ws.syncDone)
	}()

	workers := ws.cfg.Sync.Workers
	if workers == 0 {
		workers = 1
	}
	maxRetries := ws.cfg.Sync.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}

	opts := intsync.Options{
		Workers:    workers,
		MaxRetries: maxRetries,
		Progress:   progress.NewReporterWriter(log),
	}

	result, err := intsync.Run(context.Background(), ws.client, ws.cfg, opts)

	ws.syncMu.Lock()
	if result != nil {
		ws.syncResult = result
	} else {
		ws.syncResult = &intsync.Result{Errors: []error{err}}
	}
	ws.syncMu.Unlock()
}

func (ws *webServer) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ws.syncMu.Lock()
	if ws.syncLog != nil {
		// Check if previous sync is still running
		select {
		case <-ws.syncDone:
			// Previous sync finished, allow a new one
		default:
			ws.syncMu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{"error": "sync already running"})
			return
		}
	}

	// Auto-save selections before starting sync
	var req saveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ws.syncMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	ws.applySelections(req.Selections)
	if req.Delete != nil {
		ws.cfg.Sync.Delete = *req.Delete
	}
	if err := config.Write(ws.cfg, ws.cfgPath); err != nil {
		ws.syncMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	ws.syncLog = newEventLog()
	ws.syncDone = make(chan struct{})
	ws.syncResult = nil
	ws.syncMu.Unlock()

	go ws.runSync()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})

	if req.Exit {
		ws.exitOnce.Do(func() { close(ws.done) })
	}
}

func (ws *webServer) handleSyncEvents(w http.ResponseWriter, r *http.Request) {
	ws.syncMu.Lock()
	log := ws.syncLog
	ws.syncMu.Unlock()

	if log == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	cursor := 0
	if id := r.Header.Get("Last-Event-ID"); id != "" {
		if n, err := strconv.Atoi(id); err == nil {
			cursor = n + 1
		}
	}

	for {
		lines, done := log.read(cursor)
		for i, line := range lines {
			fmt.Fprintf(w, "id: %d\ndata: %s\n\n", cursor+i, line)
		}
		cursor += len(lines)
		if len(lines) > 0 {
			flusher.Flush()
		}
		if done {
			return
		}
		select {
		case <-log.notify:
		case <-r.Context().Done():
			return
		case <-ws.shutdown:
			return
		}
	}
}

func (ws *webServer) handleSyncStatus(w http.ResponseWriter, r *http.Request) {
	ws.syncMu.Lock()
	log := ws.syncLog
	result := ws.syncResult
	ws.syncMu.Unlock()

	resp := map[string]interface{}{}

	if log == nil {
		resp["state"] = "idle"
	} else if result == nil {
		resp["state"] = "running"
	} else {
		if len(result.Errors) > 0 {
			resp["state"] = "failed"
		} else {
			resp["state"] = "complete"
		}
		resp["downloaded"] = len(result.Downloaded)
		resp["deleted"] = len(result.Deleted)
		resp["retained"] = len(result.Retained)
		resp["skipped"] = result.Skipped
		resp["errors"] = len(result.Errors)
		resp["summary"] = result.Summary()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (ws *webServer) handleVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ws.syncMu.Lock()
	running := ws.syncLog != nil
	if running {
		select {
		case <-ws.syncDone:
			running = false
		default:
		}
	}
	ws.syncMu.Unlock()

	if running {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "sync is running"})
		return
	}

	result, err := intsync.Verify(ws.cfg, ws.localManifestPath, false)
	resp := map[string]interface{}{}
	if err != nil {
		resp["error"] = err.Error()
	} else {
		resp["ok"] = len(result.OK)
		resp["mismatch"] = len(result.Mismatch)
		resp["missing"] = len(result.Missing)
		resp["errors"] = len(result.Errors)
		resp["summary"] = result.Summary()
		if len(result.Mismatch) > 0 {
			resp["mismatch_files"] = result.Mismatch
		}
		if len(result.Missing) > 0 {
			resp["missing_files"] = result.Missing
		}
		if len(result.Errors) > 0 {
			errStrs := make([]string, len(result.Errors))
			for i, e := range result.Errors {
				errStrs[i] = e.Error()
			}
			resp["error_details"] = errStrs
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler"}
	default:
		cmd = "xdg-open"
	}
	args = append(args, url)
	exec.Command(cmd, args...).Start()
}

var webPort int

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Open a browser UI to select which games to sync",
	Long: `Starts a local web server and opens a browser page where you can
browse available systems, toggle individual games, save your
selections, sync files, and verify local integrity.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgPath := cfgFile
		if cfgPath == "" {
			cfgPath = config.DefaultConfigPath()
		}

		cfg, err := config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		client := storage.NewClient(&cfg.Storage)

		if cfg.Sync.BandwidthLimit != "" {
			bps, err := config.ParseBandwidthLimit(cfg.Sync.BandwidthLimit)
			if err != nil {
				return fmt.Errorf("parsing bandwidth_limit: %w", err)
			}
			if bps > 0 {
				client.SetLimiter(ratelimit.NewLimiter(bps))
			}
		}

		fmt.Print("Downloading manifest...")
		remoteData, err := client.DownloadManifest(cmd.Context())
		if err != nil {
			fmt.Println(" failed")
			return fmt.Errorf("downloading manifest: %w", err)
		}
		fmt.Println(" ok")

		remote, err := manifest.ParseJSON(remoteData)
		if err != nil {
			return fmt.Errorf("parsing manifest: %w", err)
		}

		groups := buildGroups(remote, cfg)
		if len(groups) == 0 {
			fmt.Println("No files found in remote manifest.")
			return nil
		}

		ws := &webServer{
			groups:   groups,
			cfg:      cfg,
			cfgPath:  cfgPath,
			done:     make(chan struct{}),
			shutdown: make(chan struct{}),
			client:   client,
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/", ws.handleIndex)
		mux.HandleFunc("/api/systems", ws.handleSystems)
		mux.HandleFunc("/api/save", ws.handleSave)
		mux.HandleFunc("/api/wait", ws.handleWait)
		mux.HandleFunc("/api/sync", ws.handleSync)
		mux.HandleFunc("/api/sync/events", ws.handleSyncEvents)
		mux.HandleFunc("/api/sync/status", ws.handleSyncStatus)
		mux.HandleFunc("/api/verify", ws.handleVerify)

		port := webPort
		if !cmd.Flags().Changed("port") && cfg.Web.Port > 0 {
			port = cfg.Web.Port
		}

		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return fmt.Errorf("binding to port: %w", err)
		}

		ws.server = &http.Server{Handler: mux}
		url := fmt.Sprintf("http://127.0.0.1:%d", listener.Addr().(*net.TCPAddr).Port)

		fmt.Printf("Opening %s\n", url)
		fmt.Println("Press Ctrl+C to quit without saving.")
		openBrowser(url)

		// Run server in background
		errCh := make(chan error, 1)
		go func() { errCh <- ws.server.Serve(listener) }()

		// Wait for Save & Exit, Ctrl+C, or server error
		select {
		case <-ws.done:
			fmt.Printf("\nConfig saved. Shutting down.\n")
		case <-cmd.Context().Done():
			fmt.Println("\nShutting down.")
		case err := <-errCh:
			return err
		}

		// Unblock any /api/wait clients, then gracefully shut down
		close(ws.shutdown)
		ws.server.Shutdown(context.Background())

		// Wait for sync to finish if one is running
		ws.syncMu.Lock()
		syncDone := ws.syncDone
		ws.syncMu.Unlock()
		if syncDone != nil {
			select {
			case <-syncDone:
			default:
				fmt.Println("\nSync in progress. Waiting for it to finish (Ctrl+C to force quit)...")
				<-syncDone
			}
			ws.syncMu.Lock()
			result := ws.syncResult
			ws.syncMu.Unlock()
			if result != nil {
				fmt.Print(result.Summary())
			}
		}

		return nil
	},
}

func init() {
	webCmd.Flags().IntVar(&webPort, "port", 0, "port to listen on (0 = random)")
	rootCmd.AddCommand(webCmd)
}
