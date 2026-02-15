package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/jacobfgrant/emu-sync/internal/config"
	"github.com/jacobfgrant/emu-sync/internal/token"
	"github.com/spf13/cobra"
)

const maskedKey = "********"

var generateTokenCmd = &cobra.Command{
	Use:   "generate-token",
	Short: "Generate a setup token for recipients",
	Long: `Interactively generates a base64-encoded setup token, using the current
config as defaults. Send this token to recipients so they can configure
their devices with a single 'emu-sync setup <token>' command.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgPath := cfgFile
		if cfgPath == "" {
			cfgPath = config.DefaultConfigPath()
		}

		cfg, err := config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		reader := bufio.NewReader(os.Stdin)

		fmt.Println("Generate setup token")
		fmt.Println("====================")
		fmt.Println("Press Enter to keep the default value from your config.")
		fmt.Println()

		endpoint := promptWithDefault(reader, "S3 endpoint", cfg.Storage.EndpointURL)
		region := promptWithDefault(reader, "Region", cfg.Storage.Region)
		bucket := promptWithDefault(reader, "Bucket", cfg.Storage.Bucket)
		prefix := promptWithDefault(reader, "Bucket prefix", cfg.Storage.Prefix)
		keyID := promptWithDefault(reader, "Key ID", cfg.Storage.KeyID)

		appKey := promptWithDefault(reader, "Application key", maskedKey)
		if appKey == maskedKey {
			appKey = cfg.Storage.SecretKey
		}

		emuPath := promptWithDefault(reader, "Emulation path", cfg.Sync.EmulationPath)

		syncDirsDefault := strings.Join(cfg.Sync.SyncDirs, ",")
		syncDirsStr := promptWithDefault(reader, "Sync directories (comma-separated)", syncDirsDefault)
		var syncDirs []string
		for _, d := range strings.Split(syncDirsStr, ",") {
			syncDirs = append(syncDirs, strings.TrimSpace(d))
		}

		deleteDefault := "y"
		if !cfg.Sync.Delete {
			deleteDefault = "n"
		}
		deleteStr := promptWithDefault(reader, "Delete local files removed from bucket? (y/n)", deleteDefault)
		deleteFiles := strings.HasPrefix(strings.ToLower(deleteStr), "y")

		data := &token.Data{
			EndpointURL:   endpoint,
			Bucket:        bucket,
			KeyID:         keyID,
			SecretKey:     appKey,
			Region:        region,
			Prefix:        prefix,
			EmulationPath: emuPath,
			SyncDirs:      syncDirs,
			Delete:        &deleteFiles,
		}

		encoded, err := token.Encode(data)
		if err != nil {
			return err
		}

		fmt.Println()
		fmt.Println("Setup token (send this to the recipient):")
		fmt.Println(encoded)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(generateTokenCmd)
}
