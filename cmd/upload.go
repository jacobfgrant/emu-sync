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
var uploadManifestOnly bool

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

		client := storage.NewClient(&cfg.Storage)
		result, err := upload.Run(cmd.Context(), client, source, cfg.Sync.SyncDirs, uploadDryRun, verbose, uploadManifestOnly)
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
	rootCmd.AddCommand(uploadCmd)
}
