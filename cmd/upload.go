package cmd

import (
	"fmt"

	"github.com/jacobfgrant/emu-sync/internal/config"
	"github.com/jacobfgrant/emu-sync/internal/storage"
	"github.com/jacobfgrant/emu-sync/internal/upload"
	"github.com/spf13/cobra"
)

var uploadSource string
var uploadDryRun bool

var uploadCmd = &cobra.Command{
	Use:   "upload",
	Short: "Upload ROMs and BIOS files to the bucket",
	Long: `Walks the source directory, hashes all files, and uploads new or
changed files to the configured S3-compatible bucket. Generates an
updated manifest after upload.`,
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

		client := storage.NewClient(&cfg.Storage)
		result, err := upload.Run(cmd.Context(), client, source, cfg.Sync.SyncDirs, uploadDryRun, verbose)
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
	rootCmd.AddCommand(uploadCmd)
}
