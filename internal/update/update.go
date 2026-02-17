package update

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"
)

// InstallMethod represents how emu-sync was installed.
type InstallMethod int

const (
	MethodScript InstallMethod = iota
	MethodBrew
)

var latestReleaseURL = "https://github.com/jacobfgrant/emu-sync/releases/latest"

const installScriptURL = "https://raw.githubusercontent.com/jacobfgrant/emu-sync/master/install.sh"

// CheckLatestVersion queries GitHub for the latest release tag.
// Uses an HTTP HEAD with redirect capture to avoid reading the response body.
func CheckLatestVersion() (string, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Head(latestReleaseURL)
	if err != nil {
		return "", fmt.Errorf("checking latest version: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		return "", fmt.Errorf("unexpected status %d from GitHub", resp.StatusCode)
	}

	location := resp.Header.Get("Location")
	if location == "" {
		return "", fmt.Errorf("no Location header in GitHub redirect")
	}

	// Location looks like: https://github.com/.../releases/tag/v0.7.0
	tag := path.Base(location)
	if !strings.HasPrefix(tag, "v") {
		return "", fmt.Errorf("unexpected tag format: %s", tag)
	}

	return tag, nil
}

// CompareVersions compares two version strings (e.g., "v0.3.0", "v1.0").
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
// Missing segments are treated as 0 (v1.0 == v1.0.0).
func CompareVersions(a, b string) int {
	aParts := parseVersion(a)
	bParts := parseVersion(b)

	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}

	for i := 0; i < maxLen; i++ {
		av, bv := 0, 0
		if i < len(aParts) {
			av = aParts[i]
		}
		if i < len(bParts) {
			bv = bParts[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func parseVersion(v string) []int {
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	nums := make([]int, len(parts))
	for i, p := range parts {
		n, _ := strconv.Atoi(p)
		nums[i] = n
	}
	return nums
}

// IsUpdateAvailable returns true if latest is newer than current.
// Always returns false for dev builds.
func IsUpdateAvailable(current, latest string) bool {
	if current == "dev" {
		return false
	}
	return CompareVersions(current, latest) < 0
}

// DetectInstallMethod checks whether emu-sync is managed by Homebrew.
func DetectInstallMethod() InstallMethod {
	_, err := exec.LookPath("brew")
	if err != nil {
		return MethodScript
	}
	cmd := exec.Command("brew", "list", "emu-sync")
	if err := cmd.Run(); err != nil {
		return MethodScript
	}
	return MethodBrew
}

// RunBrewUpgrade runs brew upgrade for emu-sync.
func RunBrewUpgrade() error {
	cmd := exec.Command("brew", "upgrade", "jacobfgrant/tap/emu-sync")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RunScriptUpdate downloads and runs the install script for the given version.
func RunScriptUpdate(version string) error {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(installScriptURL)
	if err != nil {
		return fmt.Errorf("downloading install script: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d downloading install script", resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp("", "emu-sync-install-*.sh")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.ReadFrom(resp.Body); err != nil {
		tmpFile.Close()
		return fmt.Errorf("writing install script: %w", err)
	}
	tmpFile.Close()

	cmd := exec.Command("sh", tmpFile.Name())
	cmd.Env = append(os.Environ(), "EMU_SYNC_VERSION="+version)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
