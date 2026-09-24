package cmd

import (
	"math/rand"
	"path/filepath"
	"testing"

	"github.com/jacobfgrant/emu-sync/internal/config"
	"github.com/jacobfgrant/emu-sync/internal/manifest"
)

// selectionKeys mirrors an EmuDeck-style layout: loose BIOS files next to
// per-emulator BIOS dirs, and nested dirs inside a system.
var selectionKeys = []string{
	"bios/scph1001.bin",
	"bios/scph5501.bin",
	"bios/pcsx2/bios.bin",
	"bios/pcsx2/resources/a.glsl",
	"bios/pcsx2/resources/b.glsl",
	"roms/gba/A.gba",
	"roms/gba/B.gba",
	"roms/psx/Game (Disc 1).bin",
	"roms/psx/Game (Disc 2).bin",
	"roms/psx/multi/Game.m3u",
	"roms/snes/A.sfc",
	"roms/snes/B.sfc",
	"roms/snes/C.sfc",
}

// selectionGroups builds groups for selectionKeys with the given selection.
func selectionGroups(t *testing.T, selected func(key string) bool) []*systemGroup {
	t.Helper()
	m := manifest.New()
	for _, key := range selectionKeys {
		m.Files[key] = manifest.FileEntry{Size: 1, MD5: "x"}
	}
	groups := buildGroups(m, &config.Config{})
	for _, g := range groups {
		for i := range g.Files {
			g.Files[i].Selected = selected(g.Files[i].Key)
		}
	}
	return groups
}

func configFor(groups []*systemGroup) *config.Config {
	cfg := &config.Config{}
	cfg.Sync.SyncDirs, cfg.Sync.SyncExclude = encodeSelections(groups)
	return cfg
}

// assertMatchesSelection fails if cfg would sync a different set of keys
// than the selection it was encoded from.
func assertMatchesSelection(t *testing.T, cfg *config.Config, selected func(key string) bool) {
	t.Helper()
	for _, key := range selectionKeys {
		if got, want := cfg.ShouldSync(key), selected(key); got != want {
			t.Errorf("ShouldSync(%q) = %v, want %v (sync_dirs=%v exclude=%v)",
				key, got, want, cfg.Sync.SyncDirs, cfg.Sync.SyncExclude)
		}
	}
}

func TestEncodeSelectionsRoundTripsRandomSelections(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 2000; i++ {
		picks := make(map[string]bool, len(selectionKeys))
		for _, key := range selectionKeys {
			picks[key] = rng.Intn(2) == 0
		}
		selected := func(key string) bool { return picks[key] }

		assertMatchesSelection(t, configFor(selectionGroups(t, selected)), selected)
		if t.Failed() {
			t.Fatalf("failed on iteration %d with selection %v", i, picks)
		}
	}
}

func TestEncodeSelectionsLooseBiosDoesNotReincludeSubdir(t *testing.T) {
	// Loose BIOS files selected, pcsx2 deselected. Saving "bios" alone
	// would also match bios/pcsx2/... by prefix.
	selected := func(key string) bool {
		return filepath.Dir(key) == "bios" || filepath.Dir(key) == "roms/snes"
	}
	cfg := configFor(selectionGroups(t, selected))

	assertMatchesSelection(t, cfg, selected)
	if cfg.ShouldSync("bios/pcsx2/bios.bin") {
		t.Error("deselected bios/pcsx2 should not sync")
	}
}

func TestEncodeSelectionsAllFollowsNewSystems(t *testing.T) {
	cfg := configFor(selectionGroups(t, func(string) bool { return true }))

	if !cfg.ShouldSync("roms/n64/New.z64") {
		t.Errorf("a system added later should sync when everything is selected (sync_dirs=%v)", cfg.Sync.SyncDirs)
	}
	if !cfg.ShouldSync("roms/snes/New.sfc") {
		t.Error("a game added later to a selected system should sync")
	}
}

func TestEncodeSelectionsFewPicksDoNotFollowNewSystems(t *testing.T) {
	selected := func(key string) bool { return filepath.Dir(key) == "roms/gba" }
	cfg := configFor(selectionGroups(t, selected))

	assertMatchesSelection(t, cfg, selected)
	if cfg.ShouldSync("roms/n64/New.z64") {
		t.Errorf("picking one system should not opt in to new ones (sync_dirs=%v)", cfg.Sync.SyncDirs)
	}
}

func TestEncodeSelectionsNoneSurvivesConfigReload(t *testing.T) {
	none := func(string) bool { return false }
	groups := selectionGroups(t, none)

	cfg := &config.Config{
		Storage: config.StorageConfig{Bucket: "b", KeyID: "k", SecretKey: "s"},
		Sync:    config.SyncConfig{EmulationPath: t.TempDir()},
	}
	cfg.Sync.SyncDirs, cfg.Sync.SyncExclude = encodeSelections(groups)

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Write(cfg, path); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	assertMatchesSelection(t, loaded, none)
}
