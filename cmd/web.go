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
	"sync"

	"github.com/jacobfgrant/emu-sync/internal/config"
	"github.com/jacobfgrant/emu-sync/internal/manifest"
	"github.com/jacobfgrant/emu-sync/internal/storage"
	"github.com/spf13/cobra"
)

//go:embed web_assets/index.html
var webAssets embed.FS

type webServer struct {
	groups   []*systemGroup
	cfg      *config.Config
	cfgPath  string
	server   *http.Server
	done     chan struct{} // closed when Save & Exit is clicked
	shutdown chan struct{} // closed just before server.Shutdown in all exit paths
	exitOnce sync.Once
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
}

type saveRequest struct {
	Selections map[string]bool `json:"selections"`
	Exit       bool            `json:"exit"`
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

	// Apply selections to groups
	for _, g := range ws.groups {
		for i := range g.Files {
			if sel, ok := req.Selections[g.Files[i].Key]; ok {
				g.Files[i].Selected = sel
			}
		}
	}

	syncDirs, syncExclude := encodeSelections(ws.groups)
	ws.cfg.Sync.SyncDirs = syncDirs
	ws.cfg.Sync.SyncExclude = syncExclude

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
browse available systems, toggle individual games, and save your
selections. The config file is updated when you click Save.`,
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
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/", ws.handleIndex)
		mux.HandleFunc("/api/systems", ws.handleSystems)
		mux.HandleFunc("/api/save", ws.handleSave)
		mux.HandleFunc("/api/wait", ws.handleWait)

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
		return nil
	},
}

func init() {
	webCmd.Flags().IntVar(&webPort, "port", 0, "port to listen on (0 = random)")
	rootCmd.AddCommand(webCmd)
}
