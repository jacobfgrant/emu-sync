package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewManifest(t *testing.T) {
	m := New()
	if m.Version != 1 {
		t.Errorf("version = %d, want 1", m.Version)
	}
	if m.Files == nil {
		t.Error("files map should be initialized")
	}
	if !m.IsEmpty() {
		t.Error("new manifest should be empty")
	}
}

func TestSaveAndLoad(t *testing.T) {
	m := New()
	m.Files["roms/snes/Game.sfc"] = FileEntry{Size: 1024, MD5: "abc123"}
	m.Files["bios/scph5501.bin"] = FileEntry{Size: 2048, MD5: "def456"}

	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	if err := m.SaveJSON(path); err != nil {
		t.Fatalf("SaveJSON: %v", err)
	}

	loaded, err := LoadJSON(path)
	if err != nil {
		t.Fatalf("LoadJSON: %v", err)
	}

	if len(loaded.Files) != 2 {
		t.Fatalf("loaded %d files, want 2", len(loaded.Files))
	}

	entry := loaded.Files["roms/snes/Game.sfc"]
	if entry.Size != 1024 || entry.MD5 != "abc123" {
		t.Errorf("round-trip mismatch: got %+v", entry)
	}
}

func TestParseJSON(t *testing.T) {
	data := []byte(`{"version":1,"generated_at":"2026-01-01T00:00:00Z","files":{"roms/test.rom":{"size":512,"md5":"aaa"}}}`)

	m, err := ParseJSON(data)
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}

	if len(m.Files) != 1 {
		t.Fatalf("got %d files, want 1", len(m.Files))
	}
	if m.Files["roms/test.rom"].Size != 512 {
		t.Errorf("size = %d, want 512", m.Files["roms/test.rom"].Size)
	}
}

func TestParseJSONEmptyFiles(t *testing.T) {
	data := []byte(`{"version":1,"generated_at":"2026-01-01T00:00:00Z"}`)

	m, err := ParseJSON(data)
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}

	if m.Files == nil {
		t.Error("files map should be initialized even when absent in JSON")
	}
}

func TestDiffAdded(t *testing.T) {
	remote := New()
	remote.Files["roms/new.rom"] = FileEntry{Size: 100, MD5: "aaa"}

	local := New()

	diff := Diff(remote, local)

	if len(diff.Added) != 1 || diff.Added[0] != "roms/new.rom" {
		t.Errorf("added = %v, want [roms/new.rom]", diff.Added)
	}
	if len(diff.Modified) != 0 {
		t.Errorf("modified = %v, want empty", diff.Modified)
	}
	if len(diff.Deleted) != 0 {
		t.Errorf("deleted = %v, want empty", diff.Deleted)
	}
}

func TestDiffModified(t *testing.T) {
	remote := New()
	remote.Files["roms/game.rom"] = FileEntry{Size: 200, MD5: "new_hash"}

	local := New()
	local.Files["roms/game.rom"] = FileEntry{Size: 100, MD5: "old_hash"}

	diff := Diff(remote, local)

	if len(diff.Added) != 0 {
		t.Errorf("added = %v, want empty", diff.Added)
	}
	if len(diff.Modified) != 1 || diff.Modified[0] != "roms/game.rom" {
		t.Errorf("modified = %v, want [roms/game.rom]", diff.Modified)
	}
}

func TestDiffDeleted(t *testing.T) {
	remote := New()

	local := New()
	local.Files["roms/old.rom"] = FileEntry{Size: 100, MD5: "aaa"}

	diff := Diff(remote, local)

	if len(diff.Deleted) != 1 || diff.Deleted[0] != "roms/old.rom" {
		t.Errorf("deleted = %v, want [roms/old.rom]", diff.Deleted)
	}
}

func TestDiffNoChanges(t *testing.T) {
	remote := New()
	remote.Files["roms/game.rom"] = FileEntry{Size: 100, MD5: "same"}

	local := New()
	local.Files["roms/game.rom"] = FileEntry{Size: 100, MD5: "same"}

	diff := Diff(remote, local)

	if len(diff.Added) != 0 || len(diff.Modified) != 0 || len(diff.Deleted) != 0 {
		t.Errorf("expected no changes, got added=%v modified=%v deleted=%v",
			diff.Added, diff.Modified, diff.Deleted)
	}
}

func TestHashFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.bin")

	// "hello" has a known MD5
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	hash, err := HashFile(path)
	if err != nil {
		t.Fatalf("HashFile: %v", err)
	}

	// md5("hello") = 5d41402abc4b2a76b9719d911017c592
	expected := "5d41402abc4b2a76b9719d911017c592"
	if hash != expected {
		t.Errorf("hash = %q, want %q", hash, expected)
	}
}

func TestToJSON(t *testing.T) {
	m := New()
	m.Files["test.rom"] = FileEntry{Size: 1, MD5: "a"}

	data, err := m.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}

	roundtrip, err := ParseJSON(data)
	if err != nil {
		t.Fatalf("ParseJSON after ToJSON: %v", err)
	}

	if len(roundtrip.Files) != 1 {
		t.Errorf("round-trip got %d files, want 1", len(roundtrip.Files))
	}
}
