package upload

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jacobfgrant/emu-sync/internal/config"
	"github.com/jacobfgrant/emu-sync/internal/manifest"
	"github.com/jacobfgrant/emu-sync/internal/retry"
	"github.com/jacobfgrant/emu-sync/internal/storage"
)

// Options controls upload behavior.
type Options struct {
	SourcePath        string
	SyncDirs          []string
	DryRun            bool
	Verbose           bool
	ManifestOnly      bool
	Workers           int    // number of parallel uploads; 0 or 1 = sequential
	MaxRetries        int    // per-file retries with backoff; 0 = no retries
	SkipDotfiles      bool   // skip files and directories starting with "."
	CachePath         string // overrides default upload cache path; used by tests
	LocalManifestPath string // if set, save the manifest locally after successful upload
}

// Result summarizes what an upload run did.
type Result struct {
	Uploaded  []string
	Skipped   int
	Deleted   []string
	Errors    []error
	CacheHits int
}

// uploadResult is sent back from worker goroutines.
type uploadResult struct {
	key string
	err error
}

// Run walks the source directory, computes hashes, uploads changed files,
// and writes a new manifest to the bucket.
func Run(ctx context.Context, client storage.Backend, opts Options) (*Result, error) {
	if err := config.ValidatePath(opts.SourcePath); err != nil {
		return nil, fmt.Errorf("source path: %w", err)
	}

	result := &Result{}

	cachePath := opts.CachePath
	if cachePath == "" {
		cachePath = config.DefaultUploadCachePath()
	}

	// Load hash cache for skipping unchanged files
	cache := loadHashCache(cachePath)

	// Build a new manifest from local files
	log.Printf("Scanning local files...")
	scan, err := buildManifest(opts.SourcePath, opts.SyncDirs, opts.SkipDotfiles, opts.Verbose, cache)
	if err != nil {
		return nil, fmt.Errorf("scanning source files (nothing was uploaded or deleted): %w", err)
	}
	newManifest := scan.manifest
	result.CacheHits = scan.cacheHits
	if scan.cacheHits > 0 {
		log.Printf("Found %d files (%d cached)", len(newManifest.Files), scan.cacheHits)
	} else {
		log.Printf("Found %d files", len(newManifest.Files))
	}

	// Save hash cache early so interrupted uploads don't lose hashes
	if !opts.DryRun {
		saveCache(cache, cachePath, newManifest, opts.Verbose)
	}

	// Download existing remote manifest for diffing
	var oldManifest *manifest.Manifest
	remoteData, err := client.DownloadManifest(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		if opts.Verbose {
			log.Printf("no existing remote manifest, assuming first upload")
		}
		oldManifest = manifest.New()
	} else if err != nil {
		return nil, fmt.Errorf("downloading remote manifest: %w", err)
	} else {
		oldManifest, err = manifest.ParseJSON(remoteData)
		if err != nil {
			return nil, fmt.Errorf("parsing remote manifest: %w", err)
		}
	}

	if err := checkUnscanned(scan.unscanned, oldManifest); err != nil {
		return nil, err
	}

	if opts.ManifestOnly {
		result.Skipped = len(newManifest.Files)
		if !opts.DryRun {
			if err := publishManifest(ctx, client, newManifest, opts); err != nil {
				return nil, err
			}
		}
		return result, nil
	}

	diff := manifest.Diff(newManifest, oldManifest)

	// Upload new and modified files
	toUpload := append(diff.Added, diff.Modified...)

	var failed []string
	if opts.DryRun {
		for _, key := range toUpload {
			fmt.Printf("would upload: %s\n", key)
			result.Uploaded = append(result.Uploaded, key)
		}
	} else if opts.Workers > 1 && len(toUpload) > 1 {
		failed = uploadParallel(ctx, client, opts, toUpload, result)
	} else {
		failed = uploadSequential(ctx, client, opts, toUpload, result)
	}

	// Delete remote files that no longer exist locally
	for _, key := range diff.Deleted {
		if opts.DryRun {
			fmt.Printf("would delete from bucket: %s\n", key)
		} else {
			if opts.Verbose {
				log.Printf("deleting from bucket: %s", key)
			}
			if err := client.DeleteObject(ctx, key); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("delete %s: %w", key, err))
				failed = append(failed, key)
				continue
			}
		}
		result.Deleted = append(result.Deleted, key)
	}

	result.Skipped = len(newManifest.Files) - len(toUpload)

	// Upload the new manifest and save cache
	if !opts.DryRun {
		saveCache(cache, cachePath, newManifest, opts.Verbose)
		published := revertFailed(newManifest, oldManifest, failed)
		if err := publishManifest(ctx, client, published, opts); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// checkUnscanned refuses to continue if the bucket holds files under a path
// that was not scanned, since publishing would delete them from the bucket
// and then from every device.
func checkUnscanned(unscanned []skippedPath, old *manifest.Manifest) error {
	var blocked []string
	for _, sp := range unscanned {
		count := 0
		for key := range old.Files {
			if key == sp.key || strings.HasPrefix(key, sp.key+"/") {
				count++
			}
		}
		if count > 0 {
			blocked = append(blocked, fmt.Sprintf("  %s (%s): %d files in bucket", sp.key, sp.reason, count))
		}
	}
	if len(blocked) == 0 {
		return nil
	}
	return fmt.Errorf("refusing to upload: these paths were not scanned but have files in the bucket, "+
		"which would be deleted from the bucket and from every device:\n%s\n"+
		"Mount or restore them, or remove them from sync_dirs to delete them intentionally. Nothing was changed.",
		strings.Join(blocked, "\n"))
}

// revertFailed returns a copy of scanned in which each failed key is reset
// to its old entry (or dropped if it was new), so the published manifest
// matches what the bucket actually holds and the next run retries it.
func revertFailed(scanned, old *manifest.Manifest, failed []string) *manifest.Manifest {
	m := &manifest.Manifest{
		Version:     scanned.Version,
		GeneratedAt: scanned.GeneratedAt,
		Files:       maps.Clone(scanned.Files),
	}
	for _, key := range failed {
		if entry, ok := old.Files[key]; ok {
			m.Files[key] = entry
		} else {
			delete(m.Files, key)
		}
	}
	return m
}

// publishManifest uploads the manifest to the bucket and saves the local copy.
func publishManifest(ctx context.Context, client storage.Backend, m *manifest.Manifest, opts Options) error {
	manifestData, err := m.ToJSON()
	if err != nil {
		return fmt.Errorf("serializing manifest: %w", err)
	}
	if err := client.UploadManifest(ctx, manifestData); err != nil {
		return fmt.Errorf("uploading manifest: %w", err)
	}
	return saveLocalManifest(m, opts)
}

func saveLocalManifest(m *manifest.Manifest, opts Options) error {
	if opts.LocalManifestPath == "" {
		return nil
	}
	if opts.Verbose {
		log.Printf("saving local manifest to %s", opts.LocalManifestPath)
	}
	if err := m.SaveJSON(opts.LocalManifestPath); err != nil {
		return fmt.Errorf("saving local manifest: %w", err)
	}
	return nil
}

// saveCache prunes the cache to only keys in the manifest and writes it to disk.
func saveCache(cache *hashCache, path string, m *manifest.Manifest, verbose bool) {
	validKeys := make(map[string]struct{}, len(m.Files))
	for key := range m.Files {
		validKeys[key] = struct{}{}
	}
	cache.prune(validKeys)
	if err := cache.save(path); err != nil && verbose {
		log.Printf("warning: failed to save upload cache: %v", err)
	}
}

// uploadSequential uploads keys one at a time and returns the keys that failed.
func uploadSequential(ctx context.Context, client storage.Backend, opts Options, keys []string, result *Result) []string {
	var failed []string
	for _, key := range keys {
		localPath := filepath.Join(opts.SourcePath, filepath.FromSlash(key))
		if opts.Verbose {
			log.Printf("uploading: %s", key)
		}
		err := retry.WithBackoff(ctx, opts.MaxRetries, func() error {
			return client.UploadFile(ctx, key, localPath)
		})
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("upload %s: %w", key, err))
			failed = append(failed, key)
			continue
		}
		result.Uploaded = append(result.Uploaded, key)
	}
	return failed
}

// uploadParallel uploads keys with a worker pool and returns the keys that failed.
func uploadParallel(ctx context.Context, client storage.Backend, opts Options, keys []string, result *Result) []string {
	jobs := make(chan string, len(keys))
	results := make(chan uploadResult, len(keys))

	var wg sync.WaitGroup
	for i := 0; i < opts.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for key := range jobs {
				localPath := filepath.Join(opts.SourcePath, filepath.FromSlash(key))
				if opts.Verbose {
					log.Printf("uploading: %s", key)
				}
				err := retry.WithBackoff(ctx, opts.MaxRetries, func() error {
					return client.UploadFile(ctx, key, localPath)
				})
				results <- uploadResult{key: key, err: err}
			}
		}()
	}

	for _, key := range keys {
		jobs <- key
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	var failed []string
	for ur := range results {
		if ur.err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("upload %s: %w", ur.key, ur.err))
			failed = append(failed, ur.key)
			continue
		}
		result.Uploaded = append(result.Uploaded, ur.key)
	}
	return failed
}

// skippedPath is a manifest key prefix that was deliberately not scanned.
type skippedPath struct {
	key    string
	reason string
}

// scanResult is the output of buildManifest.
type scanResult struct {
	manifest  *manifest.Manifest
	cacheHits int
	unscanned []skippedPath
}

// buildManifest walks each sync dir under the source path and hashes all
// files. When cache is non-nil, files with matching mtime+size reuse the
// cached hash. Any read error aborts the scan, because a partial manifest
// would delete the unread files from the bucket. Missing sync dirs,
// directory symlinks, and non-regular files are skipped and reported in
// unscanned so the caller can refuse to delete what they used to hold.
func buildManifest(sourcePath string, syncDirs []string, skipDotfiles bool, verbose bool, cache *hashCache) (*scanResult, error) {
	scan := &scanResult{manifest: manifest.New()}
	for _, dir := range syncDirs {
		dirKey := filepath.ToSlash(filepath.Clean(dir))
		if err := manifest.ValidateKey(dirKey); err != nil {
			return nil, fmt.Errorf("sync dir %q: %w", dir, err)
		}

		// Resolve symlinks so a sync dir that is itself a link (e.g.
		// roms -> /mnt/storage/roms) is walked. Keys are built from the
		// configured dir name, not the resolved path.
		root, err := filepath.EvalSymlinks(filepath.Join(sourcePath, filepath.FromSlash(dirKey)))
		if errors.Is(err, fs.ErrNotExist) {
			log.Printf("warning: skipping sync dir %s: does not exist", dir)
			scan.unscanned = append(scan.unscanned, skippedPath{dirKey, "does not exist"})
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("resolving %s: %w", dir, err)
		}

		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			relPath, err := filepath.Rel(root, path)
			if err != nil {
				return fmt.Errorf("computing relative path for %s: %w", path, err)
			}
			key := dirKey
			if relPath != "." {
				key = dirKey + "/" + filepath.ToSlash(relPath)
			}

			if path != root && skipDotfiles && strings.HasPrefix(d.Name(), ".") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}

			// Stat follows symlinks, so a file symlink records its target's size.
			info, err := os.Stat(path)
			if err != nil {
				if d.Type()&fs.ModeSymlink != 0 {
					log.Printf("warning: skipping broken symlink %s", key)
					scan.unscanned = append(scan.unscanned, skippedPath{key, "broken symlink"})
					return nil
				}
				return fmt.Errorf("stat %s: %w", path, err)
			}
			if info.IsDir() {
				log.Printf("warning: skipping directory symlink %s", key)
				scan.unscanned = append(scan.unscanned, skippedPath{key, "directory symlink"})
				return nil
			}
			if !info.Mode().IsRegular() {
				log.Printf("warning: skipping non-regular file %s", key)
				scan.unscanned = append(scan.unscanned, skippedPath{key, "not a regular file"})
				return nil
			}
			if err := manifest.ValidateKey(key); err != nil {
				return err
			}

			var hash string
			if cache != nil {
				if cached, ok := cache.lookup(key, info.Size(), info.ModTime()); ok {
					hash = cached
					scan.cacheHits++
					if verbose {
						log.Printf("cached: %s", key)
					}
				}
			}
			if hash == "" {
				if verbose {
					log.Printf("hashing: %s", key)
				}
				hash, err = manifest.HashFile(path)
				if err != nil {
					return fmt.Errorf("hashing %s: %w", path, err)
				}
				if cache != nil {
					cache.update(key, info.Size(), info.ModTime(), hash)
				}
			}

			scan.manifest.Files[key] = manifest.FileEntry{
				Size: info.Size(),
				MD5:  hash,
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return scan, nil
}

// Summary returns a human-readable summary of the upload result.
func (r *Result) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Uploaded: %d files\n", len(r.Uploaded))
	fmt.Fprintf(&b, "Skipped (unchanged): %d files\n", r.Skipped)
	fmt.Fprintf(&b, "Deleted from bucket: %d files\n", len(r.Deleted))
	if r.CacheHits > 0 {
		fmt.Fprintf(&b, "Hash cache hits: %d files\n", r.CacheHits)
	}
	if len(r.Errors) > 0 {
		fmt.Fprintf(&b, "Errors: %d\n", len(r.Errors))
		for _, err := range r.Errors {
			fmt.Fprintf(&b, "  - %v\n", err)
		}
	}
	fmt.Fprintf(&b, "Total: %d files\n", len(r.Uploaded)+r.Skipped)
	return b.String()
}
