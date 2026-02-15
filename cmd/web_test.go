package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jacobfgrant/emu-sync/internal/config"
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
