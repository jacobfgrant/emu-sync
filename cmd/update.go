package cmd

import (
	"fmt"

	"github.com/jacobfgrant/emu-sync/internal/update"
	"github.com/spf13/cobra"
)

var checkOnly bool

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update emu-sync to the latest version",
	Long: `Checks for a newer version and updates emu-sync.
Detects whether emu-sync was installed via Homebrew or the install
script and updates accordingly. Use --check to only check without updating.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		current := cmd.Root().Version

		if current == "dev" {
			fmt.Println("Development build — update not available.")
			return nil
		}

		fmt.Printf("Current version: %s\n", current)
		fmt.Println("Checking for updates...")

		latest, err := update.CheckLatestVersion()
		if err != nil {
			return fmt.Errorf("checking for updates: %w", err)
		}

		if !update.IsUpdateAvailable(current, latest) {
			fmt.Printf("Already up to date (%s).\n", current)
			return nil
		}

		fmt.Printf("Update available: %s → %s\n", current, latest)

		if checkOnly {
			return nil
		}

		method := update.DetectInstallMethod()
		switch method {
		case update.MethodBrew:
			fmt.Println("Updating via Homebrew...")
			return update.RunBrewUpgrade()
		default:
			fmt.Println("Updating via install script...")
			return update.RunScriptUpdate(latest)
		}
	},
}

func init() {
	updateCmd.Flags().BoolVar(&checkOnly, "check", false, "only check for updates, don't install")
	rootCmd.AddCommand(updateCmd)
}
