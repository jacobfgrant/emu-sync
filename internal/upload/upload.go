package upload

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/jacobfgrant/emu-sync/internal/manifest"
	"github.com/jacobfgrant/emu-sync/internal/storage"
)

// Result summarizes what an upload run did.
type Result struct {
	Uploaded []string
	Skipped  int
	Deleted  []string
	Errors   []error
}

// Run walks the source directory, computes hashes, uploads changed files,
// and writes a new manifest to the bucket.
func Run(ctx context.Context, client storage.Backend, sourcePath string, syncDirs []string, dryRun bool, verbose bool) (*Result, error) {
	result := &Result{}

	// Build a new manifest from local files
	newManifest := manifest.New()
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
				return nil
			}

			// Key is relative to sourcePath: "roms/snes/Game.sfc"
			relPath, err := filepath.Rel(sourcePath, path)
			if err != nil {
				return fmt.Errorf("computing relative path for %s: %w", path, err)
			}
			// Normalize to forward slashes for bucket keys
			key := filepath.ToSlash(relPath)

			info, err := d.Info()
			if err != nil {
				return fmt.Errorf("stat %s: %w", path, err)
			}

			hash, err := manifest.HashFile(path)
			if err != nil {
				return fmt.Errorf("hashing %s: %w", path, err)
			}

			newManifest.Files[key] = manifest.FileEntry{
				Size: info.Size(),
				MD5:  hash,
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walking %s: %w", dirPath, err)
		}
	}

	// Download existing remote manifest for diffing
	var oldManifest *manifest.Manifest
	remoteData, err := client.DownloadManifest(ctx)
	if err != nil {
		if verbose {
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
	for _, key := range toUpload {
		localPath := filepath.Join(sourcePath, filepath.FromSlash(key))
		if dryRun {
			fmt.Printf("would upload: %s\n", key)
		} else {
			if verbose {
				log.Printf("uploading: %s", key)
			}
			if err := client.UploadFile(ctx, key, localPath); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("upload %s: %w", key, err))
				continue
			}
		}
		result.Uploaded = append(result.Uploaded, key)
	}

	// Delete remote files that no longer exist locally
	for _, key := range diff.Deleted {
		if dryRun {
			fmt.Printf("would delete from bucket: %s\n", key)
		} else {
			if verbose {
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

	// Upload the new manifest
	if !dryRun {
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

// Summary returns a human-readable summary of the upload result.
func (r *Result) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Uploaded: %d files\n", len(r.Uploaded))
	fmt.Fprintf(&b, "Skipped (unchanged): %d files\n", r.Skipped)
	fmt.Fprintf(&b, "Deleted from bucket: %d files\n", len(r.Deleted))
	if len(r.Errors) > 0 {
		fmt.Fprintf(&b, "Errors: %d\n", len(r.Errors))
		for _, err := range r.Errors {
			fmt.Fprintf(&b, "  - %v\n", err)
		}
	}
	return b.String()
}
