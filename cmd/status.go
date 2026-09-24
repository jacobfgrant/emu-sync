package cmd

import (
	"fmt"

	"github.com/jacobfgrant/emu-sync/internal/config"
	"github.com/jacobfgrant/emu-sync/internal/manifest"
	"github.com/jacobfgrant/emu-sync/internal/storage"
	intsync "github.com/jacobfgrant/emu-sync/internal/sync"
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

		if err := cfg.ValidateEmulationPath(); err != nil {
			return err
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

		plan := intsync.NewPlan(remote, local, cfg)

		if len(plan.Added) == 0 && len(plan.Modified) == 0 && len(plan.Deleted) == 0 {
			fmt.Println("Up to date.")
			return nil
		}

		if len(plan.Added) > 0 {
			fmt.Printf("New files (%d):\n", len(plan.Added))
			for _, f := range plan.Added {
				fmt.Printf("  + %s\n", f)
			}
		}
		if len(plan.Modified) > 0 {
			fmt.Printf("Modified files (%d):\n", len(plan.Modified))
			for _, f := range plan.Modified {
				fmt.Printf("  ~ %s\n", f)
			}
		}
		if len(plan.Deleted) > 0 {
			if cfg.Sync.Delete {
				fmt.Printf("Deleted files (%d):\n", len(plan.Deleted))
			} else {
				fmt.Printf("Removed or deselected, kept because delete is off (%d):\n", len(plan.Deleted))
			}
			for _, f := range plan.Deleted {
				fmt.Printf("  - %s\n", f)
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
