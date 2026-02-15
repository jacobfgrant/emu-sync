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
emulation_path = "Emulation"
`
	path := writeTempConfig(t, toml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !filepath.IsAbs(cfg.Sync.EmulationPath) {
		t.Errorf("emulation_path should be absolute, got %q", cfg.Sync.EmulationPath)
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
