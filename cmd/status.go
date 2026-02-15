package cmd

import (
	"fmt"

	"github.com/jacobfgrant/emu-sync/internal/config"
	"github.com/jacobfgrant/emu-sync/internal/manifest"
	"github.com/jacobfgrant/emu-sync/internal/storage"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show differences between remote and local state",
	Long:  `Downloads the remote manifest and compares it against the local manifest to show what would change on the next sync.`,
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

		remoteData, err := client.DownloadManifest(cmd.Context())
		if err != nil {
			return fmt.Errorf("downloading remote manifest: %w", err)
		}

		remote, err := manifest.ParseJSON(remoteData)
		if err != nil {
			return fmt.Errorf("parsing remote manifest: %w", err)
		}

		localPath := config.DefaultLocalManifestPath()
		local, err := manifest.LoadJSON(localPath)
		if err != nil {
			local = manifest.New()
		}

		// Filter to configured sync dirs / exclude
		filtered := manifest.New()
		for key, entry := range remote.Files {
			if cfg.ShouldSync(key) {
				filtered.Files[key] = entry
			}
		}

		diff := manifest.Diff(filtered, local)

		if len(diff.Added) == 0 && len(diff.Modified) == 0 && len(diff.Deleted) == 0 {
			fmt.Println("Up to date.")
			return nil
		}

		if len(diff.Added) > 0 {
			fmt.Printf("New files (%d):\n", len(diff.Added))
			for _, f := range diff.Added {
				fmt.Printf("  + %s\n", f)
			}
		}
		if len(diff.Modified) > 0 {
			fmt.Printf("Modified files (%d):\n", len(diff.Modified))
			for _, f := range diff.Modified {
				fmt.Printf("  ~ %s\n", f)
			}
		}
		if len(diff.Deleted) > 0 {
			fmt.Printf("Deleted files (%d):\n", len(diff.Deleted))
			for _, f := range diff.Deleted {
				fmt.Printf("  - %s\n", f)
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
