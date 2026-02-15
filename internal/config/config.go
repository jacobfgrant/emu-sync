package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
	EmulationPath  string   `toml:"emulation_path"`
	SyncDirs       []string `toml:"sync_dirs"`
	SyncExclude    []string `toml:"sync_exclude,omitempty"`
	Delete         bool     `toml:"delete"`
	Workers        int      `toml:"workers"`
	MaxRetries     int      `toml:"max_retries"`
	BandwidthLimit string   `toml:"bandwidth_limit,omitempty"`
}

// WebConfig holds settings for the web UI.
type WebConfig struct {
	Port int `toml:"port,omitempty"`
}

// Config is the top-level configuration.
type Config struct {
	Storage StorageConfig `toml:"storage"`
	Sync    SyncConfig    `toml:"sync"`
	Web     WebConfig     `toml:"web,omitempty"`
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
		if key == ex || strings.HasPrefix(key, ex+"/") {
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

// ParseBandwidthLimit parses a human-readable bandwidth string (e.g.,
// "10MB", "500KB", "1024") into bytes per second. Returns 0 for empty
// string or "0" (unlimited).
func ParseBandwidthLimit(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, nil
	}

	upper := strings.ToUpper(s)
	var multiplier int64 = 1
	var numStr string

	switch {
	case strings.HasSuffix(upper, "GB"):
		multiplier = 1024 * 1024 * 1024
		numStr = strings.TrimSuffix(upper, "GB")
	case strings.HasSuffix(upper, "MB"):
		multiplier = 1024 * 1024
		numStr = strings.TrimSuffix(upper, "MB")
	case strings.HasSuffix(upper, "KB"):
		multiplier = 1024
		numStr = strings.TrimSuffix(upper, "KB")
	default:
		numStr = upper
	}

	numStr = strings.TrimSpace(numStr)
	n, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid bandwidth limit %q: %w", s, err)
	}
	if n < 0 {
		return 0, fmt.Errorf("bandwidth limit cannot be negative: %s", s)
	}

	return int64(n * float64(multiplier)), nil
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
