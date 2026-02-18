package cmd

import (
	"fmt"

	"github.com/jacobfgrant/emu-sync/internal/config"
	"github.com/jacobfgrant/emu-sync/internal/ratelimit"
	"github.com/jacobfgrant/emu-sync/internal/storage"
	"github.com/jacobfgrant/emu-sync/internal/upload"
	"github.com/spf13/cobra"
)

var uploadSource string
var uploadDryRun bool
var uploadManifestOnly bool
var uploadWorkers int

var uploadCmd = &cobra.Command{
	Use:   "upload",
	Short: "Upload ROMs and BIOS files to the bucket",
	Long: `Walks the source directory, hashes all files, and uploads new or
changed files to the configured S3-compatible bucket. Generates an
updated manifest after upload.

Use --manifest-only to skip file uploads and just regenerate the
manifest from local files. Useful when another tool handles file
uploads and you just need to update the manifest.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgPath := cfgFile
		if cfgPath == "" {
			cfgPath = config.DefaultConfigPath()
		}

		cfg, err := config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		source := uploadSource
		if source == "" {
			source = cfg.Sync.EmulationPath
		}

		if err := config.ValidatePath(source); err != nil {
			return fmt.Errorf("source directory: %w", err)
		}

		workers := uploadWorkers
		if !cmd.Flags().Changed("workers") && cfg.Sync.Workers > 0 {
			workers = cfg.Sync.Workers
		}

		maxRetries := cfg.Sync.MaxRetries
		if maxRetries == 0 {
			maxRetries = 3
		}

		client := storage.NewClient(&cfg.Storage)

		if cfg.Sync.BandwidthLimit != "" {
			bps, err := config.ParseBandwidthLimit(cfg.Sync.BandwidthLimit)
			if err != nil {
				return fmt.Errorf("parsing bandwidth_limit: %w", err)
			}
			if bps > 0 {
				client.SetLimiter(ratelimit.NewLimiter(bps))
			}
		}

		// Save a local manifest when uploading from the emulation path
		// so a subsequent sync knows these files are already present.
		localManifestPath := ""
		if source == cfg.Sync.EmulationPath {
			localManifestPath = config.DefaultLocalManifestPath()
		}

		result, err := upload.Run(cmd.Context(), client, upload.Options{
			SourcePath:        source,
			SyncDirs:          cfg.Sync.SyncDirs,
			DryRun:            uploadDryRun,
			Verbose:           verbose,
			ManifestOnly:      uploadManifestOnly,
			Workers:           workers,
			MaxRetries:        maxRetries,
			SkipDotfiles:      *cfg.Sync.SkipDotfiles,
			LocalManifestPath: localManifestPath,
		})
		if err != nil {
			return err
		}

		fmt.Print(result.Summary())
		return nil
	},
}

func init() {
	uploadCmd.Flags().StringVar(&uploadSource, "source", "", "source directory (defaults to config emulation_path)")
	uploadCmd.Flags().BoolVar(&uploadDryRun, "dry-run", false, "show what would be uploaded without uploading")
	uploadCmd.Flags().BoolVar(&uploadManifestOnly, "manifest-only", false, "regenerate and upload manifest without uploading files")
	uploadCmd.Flags().IntVar(&uploadWorkers, "workers", 1, "number of parallel uploads (1 = sequential)")
	rootCmd.AddCommand(uploadCmd)
}
