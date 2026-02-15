package sync

import (
	"context"
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jacobfgrant/emu-sync/internal/config"
	"github.com/jacobfgrant/emu-sync/internal/manifest"
	"github.com/jacobfgrant/emu-sync/internal/storage"
)

func TestSyncDownloadsNewFiles(t *testing.T) {
	emuDir := t.TempDir()
	manifestPath := filepath.Join(t.TempDir(), "local-manifest.json")

	mock := mockWithManifest(t, map[string]mockFile{
		"roms/snes/Game.sfc": {content: "snes rom data", size: 13},
		"bios/scph5501.bin":  {content: "bios data", size: 9},
	})

	cfg := testConfig(emuDir)
	result, err := Run(context.Background(), mock, cfg, Options{LocalManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.Downloaded) != 2 {
		t.Errorf("downloaded %d, want 2", len(result.Downloaded))
	}
	if result.Skipped != 0 {
		t.Errorf("skipped %d, want 0", result.Skipped)
	}

	// Verify files exist on disk
	assertFileContent(t, filepath.Join(emuDir, "roms/snes/Game.sfc"), "snes rom data")
	assertFileContent(t, filepath.Join(emuDir, "bios/scph5501.bin"), "bios data")

	// Verify local manifest was saved
	local, err := manifest.LoadJSON(manifestPath)
	if err != nil {
		t.Fatalf("loading local manifest: %v", err)
	}
	if len(local.Files) != 2 {
		t.Errorf("local manifest has %d entries, want 2", len(local.Files))
	}
}

func TestSyncSkipsUnchanged(t *testing.T) {
	emuDir := t.TempDir()
	manifestPath := filepath.Join(t.TempDir(), "local-manifest.json")

	mock := mockWithManifest(t, map[string]mockFile{
		"roms/snes/Game.sfc": {content: "data", size: 4},
	})

	cfg := testConfig(emuDir)

	// First sync
	_, err := Run(context.Background(), mock, cfg, Options{LocalManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Second sync — file unchanged
	mock.Calls = nil
	result, err := Run(context.Background(), mock, cfg, Options{LocalManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}

	if len(result.Downloaded) != 0 {
		t.Errorf("downloaded %d on second sync, want 0", len(result.Downloaded))
	}
	if result.Skipped != 1 {
		t.Errorf("skipped %d, want 1", result.Skipped)
	}
}

func TestSyncDeletesRemovedFiles(t *testing.T) {
	emuDir := t.TempDir()
	manifestPath := filepath.Join(t.TempDir(), "local-manifest.json")

	// Initial sync with two files
	mock := mockWithManifest(t, map[string]mockFile{
		"roms/snes/Game1.sfc": {content: "game1", size: 5},
		"roms/snes/Game2.sfc": {content: "game2", size: 5},
	})

	cfg := testConfig(emuDir)
	cfg.Sync.Delete = true

	_, err := Run(context.Background(), mock, cfg, Options{LocalManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Update remote manifest to only have Game1
	mock = mockWithManifest(t, map[string]mockFile{
		"roms/snes/Game1.sfc": {content: "game1", size: 5},
	})

	result, err := Run(context.Background(), mock, cfg, Options{LocalManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}

	if len(result.Deleted) != 1 {
		t.Errorf("deleted %d, want 1", len(result.Deleted))
	}

	// Game2 should be gone from disk
	if _, err := os.Stat(filepath.Join(emuDir, "roms/snes/Game2.sfc")); !os.IsNotExist(err) {
		t.Error("Game2.sfc should have been deleted")
	}
}

func TestSyncNoDeleteFlag(t *testing.T) {
	emuDir := t.TempDir()
	manifestPath := filepath.Join(t.TempDir(), "local-manifest.json")

	mock := mockWithManifest(t, map[string]mockFile{
		"roms/snes/Game.sfc": {content: "data", size: 4},
	})

	cfg := testConfig(emuDir)
	cfg.Sync.Delete = true

	// Sync the file
	_, err := Run(context.Background(), mock, cfg, Options{LocalManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Remove from remote
	mock = mockWithManifest(t, map[string]mockFile{})

	// Sync with NoDelete — file should remain
	result, err := Run(context.Background(), mock, cfg, Options{
		LocalManifestPath: manifestPath,
		NoDelete:          true,
	})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}

	if _, err := os.Stat(filepath.Join(emuDir, "roms/snes/Game.sfc")); os.IsNotExist(err) {
		t.Error("Game.sfc should NOT have been deleted with NoDelete")
	}
	if len(result.Deleted) != 0 {
		t.Errorf("deleted %d, want 0 with NoDelete", len(result.Deleted))
	}
	if len(result.Retained) != 1 {
		t.Errorf("retained %d, want 1 with NoDelete", len(result.Retained))
	}
	if len(result.Errors) != 0 {
		t.Errorf("errors = %d, want 0", len(result.Errors))
	}
}

func TestSyncDryRun(t *testing.T) {
	emuDir := t.TempDir()
	manifestPath := filepath.Join(t.TempDir(), "local-manifest.json")

	mock := mockWithManifest(t, map[string]mockFile{
		"roms/snes/Game.sfc": {content: "data", size: 4},
	})

	cfg := testConfig(emuDir)
	result, err := Run(context.Background(), mock, cfg, Options{
		LocalManifestPath: manifestPath,
		DryRun:            true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.Downloaded) != 1 {
		t.Errorf("dry run reported %d downloads, want 1", len(result.Downloaded))
	}

	// File should NOT exist on disk
	if _, err := os.Stat(filepath.Join(emuDir, "roms/snes/Game.sfc")); !os.IsNotExist(err) {
		t.Error("dry run should not create files")
	}
}

func TestSyncFiltersBySyncDirs(t *testing.T) {
	emuDir := t.TempDir()
	manifestPath := filepath.Join(t.TempDir(), "local-manifest.json")

	mock := mockWithManifest(t, map[string]mockFile{
		"roms/snes/Game.sfc": {content: "rom", size: 3},
		"bios/scph5501.bin":  {content: "bios", size: 4},
		"saves/game.sav":     {content: "save", size: 4},
	})

	cfg := testConfig(emuDir)
	cfg.Sync.SyncDirs = []string{"roms"} // only sync roms, not bios or saves

	result, err := Run(context.Background(), mock, cfg, Options{LocalManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.Downloaded) != 1 {
		t.Errorf("downloaded %d, want 1 (only roms)", len(result.Downloaded))
	}
}

func TestSyncHandlesDownloadError(t *testing.T) {
	emuDir := t.TempDir()
	manifestPath := filepath.Join(t.TempDir(), "local-manifest.json")

	mock := mockWithManifest(t, map[string]mockFile{
		"roms/snes/Good.sfc": {content: "good", size: 4},
		"roms/snes/Bad.sfc":  {content: "bad", size: 3},
	})
	mock.DownloadErrors["roms/snes/Bad.sfc"] = fmt.Errorf("simulated download error")

	cfg := testConfig(emuDir)
	result, err := Run(context.Background(), mock, cfg, Options{LocalManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("Run should not return fatal error: %v", err)
	}

	if len(result.Errors) != 1 {
		t.Errorf("errors = %d, want 1", len(result.Errors))
	}

	// Good file should exist
	assertFileContent(t, filepath.Join(emuDir, "roms/snes/Good.sfc"), "good")

	// Bad file should NOT exist
	if _, err := os.Stat(filepath.Join(emuDir, "roms/snes/Bad.sfc")); !os.IsNotExist(err) {
		t.Error("Bad.sfc should not exist after download error")
	}

	// Local manifest should only have Good.sfc
	local, err := manifest.LoadJSON(manifestPath)
	if err != nil {
		t.Fatalf("loading local manifest: %v", err)
	}
	if _, ok := local.Files["roms/snes/Bad.sfc"]; ok {
		t.Error("Bad.sfc should NOT be in local manifest after error")
	}
}

func TestSyncCleansUpTempFiles(t *testing.T) {
	emuDir := t.TempDir()

	// Create a leftover temp file
	romsDir := filepath.Join(emuDir, "roms", "snes")
	os.MkdirAll(romsDir, 0o755)
	tmpFile := filepath.Join(romsDir, "Game.sfc"+tmpSuffix)
	os.WriteFile(tmpFile, []byte("partial download"), 0o644)

	manifestPath := filepath.Join(t.TempDir(), "local-manifest.json")
	mock := mockWithManifest(t, map[string]mockFile{})

	cfg := testConfig(emuDir)
	_, err := Run(context.Background(), mock, cfg, Options{LocalManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
		t.Error("temp file should have been cleaned up")
	}
}

func TestSyncParallelDownloads(t *testing.T) {
	emuDir := t.TempDir()
	manifestPath := filepath.Join(t.TempDir(), "local-manifest.json")

	files := map[string]mockFile{
		"roms/snes/Game1.sfc": {content: "game1 data", size: 10},
		"roms/snes/Game2.sfc": {content: "game2 data", size: 10},
		"roms/snes/Game3.sfc": {content: "game3 data", size: 10},
		"roms/gba/Game4.gba":  {content: "game4 data", size: 10},
		"bios/scph5501.bin":   {content: "bios data", size: 9},
	}
	mock := mockWithManifest(t, files)

	cfg := testConfig(emuDir)
	result, err := Run(context.Background(), mock, cfg, Options{
		LocalManifestPath: manifestPath,
		Workers:           3,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.Downloaded) != 5 {
		t.Errorf("downloaded %d, want 5", len(result.Downloaded))
	}
	if len(result.Errors) != 0 {
		t.Errorf("errors = %d, want 0", len(result.Errors))
	}

	// Verify all files exist with correct content
	for key, f := range files {
		assertFileContent(t, filepath.Join(emuDir, filepath.FromSlash(key)), f.content)
	}

	// Verify local manifest has all entries
	local, err := manifest.LoadJSON(manifestPath)
	if err != nil {
		t.Fatalf("loading local manifest: %v", err)
	}
	if len(local.Files) != 5 {
		t.Errorf("local manifest has %d entries, want 5", len(local.Files))
	}
}

func TestSyncParallelWithErrors(t *testing.T) {
	emuDir := t.TempDir()
	manifestPath := filepath.Join(t.TempDir(), "local-manifest.json")

	mock := mockWithManifest(t, map[string]mockFile{
		"roms/snes/Good1.sfc": {content: "good1", size: 5},
		"roms/snes/Good2.sfc": {content: "good2", size: 5},
		"roms/snes/Bad.sfc":   {content: "bad", size: 3},
	})
	mock.DownloadErrors["roms/snes/Bad.sfc"] = fmt.Errorf("simulated error")

	cfg := testConfig(emuDir)
	result, err := Run(context.Background(), mock, cfg, Options{
		LocalManifestPath: manifestPath,
		Workers:           2,
	})
	if err != nil {
		t.Fatalf("Run should not return fatal error: %v", err)
	}

	if len(result.Downloaded) != 2 {
		t.Errorf("downloaded %d, want 2", len(result.Downloaded))
	}
	if len(result.Errors) != 1 {
		t.Errorf("errors = %d, want 1", len(result.Errors))
	}
}

func TestSyncRedownloadsMissingFiles(t *testing.T) {
	emuDir := t.TempDir()
	manifestPath := filepath.Join(t.TempDir(), "local-manifest.json")

	mock := mockWithManifest(t, map[string]mockFile{
		"roms/snes/Game.sfc": {content: "rom data", size: 8},
	})

	cfg := testConfig(emuDir)

	// First sync — downloads the file
	_, err := Run(context.Background(), mock, cfg, Options{LocalManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	assertFileContent(t, filepath.Join(emuDir, "roms/snes/Game.sfc"), "rom data")

	// Delete the file from disk (simulating accidental deletion)
	os.Remove(filepath.Join(emuDir, "roms/snes/Game.sfc"))

	// Second sync — should detect missing file and re-download
	mock.Calls = nil
	result, err := Run(context.Background(), mock, cfg, Options{LocalManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}

	if len(result.Downloaded) != 1 {
		t.Errorf("downloaded %d, want 1 (missing file)", len(result.Downloaded))
	}
	assertFileContent(t, filepath.Join(emuDir, "roms/snes/Game.sfc"), "rom data")
}

func TestSyncModifiedAndMissingNotDuplicated(t *testing.T) {
	emuDir := t.TempDir()
	manifestPath := filepath.Join(t.TempDir(), "local-manifest.json")

	// First sync — download a file
	mock := mockWithManifest(t, map[string]mockFile{
		"roms/snes/Game.sfc": {content: "v1 data", size: 7},
	})
	cfg := testConfig(emuDir)

	_, err := Run(context.Background(), mock, cfg, Options{LocalManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Delete file from disk AND update remote with new content
	// This triggers both diff.Modified (hash changed) and missing-from-disk
	os.Remove(filepath.Join(emuDir, "roms/snes/Game.sfc"))
	mock = mockWithManifest(t, map[string]mockFile{
		"roms/snes/Game.sfc": {content: "v2 data updated", size: 15},
	})

	result, err := Run(context.Background(), mock, cfg, Options{LocalManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}

	if len(result.Downloaded) != 1 {
		t.Errorf("downloaded %d, want 1 (should not duplicate)", len(result.Downloaded))
	}
	assertFileContent(t, filepath.Join(emuDir, "roms/snes/Game.sfc"), "v2 data updated")
}

func TestSyncLockPreventsOverlap(t *testing.T) {
	// Acquire the lock directly to simulate another sync in progress
	lock, err := acquireLock()
	if err != nil {
		t.Fatalf("acquireLock: %v", err)
	}
	defer releaseLock(lock)

	emuDir := t.TempDir()
	manifestPath := filepath.Join(t.TempDir(), "local-manifest.json")

	mock := mockWithManifest(t, map[string]mockFile{
		"roms/snes/Game.sfc": {content: "data", size: 4},
	})

	cfg := testConfig(emuDir)
	_, err = Run(context.Background(), mock, cfg, Options{LocalManifestPath: manifestPath})
	if err == nil {
		t.Fatal("expected error when lock is held, got nil")
	}
	if !strings.Contains(err.Error(), "another sync is already running") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSyncLockSkippedForDryRun(t *testing.T) {
	// Hold the lock — dry-run should still succeed
	lock, err := acquireLock()
	if err != nil {
		t.Fatalf("acquireLock: %v", err)
	}
	defer releaseLock(lock)

	emuDir := t.TempDir()
	manifestPath := filepath.Join(t.TempDir(), "local-manifest.json")

	mock := mockWithManifest(t, map[string]mockFile{
		"roms/snes/Game.sfc": {content: "data", size: 4},
	})

	cfg := testConfig(emuDir)
	result, err := Run(context.Background(), mock, cfg, Options{
		LocalManifestPath: manifestPath,
		DryRun:            true,
	})
	if err != nil {
		t.Fatalf("dry-run should succeed even with lock held: %v", err)
	}
	if len(result.Downloaded) != 1 {
		t.Errorf("downloaded %d, want 1", len(result.Downloaded))
	}
}

// --- helpers ---

type mockFile struct {
	content string
	size    int64
}

func mockWithManifest(t *testing.T, files map[string]mockFile) *storage.MockBackend {
	t.Helper()
	mock := storage.NewMockBackend()

	m := manifest.New()
	for key, f := range files {
		// Compute real MD5 for the content
		hash := md5hex(f.content)
		m.Files[key] = manifest.FileEntry{Size: f.size, MD5: hash}
		mock.Objects[key] = []byte(f.content)
	}

	data, err := m.ToJSON()
	if err != nil {
		t.Fatalf("serializing manifest: %v", err)
	}
	mock.Objects[storage.ManifestKey] = data

	return mock
}

func testConfig(emuDir string) *config.Config {
	return &config.Config{
		Storage: config.StorageConfig{
			Bucket:    "test",
			KeyID:     "key",
			SecretKey: "secret",
		},
		Sync: config.SyncConfig{
			EmulationPath: emuDir,
			SyncDirs:      []string{"roms", "bios"},
			Delete:        true,
		},
	}
}

func assertFileContent(t *testing.T, path, expected string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if string(data) != expected {
		t.Errorf("%s content = %q, want %q", path, string(data), expected)
	}
}

func md5hex(s string) string {
	h := md5.New()
	h.Write([]byte(s))
	return fmt.Sprintf("%x", h.Sum(nil))
}
