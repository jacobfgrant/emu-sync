package manifest

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// FileEntry holds metadata for a single file in the manifest.
type FileEntry struct {
	Size int64  `json:"size"`
	MD5  string `json:"md5"`
}

// Manifest represents the full file manifest stored in the bucket.
type Manifest struct {
	Version     int                  `json:"version"`
	GeneratedAt time.Time            `json:"generated_at"`
	Files       map[string]FileEntry `json:"files"`
}

// DiffResult describes what changed between a remote and local manifest.
type DiffResult struct {
	Added    []string // files in remote but not local
	Modified []string // files in both but different hash/size
	Deleted  []string // files in local but not remote
}

// New creates an empty manifest.
func New() *Manifest {
	return &Manifest{
		Version:     1,
		GeneratedAt: time.Now().UTC(),
		Files:       make(map[string]FileEntry),
	}
}

// LoadJSON reads a manifest from a JSON file on disk.
func LoadJSON(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}

	if m.Files == nil {
		m.Files = make(map[string]FileEntry)
	}

	return &m, nil
}

// ParseJSON parses a manifest from raw JSON bytes.
func ParseJSON(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}

	if m.Files == nil {
		m.Files = make(map[string]FileEntry)
	}

	return &m, nil
}

// SaveJSON writes the manifest to a JSON file on disk.
func (m *Manifest) SaveJSON(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating manifest directory: %w", err)
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("serializing manifest: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming manifest: %w", err)
	}

	return nil
}

// ToJSON serializes the manifest to JSON bytes.
func (m *Manifest) ToJSON() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// Diff compares a remote manifest against a local one and returns what changed.
func Diff(remote, local *Manifest) DiffResult {
	var result DiffResult

	for path, remoteEntry := range remote.Files {
		localEntry, exists := local.Files[path]
		if !exists {
			result.Added = append(result.Added, path)
		} else if localEntry.MD5 != remoteEntry.MD5 || localEntry.Size != remoteEntry.Size {
			result.Modified = append(result.Modified, path)
		}
	}

	for path := range local.Files {
		if _, exists := remote.Files[path]; !exists {
			result.Deleted = append(result.Deleted, path)
		}
	}

	return result
}

// HashFile computes the MD5 hex digest of a file.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening file for hashing: %w", err)
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hashing file: %w", err)
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// IsEmpty returns true if the manifest has no files.
func (m *Manifest) IsEmpty() bool {
	return len(m.Files) == 0
}
