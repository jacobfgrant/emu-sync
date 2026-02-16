package upload

import (
	"context"
	"fmt"
	"log"
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
	SourcePath   string
	SyncDirs     []string
	DryRun       bool
	Verbose      bool
	ManifestOnly bool
	Workers      int    // number of parallel uploads; 0 or 1 = sequential
	MaxRetries   int    // per-file retries with backoff; 0 = no retries
	SkipDotfiles bool   // skip files and directories starting with "."
	CachePath    string // overrides default upload cache path; used by tests
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
	result := &Result{}

	cachePath := opts.CachePath
	if cachePath == "" {
		cachePath = config.DefaultUploadCachePath()
	}

	// Load hash cache for skipping unchanged files
	cache := loadHashCache(cachePath)

	// Build a new manifest from local files
	log.Printf("Scanning local files...")
	newManifest, cacheHits := buildManifest(opts.SourcePath, opts.SyncDirs, opts.SkipDotfiles, opts.Verbose, cache)
	result.CacheHits = cacheHits
	if cacheHits > 0 {
		log.Printf("Found %d files (%d cached)", len(newManifest.Files), cacheHits)
	} else {
		log.Printf("Found %d files", len(newManifest.Files))
	}

	if opts.ManifestOnly {
		result.Skipped = len(newManifest.Files)
		if !opts.DryRun {
			saveCache(cache, cachePath, newManifest, opts.Verbose)
			manifestData, err := newManifest.ToJSON()
			if err != nil {
				return nil, fmt.Errorf("serializing manifest: %w", err)
			}
			if err := client.UploadManifest(ctx, manifestData); err != nil {
				return nil, fmt.Errorf("uploading manifest: %w", err)
			}
		}
		return result, nil
	}

	// Download existing remote manifest for diffing
	var oldManifest *manifest.Manifest
	remoteData, err := client.DownloadManifest(ctx)
	if err != nil {
		if opts.Verbose {
			log.Printf("no existing remote manifest, assuming first upload")
		}
		oldManifest = manifest.New()
	} else {
		oldManifest, err = manifest.ParseJSON(remoteData)
		if err != nil {
			return nil, fmt.Errorf("parsing remote manifest: %w", err)
		}
	}

	diff := manifest.Diff(newManifest, oldManifest)

	// Upload new and modified files
	toUpload := append(diff.Added, diff.Modified...)

	if opts.DryRun {
		for _, key := range toUpload {
			fmt.Printf("would upload: %s\n", key)
			result.Uploaded = append(result.Uploaded, key)
		}
	} else if opts.Workers > 1 && len(toUpload) > 1 {
		uploadParallel(ctx, client, opts, toUpload, result)
	} else {
		uploadSequential(ctx, client, opts, toUpload, result)
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
				continue
			}
		}
		result.Deleted = append(result.Deleted, key)
	}

	result.Skipped = len(newManifest.Files) - len(toUpload)

	// Upload the new manifest and save cache
	if !opts.DryRun {
		saveCache(cache, cachePath, newManifest, opts.Verbose)
		manifestData, err := newManifest.ToJSON()
		if err != nil {
			return nil, fmt.Errorf("serializing manifest: %w", err)
		}
		if err := client.UploadManifest(ctx, manifestData); err != nil {
			return nil, fmt.Errorf("uploading manifest: %w", err)
		}
	}

	return result, nil
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

func uploadSequential(ctx context.Context, client storage.Backend, opts Options, keys []string, result *Result) {
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
			continue
		}
		result.Uploaded = append(result.Uploaded, key)
	}
}

func uploadParallel(ctx context.Context, client storage.Backend, opts Options, keys []string, result *Result) {
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

	for ur := range results {
		if ur.err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("upload %s: %w", ur.key, ur.err))
			continue
		}
		result.Uploaded = append(result.Uploaded, ur.key)
	}
}

// buildManifest walks the source directory and hashes all files.
// When cache is non-nil, files with matching mtime+size reuse the cached hash.
// Returns the manifest and the number of cache hits.
func buildManifest(sourcePath string, syncDirs []string, skipDotfiles bool, verbose bool, cache *hashCache) (*manifest.Manifest, int) {
	m := manifest.New()
	cacheHits := 0
	for _, dir := range syncDirs {
		dirPath := filepath.Join(sourcePath, dir)
		if _, err := os.Stat(dirPath); os.IsNotExist(err) {
			if verbose {
				log.Printf("skipping %s: directory does not exist", dir)
			}
			continue
		}

		err := filepath.WalkDir(dirPath, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if skipDotfiles && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if skipDotfiles && strings.HasPrefix(d.Name(), ".") {
				return nil
			}

			relPath, err := filepath.Rel(sourcePath, path)
			if err != nil {
				return fmt.Errorf("computing relative path for %s: %w", path, err)
			}
			key := filepath.ToSlash(relPath)

			info, err := d.Info()
			if err != nil {
				return fmt.Errorf("stat %s: %w", path, err)
			}

			var hash string
			if cache != nil {
				if cached, ok := cache.lookup(key, info.Size(), info.ModTime()); ok {
					hash = cached
					cacheHits++
					if verbose {
						log.Printf("cached: %s", key)
					}
				}
			}
			if hash == "" {
				if verbose {
					log.Printf("hashing: %s", key)
				}
				var err error
				hash, err = manifest.HashFile(path)
				if err != nil {
					return fmt.Errorf("hashing %s: %w", path, err)
				}
				if cache != nil {
					cache.update(key, info.Size(), info.ModTime(), hash)
				}
			}

			m.Files[key] = manifest.FileEntry{
				Size: info.Size(),
				MD5:  hash,
			}
			return nil
		})
		if err != nil {
			if verbose {
				log.Printf("error walking %s: %v", dirPath, err)
			}
		}
	}
	return m, cacheHits
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
