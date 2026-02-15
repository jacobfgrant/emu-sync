package upload

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jacobfgrant/emu-sync/internal/manifest"
	"github.com/jacobfgrant/emu-sync/internal/storage"
)

func TestUploadNewFiles(t *testing.T) {
	source := setupSourceDir(t, map[string]string{
		"roms/snes/Game.sfc": "snes rom data",
		"bios/scph5501.bin":  "bios data",
	})

	mock := storage.NewMockBackend()
	result, err := Run(context.Background(), mock, source, []string{"roms", "bios"}, false, false, false)
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

	// First upload
	_, err := Run(context.Background(), mock, source, []string{"roms"}, false, false, false)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Second upload with same files — should skip
	mock.Calls = nil
	result, err := Run(context.Background(), mock, source, []string{"roms"}, false, false, false)
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

	// First upload
	_, err := Run(context.Background(), mock, source, []string{"roms"}, false, false, false)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Modify the file
	os.WriteFile(filepath.Join(source, "roms/snes/Game.sfc"), []byte("modified data"), 0o644)

	// Second upload — should detect change
	result, err := Run(context.Background(), mock, source, []string{"roms"}, false, false, false)
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

	// Upload both
	_, err := Run(context.Background(), mock, source, []string{"roms"}, false, false, false)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Remove one file locally
	os.Remove(filepath.Join(source, "roms/snes/Game2.sfc"))

	// Re-upload — should delete Game2 from bucket
	result, err := Run(context.Background(), mock, source, []string{"roms"}, false, false, false)
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
	result, err := Run(context.Background(), mock, source, []string{"roms"}, true, false, false)
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
	result, err := Run(context.Background(), mock, source, []string{"roms", "bios"}, false, false, false)
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

	result, err := Run(context.Background(), mock, source, []string{"roms"}, false, false, false)
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
