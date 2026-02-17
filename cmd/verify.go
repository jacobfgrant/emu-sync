package cmd

import (
	"fmt"

	"github.com/jacobfgrant/emu-sync/internal/config"
	intsync "github.com/jacobfgrant/emu-sync/internal/sync"
	"github.com/spf13/cobra"
)

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify local files against the manifest",
	Long: `Re-hashes local files and compares against the local manifest.
Files that don't match are removed from the manifest so they
will be re-downloaded on the next sync.`,
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

		result, err := intsync.Verify(cfg, "", verbose)
		if err != nil {
			return err
		}

		fmt.Print(result.Summary())
		return nil
	},
}

func init() {
	rootCmd.AddCommand(verifyCmd)
}
