package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jacobfgrant/emu-sync/internal/config"
	"github.com/jacobfgrant/emu-sync/internal/manifest"
)

// VerifyResult summarizes a verification run.
type VerifyResult struct {
	OK       []string // files that match the manifest
	Mismatch []string // files with wrong hash or size
	Missing  []string // files in manifest but not on disk
	Errors   []error
}

// Verify re-hashes local files against the local manifest and reports
// any that don't match. Mismatched entries are removed from the local
// manifest so the next sync re-downloads them.
func Verify(cfg *config.Config, localManifestPath string, verbose bool) (*VerifyResult, error) {
	if localManifestPath == "" {
		localManifestPath = config.DefaultLocalManifestPath()
	}

	local, err := manifest.LoadJSON(localManifestPath)
	if err != nil {
		return nil, fmt.Errorf("loading local manifest: %w", err)
	}

	result := &VerifyResult{}
	var toRemove []string

	for key, entry := range local.Files {
		localPath := filepath.Join(cfg.Sync.EmulationPath, filepath.FromSlash(key))

		info, err := os.Stat(localPath)
		if os.IsNotExist(err) {
			result.Missing = append(result.Missing, key)
			toRemove = append(toRemove, key)
			continue
		}
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("stat %s: %w", key, err))
			continue
		}

		if info.Size() != entry.Size {
			result.Mismatch = append(result.Mismatch, key)
			toRemove = append(toRemove, key)
			continue
		}

		hash, err := manifest.HashFile(localPath)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("hashing %s: %w", key, err))
			continue
		}

		if hash != entry.MD5 {
			result.Mismatch = append(result.Mismatch, key)
			toRemove = append(toRemove, key)
			continue
		}

		result.OK = append(result.OK, key)
	}

	// Remove mismatched/missing entries so next sync re-downloads them
	if len(toRemove) > 0 {
		for _, key := range toRemove {
			delete(local.Files, key)
		}
		if err := local.SaveJSON(localManifestPath); err != nil {
			return result, fmt.Errorf("saving updated manifest: %w", err)
		}
	}

	return result, nil
}

// Summary returns a human-readable summary of the verification.
func (r *VerifyResult) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Verified: %d files OK\n", len(r.OK))
	if len(r.Mismatch) > 0 {
		fmt.Fprintf(&b, "Mismatched: %d files (will re-download on next sync)\n", len(r.Mismatch))
		for _, f := range r.Mismatch {
			fmt.Fprintf(&b, "  ~ %s\n", f)
		}
	}
	if len(r.Missing) > 0 {
		fmt.Fprintf(&b, "Missing: %d files (will re-download on next sync)\n", len(r.Missing))
		for _, f := range r.Missing {
			fmt.Fprintf(&b, "  - %s\n", f)
		}
	}
	if len(r.Errors) > 0 {
		fmt.Fprintf(&b, "Errors: %d\n", len(r.Errors))
		for _, err := range r.Errors {
			fmt.Fprintf(&b, "  ! %v\n", err)
		}
	}
	if len(r.Mismatch) == 0 && len(r.Missing) == 0 && len(r.Errors) == 0 {
		fmt.Fprintln(&b, "All files match the manifest.")
	}
	return b.String()
}
