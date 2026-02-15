package cmd

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/jacobfgrant/emu-sync/internal/config"
	"github.com/jacobfgrant/emu-sync/internal/storage"
	"github.com/spf13/cobra"
)

var b2RegionRe = regexp.MustCompile(`s3\.([^.]+)\.backblazeb2\.com`)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Interactive configuration wizard",
	Long:  `Walks through setting up the emu-sync config file with prompts for each field.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		reader := bufio.NewReader(os.Stdin)

		fmt.Println("emu-sync configuration wizard")
		fmt.Println("=============================")
		fmt.Println()

		endpoint := prompt(reader, "S3 endpoint (e.g., s3.us-west-002.backblazeb2.com): ")
		if endpoint != "" && !strings.Contains(endpoint, "://") {
			endpoint = "https://" + endpoint
		}

		bucket := prompt(reader, "Bucket name: ")
		prefix := prompt(reader, "Bucket prefix (leave blank for root): ")
		keyID := prompt(reader, "Access key ID: ")
		secretKey := prompt(reader, "Secret access key: ")

		// Auto-detect region from B2 endpoint URLs
		var region string
		if m := b2RegionRe.FindStringSubmatch(endpoint); m != nil {
			region = m[1]
			fmt.Printf("Detected region: %s\n", region)
		} else {
			region = prompt(reader, "Region: ")
		}

		defaultPath := "/run/media/mmcblk0p1/Emulation"
		emuPath := prompt(reader, fmt.Sprintf("Emulation path [%s]: ", defaultPath))
		if emuPath == "" {
			emuPath = defaultPath
		}

		syncDirsStr := prompt(reader, "Sync directories (comma-separated) [roms,bios]: ")
		var syncDirs []string
		if syncDirsStr == "" {
			syncDirs = []string{"roms", "bios"}
		} else {
			for _, d := range strings.Split(syncDirsStr, ",") {
				syncDirs = append(syncDirs, strings.TrimSpace(d))
			}
		}

		deleteStr := prompt(reader, "Delete local files removed from bucket? (y/n) [y]: ")
		deleteFiles := deleteStr == "" || strings.HasPrefix(strings.ToLower(deleteStr), "y")

		cfg := &config.Config{
			Storage: config.StorageConfig{
				EndpointURL: endpoint,
				Bucket:      bucket,
				KeyID:       keyID,
				SecretKey:   secretKey,
				Region:      region,
				Prefix:      prefix,
			},
			Sync: config.SyncConfig{
				EmulationPath: emuPath,
				SyncDirs:      syncDirs,
				Delete:        deleteFiles,
			},
		}

		fmt.Print("\nVerifying credentials...")
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
	rootCmd.AddCommand(initCmd)
}
