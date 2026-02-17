package sync

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	gosync "sync"
	"syscall"

	"github.com/jacobfgrant/emu-sync/internal/config"
	"github.com/jacobfgrant/emu-sync/internal/manifest"
	"github.com/jacobfgrant/emu-sync/internal/progress"
	"github.com/jacobfgrant/emu-sync/internal/retry"
	"github.com/jacobfgrant/emu-sync/internal/storage"
)

const tmpSuffix = ".emu-sync-tmp"

func acquireLock() (*os.File, error) {
	lockDir := filepath.Dir(config.DefaultLocalManifestPath())
	os.MkdirAll(lockDir, 0o755)
	f, err := os.OpenFile(
		filepath.Join(lockDir, "sync.lock"),
		os.O_CREATE|os.O_RDWR, 0o644,
	)
	if err != nil {
		return nil, fmt.Errorf("opening lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another sync is already running")
	}
	return f, nil
}

func releaseLock(f *os.File) {
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	f.Close()
}

// Options controls sync behavior.
type Options struct {
	DryRun            bool
	NoDelete          bool
	Verbose           bool
	Workers           int                // number of parallel downloads; 0 or 1 = sequential
	MaxRetries        int                // per-file retries with backoff; 0 = no retries
	SaveThreshold     int64              // bytes downloaded before mid-sync manifest save; 0 = default (50 MB)
	Progress          *progress.Reporter // emits JSON progress events; nil = no-op
	LocalManifestPath string             // overrides default; used by tests
}

// Result summarizes what a sync run did.
type Result struct {
	Downloaded []string
	Deleted    []string
	Retained   []string // deselected files kept on disk (delete disabled)
	Skipped    int
	Errors     []error
}

// downloadResult is sent back from worker goroutines.
type downloadResult struct {
	key   string
	entry manifest.FileEntry
	err   error
}

// Run downloads the remote manifest, diffs against local, and syncs files.
func Run(ctx context.Context, client storage.Backend, cfg *config.Config, opts Options) (*Result, error) {
	if !opts.DryRun {
		lock, err := acquireLock()
		if err != nil {
			return nil, err
		}
		defer releaseLock(lock)
	}

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

	// Filter remote manifest to configured sync_dirs / sync_exclude
	filteredRemote := manifest.New()
	filteredRemote.GeneratedAt = remote.GeneratedAt
	for key, entry := range remote.Files {
		if cfg.ShouldSync(key) {
			filteredRemote.Files[key] = entry
		}
	}

	diff := manifest.Diff(filteredRemote, local)

	// Check for files that the local manifest says exist but are
	// missing from disk (e.g., accidentally deleted by the user).
	// Build a set of keys already queued for download (Added + Modified)
	// to avoid duplicates.
	queued := make(map[string]bool, len(diff.Added)+len(diff.Modified))
	for _, key := range diff.Added {
		queued[key] = true
	}
	for _, key := range diff.Modified {
		queued[key] = true
	}
	for key := range filteredRemote.Files {
		if queued[key] {
			continue // already scheduled for download
		}
		if _, inLocal := local.Files[key]; !inLocal {
			continue // not in local manifest, already in diff.Added
		}
		localPath := filepath.Join(cfg.Sync.EmulationPath, filepath.FromSlash(key))
		if _, err := os.Stat(localPath); os.IsNotExist(err) {
			if opts.Verbose {
				log.Printf("file missing from disk, will re-download: %s", key)
			}
			diff.Added = append(diff.Added, key)
			delete(local.Files, key)
		}
	}

	// Clean up any leftover temp files from interrupted syncs
	if !opts.DryRun {
		cleanTempFiles(cfg.Sync.EmulationPath, opts.Verbose)
	}

	// Resolve save threshold (default 50 MB)
	threshold := opts.SaveThreshold
	if threshold <= 0 {
		threshold = 50 * 1024 * 1024
	}

	// Download new and modified files
	toDownload := append(diff.Added, diff.Modified...)

	if opts.DryRun {
		for _, key := range toDownload {
			fmt.Printf("would download: %s\n", key)
			result.Downloaded = append(result.Downloaded, key)
		}
	} else if opts.Workers > 1 && len(toDownload) > 1 {
		downloadParallel(ctx, client, cfg, filteredRemote, toDownload, opts, result, local, localManifestPath, threshold)
	} else {
		downloadSequential(ctx, client, cfg, filteredRemote, toDownload, opts, result, local, localManifestPath, threshold)
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
			result.Retained = append(result.Retained, key)
			if opts.Progress != nil {
				opts.Progress.Retain(key)
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
		if opts.Progress != nil {
			opts.Progress.Delete(key)
		}
	}

	result.Skipped = len(filteredRemote.Files) - len(toDownload)

	if opts.Progress != nil {
		opts.Progress.Done(len(result.Downloaded), len(result.Deleted), len(result.Retained), len(result.Errors), result.Skipped)
	}

	// Save updated local manifest
	if !opts.DryRun {
		if err := local.SaveJSON(localManifestPath); err != nil {
			return result, fmt.Errorf("saving local manifest: %w", err)
		}
	}

	return result, nil
}

func downloadSequential(ctx context.Context, client storage.Backend, cfg *config.Config, filteredRemote *manifest.Manifest, keys []string, opts Options, result *Result, local *manifest.Manifest, localManifestPath string, saveThreshold int64) {
	prog := opts.Progress
	maxRetries := opts.MaxRetries
	var unsavedBytes int64
	for _, key := range keys {
		entry := filteredRemote.Files[key]
		if prog != nil {
			prog.Start(key, entry.Size)
		}
		err := retry.WithBackoff(ctx, maxRetries, func() error {
			return downloadOne(ctx, client, cfg.Sync.EmulationPath, key, opts.Verbose)
		})
		if err != nil {
			result.Errors = append(result.Errors, err)
			if prog != nil {
				prog.FileError(key, err)
			}
			continue
		}
		local.Files[key] = entry
		result.Downloaded = append(result.Downloaded, key)
		if prog != nil {
			prog.Complete(key)
		}
		unsavedBytes += entry.Size
		if unsavedBytes >= saveThreshold {
			if err := local.SaveJSON(localManifestPath); err != nil {
				if opts.Verbose {
					log.Printf("warning: mid-sync manifest save: %v", err)
				}
			}
			unsavedBytes = 0
		}
	}
}

func downloadParallel(ctx context.Context, client storage.Backend, cfg *config.Config, filteredRemote *manifest.Manifest, keys []string, opts Options, result *Result, local *manifest.Manifest, localManifestPath string, saveThreshold int64) {
	// Channel for sending keys to workers
	jobs := make(chan string, len(keys))
	// Channel for collecting results from workers
	results := make(chan downloadResult, len(keys))

	maxRetries := opts.MaxRetries

	// Start worker goroutines
	var wg gosync.WaitGroup
	for i := 0; i < opts.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for key := range jobs {
				entry := filteredRemote.Files[key]
				if opts.Progress != nil {
					opts.Progress.Start(key, entry.Size)
				}
				err := retry.WithBackoff(ctx, maxRetries, func() error {
					return downloadOne(ctx, client, cfg.Sync.EmulationPath, key, opts.Verbose)
				})
				results <- downloadResult{
					key:   key,
					entry: entry,
					err:   err,
				}
			}
		}()
	}

	// Send all keys to the jobs channel, then close it
	for _, key := range keys {
		jobs <- key
	}
	close(jobs)

	// Wait for all workers to finish, then close results
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	prog := opts.Progress
	var unsavedBytes int64
	for dr := range results {
		if dr.err != nil {
			result.Errors = append(result.Errors, dr.err)
			if prog != nil {
				prog.FileError(dr.key, dr.err)
			}
			continue
		}
		local.Files[dr.key] = dr.entry
		result.Downloaded = append(result.Downloaded, dr.key)
		if prog != nil {
			prog.Complete(dr.key)
		}
		unsavedBytes += dr.entry.Size
		if unsavedBytes >= saveThreshold {
			if err := local.SaveJSON(localManifestPath); err != nil {
				if opts.Verbose {
					log.Printf("warning: mid-sync manifest save: %v", err)
				}
			}
			unsavedBytes = 0
		}
	}
}

// downloadOne downloads a single file atomically.
func downloadOne(ctx context.Context, client storage.Backend, emuPath, key string, verbose bool) error {
	localPath := filepath.Join(emuPath, filepath.FromSlash(key))
	tmpPath := localPath + tmpSuffix

	if verbose {
		log.Printf("downloading: %s", key)
	}

	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return fmt.Errorf("mkdir for %s: %w", key, err)
	}

	if err := client.DownloadFile(ctx, key, tmpPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("download %s: %w", key, err)
	}

	if err := os.Rename(tmpPath, localPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename %s: %w", key, err)
	}

	return nil
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
	if len(r.Retained) > 0 {
		fmt.Fprintf(&b, "Retained: %d files (deselected, delete disabled)\n", len(r.Retained))
	}
	fmt.Fprintf(&b, "Unchanged: %d files\n", r.Skipped)
	if len(r.Errors) > 0 {
		fmt.Fprintf(&b, "Errors: %d\n", len(r.Errors))
		for _, err := range r.Errors {
			fmt.Fprintf(&b, "  - %v\n", err)
		}
	}
	fmt.Fprintf(&b, "Total: %d files\n", len(r.Downloaded)+r.Skipped)
	return b.String()
}
