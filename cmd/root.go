package cmd

import (
	"github.com/spf13/cobra"
)

var (
	cfgFile string
	verbose bool
)

var rootCmd = &cobra.Command{
	Use:   "emu-sync",
	Short: "Sync ROMs and BIOS files from an S3-compatible bucket",
	Long: `emu-sync syncs ROMs and BIOS files from an S3-compatible cloud bucket
(Backblaze B2, AWS S3, DigitalOcean Spaces) to one or more devices.

Upload from your main machine, sync to your Steam Decks.`,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file path (default ~/.config/emu-sync/config.toml)")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "enable debug logging")
}

func Execute() error {
	return rootCmd.Execute()
}
