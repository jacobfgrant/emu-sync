package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/pelletier/go-toml/v2"
)

// StorageConfig holds S3-compatible storage credentials and settings.
type StorageConfig struct {
	EndpointURL string `toml:"endpoint_url"`
	Bucket      string `toml:"bucket"`
	KeyID       string `toml:"key_id"`
	SecretKey   string `toml:"secret_key"`
	Region      string `toml:"region"`
}

// SyncConfig holds local sync settings.
type SyncConfig struct {
	EmulationPath string   `toml:"emulation_path"`
	SyncDirs      []string `toml:"sync_dirs"`
	Delete        bool     `toml:"delete"`
	Workers       int      `toml:"workers"`
}

// Config is the top-level configuration.
type Config struct {
	Storage StorageConfig `toml:"storage"`
	Sync    SyncConfig    `toml:"sync"`
}

// DefaultConfigPath returns the platform-appropriate config file path.
func DefaultConfigPath() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "emu-sync", "config.toml")
	}
	return filepath.Join(os.Getenv("HOME"), ".config", "emu-sync", "config.toml")
}

// DefaultLocalManifestPath returns the platform-appropriate local manifest path.
func DefaultLocalManifestPath() string {
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "emu-sync", "local-manifest.json")
	default:
		if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
			return filepath.Join(dir, "emu-sync", "local-manifest.json")
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", "emu-sync", "local-manifest.json")
	}
}

// Load reads and parses a TOML config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if c.Storage.Bucket == "" {
		return fmt.Errorf("config: storage.bucket is required")
	}
	if c.Storage.KeyID == "" {
		return fmt.Errorf("config: storage.key_id is required")
	}
	if c.Storage.SecretKey == "" {
		return fmt.Errorf("config: storage.secret_key is required")
	}
	if c.Sync.EmulationPath == "" {
		return fmt.Errorf("config: sync.emulation_path is required")
	}
	if len(c.Sync.SyncDirs) == 0 {
		c.Sync.SyncDirs = []string{"roms", "bios"}
	}
	return nil
}

// Write serializes a Config to TOML and writes it to the given path.
func Write(cfg *Config, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("serializing config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	return nil
}
