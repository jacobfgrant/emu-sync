package sync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jacobfgrant/emu-sync/internal/manifest"
)

func TestVerifyAllOK(t *testing.T) {
	emuDir := t.TempDir()
	manifestPath := filepath.Join(t.TempDir(), "local-manifest.json")

	// Write a file and create a matching manifest
	writeFile(t, filepath.Join(emuDir, "roms/snes/Game.sfc"), "game data")

	m := manifest.New()
	m.Files["roms/snes/Game.sfc"] = manifest.FileEntry{
		Size: 9,
		MD5:  md5hex("game data"),
	}
	m.SaveJSON(manifestPath)

	cfg := testConfig(emuDir)
	result, err := Verify(cfg, manifestPath, false)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if len(result.OK) != 1 {
		t.Errorf("OK = %d, want 1", len(result.OK))
	}
	if len(result.Mismatch) != 0 {
		t.Errorf("Mismatch = %d, want 0", len(result.Mismatch))
	}
	if len(result.Missing) != 0 {
		t.Errorf("Missing = %d, want 0", len(result.Missing))
	}
}

func TestVerifyDetectsMismatch(t *testing.T) {
	emuDir := t.TempDir()
	manifestPath := filepath.Join(t.TempDir(), "local-manifest.json")

	// Write a file, but manifest has different hash
	writeFile(t, filepath.Join(emuDir, "roms/snes/Game.sfc"), "modified data")

	m := manifest.New()
	m.Files["roms/snes/Game.sfc"] = manifest.FileEntry{
		Size: 13,
		MD5:  md5hex("original data"),
	}
	m.SaveJSON(manifestPath)

	cfg := testConfig(emuDir)
	result, err := Verify(cfg, manifestPath, false)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if len(result.Mismatch) != 1 {
		t.Errorf("Mismatch = %d, want 1", len(result.Mismatch))
	}

	// Manifest entry should be removed
	updated, _ := manifest.LoadJSON(manifestPath)
	if _, ok := updated.Files["roms/snes/Game.sfc"]; ok {
		t.Error("mismatched entry should be removed from manifest")
	}
}

func TestVerifyDetectsSizeMismatch(t *testing.T) {
	emuDir := t.TempDir()
	manifestPath := filepath.Join(t.TempDir(), "local-manifest.json")

	writeFile(t, filepath.Join(emuDir, "roms/snes/Game.sfc"), "short")

	m := manifest.New()
	m.Files["roms/snes/Game.sfc"] = manifest.FileEntry{
		Size: 9999, // wrong size
		MD5:  md5hex("short"),
	}
	m.SaveJSON(manifestPath)

	cfg := testConfig(emuDir)
	result, err := Verify(cfg, manifestPath, false)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if len(result.Mismatch) != 1 {
		t.Errorf("Mismatch = %d, want 1", len(result.Mismatch))
	}
}

func TestVerifyDetectsMissing(t *testing.T) {
	emuDir := t.TempDir()
	manifestPath := filepath.Join(t.TempDir(), "local-manifest.json")

	// Manifest references a file that doesn't exist
	m := manifest.New()
	m.Files["roms/snes/Gone.sfc"] = manifest.FileEntry{
		Size: 100,
		MD5:  "abc123",
	}
	m.SaveJSON(manifestPath)

	cfg := testConfig(emuDir)
	result, err := Verify(cfg, manifestPath, false)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if len(result.Missing) != 1 {
		t.Errorf("Missing = %d, want 1", len(result.Missing))
	}

	// Missing entry should be removed from manifest
	updated, _ := manifest.LoadJSON(manifestPath)
	if _, ok := updated.Files["roms/snes/Gone.sfc"]; ok {
		t.Error("missing entry should be removed from manifest")
	}
}

func TestVerifyMixed(t *testing.T) {
	emuDir := t.TempDir()
	manifestPath := filepath.Join(t.TempDir(), "local-manifest.json")

	writeFile(t, filepath.Join(emuDir, "roms/snes/Good.sfc"), "good")
	writeFile(t, filepath.Join(emuDir, "roms/snes/Bad.sfc"), "tampered")

	m := manifest.New()
	m.Files["roms/snes/Good.sfc"] = manifest.FileEntry{Size: 4, MD5: md5hex("good")}
	m.Files["roms/snes/Bad.sfc"] = manifest.FileEntry{Size: 8, MD5: md5hex("original")}
	m.Files["roms/snes/Gone.sfc"] = manifest.FileEntry{Size: 5, MD5: "aaa"}
	m.SaveJSON(manifestPath)

	cfg := testConfig(emuDir)
	result, err := Verify(cfg, manifestPath, false)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if len(result.OK) != 1 {
		t.Errorf("OK = %d, want 1", len(result.OK))
	}
	if len(result.Mismatch) != 1 {
		t.Errorf("Mismatch = %d, want 1", len(result.Mismatch))
	}
	if len(result.Missing) != 1 {
		t.Errorf("Missing = %d, want 1", len(result.Missing))
	}

	// Only Good.sfc should remain in manifest
	updated, _ := manifest.LoadJSON(manifestPath)
	if len(updated.Files) != 1 {
		t.Errorf("manifest should have 1 entry, got %d", len(updated.Files))
	}
	if _, ok := updated.Files["roms/snes/Good.sfc"]; !ok {
		t.Error("Good.sfc should remain in manifest")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
