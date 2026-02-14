package cmd

import (
	"fmt"

	"github.com/jacobfgrant/emu-sync/internal/config"
	"github.com/jacobfgrant/emu-sync/internal/storage"
	intsync "github.com/jacobfgrant/emu-sync/internal/sync"
	"github.com/spf13/cobra"
)

var syncDryRun bool
var syncNoDelete bool
var syncWorkers int

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

		client := storage.NewClient(&cfg.Storage)
		opts := intsync.Options{
			DryRun:   syncDryRun,
			NoDelete: syncNoDelete,
			Verbose:  verbose,
			Workers:  syncWorkers,
		}
		result, err := intsync.Run(cmd.Context(), client, cfg, opts)
		if err != nil {
			return err
		}

		fmt.Print(result.Summary())
		return nil
	},
}

func init() {
	syncCmd.Flags().BoolVar(&syncDryRun, "dry-run", false, "show what would change without downloading")
	syncCmd.Flags().BoolVar(&syncNoDelete, "no-delete", false, "don't delete files removed from bucket")
	syncCmd.Flags().IntVar(&syncWorkers, "workers", 1, "number of parallel downloads (1 = sequential)")
	rootCmd.AddCommand(syncCmd)
}
