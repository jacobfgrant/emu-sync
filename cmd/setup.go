package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/jacobfgrant/emu-sync/internal/config"
	"github.com/jacobfgrant/emu-sync/internal/storage"
	"github.com/jacobfgrant/emu-sync/internal/token"
	"github.com/spf13/cobra"
)

var setupCmd = &cobra.Command{
	Use:   "setup [token]",
	Short: "Configure emu-sync from a setup token",
	Long: `Decodes a setup token (from emu-sync generate-token) and writes
the config file. If no token is provided as an argument, prompts
for it interactively (keeping it out of shell history).`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var tokenStr string
		if len(args) > 0 {
			tokenStr = args[0]
		} else {
			reader := bufio.NewReader(os.Stdin)
			fmt.Print("Paste setup token: ")
			text, _ := reader.ReadString('\n')
			tokenStr = strings.TrimSpace(text)
			if tokenStr == "" {
				return fmt.Errorf("no token provided")
			}
		}

		data, err := token.Decode(tokenStr)
		if err != nil {
			return err
		}

		cfg := data.ToConfig()

		// Check if the default emulation path exists
		if _, err := os.Stat(cfg.Sync.EmulationPath); os.IsNotExist(err) {
			fmt.Printf("Default emulation path not found: %s\n", cfg.Sync.EmulationPath)
			reader := bufio.NewReader(os.Stdin)
			fmt.Print("Enter your emulation path: ")
			text, _ := reader.ReadString('\n')
			path := strings.TrimSpace(text)
			if path != "" {
				cfg.Sync.EmulationPath = path
			}
		}

		fmt.Print("Verifying credentials...")
		client := storage.NewClient(&cfg.Storage)
		if err := client.Ping(cmd.Context()); err != nil {
			fmt.Println(" failed")
			return fmt.Errorf("credential check failed: %w", err)
		}
		fmt.Println(" ok")

		cfgPath := cfgFile
		if cfgPath == "" {
			cfgPath = config.DefaultConfigPath()
		}

		if err := config.Write(cfg, cfgPath); err != nil {
			return err
		}

		fmt.Printf("Config written to %s\n", cfgPath)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(setupCmd)
}
