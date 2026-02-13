package cmd

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

//go:embed install_assets/emu-sync.service
var serviceUnit string

//go:embed install_assets/emu-sync.timer
var timerUnit string

//go:embed install_assets/emu-sync.desktop
var desktopEntry string

//go:embed install_assets/emu-sync-gui.sh
var guiScript string

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install systemd timer and desktop shortcut (Linux only)",
	Long: `Installs a systemd user timer that syncs every 6 hours and a desktop
shortcut for manual syncing via KDE's application menu.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if runtime.GOOS != "linux" {
			return fmt.Errorf("install is only supported on Linux (SteamOS, Ubuntu, etc.)")
		}

		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("finding home directory: %w", err)
		}

		// Install systemd units
		systemdDir := filepath.Join(home, ".config", "systemd", "user")
		if err := os.MkdirAll(systemdDir, 0o755); err != nil {
			return fmt.Errorf("creating systemd directory: %w", err)
		}

		servicePath := filepath.Join(systemdDir, "emu-sync.service")
		if err := os.WriteFile(servicePath, []byte(serviceUnit), 0o644); err != nil {
			return fmt.Errorf("writing service unit: %w", err)
		}
		fmt.Printf("Installed %s\n", servicePath)

		timerPath := filepath.Join(systemdDir, "emu-sync.timer")
		if err := os.WriteFile(timerPath, []byte(timerUnit), 0o644); err != nil {
			return fmt.Errorf("writing timer unit: %w", err)
		}
		fmt.Printf("Installed %s\n", timerPath)

		// Enable and start the timer
		if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
			fmt.Printf("Warning: could not reload systemd: %v\n", err)
		}
		if err := exec.Command("systemctl", "--user", "enable", "--now", "emu-sync.timer").Run(); err != nil {
			fmt.Printf("Warning: could not enable timer: %v\n", err)
		} else {
			fmt.Println("Enabled emu-sync.timer (syncs every 6 hours)")
		}

		// Install desktop shortcut
		applicationsDir := filepath.Join(home, ".local", "share", "applications")
		if err := os.MkdirAll(applicationsDir, 0o755); err != nil {
			return fmt.Errorf("creating applications directory: %w", err)
		}

		desktopPath := filepath.Join(applicationsDir, "emu-sync.desktop")
		if err := os.WriteFile(desktopPath, []byte(desktopEntry), 0o644); err != nil {
			return fmt.Errorf("writing desktop entry: %w", err)
		}
		fmt.Printf("Installed %s\n", desktopPath)

		// Install GUI script
		binDir := filepath.Join(home, ".local", "bin")
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			return fmt.Errorf("creating bin directory: %w", err)
		}

		guiPath := filepath.Join(binDir, "emu-sync-gui.sh")
		if err := os.WriteFile(guiPath, []byte(guiScript), 0o755); err != nil {
			return fmt.Errorf("writing GUI script: %w", err)
		}
		fmt.Printf("Installed %s\n", guiPath)

		fmt.Println("\nDone! Sync will run automatically every 6 hours.")
		fmt.Println("You can also use the 'Sync ROMs' shortcut in your application menu.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(installCmd)
}
