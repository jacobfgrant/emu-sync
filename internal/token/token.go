package token

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/jacobfgrant/emu-sync/internal/config"
)

// Data holds the fields encoded in a setup token.
type Data struct {
	EndpointURL   string `json:"endpoint_url"`
	Bucket        string `json:"bucket"`
	KeyID         string `json:"key_id"`
	SecretKey     string `json:"secret_key"`
	Region        string `json:"region"`
	Prefix        string `json:"prefix,omitempty"`
	EmulationPath string `json:"emulation_path"`
}

// Encode creates a base64 token from token data.
func Encode(d *Data) (string, error) {
	jsonBytes, err := json.Marshal(d)
	if err != nil {
		return "", fmt.Errorf("encoding token: %w", err)
	}
	return base64.StdEncoding.EncodeToString(jsonBytes), nil
}

// Decode parses a base64 token string back into token data.
func Decode(tokenStr string) (*Data, error) {
	jsonBytes, err := base64.StdEncoding.DecodeString(tokenStr)
	if err != nil {
		return nil, fmt.Errorf("invalid token (not valid base64): %w", err)
	}

	var d Data
	if err := json.Unmarshal(jsonBytes, &d); err != nil {
		return nil, fmt.Errorf("invalid token (not valid JSON): %w", err)
	}

	if d.Bucket == "" || d.KeyID == "" || d.SecretKey == "" {
		return nil, fmt.Errorf("invalid token: missing required fields (bucket, key_id, secret_key)")
	}

	return &d, nil
}

// ToConfig converts token data into a full Config.
func (d *Data) ToConfig() *config.Config {
	return &config.Config{
		Storage: config.StorageConfig{
			EndpointURL: d.EndpointURL,
			Bucket:      d.Bucket,
			KeyID:       d.KeyID,
			SecretKey:   d.SecretKey,
			Region:      d.Region,
			Prefix:      d.Prefix,
		},
		Sync: config.SyncConfig{
			EmulationPath: d.EmulationPath,
			SyncDirs:      []string{"roms", "bios"},
			Delete:        true,
		},
	}
}

// FromConfig creates token data from an existing config.
func FromConfig(cfg *config.Config) *Data {
	return &Data{
		EndpointURL:   cfg.Storage.EndpointURL,
		Bucket:        cfg.Storage.Bucket,
		KeyID:         cfg.Storage.KeyID,
		SecretKey:     cfg.Storage.SecretKey,
		Region:        cfg.Storage.Region,
		Prefix:        cfg.Storage.Prefix,
		EmulationPath: cfg.Sync.EmulationPath,
	}
}
