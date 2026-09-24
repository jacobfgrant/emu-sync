package sync

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/jacobfgrant/emu-sync/internal/manifest"
)

// writeLocalFiles creates each key under emuDir with placeholder content.
func writeLocalFiles(t *testing.T, emuDir string, keys ...string) {
	t.Helper()
	for _, key := range keys {
		path := filepath.Join(emuDir, filepath.FromSlash(key))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPlanClassifiesChanges(t *testing.T) {
	emuDir := t.TempDir()
	cfg := testConfig(emuDir)

	remote := manifest.New()
	remote.Files["roms/snes/New.sfc"] = manifest.FileEntry{Size: 1, MD5: "new"}
	remote.Files["roms/snes/Changed.sfc"] = manifest.FileEntry{Size: 1, MD5: "v2"}
	remote.Files["roms/snes/Same.sfc"] = manifest.FileEntry{Size: 1, MD5: "same"}
	remote.Files["roms/snes/Missing.sfc"] = manifest.FileEntry{Size: 1, MD5: "gone"}

	local := manifest.New()
	local.Files["roms/snes/Changed.sfc"] = manifest.FileEntry{Size: 1, MD5: "v1"}
	local.Files["roms/snes/Same.sfc"] = manifest.FileEntry{Size: 1, MD5: "same"}
	local.Files["roms/snes/Missing.sfc"] = manifest.FileEntry{Size: 1, MD5: "gone"}
	local.Files["roms/snes/Removed.sfc"] = manifest.FileEntry{Size: 1, MD5: "old"}
	writeLocalFiles(t, emuDir, "roms/snes/Changed.sfc", "roms/snes/Same.sfc", "roms/snes/Removed.sfc")

	plan := NewPlan(remote, local, cfg)

	if want := []string{"roms/snes/Missing.sfc", "roms/snes/New.sfc"}; !slices.Equal(plan.Added, want) {
		t.Errorf("Added = %v, want %v", plan.Added, want)
	}
	if want := []string{"roms/snes/Changed.sfc"}; !slices.Equal(plan.Modified, want) {
		t.Errorf("Modified = %v, want %v", plan.Modified, want)
	}
	if want := []string{"roms/snes/Removed.sfc"}; !slices.Equal(plan.Deleted, want) {
		t.Errorf("Deleted = %v, want %v", plan.Deleted, want)
	}
	if want := []string{"roms/snes/Missing.sfc"}; !slices.Equal(plan.Missing, want) {
		t.Errorf("Missing = %v, want %v", plan.Missing, want)
	}
	if plan.Unchanged != 1 {
		t.Errorf("Unchanged = %d, want 1", plan.Unchanged)
	}
	if _, ok := local.Files["roms/snes/Missing.sfc"]; !ok {
		t.Error("NewPlan should not modify the local manifest")
	}
}

func TestPlanDeletesDeselectedFiles(t *testing.T) {
	emuDir := t.TempDir()
	cfg := testConfig(emuDir)
	cfg.Sync.SyncExclude = []string{"roms/gba"}

	entry := manifest.FileEntry{Size: 1, MD5: "a"}
	remote := manifest.New()
	remote.Files["roms/snes/Keep.sfc"] = entry
	remote.Files["roms/gba/Deselected.gba"] = entry

	local := manifest.New()
	local.Files["roms/snes/Keep.sfc"] = entry
	local.Files["roms/gba/Deselected.gba"] = entry
	writeLocalFiles(t, emuDir, "roms/snes/Keep.sfc", "roms/gba/Deselected.gba")

	plan := NewPlan(remote, local, cfg)

	// Still in the bucket, but no longer selected: sync will remove it.
	if want := []string{"roms/gba/Deselected.gba"}; !slices.Equal(plan.Deleted, want) {
		t.Errorf("Deleted = %v, want %v", plan.Deleted, want)
	}
	if plan.Unchanged != 1 {
		t.Errorf("Unchanged = %d, want 1", plan.Unchanged)
	}
}
