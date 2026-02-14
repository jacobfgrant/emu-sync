package sync

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/jacobfgrant/emu-sync/internal/config"
	"github.com/jacobfgrant/emu-sync/internal/manifest"
	"github.com/jacobfgrant/emu-sync/internal/storage"
)

const tmpSuffix = ".emu-sync-tmp"

// Options controls sync behavior.
type Options struct {
	DryRun            bool
	NoDelete          bool
	Verbose           bool
	LocalManifestPath string // overrides default; used by tests
}

// Result summarizes what a sync run did.
type Result struct {
	Downloaded []string
	Deleted    []string
	Skipped    int
	Errors     []error
}

// Run downloads the remote manifest, diffs against local, and syncs files.
func Run(ctx context.Context, client storage.Backend, cfg *config.Config, opts Options) (*Result, error) {
	result := &Result{}

	// Download remote manifest
	remoteData, err := client.DownloadManifest(ctx)
	if err != nil {
		return nil, fmt.Errorf("downloading remote manifest: %w", err)
	}

	remote, err := manifest.ParseJSON(remoteData)
	if err != nil {
		return nil, fmt.Errorf("parsing remote manifest: %w", err)
	}

	// Load local manifest (or start empty)
	localManifestPath := opts.LocalManifestPath
	if localManifestPath == "" {
		localManifestPath = config.DefaultLocalManifestPath()
	}
	local, err := manifest.LoadJSON(localManifestPath)
	if err != nil {
		if opts.Verbose {
			log.Printf("no local manifest found, treating as first sync: %v", err)
		}
		local = manifest.New()
	}

	// Filter remote manifest to only configured sync_dirs
	filteredRemote := manifest.New()
	filteredRemote.GeneratedAt = remote.GeneratedAt
	for key, entry := range remote.Files {
		for _, dir := range cfg.Sync.SyncDirs {
			if strings.HasPrefix(key, dir+"/") {
				filteredRemote.Files[key] = entry
				break
			}
		}
	}

	diff := manifest.Diff(filteredRemote, local)

	// Clean up any leftover temp files from interrupted syncs
	if !opts.DryRun {
		cleanTempFiles(cfg.Sync.EmulationPath, opts.Verbose)
	}

	// Download new and modified files
	toDownload := append(diff.Added, diff.Modified...)
	for _, key := range toDownload {
		localPath := filepath.Join(cfg.Sync.EmulationPath, filepath.FromSlash(key))
		tmpPath := localPath + tmpSuffix

		if opts.DryRun {
			fmt.Printf("would download: %s\n", key)
			result.Downloaded = append(result.Downloaded, key)
			continue
		}

		if opts.Verbose {
			log.Printf("downloading: %s", key)
		}

		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("mkdir for %s: %w", key, err))
			continue
		}

		// Atomic download: write to temp file, then rename
		if err := client.DownloadFile(ctx, key, tmpPath); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("download %s: %w", key, err))
			os.Remove(tmpPath)
			continue
		}

		if err := os.Rename(tmpPath, localPath); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("rename %s: %w", key, err))
			os.Remove(tmpPath)
			continue
		}

		// Update local manifest entry only after successful rename
		local.Files[key] = filteredRemote.Files[key]
		result.Downloaded = append(result.Downloaded, key)
	}

	// Delete local files removed from remote
	deleteAllowed := cfg.Sync.Delete && !opts.NoDelete
	for _, key := range diff.Deleted {
		localPath := filepath.Join(cfg.Sync.EmulationPath, filepath.FromSlash(key))

		if opts.DryRun {
			if deleteAllowed {
				fmt.Printf("would delete: %s\n", key)
			} else {
				fmt.Printf("would delete (skipped, delete disabled): %s\n", key)
			}
			result.Deleted = append(result.Deleted, key)
			continue
		}

		if !deleteAllowed {
			if opts.Verbose {
				log.Printf("skipping delete (disabled): %s", key)
			}
			continue
		}

		if opts.Verbose {
			log.Printf("deleting: %s", key)
		}

		if err := os.Remove(localPath); err != nil && !os.IsNotExist(err) {
			result.Errors = append(result.Errors, fmt.Errorf("delete %s: %w", key, err))
			continue
		}

		delete(local.Files, key)
		result.Deleted = append(result.Deleted, key)
	}

	result.Skipped = len(filteredRemote.Files) - len(toDownload)

	// Save updated local manifest
	if !opts.DryRun {
		if err := local.SaveJSON(localManifestPath); err != nil {
			return result, fmt.Errorf("saving local manifest: %w", err)
		}
	}

	return result, nil
}

// cleanTempFiles removes leftover .emu-sync-tmp files from interrupted syncs.
func cleanTempFiles(basePath string, verbose bool) {
	filepath.WalkDir(basePath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(path, tmpSuffix) {
			if verbose {
				log.Printf("cleaning up temp file: %s", path)
			}
			os.Remove(path)
		}
		return nil
	})
}

// Summary returns a human-readable summary of the sync result.
func (r *Result) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Downloaded: %d files\n", len(r.Downloaded))
	fmt.Fprintf(&b, "Deleted: %d files\n", len(r.Deleted))
	fmt.Fprintf(&b, "Unchanged: %d files\n", r.Skipped)
	if len(r.Errors) > 0 {
		fmt.Fprintf(&b, "Errors: %d\n", len(r.Errors))
		for _, err := range r.Errors {
			fmt.Fprintf(&b, "  - %v\n", err)
		}
	}
	return b.String()
}
