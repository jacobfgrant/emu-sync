package upload

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCacheLookupHit(t *testing.T) {
	c := newHashCache()
	mtime := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	c.update("roms/snes/Game.sfc", 1024, mtime, "abc123")

	hash, ok := c.lookup("roms/snes/Game.sfc", 1024, mtime)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if hash != "abc123" {
		t.Errorf("hash = %q, want %q", hash, "abc123")
	}
}

func TestCacheLookupMissWrongSize(t *testing.T) {
	c := newHashCache()
	mtime := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	c.update("roms/snes/Game.sfc", 1024, mtime, "abc123")

	_, ok := c.lookup("roms/snes/Game.sfc", 2048, mtime)
	if ok {
		t.Fatal("expected cache miss for wrong size")
	}
}

func TestCacheLookupMissWrongMtime(t *testing.T) {
	c := newHashCache()
	mtime := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	c.update("roms/snes/Game.sfc", 1024, mtime, "abc123")

	_, ok := c.lookup("roms/snes/Game.sfc", 1024, mtime.Add(time.Second))
	if ok {
		t.Fatal("expected cache miss for wrong mtime")
	}
}

func TestCacheLookupMissMissingKey(t *testing.T) {
	c := newHashCache()

	_, ok := c.lookup("roms/snes/Game.sfc", 1024, time.Now())
	if ok {
		t.Fatal("expected cache miss for missing key")
	}
}

func TestCacheSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")

	c := newHashCache()
	mtime := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	c.update("roms/snes/Game.sfc", 1024, mtime, "abc123")
	c.update("bios/scph5501.bin", 512, mtime, "def456")

	if err := c.save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded := loadHashCache(path)
	if len(loaded.Files) != 2 {
		t.Fatalf("loaded %d entries, want 2", len(loaded.Files))
	}

	hash, ok := loaded.lookup("roms/snes/Game.sfc", 1024, mtime)
	if !ok || hash != "abc123" {
		t.Errorf("round-trip failed: ok=%v hash=%q", ok, hash)
	}
}

func TestLoadCacheMissingFile(t *testing.T) {
	c := loadHashCache("/nonexistent/path/cache.json")
	if len(c.Files) != 0 {
		t.Errorf("expected empty cache, got %d entries", len(c.Files))
	}
}

func TestLoadCacheCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")
	os.WriteFile(path, []byte("not json{{{"), 0o644)

	c := loadHashCache(path)
	if len(c.Files) != 0 {
		t.Errorf("expected empty cache for corrupt file, got %d entries", len(c.Files))
	}
}

func TestCachePrune(t *testing.T) {
	c := newHashCache()
	mtime := time.Now()
	c.update("keep-me", 100, mtime, "aaa")
	c.update("remove-me", 200, mtime, "bbb")

	c.prune(map[string]struct{}{"keep-me": {}})

	if len(c.Files) != 1 {
		t.Fatalf("expected 1 entry after prune, got %d", len(c.Files))
	}
	if _, ok := c.Files["keep-me"]; !ok {
		t.Error("keep-me should survive prune")
	}
	if _, ok := c.Files["remove-me"]; ok {
		t.Error("remove-me should be pruned")
	}
}
