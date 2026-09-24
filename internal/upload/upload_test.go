package upload

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jacobfgrant/emu-sync/internal/manifest"
	"github.com/jacobfgrant/emu-sync/internal/storage"
)

func TestUploadNewFiles(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/snes/Game.sfc": "snes rom data",
		"bios/scph5501.bin":  "bios data",
	})

	mock := storage.NewMockBackend()
	result, err := Run(context.Background(), mock, Options{
		SourcePath: source,
		SyncDirs:   []string{"roms", "bios"},
		CachePath:  tempCachePath(t),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.Uploaded) != 2 {
		t.Errorf("uploaded %d files, want 2", len(result.Uploaded))
	}
	if result.Skipped != 0 {
		t.Errorf("skipped %d, want 0", result.Skipped)
	}

	// Verify manifest was uploaded
	if _, ok := mock.Objects[storage.ManifestKey]; !ok {
		t.Error("manifest was not uploaded to bucket")
	}

	// Verify file content in bucket
	if string(mock.Objects["roms/snes/Game.sfc"]) != "snes rom data" {
		t.Error("uploaded file content mismatch")
	}
}

func TestUploadSkipsUnchanged(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/snes/Game.sfc": "snes rom data",
	})

	mock := storage.NewMockBackend()
	opts := Options{SourcePath: source, SyncDirs: []string{"roms"}, CachePath: tempCachePath(t)}

	// First upload
	_, err := Run(context.Background(), mock, opts)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Second upload with same files — should skip
	mock.Calls = nil
	result, err := Run(context.Background(), mock, opts)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}

	if len(result.Uploaded) != 0 {
		t.Errorf("uploaded %d files on second run, want 0", len(result.Uploaded))
	}
	if result.Skipped != 1 {
		t.Errorf("skipped %d, want 1", result.Skipped)
	}
}

func TestUploadDetectsModified(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/snes/Game.sfc": "original data",
	})

	mock := storage.NewMockBackend()
	opts := Options{SourcePath: source, SyncDirs: []string{"roms"}, CachePath: tempCachePath(t)}

	// First upload
	_, err := Run(context.Background(), mock, opts)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Modify the file
	os.WriteFile(filepath.Join(source, "roms/snes/Game.sfc"), []byte("modified data"), 0o644)

	// Second upload — should detect change
	result, err := Run(context.Background(), mock, opts)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}

	if len(result.Uploaded) != 1 {
		t.Errorf("uploaded %d, want 1 (modified file)", len(result.Uploaded))
	}
	if string(mock.Objects["roms/snes/Game.sfc"]) != "modified data" {
		t.Error("bucket should have updated content")
	}
}

func TestUploadDeletesRemoved(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/snes/Game1.sfc": "game 1",
		"roms/snes/Game2.sfc": "game 2",
	})

	mock := storage.NewMockBackend()
	opts := Options{SourcePath: source, SyncDirs: []string{"roms"}, CachePath: tempCachePath(t)}

	// Upload both
	_, err := Run(context.Background(), mock, opts)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Remove one file locally
	os.Remove(filepath.Join(source, "roms/snes/Game2.sfc"))

	// Re-upload — should delete Game2 from bucket
	result, err := Run(context.Background(), mock, opts)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}

	if len(result.Deleted) != 1 {
		t.Errorf("deleted %d, want 1", len(result.Deleted))
	}
	if _, ok := mock.Objects["roms/snes/Game2.sfc"]; ok {
		t.Error("Game2 should be deleted from bucket")
	}
}

func TestUploadDryRun(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/snes/Game.sfc": "data",
	})

	mock := storage.NewMockBackend()
	result, err := Run(context.Background(), mock, Options{
		SourcePath: source,
		SyncDirs:   []string{"roms"},
		DryRun:     true,
		CachePath:  tempCachePath(t),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.Uploaded) != 1 {
		t.Errorf("uploaded count = %d, want 1", len(result.Uploaded))
	}
	// Bucket should be empty — dry run shouldn't actually upload
	if _, ok := mock.Objects["roms/snes/Game.sfc"]; ok {
		t.Error("dry run should not upload files")
	}
	if _, ok := mock.Objects[storage.ManifestKey]; ok {
		t.Error("dry run should not upload manifest")
	}
}

func TestUploadSkipsMissingDir(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/snes/Game.sfc": "data",
	})

	mock := storage.NewMockBackend()
	// "bios" dir doesn't exist — should be silently skipped
	result, err := Run(context.Background(), mock, Options{
		SourcePath: source,
		SyncDirs:   []string{"roms", "bios"},
		CachePath:  tempCachePath(t),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.Uploaded) != 1 {
		t.Errorf("uploaded %d, want 1", len(result.Uploaded))
	}
}

func TestUploadHandlesFileError(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/snes/Good.sfc": "good data",
		"roms/snes/Bad.sfc":  "bad data",
	})

	mock := storage.NewMockBackend()
	mock.UploadErrors["roms/snes/Bad.sfc"] = fmt.Errorf("simulated upload error")

	result, err := Run(context.Background(), mock, Options{
		SourcePath: source,
		SyncDirs:   []string{"roms"},
		CachePath:  tempCachePath(t),
	})
	if err != nil {
		t.Fatalf("Run should not return fatal error: %v", err)
	}

	if len(result.Errors) != 1 {
		t.Errorf("errors = %d, want 1", len(result.Errors))
	}
	// Good file should still be uploaded
	if _, ok := mock.Objects["roms/snes/Good.sfc"]; !ok {
		t.Error("Good.sfc should have been uploaded despite Bad.sfc error")
	}
}

func TestUploadParallel(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/snes/Game1.sfc": "game1 data",
		"roms/snes/Game2.sfc": "game2 data",
		"roms/snes/Game3.sfc": "game3 data",
		"roms/gba/Game4.gba":  "game4 data",
		"bios/scph5501.bin":   "bios data",
	})

	mock := storage.NewMockBackend()
	result, err := Run(context.Background(), mock, Options{
		SourcePath: source,
		SyncDirs:   []string{"roms", "bios"},
		Workers:    3,
		CachePath:  tempCachePath(t),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.Uploaded) != 5 {
		t.Errorf("uploaded %d, want 5", len(result.Uploaded))
	}
	if len(result.Errors) != 0 {
		t.Errorf("errors = %d, want 0", len(result.Errors))
	}

	// Verify all files in bucket
	for _, key := range []string{
		"roms/snes/Game1.sfc", "roms/snes/Game2.sfc", "roms/snes/Game3.sfc",
		"roms/gba/Game4.gba", "bios/scph5501.bin",
	} {
		if _, ok := mock.Objects[key]; !ok {
			t.Errorf("%s not found in bucket", key)
		}
	}
}

func TestUploadParallelWithErrors(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/snes/Good1.sfc": "good1",
		"roms/snes/Good2.sfc": "good2",
		"roms/snes/Bad.sfc":   "bad",
	})

	mock := storage.NewMockBackend()
	mock.UploadErrors["roms/snes/Bad.sfc"] = fmt.Errorf("simulated error")

	result, err := Run(context.Background(), mock, Options{
		SourcePath: source,
		SyncDirs:   []string{"roms"},
		Workers:    2,
		CachePath:  tempCachePath(t),
	})
	if err != nil {
		t.Fatalf("Run should not return fatal error: %v", err)
	}

	if len(result.Uploaded) != 2 {
		t.Errorf("uploaded %d, want 2", len(result.Uploaded))
	}
	if len(result.Errors) != 1 {
		t.Errorf("errors = %d, want 1", len(result.Errors))
	}
}

func TestUploadSkipsDotfiles(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/snes/Game.sfc":    "snes rom data",
		"roms/snes/.DS_Store":   "mac junk",
		"roms/.DS_Store":        "mac junk",
		"bios/scph5501.bin":     "bios data",
		"bios/.hidden":          "hidden file",
	})

	mock := storage.NewMockBackend()
	result, err := Run(context.Background(), mock, Options{
		SourcePath:   source,
		SyncDirs:     []string{"roms", "bios"},
		SkipDotfiles: true,
		CachePath:    tempCachePath(t),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.Uploaded) != 2 {
		t.Errorf("uploaded %d files, want 2", len(result.Uploaded))
	}

	// Dotfiles should not be in bucket
	for key := range mock.Objects {
		if key == storage.ManifestKey {
			continue
		}
		if filepath.Base(key)[0] == '.' {
			t.Errorf("dotfile %q should not have been uploaded", key)
		}
	}

	// Manifest should not contain dotfiles
	m := verifyManifest(t, mock)
	for key := range m.Files {
		if filepath.Base(key)[0] == '.' {
			t.Errorf("dotfile %q should not be in manifest", key)
		}
	}
}

func TestUploadSkipsDotfileDirectories(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/snes/Game.sfc":       "snes rom data",
		"roms/.git/config":         "git config",
		"roms/.git/objects/abc":    "git object",
	})

	mock := storage.NewMockBackend()
	result, err := Run(context.Background(), mock, Options{
		SourcePath:   source,
		SyncDirs:     []string{"roms"},
		SkipDotfiles: true,
		CachePath:    tempCachePath(t),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.Uploaded) != 1 {
		t.Errorf("uploaded %d files, want 1", len(result.Uploaded))
	}
}

func TestUploadIncludesDotfilesWhenDisabled(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/snes/Game.sfc":  "snes rom data",
		"roms/snes/.DS_Store": "mac junk",
	})

	mock := storage.NewMockBackend()
	result, err := Run(context.Background(), mock, Options{
		SourcePath:   source,
		SyncDirs:     []string{"roms"},
		SkipDotfiles: false,
		CachePath:    tempCachePath(t),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.Uploaded) != 2 {
		t.Errorf("uploaded %d files, want 2 (dotfile included)", len(result.Uploaded))
	}
}

// --- cache integration tests ---

func TestUploadCacheHitsOnSecondRun(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/snes/Game1.sfc": "game1 data",
		"roms/snes/Game2.sfc": "game2 data",
	})

	mock := storage.NewMockBackend()
	cachePath := tempCachePath(t)
	opts := Options{SourcePath: source, SyncDirs: []string{"roms"}, CachePath: cachePath}

	// First run — no cache hits
	result1, err := Run(context.Background(), mock, opts)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if result1.CacheHits != 0 {
		t.Errorf("first run cache hits = %d, want 0", result1.CacheHits)
	}

	// Second run — everything cached
	result2, err := Run(context.Background(), mock, opts)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if result2.CacheHits != 2 {
		t.Errorf("second run cache hits = %d, want 2", result2.CacheHits)
	}
}

func TestUploadCacheRehashesOnMtimeChange(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/snes/Game.sfc": "original data",
	})

	mock := storage.NewMockBackend()
	cachePath := tempCachePath(t)
	opts := Options{SourcePath: source, SyncDirs: []string{"roms"}, CachePath: cachePath}

	// First run — populates cache
	_, err := Run(context.Background(), mock, opts)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Touch file to change mtime (keep same content to prove cache miss is mtime-based)
	path := filepath.Join(source, "roms/snes/Game.sfc")
	now := time.Now().Add(time.Second)
	os.Chtimes(path, now, now)

	// Second run — should re-hash due to mtime change
	result, err := Run(context.Background(), mock, opts)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if result.CacheHits != 0 {
		t.Errorf("cache hits = %d, want 0 (file was touched)", result.CacheHits)
	}
}

func TestUploadDryRunDoesNotSaveCache(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/snes/Game.sfc": "data",
	})

	cachePath := tempCachePath(t)
	mock := storage.NewMockBackend()
	_, err := Run(context.Background(), mock, Options{
		SourcePath: source,
		SyncDirs:   []string{"roms"},
		DryRun:     true,
		CachePath:  cachePath,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Cache file should not exist after dry run
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Error("cache file should not exist after dry run")
	}
}

func TestUploadCacheSavedBeforeUpload(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/snes/Game1.sfc": "game1 data",
		"roms/snes/Game2.sfc": "game2 data",
	})

	mock := storage.NewMockBackend()
	// Fail ALL uploads — cache should still be saved before uploads start
	mock.UploadErrors["roms/snes/Game1.sfc"] = fmt.Errorf("simulated error")
	mock.UploadErrors["roms/snes/Game2.sfc"] = fmt.Errorf("simulated error")

	cachePath := tempCachePath(t)
	_, err := Run(context.Background(), mock, Options{
		SourcePath: source,
		SyncDirs:   []string{"roms"},
		CachePath:  cachePath,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Cache file should exist even though all uploads failed
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		t.Fatal("cache file should exist (saved before uploads)")
	}

	// Verify cache has entries by loading it
	cache := loadHashCache(cachePath)
	if len(cache.Files) < 2 {
		t.Errorf("cache has %d entries, want at least 2", len(cache.Files))
	}
}

func TestUploadManifestOnly(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/snes/Game1.sfc": "game1 data",
		"roms/snes/Game2.sfc": "game2 data",
	})

	mock := storage.NewMockBackend()
	result, err := Run(context.Background(), mock, Options{
		SourcePath:   source,
		SyncDirs:     []string{"roms"},
		ManifestOnly: true,
		CachePath:    tempCachePath(t),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// No files should be uploaded
	if len(result.Uploaded) != 0 {
		t.Errorf("uploaded %d files, want 0 with ManifestOnly", len(result.Uploaded))
	}

	// No UploadFile calls should have been made
	for _, call := range mock.Calls {
		if strings.HasPrefix(call, "UploadFile:") {
			t.Errorf("unexpected UploadFile call: %s", call)
		}
	}

	// Manifest should be in bucket
	m := verifyManifest(t, mock)
	if len(m.Files) != 2 {
		t.Errorf("manifest has %d entries, want 2", len(m.Files))
	}
}

func TestUploadSavesLocalManifest(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/snes/Game.sfc": "snes rom data",
		"bios/scph5501.bin":  "bios data",
	})

	localManifest := filepath.Join(t.TempDir(), "local-manifest.json")
	mock := storage.NewMockBackend()
	_, err := Run(context.Background(), mock, Options{
		SourcePath:        source,
		SyncDirs:          []string{"roms", "bios"},
		CachePath:         tempCachePath(t),
		LocalManifestPath: localManifest,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	m, err := manifest.LoadJSON(localManifest)
	if err != nil {
		t.Fatalf("loading local manifest: %v", err)
	}
	if len(m.Files) != 2 {
		t.Errorf("local manifest has %d files, want 2", len(m.Files))
	}
	if _, ok := m.Files["roms/snes/Game.sfc"]; !ok {
		t.Error("expected roms/snes/Game.sfc in local manifest")
	}
}

func TestUploadNoLocalManifestWithoutPath(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/snes/Game.sfc": "data",
	})

	localManifest := filepath.Join(t.TempDir(), "local-manifest.json")
	mock := storage.NewMockBackend()
	_, err := Run(context.Background(), mock, Options{
		SourcePath: source,
		SyncDirs:   []string{"roms"},
		CachePath:  tempCachePath(t),
		// LocalManifestPath not set
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := os.Stat(localManifest); !os.IsNotExist(err) {
		t.Error("local manifest should not be created when LocalManifestPath is empty")
	}
}

func TestUploadDryRunSkipsLocalManifest(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/snes/Game.sfc": "data",
	})

	localManifest := filepath.Join(t.TempDir(), "local-manifest.json")
	mock := storage.NewMockBackend()
	_, err := Run(context.Background(), mock, Options{
		SourcePath:        source,
		SyncDirs:          []string{"roms"},
		DryRun:            true,
		CachePath:         tempCachePath(t),
		LocalManifestPath: localManifest,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := os.Stat(localManifest); !os.IsNotExist(err) {
		t.Error("local manifest should not be created during dry run")
	}
}

func TestUploadManifestOnlySavesLocalManifest(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/snes/Game.sfc": "data",
	})

	localManifest := filepath.Join(t.TempDir(), "local-manifest.json")
	mock := storage.NewMockBackend()
	_, err := Run(context.Background(), mock, Options{
		SourcePath:        source,
		SyncDirs:          []string{"roms"},
		ManifestOnly:      true,
		CachePath:         tempCachePath(t),
		LocalManifestPath: localManifest,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	m, err := manifest.LoadJSON(localManifest)
	if err != nil {
		t.Fatalf("loading local manifest: %v", err)
	}
	if len(m.Files) != 1 {
		t.Errorf("local manifest has %d files, want 1", len(m.Files))
	}
}

func TestUploadRejectsMissingSourcePath(t *testing.T) {
	mock := storage.NewMockBackend()
	_, err := Run(context.Background(), mock, Options{
		SourcePath: "/nonexistent/source/path",
		SyncDirs:   []string{"roms"},
		CachePath:  tempCachePath(t),
	})
	if err == nil {
		t.Fatal("expected error for nonexistent source path")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("expected 'does not exist' in error, got: %v", err)
	}

	// Bucket should be untouched — no manifest uploaded
	if _, ok := mock.Objects[storage.ManifestKey]; ok {
		t.Error("manifest should not be uploaded when source path doesn't exist")
	}
}

// --- helpers ---

// setupSourceDir creates a temp directory tree with the given files.
func setupSourceDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for path, content := range files {
		fullPath := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return dir
}

// tempCachePath returns a path for an isolated upload cache in a temp dir.
func tempCachePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "upload-cache.json")
}

// verifyManifest parses the manifest from the mock and returns it.
func verifyManifest(t *testing.T, mock *storage.MockBackend) *manifest.Manifest {
	t.Helper()
	data, ok := mock.Objects[storage.ManifestKey]
	if !ok {
		t.Fatal("no manifest in bucket")
	}
	m, err := manifest.ParseJSON(data)
	if err != nil {
		t.Fatalf("parsing manifest: %v", err)
	}
	return m
}

func TestUploadAbortsWhenManifestFetchFails(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/snes/Game.sfc": "snes rom data",
	})

	mock := storage.NewMockBackend()
	mock.DownloadErrors[storage.ManifestKey] = errors.New("connection reset")

	_, err := Run(context.Background(), mock, Options{
		SourcePath: source,
		SyncDirs:   []string{"roms"},
		CachePath:  tempCachePath(t),
	})
	if err == nil {
		t.Fatal("expected error when manifest fetch fails, got nil")
	}

	for _, call := range mock.Calls {
		if strings.HasPrefix(call, "UploadFile:") || strings.HasPrefix(call, "UploadBytes:") {
			t.Errorf("unexpected write after failed manifest fetch: %s", call)
		}
	}
}

// assertNoBucketWrites fails if any upload or delete call was made.
func assertNoBucketWrites(t *testing.T, mock *storage.MockBackend) {
	t.Helper()
	for _, call := range mock.Calls {
		if strings.HasPrefix(call, "Upload") || strings.HasPrefix(call, "DeleteObject:") {
			t.Errorf("unexpected bucket write: %s", call)
		}
	}
}

func TestUploadAbortsOnUnreadableFile(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/gba/Locked.gba": "locked",
		"roms/snes/Game.sfc":  "snes",
	})
	mock := storage.NewMockBackend()
	opts := Options{SourcePath: source, SyncDirs: []string{"roms"}, CachePath: tempCachePath(t)}

	if _, err := Run(context.Background(), mock, opts); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Touch the file so the cache misses and it must be re-read.
	locked := filepath.Join(source, "roms/gba/Locked.gba")
	if err := os.WriteFile(locked, []byte("locked v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o644) })

	mock.Calls = nil
	if _, err := Run(context.Background(), mock, opts); err == nil {
		t.Fatal("expected error for unreadable file, got nil")
	}
	assertNoBucketWrites(t, mock)
	if len(verifyManifest(t, mock).Files) != 2 {
		t.Error("remote manifest should still list both files")
	}
}

func TestUploadRefusesWhenSyncDirMissing(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/snes/Game.sfc": "snes",
		"bios/scph5501.bin":  "bios",
	})
	mock := storage.NewMockBackend()
	opts := Options{SourcePath: source, SyncDirs: []string{"roms", "bios"}, CachePath: tempCachePath(t)}

	if _, err := Run(context.Background(), mock, opts); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Simulate an unmounted drive
	if err := os.RemoveAll(filepath.Join(source, "bios")); err != nil {
		t.Fatal(err)
	}

	for _, manifestOnly := range []bool{false, true} {
		mock.Calls = nil
		opts.ManifestOnly = manifestOnly
		_, err := Run(context.Background(), mock, opts)
		if err == nil {
			t.Fatalf("ManifestOnly=%v: expected error for missing sync dir with remote files, got nil", manifestOnly)
		}
		if !strings.Contains(err.Error(), "bios") {
			t.Errorf("error should name the missing dir: %v", err)
		}
		assertNoBucketWrites(t, mock)
	}
	if _, ok := mock.Objects["bios/scph5501.bin"]; !ok {
		t.Error("bios file should still be in the bucket")
	}
}

func TestUploadRemovedSyncDirDeletesIntentionally(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/snes/Game.sfc": "snes",
		"bios/scph5501.bin":  "bios",
	})
	mock := storage.NewMockBackend()
	opts := Options{SourcePath: source, SyncDirs: []string{"roms", "bios"}, CachePath: tempCachePath(t)}

	if _, err := Run(context.Background(), mock, opts); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Dropping bios from sync_dirs is an explicit choice to stop publishing it.
	opts.SyncDirs = []string{"roms"}
	result, err := Run(context.Background(), mock, opts)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if len(result.Deleted) != 1 || result.Deleted[0] != "bios/scph5501.bin" {
		t.Errorf("deleted = %v, want [bios/scph5501.bin]", result.Deleted)
	}
}

func TestUploadFollowsSymlinkedSyncDir(t *testing.T) {
	storageDir := setupSourceDir(t, map[string]string{
		"snes/Game.sfc": "snes",
	})
	source := t.TempDir()
	if err := os.Symlink(storageDir, filepath.Join(source, "roms")); err != nil {
		t.Fatal(err)
	}

	mock := storage.NewMockBackend()
	result, err := Run(context.Background(), mock, Options{
		SourcePath: source,
		SyncDirs:   []string{"roms"},
		CachePath:  tempCachePath(t),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(result.Uploaded) != 1 {
		t.Errorf("uploaded %d, want 1", len(result.Uploaded))
	}
	if _, ok := verifyManifest(t, mock).Files["roms/snes/Game.sfc"]; !ok {
		t.Error("manifest should key files by the configured dir name")
	}
}

func TestUploadSkipsNestedDirSymlink(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/genesis/Game.md": "genesis",
		"roms/zz/Late.rom":     "sorts after the symlink",
	})
	if err := os.Symlink("genesis", filepath.Join(source, "roms/megadrive")); err != nil {
		t.Fatal(err)
	}

	mock := storage.NewMockBackend()
	_, err := Run(context.Background(), mock, Options{
		SourcePath: source,
		SyncDirs:   []string{"roms"},
		CachePath:  tempCachePath(t),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	m := verifyManifest(t, mock)
	if len(m.Files) != 2 {
		t.Errorf("manifest has %d files, want 2: %v", len(m.Files), m.Files)
	}
	for key := range m.Files {
		if strings.HasPrefix(key, "roms/megadrive/") {
			t.Errorf("directory symlink contents should be skipped: %s", key)
		}
	}
}

func TestUploadRefusesWhenDirReplacedBySymlink(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/psx/Game.bin": "psx",
	})
	mock := storage.NewMockBackend()
	opts := Options{SourcePath: source, SyncDirs: []string{"roms"}, CachePath: tempCachePath(t)}

	if _, err := Run(context.Background(), mock, opts); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Move psx to another disk and leave a symlink behind
	elsewhere := t.TempDir()
	if err := os.Rename(filepath.Join(source, "roms/psx"), filepath.Join(elsewhere, "psx")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(elsewhere, "psx"), filepath.Join(source, "roms/psx")); err != nil {
		t.Fatal(err)
	}

	mock.Calls = nil
	if _, err := Run(context.Background(), mock, opts); err == nil {
		t.Fatal("expected error when a symlinked dir hides files already in the bucket")
	}
	assertNoBucketWrites(t, mock)
}

func TestUploadFileSymlinkUsesTargetSize(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/snes/Real.sfc": "ten bytes!",
	})
	target := filepath.Join(source, "roms/snes/Real.sfc")
	if err := os.Symlink(target, filepath.Join(source, "roms/snes/Link.sfc")); err != nil {
		t.Fatal(err)
	}

	mock := storage.NewMockBackend()
	if _, err := Run(context.Background(), mock, Options{
		SourcePath: source,
		SyncDirs:   []string{"roms"},
		CachePath:  tempCachePath(t),
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	entry, ok := verifyManifest(t, mock).Files["roms/snes/Link.sfc"]
	if !ok {
		t.Fatal("file symlink should be uploaded")
	}
	if entry.Size != 10 {
		t.Errorf("size = %d, want 10 (target size, not link size)", entry.Size)
	}
}
