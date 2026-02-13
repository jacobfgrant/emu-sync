package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/jacobfgrant/emu-sync/internal/config"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Interactive configuration wizard",
	Long:  `Walks through setting up the emu-sync config file with prompts for each field.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		reader := bufio.NewReader(os.Stdin)

		fmt.Println("emu-sync configuration wizard")
		fmt.Println("=============================")
		fmt.Println()

		endpoint := prompt(reader, "S3 endpoint URL (e.g., https://s3.us-west-004.backblazeb2.com): ")
		bucket := prompt(reader, "Bucket name: ")
		keyID := prompt(reader, "Access key ID: ")
		secretKey := prompt(reader, "Secret access key: ")
		region := prompt(reader, "Region (e.g., us-west-004): ")

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
			},
			Sync: config.SyncConfig{
				EmulationPath: emuPath,
				SyncDirs:      syncDirs,
				Delete:        deleteFiles,
			},
		}

		cfgPath := cfgFile
		if cfgPath == "" {
			cfgPath = config.DefaultConfigPath()
		}

		if err := config.Write(cfg, cfgPath); err != nil {
			return err
		}

		fmt.Printf("\nConfig written to %s\n", cfgPath)
		return nil
	},
}

func prompt(reader *bufio.Reader, message string) string {
	fmt.Print(message)
	text, _ := reader.ReadString('\n')
	return strings.TrimSpace(text)
}

func init() {
	rootCmd.AddCommand(initCmd)
}
