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
	Short: "Remove automatic sync schedule",
	Long: `Removes the automatic sync schedule installed by 'emu-sync install'.
On Linux: stops the systemd timer and removes service files, desktop shortcuts,
and the web UI shortcut.
On macOS: unloads the launchd agent, removes the plist and app bundle.
Does not remove the binary, config, or synced files.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		switch runtime.GOOS {
		case "linux":
			return uninstallLinux()
		case "darwin":
			return uninstallMacOS()
		default:
			return fmt.Errorf("uninstall is not supported on %s", runtime.GOOS)
		}
	},
}

func uninstallLinux() error {
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
		removeFile(filepath.Join(systemdDir, name))
	}

	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()

	// Remove desktop shortcuts and GUI script
	removeFile(filepath.Join(home, ".local", "share", "applications", "emu-sync.desktop"))
	removeFile(filepath.Join(home, ".local", "share", "applications", "emu-sync-web.desktop"))
	removeFile(filepath.Join(home, ".local", "bin", "emu-sync-gui.sh"))

	fmt.Println("\nDone! Automatic syncing has been removed.")
	fmt.Println("Your synced files, config, and the emu-sync binary are still in place.")
	return nil
}

func uninstallMacOS() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("finding home directory: %w", err)
	}

	plistPath := filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist")

	// Unload the agent
	if err := exec.Command("launchctl", "unload", plistPath).Run(); err != nil {
		fmt.Println("Agent was not loaded (may already be uninstalled)")
	} else {
		fmt.Println("Unloaded launch agent")
	}

	removeFile(plistPath)

	// Remove app bundle
	appPath := filepath.Join(home, "Applications", "emu-sync.app")
	if _, err := os.Stat(appPath); err == nil {
		if err := os.RemoveAll(appPath); err != nil {
			fmt.Printf("Warning: could not remove %s: %v\n", appPath, err)
		} else {
			fmt.Printf("Removed %s\n", appPath)
		}
	}

	fmt.Println("\nDone! Automatic syncing has been removed.")
	fmt.Println("Your synced files, config, and the emu-sync binary are still in place.")
	return nil
}

func removeFile(path string) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		fmt.Printf("Warning: could not remove %s: %v\n", path, err)
	} else if err == nil {
		fmt.Printf("Removed %s\n", path)
	}
}

func init() {
	rootCmd.AddCommand(uninstallCmd)
}
