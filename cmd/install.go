package cmd

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

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

//go:embed install_assets/emu-sync-web.desktop
var webDesktopEntry string

//go:embed install_assets/emu-sync-web-Info.plist
var webAppPlist string

//go:embed install_assets/emu-sync-web.sh
var webAppLauncher string

//go:embed install_assets/com.jacobfgrant.emu-sync.plist
var launchdPlist string

const launchdLabel = "com.jacobfgrant.emu-sync"

var noShortcuts bool

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install automatic sync schedule",
	Long: `On Linux: installs a systemd user timer and desktop shortcuts.
The "Sync ROMs" shortcut runs a headless sync; the "emu-sync" shortcut
opens the web UI for managing game selections.
On macOS: installs a launchd user agent and an emu-sync app bundle
in ~/Applications that opens the web UI.
Use --no-shortcuts to skip shortcuts/app and only install the
timer/schedule. Syncs automatically every 6 hours.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Resolve the actual binary path
		binPath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolving binary path: %w", err)
		}
		binPath, err = filepath.EvalSymlinks(binPath)
		if err != nil {
			return fmt.Errorf("resolving binary symlinks: %w", err)
		}

		switch runtime.GOOS {
		case "linux":
			return installLinux(binPath)
		case "darwin":
			return installMacOS(binPath)
		default:
			return fmt.Errorf("install is not supported on %s", runtime.GOOS)
		}
	},
}

func installLinux(binPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("finding home directory: %w", err)
	}

	// Install systemd units
	systemdDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(systemdDir, 0o755); err != nil {
		return fmt.Errorf("creating systemd directory: %w", err)
	}

	resolvedService := strings.Replace(serviceUnit, "BINARY_PATH", binPath, 1)

	servicePath := filepath.Join(systemdDir, "emu-sync.service")
	if err := os.WriteFile(servicePath, []byte(resolvedService), 0o644); err != nil {
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

	if !noShortcuts {
		// Install desktop shortcut for headless sync
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

		// Install desktop shortcut for web UI
		resolvedWeb := strings.Replace(webDesktopEntry, "BINARY_PATH", binPath, 1)
		webDesktopPath := filepath.Join(applicationsDir, "emu-sync-web.desktop")
		if err := os.WriteFile(webDesktopPath, []byte(resolvedWeb), 0o644); err != nil {
			return fmt.Errorf("writing web desktop entry: %w", err)
		}
		fmt.Printf("Installed %s\n", webDesktopPath)
	}

	fmt.Println("\nDone! Sync will run automatically every 6 hours.")
	if !noShortcuts {
		fmt.Println("You can also use the 'Sync ROMs' or 'emu-sync' shortcuts in your application menu.")
	}
	return nil
}

func installMacOS(binPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("finding home directory: %w", err)
	}

	// Prepare the plist with resolved paths
	logDir := filepath.Join(home, "Library", "Logs")
	resolved := strings.Replace(launchdPlist, "BINARY_PATH", binPath, 1)
	resolved = strings.Replace(resolved, "LOG_DIR", logDir, 2)

	// Write the plist
	agentsDir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return fmt.Errorf("creating LaunchAgents directory: %w", err)
	}

	plistPath := filepath.Join(agentsDir, launchdLabel+".plist")
	if err := os.WriteFile(plistPath, []byte(resolved), 0o644); err != nil {
		return fmt.Errorf("writing plist: %w", err)
	}
	fmt.Printf("Installed %s\n", plistPath)

	// Unload first in case it's already loaded (makes install idempotent)
	_ = exec.Command("launchctl", "unload", plistPath).Run()

	if err := exec.Command("launchctl", "load", plistPath).Run(); err != nil {
		fmt.Printf("Warning: could not load agent: %v\n", err)
	} else {
		fmt.Println("Loaded launch agent (syncs every 6 hours)")
	}

	if !noShortcuts {
		// Install minimal .app bundle for web UI
		appDir := filepath.Join(home, "Applications", "emu-sync.app", "Contents")
		macosDir := filepath.Join(appDir, "MacOS")
		resourcesDir := filepath.Join(appDir, "Resources")
		if err := os.MkdirAll(macosDir, 0o755); err != nil {
			return fmt.Errorf("creating app bundle: %w", err)
		}
		if err := os.MkdirAll(resourcesDir, 0o755); err != nil {
			return fmt.Errorf("creating app resources: %w", err)
		}

		plistDst := filepath.Join(appDir, "Info.plist")
		if err := os.WriteFile(plistDst, []byte(webAppPlist), 0o644); err != nil {
			return fmt.Errorf("writing app Info.plist: %w", err)
		}

		resolvedLauncher := strings.Replace(webAppLauncher, "BINARY_PATH", binPath, 1)
		launcherPath := filepath.Join(macosDir, "emu-sync-web")
		if err := os.WriteFile(launcherPath, []byte(resolvedLauncher), 0o755); err != nil {
			return fmt.Errorf("writing app launcher: %w", err)
		}

		// Try to use a system icon (best-effort)
		iconDst := filepath.Join(resourcesDir, "icon.icns")
		iconCandidates := []string{
			"/Applications/Safari.app/Contents/Resources/AppIcon.icns",
			"/System/Library/CoreServices/CoreTypes.bundle/Contents/Resources/GenericNetworkIcon.icns",
		}
		for _, src := range iconCandidates {
			if copyFile(src, iconDst) == nil {
				break
			}
		}

		fmt.Printf("Installed %s\n", filepath.Join(home, "Applications", "emu-sync.app"))
	}

	fmt.Printf("\nDone! Sync will run automatically every 6 hours.\n")
	fmt.Printf("Logs: %s/emu-sync.log\n", logDir)
	if !noShortcuts {
		fmt.Println("You can also open the emu-sync app in ~/Applications.")
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func init() {
	installCmd.Flags().BoolVar(&noShortcuts, "no-shortcuts", false, "skip desktop shortcuts, only install timer/schedule")
	rootCmd.AddCommand(installCmd)
}
