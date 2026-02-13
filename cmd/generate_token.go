package cmd

import (
	"fmt"

	"github.com/jacobfgrant/emu-sync/internal/config"
	"github.com/jacobfgrant/emu-sync/internal/token"
	"github.com/spf13/cobra"
)

var generateTokenCmd = &cobra.Command{
	Use:   "generate-token",
	Short: "Generate a setup token for recipients",
	Long: `Reads the current config and generates a base64-encoded setup token.
Send this token to recipients so they can configure their devices
with a single 'emu-sync setup <token>' command.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgPath := cfgFile
		if cfgPath == "" {
			cfgPath = config.DefaultConfigPath()
		}

		cfg, err := config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		data := token.FromConfig(cfg)
		encoded, err := token.Encode(data)
		if err != nil {
			return err
		}

		fmt.Println("Setup token (send this to the recipient):")
		fmt.Println(encoded)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(generateTokenCmd)
}
