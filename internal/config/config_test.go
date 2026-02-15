package config

import (
	"os"
	"path/filepath"
	"testing"
)

const validTOML = `
[storage]
endpoint_url = "https://s3.us-west-004.backblazeb2.com"
bucket = "my-roms"
key_id = "004abc"
secret_key = "K004xyz"
region = "us-west-004"

[sync]
emulation_path = "/tmp/Emulation"
sync_dirs = ["roms", "bios"]
delete = true
`

func TestLoadValid(t *testing.T) {
	path := writeTempConfig(t, validTOML)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Storage.Bucket != "my-roms" {
		t.Errorf("bucket = %q, want %q", cfg.Storage.Bucket, "my-roms")
	}
	if cfg.Storage.EndpointURL != "https://s3.us-west-004.backblazeb2.com" {
		t.Errorf("endpoint_url = %q, want backblaze URL", cfg.Storage.EndpointURL)
	}
	if cfg.Sync.EmulationPath != "/tmp/Emulation" {
		t.Errorf("emulation_path = %q, want /tmp/Emulation", cfg.Sync.EmulationPath)
	}
	if !cfg.Sync.Delete {
		t.Error("delete should be true")
	}
}

func TestLoadMissingBucket(t *testing.T) {
	toml := `
[storage]
key_id = "abc"
secret_key = "xyz"
[sync]
emulation_path = "/tmp"
`
	path := writeTempConfig(t, toml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing bucket")
	}
}

func TestLoadMissingEmulationPath(t *testing.T) {
	toml := `
[storage]
bucket = "b"
key_id = "abc"
secret_key = "xyz"
`
	path := writeTempConfig(t, toml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing emulation_path")
	}
}

func TestLoadDefaultSyncDirs(t *testing.T) {
	toml := `
[storage]
bucket = "b"
key_id = "abc"
secret_key = "xyz"
[sync]
emulation_path = "/tmp"
`
	path := writeTempConfig(t, toml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Sync.SyncDirs) != 2 || cfg.Sync.SyncDirs[0] != "roms" || cfg.Sync.SyncDirs[1] != "bios" {
		t.Errorf("sync_dirs = %v, want [roms bios]", cfg.Sync.SyncDirs)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/config.toml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestWriteAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "config.toml")

	cfg := &Config{
		Storage: StorageConfig{
			EndpointURL: "https://example.com",
			Bucket:      "test",
			KeyID:       "key",
			SecretKey:   "secret",
			Region:      "us-east-1",
		},
		Sync: SyncConfig{
			EmulationPath: "/tmp/emu",
			SyncDirs:      []string{"roms"},
			Delete:        false,
		},
	}

	if err := Write(cfg, path); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Write failed: %v", err)
	}

	if loaded.Storage.Bucket != "test" {
		t.Errorf("round-trip bucket = %q, want %q", loaded.Storage.Bucket, "test")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file permissions = %o, want 600", perm)
	}
}

func TestExpandTildePath(t *testing.T) {
	toml := `
[storage]
bucket = "b"
key_id = "abc"
secret_key = "xyz"
[sync]
emulation_path = "~/Emulation"
`
	path := writeTempConfig(t, toml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	home, _ := os.UserHomeDir()
	want := filepath.Join(home, "Emulation")
	if cfg.Sync.EmulationPath != want {
		t.Errorf("emulation_path = %q, want %q", cfg.Sync.EmulationPath, want)
	}
}

func TestExpandRelativePath(t *testing.T) {
	toml := `
[storage]
bucket = "b"
key_id = "abc"
secret_key = "xyz"
[sync]
emulation_path = "emu-sync/Emulation"
`
	path := writeTempConfig(t, toml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	home, _ := os.UserHomeDir()
	want := filepath.Join(home, "emu-sync", "Emulation")
	if cfg.Sync.EmulationPath != want {
		t.Errorf("emulation_path = %q, want %q", cfg.Sync.EmulationPath, want)
	}
}

func TestExpandEnvVarPath(t *testing.T) {
	t.Setenv("EMU_SYNC_TEST_DIR", "/opt/emulation")
	toml := `
[storage]
bucket = "b"
key_id = "abc"
secret_key = "xyz"
[sync]
emulation_path = "$EMU_SYNC_TEST_DIR/roms"
`
	path := writeTempConfig(t, toml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "/opt/emulation/roms"
	if cfg.Sync.EmulationPath != want {
		t.Errorf("emulation_path = %q, want %q", cfg.Sync.EmulationPath, want)
	}
}

func TestExpandEnvVarWithHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	toml := `
[storage]
bucket = "b"
key_id = "abc"
secret_key = "xyz"
[sync]
emulation_path = "$HOME/Emulation"
`
	path := writeTempConfig(t, toml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := filepath.Join(home, "Emulation")
	if cfg.Sync.EmulationPath != want {
		t.Errorf("emulation_path = %q, want %q", cfg.Sync.EmulationPath, want)
	}
}

func TestShouldSync(t *testing.T) {
	cfg := &Config{
		Sync: SyncConfig{
			SyncDirs:    []string{"roms", "bios"},
			SyncExclude: []string{"roms/snes/Bad.sfc", "roms/gba"},
		},
	}

	tests := []struct {
		key  string
		want bool
	}{
		{"roms/snes/Game.sfc", true},
		{"roms/gba/Game.gba", false},  // excluded by directory prefix
		{"roms/gba", false},           // exact match on excluded dir
		{"roms/gbatest/Game.gba", true}, // "roms/gba" prefix but not "roms/gba/"
		{"bios/scph5501.bin", true},
		{"saves/game.sav", false},
		{"roms/snes/Bad.sfc", false},  // excluded by exact match
		{"roms", true},                // exact dir match
		{"romshack/file", false},      // "roms" prefix but not "roms/"
	}

	for _, tt := range tests {
		got := cfg.ShouldSync(tt.key)
		if got != tt.want {
			t.Errorf("ShouldSync(%q) = %v, want %v", tt.key, got, tt.want)
		}
	}
}

func TestShouldSyncEmptyExclude(t *testing.T) {
	cfg := &Config{
		Sync: SyncConfig{
			SyncDirs: []string{"roms"},
		},
	}

	if !cfg.ShouldSync("roms/snes/Game.sfc") {
		t.Error("ShouldSync should work with empty exclude list")
	}
}

func TestParseBandwidthLimit(t *testing.T) {
	tests := []struct {
		input   string
		want    int64
		wantErr bool
	}{
		{"", 0, false},
		{"0", 0, false},
		{"1024", 1024, false},
		{"500KB", 500 * 1024, false},
		{"500kb", 500 * 1024, false},
		{"10MB", 10 * 1024 * 1024, false},
		{"10mb", 10 * 1024 * 1024, false},
		{"1GB", 1024 * 1024 * 1024, false},
		{"1.5MB", int64(1.5 * 1024 * 1024), false},
		{" 10MB ", 10 * 1024 * 1024, false},
		{"abc", 0, true},
		{"-5MB", 0, true},
	}

	for _, tt := range tests {
		got, err := ParseBandwidthLimit(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseBandwidthLimit(%q) err = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseBandwidthLimit(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}
	return path
}
