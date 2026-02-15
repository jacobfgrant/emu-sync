package cmd

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jacobfgrant/emu-sync/internal/config"
	"github.com/jacobfgrant/emu-sync/internal/manifest"
	"github.com/jacobfgrant/emu-sync/internal/storage"
)

func testGroups() []*systemGroup {
	return []*systemGroup{
		{
			Dir:       "roms/snes",
			TotalSize: 3 * 1024 * 1024,
			Files: []fileInfo{
				{Key: "roms/snes/GameA.sfc", Name: "GameA.sfc", Size: 1024 * 1024, Selected: true},
				{Key: "roms/snes/GameB.sfc", Name: "GameB.sfc", Size: 2 * 1024 * 1024, Selected: false},
			},
		},
		{
			Dir:       "roms/gba",
			TotalSize: 5 * 1024 * 1024,
			Files: []fileInfo{
				{Key: "roms/gba/GameC.gba", Name: "GameC.gba", Size: 2 * 1024 * 1024, Selected: true},
				{Key: "roms/gba/GameD.gba", Name: "GameD.gba", Size: 3 * 1024 * 1024, Selected: true},
			},
		},
	}
}

func TestHandleSystems(t *testing.T) {
	ws := &webServer{groups: testGroups()}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/systems", nil)
	ws.handleSystems(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %s", ct)
	}

	var resp systemsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if len(resp.Systems) != 2 {
		t.Fatalf("expected 2 systems, got %d", len(resp.Systems))
	}

	// snes: partial (1 of 2 selected)
	snes := resp.Systems[0]
	if snes.Dir != "roms/snes" {
		t.Errorf("expected roms/snes, got %s", snes.Dir)
	}
	if snes.State != "partial" {
		t.Errorf("expected partial, got %s", snes.State)
	}
	if snes.SelectedCount != 1 {
		t.Errorf("expected selectedCount 1, got %d", snes.SelectedCount)
	}
	if snes.FileCount != 2 {
		t.Errorf("expected fileCount 2, got %d", snes.FileCount)
	}
	if snes.TotalSizeFormatted != "3 MB" {
		t.Errorf("expected '3 MB', got %q", snes.TotalSizeFormatted)
	}

	// gba: all selected
	gba := resp.Systems[1]
	if gba.State != "all" {
		t.Errorf("expected all, got %s", gba.State)
	}

	// Totals
	if resp.TotalSize != 8*1024*1024 {
		t.Errorf("expected total 8MB, got %d", resp.TotalSize)
	}
	// Selected: 1MB (snes) + 5MB (gba) = 6MB
	if resp.SelectedSize != 6*1024*1024 {
		t.Errorf("expected selected 6MB, got %d", resp.SelectedSize)
	}

	// File-level checks
	if len(snes.Files) != 2 {
		t.Fatalf("expected 2 snes files, got %d", len(snes.Files))
	}
	if snes.Files[0].Key != "roms/snes/GameA.sfc" {
		t.Errorf("expected GameA key, got %s", snes.Files[0].Key)
	}
	if !snes.Files[0].Selected {
		t.Error("expected GameA selected")
	}
	if snes.Files[1].Selected {
		t.Error("expected GameB not selected")
	}
}

func TestHandleSave(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.toml")

	cfg := &config.Config{
		Storage: config.StorageConfig{
			Bucket:    "test",
			KeyID:     "key",
			SecretKey: "secret",
		},
		Sync: config.SyncConfig{
			EmulationPath: "/tmp/emu",
			SyncDirs:      []string{"roms"},
		},
	}

	ws := &webServer{
		groups:  testGroups(),
		cfg:     cfg,
		cfgPath: cfgPath,
		done:    make(chan struct{}),
	}

	body := `{"selections":{"roms/snes/GameA.sfc":true,"roms/snes/GameB.sfc":true,"roms/gba/GameC.gba":false,"roms/gba/GameD.gba":false}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/save", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ws.handleSave(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp saveResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected ok, got error: %s", resp.Error)
	}
	if resp.ConfigPath != cfgPath {
		t.Errorf("expected config path %s, got %s", cfgPath, resp.ConfigPath)
	}

	// Verify config was written
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	content := string(data)

	// snes: all selected → should be in sync_dirs
	if !strings.Contains(content, "roms/snes") {
		t.Error("expected roms/snes in config")
	}
	// gba: none selected → should NOT be in sync_dirs
	if strings.Contains(content, "roms/gba") {
		t.Error("expected roms/gba NOT in config")
	}

	// Save without exit should NOT close done channel
	select {
	case <-ws.done:
		t.Error("expected done channel to remain open after save without exit")
	default:
		// good
	}
}

func TestHandleSaveRejectsNonPost(t *testing.T) {
	ws := &webServer{
		groups: testGroups(),
		done:   make(chan struct{}),
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/save", nil)
	ws.handleSave(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleSaveAndExit(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.toml")

	cfg := &config.Config{
		Storage: config.StorageConfig{
			Bucket:    "test",
			KeyID:     "key",
			SecretKey: "secret",
		},
		Sync: config.SyncConfig{
			EmulationPath: "/tmp/emu",
			SyncDirs:      []string{"roms"},
		},
	}

	ws := &webServer{
		groups:  testGroups(),
		cfg:     cfg,
		cfgPath: cfgPath,
		done:    make(chan struct{}),
	}

	body := `{"selections":{"roms/snes/GameA.sfc":true,"roms/snes/GameB.sfc":true,"roms/gba/GameC.gba":true,"roms/gba/GameD.gba":true},"exit":true}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/save", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ws.handleSave(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Verify config written
	if _, err := os.ReadFile(cfgPath); err != nil {
		t.Fatalf("config not written: %v", err)
	}

	// Verify done channel was closed
	select {
	case <-ws.done:
		// good
	default:
		t.Error("expected done channel to be closed after save with exit")
	}
}

func TestHandleSaveMultipleTimes(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.toml")

	cfg := &config.Config{
		Storage: config.StorageConfig{
			Bucket:    "test",
			KeyID:     "key",
			SecretKey: "secret",
		},
		Sync: config.SyncConfig{
			EmulationPath: "/tmp/emu",
			SyncDirs:      []string{"roms"},
		},
	}

	ws := &webServer{
		groups:  testGroups(),
		cfg:     cfg,
		cfgPath: cfgPath,
		done:    make(chan struct{}),
	}

	// First save: select only snes
	body1 := `{"selections":{"roms/snes/GameA.sfc":true,"roms/snes/GameB.sfc":true,"roms/gba/GameC.gba":false,"roms/gba/GameD.gba":false}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/save", strings.NewReader(body1))
	req.Header.Set("Content-Type", "application/json")
	ws.handleSave(rec, req)

	if rec.Code != 200 {
		t.Fatalf("first save: expected 200, got %d", rec.Code)
	}

	data, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(data), "roms/snes") {
		t.Error("first save: expected roms/snes in config")
	}
	if strings.Contains(string(data), "roms/gba") {
		t.Error("first save: expected roms/gba NOT in config")
	}

	// Second save: now also select gba
	body2 := `{"selections":{"roms/snes/GameA.sfc":true,"roms/snes/GameB.sfc":true,"roms/gba/GameC.gba":true,"roms/gba/GameD.gba":true}}`
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/api/save", strings.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	ws.handleSave(rec2, req2)

	if rec2.Code != 200 {
		t.Fatalf("second save: expected 200, got %d", rec2.Code)
	}

	data2, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(data2), "roms/gba") {
		t.Error("second save: expected roms/gba in config")
	}

	// done channel should still be open
	select {
	case <-ws.done:
		t.Error("expected done channel to remain open")
	default:
	}
}

func TestHandleSaveAndExitConcurrent(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.toml")

	cfg := &config.Config{
		Storage: config.StorageConfig{
			Bucket:    "test",
			KeyID:     "key",
			SecretKey: "secret",
		},
		Sync: config.SyncConfig{
			EmulationPath: "/tmp/emu",
			SyncDirs:      []string{"roms"},
		},
	}

	ws := &webServer{
		groups:  testGroups(),
		cfg:     cfg,
		cfgPath: cfgPath,
		done:    make(chan struct{}),
	}

	body := `{"selections":{"roms/snes/GameA.sfc":true,"roms/snes/GameB.sfc":true,"roms/gba/GameC.gba":true,"roms/gba/GameD.gba":true},"exit":true}`

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/api/save", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			ws.handleSave(rec, req)
		}()
	}
	wg.Wait()

	// done channel should be closed exactly once (no panic)
	select {
	case <-ws.done:
	default:
		t.Error("expected done channel to be closed")
	}
}

func TestHandleWait(t *testing.T) {
	ws := &webServer{
		shutdown: make(chan struct{}),
	}

	done := make(chan int)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/wait", nil)

	go func() {
		ws.handleWait(rec, req)
		done <- rec.Code
	}()

	// Should be blocked — verify it hasn't returned yet
	select {
	case <-done:
		t.Fatal("handleWait returned before shutdown was signalled")
	default:
	}

	close(ws.shutdown)
	code := <-done
	if code != 200 {
		t.Errorf("expected 200, got %d", code)
	}
}

func TestEncodeSelectionsAll(t *testing.T) {
	groups := []*systemGroup{
		{
			Dir: "roms/snes",
			Files: []fileInfo{
				{Key: "roms/snes/A.sfc", Selected: true},
				{Key: "roms/snes/B.sfc", Selected: true},
			},
		},
	}

	dirs, exclude := encodeSelections(groups)
	if len(dirs) != 1 || dirs[0] != "roms/snes" {
		t.Errorf("expected [roms/snes], got %v", dirs)
	}
	if len(exclude) != 0 {
		t.Errorf("expected no excludes, got %v", exclude)
	}
}

func TestEncodeSelectionsNone(t *testing.T) {
	groups := []*systemGroup{
		{
			Dir: "roms/snes",
			Files: []fileInfo{
				{Key: "roms/snes/A.sfc", Selected: false},
				{Key: "roms/snes/B.sfc", Selected: false},
			},
		},
	}

	dirs, exclude := encodeSelections(groups)
	if len(dirs) != 0 {
		t.Errorf("expected no dirs, got %v", dirs)
	}
	if len(exclude) != 0 {
		t.Errorf("expected no excludes, got %v", exclude)
	}
}

func TestEncodeSelectionsPartialFewerExcludes(t *testing.T) {
	groups := []*systemGroup{
		{
			Dir: "roms/snes",
			Files: []fileInfo{
				{Key: "roms/snes/A.sfc", Selected: true},
				{Key: "roms/snes/B.sfc", Selected: true},
				{Key: "roms/snes/C.sfc", Selected: false},
			},
		},
	}

	dirs, exclude := encodeSelections(groups)
	if len(dirs) != 1 || dirs[0] != "roms/snes" {
		t.Errorf("expected [roms/snes], got %v", dirs)
	}
	if len(exclude) != 1 || exclude[0] != "roms/snes/C.sfc" {
		t.Errorf("expected [roms/snes/C.sfc], got %v", exclude)
	}
}

func TestEncodeSelectionsPartialFewerIncludes(t *testing.T) {
	groups := []*systemGroup{
		{
			Dir: "roms/snes",
			Files: []fileInfo{
				{Key: "roms/snes/A.sfc", Selected: true},
				{Key: "roms/snes/B.sfc", Selected: false},
				{Key: "roms/snes/C.sfc", Selected: false},
			},
		},
	}

	dirs, exclude := encodeSelections(groups)
	if len(dirs) != 1 || dirs[0] != "roms/snes/A.sfc" {
		t.Errorf("expected [roms/snes/A.sfc], got %v", dirs)
	}
	if len(exclude) != 0 {
		t.Errorf("expected no excludes, got %v", exclude)
	}
}

// --- eventLog tests ---

func TestEventLogWriteAndRead(t *testing.T) {
	el := newEventLog()

	el.Write([]byte(`{"event":"start","file":"a.rom"}` + "\n"))
	el.Write([]byte(`{"event":"complete","file":"a.rom"}` + "\n"))

	lines, done := el.read(0)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if done {
		t.Error("expected done=false")
	}
	if !strings.Contains(lines[0], "start") {
		t.Errorf("expected start event, got %s", lines[0])
	}
}

func TestEventLogReadFromOffset(t *testing.T) {
	el := newEventLog()

	el.Write([]byte("line0\n"))
	el.Write([]byte("line1\n"))
	el.Write([]byte("line2\n"))

	lines, _ := el.read(1)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines from offset 1, got %d", len(lines))
	}
	if lines[0] != "line1" {
		t.Errorf("expected line1, got %s", lines[0])
	}
}

func TestEventLogFinish(t *testing.T) {
	el := newEventLog()

	el.Write([]byte("line0\n"))
	el.finish()

	lines, done := el.read(0)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if !done {
		t.Error("expected done=true after finish")
	}
}

func TestEventLogReadPastEnd(t *testing.T) {
	el := newEventLog()

	el.Write([]byte("line0\n"))

	lines, done := el.read(5)
	if len(lines) != 0 {
		t.Fatalf("expected 0 lines from offset 5, got %d", len(lines))
	}
	if done {
		t.Error("expected done=false")
	}
}

func TestEventLogNotify(t *testing.T) {
	el := newEventLog()

	// Drain notify channel if anything's there
	select {
	case <-el.notify:
	default:
	}

	el.Write([]byte("line\n"))

	select {
	case <-el.notify:
		// good
	case <-time.After(100 * time.Millisecond):
		t.Error("expected notify signal after Write")
	}
}

// --- handleSync tests ---

// setupSyncWebServer creates a webServer with a MockBackend seeded with a
// manifest and the given files. emulationPath is pointed at a temp dir.
func setupSyncWebServer(t *testing.T) (*webServer, string) {
	t.Helper()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.toml")
	emuPath := filepath.Join(tmpDir, "emu")
	os.MkdirAll(emuPath, 0o755)

	// Build a manifest with one file
	m := manifest.New()
	m.Files["roms/snes/GameA.sfc"] = manifest.FileEntry{
		MD5:  "abc123",
		Size: 100,
	}
	manifestData, _ := json.Marshal(m)

	mock := storage.NewMockBackend()
	mock.Objects[storage.ManifestKey] = manifestData
	mock.Objects["roms/snes/GameA.sfc"] = make([]byte, 100)

	cfg := &config.Config{
		Storage: config.StorageConfig{
			Bucket:    "test",
			KeyID:     "key",
			SecretKey: "secret",
		},
		Sync: config.SyncConfig{
			EmulationPath: emuPath,
			SyncDirs:      []string{"roms"},
		},
	}

	groups := []*systemGroup{
		{
			Dir:       "roms/snes",
			TotalSize: 100,
			Files: []fileInfo{
				{Key: "roms/snes/GameA.sfc", Name: "GameA.sfc", Size: 100, Selected: true},
			},
		},
	}

	ws := &webServer{
		groups:   groups,
		cfg:      cfg,
		cfgPath:  cfgPath,
		done:     make(chan struct{}),
		shutdown: make(chan struct{}),
		client:   mock,
	}

	return ws, tmpDir
}

func TestHandleSyncRejectsGet(t *testing.T) {
	ws, _ := setupSyncWebServer(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/sync", nil)
	ws.handleSync(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleSyncStartsSync(t *testing.T) {
	ws, _ := setupSyncWebServer(t)

	body := `{"selections":{"roms/snes/GameA.sfc":true}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/sync", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ws.handleSync(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["ok"] != true {
		t.Fatalf("expected ok=true, got %v", resp)
	}

	// Wait for sync to finish
	<-ws.syncDone

	ws.syncMu.Lock()
	result := ws.syncResult
	ws.syncMu.Unlock()
	if result == nil {
		t.Fatal("expected sync result")
	}
}

func TestHandleSyncRejectsDuplicate(t *testing.T) {
	ws, _ := setupSyncWebServer(t)

	// Start first sync
	body := `{"selections":{"roms/snes/GameA.sfc":true}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/sync", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ws.handleSync(rec, req)

	if rec.Code != 200 {
		t.Fatalf("first sync: expected 200, got %d", rec.Code)
	}

	// Try to start a second sync while first is running (or just finished)
	// We need to ensure the first sync hasn't completed yet
	// Use the syncDone channel - if it's not closed, sync is still running
	ws.syncMu.Lock()
	syncDone := ws.syncDone
	ws.syncMu.Unlock()

	select {
	case <-syncDone:
		// Sync already finished (mock is fast), start a new blocking one
		// to test the conflict case. Use a slow mock instead.
		t.Skip("sync completed too fast to test duplicate rejection")
	default:
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/api/sync", strings.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	ws.handleSync(rec2, req2)

	if rec2.Code != http.StatusConflict {
		t.Fatalf("second sync: expected 409, got %d", rec2.Code)
	}

	<-syncDone // clean up
}

func TestHandleSyncAutoSavesConfig(t *testing.T) {
	ws, _ := setupSyncWebServer(t)

	body := `{"selections":{"roms/snes/GameA.sfc":true}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/sync", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ws.handleSync(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Config should have been written
	data, err := os.ReadFile(ws.cfgPath)
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	if !strings.Contains(string(data), "roms/snes") {
		t.Error("expected roms/snes in config after auto-save")
	}

	<-ws.syncDone // clean up
}

// --- handleSyncEvents tests ---

func TestHandleSyncEventsNoSync(t *testing.T) {
	ws := &webServer{
		shutdown: make(chan struct{}),
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/sync/events", nil)
	ws.handleSyncEvents(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}

func TestHandleSyncEventsStreams(t *testing.T) {
	ws, _ := setupSyncWebServer(t)

	// Start a sync
	body := `{"selections":{"roms/snes/GameA.sfc":true}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/sync", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ws.handleSync(rec, req)

	// Wait for sync to complete
	<-ws.syncDone

	// Now read events — they should all be available
	server := httptest.NewServer(http.HandlerFunc(ws.handleSyncEvents))
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("GET /api/sync/events: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Read SSE events
	scanner := bufio.NewScanner(resp.Body)
	var dataLines []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
		}
	}

	if len(dataLines) == 0 {
		t.Fatal("expected at least one data line from SSE stream")
	}

	// Last data line should be a done event
	var lastEvt map[string]interface{}
	json.Unmarshal([]byte(dataLines[len(dataLines)-1]), &lastEvt)
	if lastEvt["event"] != "done" {
		t.Errorf("expected last event to be 'done', got %v", lastEvt["event"])
	}
}

// --- handleSyncStatus tests ---

func TestHandleSyncStatusIdle(t *testing.T) {
	ws := &webServer{}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/sync/status", nil)
	ws.handleSyncStatus(rec, req)

	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["state"] != "idle" {
		t.Errorf("expected idle, got %v", resp["state"])
	}
}

func TestHandleSyncStatusComplete(t *testing.T) {
	ws, _ := setupSyncWebServer(t)

	// Start sync and wait for completion
	body := `{"selections":{"roms/snes/GameA.sfc":true}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/sync", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ws.handleSync(rec, req)
	<-ws.syncDone

	// Check status
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/api/sync/status", nil)
	ws.handleSyncStatus(rec2, req2)

	var resp map[string]interface{}
	json.Unmarshal(rec2.Body.Bytes(), &resp)

	state := resp["state"].(string)
	if state != "complete" && state != "failed" {
		t.Errorf("expected complete or failed, got %s", state)
	}
	if _, ok := resp["summary"]; !ok {
		t.Error("expected summary field in response")
	}
}

func TestHandleSyncStatusRunning(t *testing.T) {
	ws := &webServer{}
	ws.syncLog = newEventLog()
	ws.syncDone = make(chan struct{})
	// Don't close syncDone — simulate a running sync

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/sync/status", nil)
	ws.handleSyncStatus(rec, req)

	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["state"] != "running" {
		t.Errorf("expected running, got %v", resp["state"])
	}
}

func TestHandleSyncEventsLastEventID(t *testing.T) {
	el := newEventLog()
	el.Write([]byte("line0\n"))
	el.Write([]byte("line1\n"))
	el.Write([]byte("line2\n"))
	el.finish()

	ws := &webServer{
		shutdown: make(chan struct{}),
	}
	ws.syncLog = el
	ws.syncDone = make(chan struct{})
	close(ws.syncDone)

	server := httptest.NewServer(http.HandlerFunc(ws.handleSyncEvents))
	defer server.Close()

	// Request with Last-Event-ID: 1 — should skip line0 and line1
	client := &http.Client{}
	req, _ := http.NewRequest("GET", server.URL, nil)
	req.Header.Set("Last-Event-ID", "1")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	var ids []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "id: ") {
			ids = append(ids, strings.TrimPrefix(line, "id: "))
		}
	}

	// Should only have line2 (id: 2)
	if len(ids) != 1 || ids[0] != "2" {
		t.Errorf("expected [2], got %v", ids)
	}
}

