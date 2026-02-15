package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// StorageConfig holds S3-compatible storage credentials and settings.
type StorageConfig struct {
	EndpointURL string `toml:"endpoint_url"`
	Bucket      string `toml:"bucket"`
	KeyID       string `toml:"key_id"`
	SecretKey   string `toml:"secret_key"`
	Region      string `toml:"region"`
	Prefix      string `toml:"prefix,omitempty"`
}

// SyncConfig holds local sync settings.
type SyncConfig struct {
	EmulationPath string   `toml:"emulation_path"`
	SyncDirs      []string `toml:"sync_dirs"`
	SyncExclude   []string `toml:"sync_exclude,omitempty"`
	Delete        bool     `toml:"delete"`
	Workers       int      `toml:"workers"`
}

// Config is the top-level configuration.
type Config struct {
	Storage StorageConfig `toml:"storage"`
	Sync    SyncConfig    `toml:"sync"`
}

// DefaultConfigPath returns the config file path, using XDG_CONFIG_HOME
// if set, otherwise ~/.config.
func DefaultConfigPath() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "emu-sync", "config.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "emu-sync", "config.toml")
}

// DefaultLocalManifestPath returns the local manifest path, using
// XDG_DATA_HOME if set, otherwise ~/.local/share.
func DefaultLocalManifestPath() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "emu-sync", "local-manifest.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "emu-sync", "local-manifest.json")
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
	c.Sync.EmulationPath = expandPath(c.Sync.EmulationPath)
	if len(c.Sync.SyncDirs) == 0 {
		c.Sync.SyncDirs = []string{"roms", "bios"}
	}
	return nil
}

// expandPath resolves environment variables, ~, and relative paths to
// absolute paths. Relative paths are resolved against the user's home
// directory (not the working directory) so the result is stable
// regardless of where the command is run from.
func expandPath(p string) string {
	p = os.ExpandEnv(p)
	if filepath.IsAbs(p) {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if strings.HasPrefix(p, "~/") || p == "~" {
		return filepath.Join(home, p[2:])
	}
	return filepath.Join(home, p)
}

// ShouldSync returns true if the given key passes the sync_dirs include
// filter and is not in sync_exclude. Keys match sync_dirs by prefix
// (e.g., "roms/snes" matches "roms/snes/Game.sfc") or exact match
// (for individual file entries).
func (c *Config) ShouldSync(key string) bool {
	for _, ex := range c.Sync.SyncExclude {
		if key == ex {
			return false
		}
	}
	for _, dir := range c.Sync.SyncDirs {
		if key == dir || strings.HasPrefix(key, dir+"/") {
			return true
		}
	}
	return false
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
