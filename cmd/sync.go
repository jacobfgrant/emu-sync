package cmd

import (
	"fmt"

	"github.com/jacobfgrant/emu-sync/internal/config"
	"github.com/jacobfgrant/emu-sync/internal/progress"
	"github.com/jacobfgrant/emu-sync/internal/ratelimit"
	"github.com/jacobfgrant/emu-sync/internal/storage"
	intsync "github.com/jacobfgrant/emu-sync/internal/sync"
	"github.com/spf13/cobra"
)

var syncDryRun bool
var syncNoDelete bool
var syncWorkers int
var syncProgressJSON bool

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync files from the bucket to this device",
	Long: `Downloads the remote manifest, compares against local state, and
downloads new or changed files. Optionally deletes local files that
were removed from the bucket.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgPath := cfgFile
		if cfgPath == "" {
			cfgPath = config.DefaultConfigPath()
		}

		cfg, err := config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		if err := cfg.ValidateEmulationPath(); err != nil {
			return err
		}

		workers := syncWorkers
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

		opts := intsync.Options{
			DryRun:     syncDryRun,
			NoDelete:   syncNoDelete,
			Verbose:    verbose,
			Workers:    workers,
			MaxRetries: maxRetries,
		}

		if cfg.Sync.SaveThreshold != "" {
			bytes, err := config.ParseBandwidthLimit(cfg.Sync.SaveThreshold)
			if err != nil {
				return fmt.Errorf("parsing save_threshold: %w", err)
			}
			if bytes > 0 {
				opts.SaveThreshold = bytes
			}
		}

		if syncProgressJSON {
			opts.Progress = progress.NewReporter(true)
		}

		result, err := intsync.Run(cmd.Context(), client, cfg, opts)
		if err != nil {
			return err
		}

		if !syncProgressJSON {
			fmt.Print(result.Summary())
		}
		return nil
	},
}

func init() {
	syncCmd.Flags().BoolVar(&syncDryRun, "dry-run", false, "show what would change without downloading")
	syncCmd.Flags().BoolVar(&syncNoDelete, "no-delete", false, "don't delete files removed from bucket")
	syncCmd.Flags().IntVar(&syncWorkers, "workers", 1, "number of parallel downloads (1 = sequential)")
	syncCmd.Flags().BoolVar(&syncProgressJSON, "progress-json", false, "emit JSON progress events to stdout")
	rootCmd.AddCommand(syncCmd)
}
