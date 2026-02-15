package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove systemd timer and desktop shortcut (Linux only)",
	Long: `Stops and removes the systemd user timer and desktop shortcut
installed by 'emu-sync install'. Does not remove the binary,
config, or synced files.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if runtime.GOOS != "linux" {
			return fmt.Errorf("uninstall is only supported on Linux (SteamOS, Ubuntu, etc.)")
		}

		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("finding home directory: %w", err)
		}

		// Stop and disable the timer
		_ = exec.Command("systemctl", "--user", "stop", "emu-sync.timer").Run()
		_ = exec.Command("systemctl", "--user", "disable", "emu-sync.timer").Run()
		fmt.Println("Stopped and disabled emu-sync.timer")

		// Remove systemd units
		systemdDir := filepath.Join(home, ".config", "systemd", "user")
		for _, name := range []string{"emu-sync.service", "emu-sync.timer"} {
			path := filepath.Join(systemdDir, name)
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				fmt.Printf("Warning: could not remove %s: %v\n", path, err)
			} else if err == nil {
				fmt.Printf("Removed %s\n", path)
			}
		}

		_ = exec.Command("systemctl", "--user", "daemon-reload").Run()

		// Remove desktop shortcut
		desktopPath := filepath.Join(home, ".local", "share", "applications", "emu-sync.desktop")
		if err := os.Remove(desktopPath); err != nil && !os.IsNotExist(err) {
			fmt.Printf("Warning: could not remove %s: %v\n", desktopPath, err)
		} else if err == nil {
			fmt.Printf("Removed %s\n", desktopPath)
		}

		// Remove GUI script
		guiPath := filepath.Join(home, ".local", "bin", "emu-sync-gui.sh")
		if err := os.Remove(guiPath); err != nil && !os.IsNotExist(err) {
			fmt.Printf("Warning: could not remove %s: %v\n", guiPath, err)
		} else if err == nil {
			fmt.Printf("Removed %s\n", guiPath)
		}

		fmt.Println("\nDone! Automatic syncing has been removed.")
		fmt.Println("Your synced files, config, and the emu-sync binary are still in place.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(uninstallCmd)
}
