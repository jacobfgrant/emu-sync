package token

import (
	"testing"

	"github.com/jacobfgrant/emu-sync/internal/config"
)

func TestEncodeAndDecode(t *testing.T) {
	original := &Data{
		EndpointURL:   "https://s3.us-west-004.backblazeb2.com",
		Bucket:        "my-roms",
		KeyID:         "004abc",
		SecretKey:     "K004xyz",
		Region:        "us-west-004",
		EmulationPath: "/run/media/mmcblk0p1/Emulation",
	}

	encoded, err := Encode(original)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if decoded.Bucket != original.Bucket {
		t.Errorf("bucket = %q, want %q", decoded.Bucket, original.Bucket)
	}
	if decoded.EndpointURL != original.EndpointURL {
		t.Errorf("endpoint = %q, want %q", decoded.EndpointURL, original.EndpointURL)
	}
	if decoded.EmulationPath != original.EmulationPath {
		t.Errorf("path = %q, want %q", decoded.EmulationPath, original.EmulationPath)
	}
}

func TestDecodeInvalidBase64(t *testing.T) {
	_, err := Decode("not-valid-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestDecodeInvalidJSON(t *testing.T) {
	// Valid base64 but not JSON
	_, err := Decode("aGVsbG8gd29ybGQ=")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestDecodeMissingFields(t *testing.T) {
	// Valid base64 JSON but missing required fields
	_, err := Decode("eyJidWNrZXQiOiIifQ==") // {"bucket":""}
	if err == nil {
		t.Fatal("expected error for missing fields")
	}
}

func TestToConfigDefaults(t *testing.T) {
	d := &Data{
		EndpointURL:   "https://example.com",
		Bucket:        "test",
		KeyID:         "key",
		SecretKey:     "secret",
		Region:        "us-east-1",
		EmulationPath: "/tmp/emu",
	}

	cfg := d.ToConfig()

	if cfg.Storage.Bucket != "test" {
		t.Errorf("bucket = %q", cfg.Storage.Bucket)
	}
	if cfg.Sync.EmulationPath != "/tmp/emu" {
		t.Errorf("path = %q", cfg.Sync.EmulationPath)
	}
	if !cfg.Sync.Delete {
		t.Error("delete should default to true")
	}
	if len(cfg.Sync.SyncDirs) != 2 || cfg.Sync.SyncDirs[0] != "roms" {
		t.Errorf("sync_dirs should default to [roms, bios], got %v", cfg.Sync.SyncDirs)
	}
}

func TestToConfigWithSyncDirsAndDelete(t *testing.T) {
	deleteFalse := false
	d := &Data{
		EndpointURL:   "https://example.com",
		Bucket:        "test",
		KeyID:         "key",
		SecretKey:     "secret",
		Region:        "us-east-1",
		EmulationPath: "/tmp/emu",
		SyncDirs:      []string{"roms", "bios", "saves"},
		Delete:        &deleteFalse,
	}

	cfg := d.ToConfig()

	if cfg.Sync.Delete {
		t.Error("delete should be false when explicitly set")
	}
	if len(cfg.Sync.SyncDirs) != 3 || cfg.Sync.SyncDirs[2] != "saves" {
		t.Errorf("sync_dirs = %v, want [roms bios saves]", cfg.Sync.SyncDirs)
	}
}

func TestFromConfig(t *testing.T) {
	cfg := &config.Config{
		Storage: config.StorageConfig{
			EndpointURL: "https://example.com",
			Bucket:      "test",
			KeyID:       "key",
			SecretKey:   "secret",
			Region:      "us-east-1",
		},
		Sync: config.SyncConfig{
			EmulationPath: "/tmp/emu",
			SyncDirs:      []string{"roms", "saves"},
			Delete:        false,
		},
	}

	d := FromConfig(cfg)

	if d.Bucket != "test" {
		t.Errorf("bucket = %q", d.Bucket)
	}
	if d.EmulationPath != "/tmp/emu" {
		t.Errorf("path = %q", d.EmulationPath)
	}
	if len(d.SyncDirs) != 2 || d.SyncDirs[1] != "saves" {
		t.Errorf("sync_dirs = %v, want [roms saves]", d.SyncDirs)
	}
	if d.Delete == nil || *d.Delete != false {
		t.Error("delete should be false")
	}
}

func TestBackwardCompatibility(t *testing.T) {
	// Simulate a v0.1.0 token (no sync_dirs or delete fields)
	original := &Data{
		EndpointURL:   "https://example.com",
		Bucket:        "test",
		KeyID:         "key",
		SecretKey:     "secret",
		Region:        "us-east-1",
		EmulationPath: "/tmp/emu",
	}

	encoded, err := Encode(original)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	// Old tokens should have nil for new fields
	if decoded.SyncDirs != nil {
		t.Errorf("sync_dirs should be nil for old tokens, got %v", decoded.SyncDirs)
	}
	if decoded.Delete != nil {
		t.Errorf("delete should be nil for old tokens, got %v", decoded.Delete)
	}

	// ToConfig should apply defaults for old tokens
	cfg := decoded.ToConfig()
	if len(cfg.Sync.SyncDirs) != 2 {
		t.Errorf("old token should get default sync_dirs, got %v", cfg.Sync.SyncDirs)
	}
	if !cfg.Sync.Delete {
		t.Error("old token should get delete=true default")
	}
}
