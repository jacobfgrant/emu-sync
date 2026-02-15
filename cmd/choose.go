package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/jacobfgrant/emu-sync/internal/config"
	"github.com/jacobfgrant/emu-sync/internal/manifest"
	"github.com/jacobfgrant/emu-sync/internal/storage"
	"github.com/spf13/cobra"
)

// systemGroup holds aggregated info about a directory of files.
type systemGroup struct {
	Dir       string
	Files     []fileInfo
	TotalSize int64
}

type fileInfo struct {
	Key      string
	Name     string
	Size     int64
	Selected bool
}

// groupState returns the selection state of a group: "all", "none", or "partial".
func (g *systemGroup) groupState() string {
	selected := 0
	for _, f := range g.Files {
		if f.Selected {
			selected++
		}
	}
	if selected == 0 {
		return "none"
	}
	if selected == len(g.Files) {
		return "all"
	}
	return "partial"
}

func (g *systemGroup) selectedCount() int {
	n := 0
	for _, f := range g.Files {
		if f.Selected {
			n++
		}
	}
	return n
}

func (g *systemGroup) selectedSize() int64 {
	var size int64
	for _, f := range g.Files {
		if f.Selected {
			size += f.Size
		}
	}
	return size
}

var chooseCmd = &cobra.Command{
	Use:   "choose",
	Short: "Interactively select which systems and games to sync",
	Long: `Downloads the remote manifest and shows available systems with their
sizes. Select a system by number to see its games and toggle them
individually. Use 'all' or 'none' to select or deselect everything
in a system. Saves selections to your config file.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgPath := cfgFile
		if cfgPath == "" {
			cfgPath = config.DefaultConfigPath()
		}

		cfg, err := config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		client := storage.NewClient(&cfg.Storage)

		fmt.Print("Downloading manifest...")
		remoteData, err := client.DownloadManifest(cmd.Context())
		if err != nil {
			fmt.Println(" failed")
			return fmt.Errorf("downloading manifest: %w", err)
		}
		fmt.Println(" ok")

		remote, err := manifest.ParseJSON(remoteData)
		if err != nil {
			return fmt.Errorf("parsing manifest: %w", err)
		}

		groups := buildGroups(remote, cfg)
		if len(groups) == 0 {
			fmt.Println("No files found in remote manifest.")
			return nil
		}

		reader := bufio.NewReader(os.Stdin)

		// Main loop
		for {
			printSystems(groups)
			fmt.Println()
			printTotals(groups)
			fmt.Println()
			fmt.Print("Enter a number to browse, or Enter to save: ")
			input := prompt(reader, "")
			if input == "" {
				break
			}

			idx, err := strconv.Atoi(strings.TrimSpace(input))
			if err != nil || idx < 1 || idx > len(groups) {
				fmt.Printf("  invalid: %s\n", input)
				continue
			}
			drillInto(reader, groups[idx-1])
		}

		syncDirs, syncExclude := encodeSelections(groups)
		cfg.Sync.SyncDirs = syncDirs
		cfg.Sync.SyncExclude = syncExclude

		if err := config.Write(cfg, cfgPath); err != nil {
			return err
		}

		fmt.Printf("\nConfig updated: %s\n", cfgPath)
		fmt.Printf("  sync_dirs: %v\n", syncDirs)
		if len(syncExclude) > 0 {
			fmt.Printf("  sync_exclude: %v\n", syncExclude)
		}
		return nil
	},
}

// encodeSelections converts group selections into sync_dirs and sync_exclude
// slices for the config file. For partial groups, it picks whichever
// representation is shorter: dir + exclusions, or individual file inclusions.
func encodeSelections(groups []*systemGroup) (syncDirs, syncExclude []string) {
	for _, g := range groups {
		state := g.groupState()
		switch state {
		case "all":
			syncDirs = append(syncDirs, g.Dir)
		case "partial":
			excluded := len(g.Files) - g.selectedCount()
			selected := g.selectedCount()
			if excluded < selected {
				syncDirs = append(syncDirs, g.Dir)
				for _, f := range g.Files {
					if !f.Selected {
						syncExclude = append(syncExclude, f.Key)
					}
				}
			} else {
				for _, f := range g.Files {
					if f.Selected {
						syncDirs = append(syncDirs, f.Key)
					}
				}
			}
		}
	}
	return syncDirs, syncExclude
}

// buildGroups aggregates manifest files into system-level groups and marks
// which are currently selected based on the existing config.
func buildGroups(m *manifest.Manifest, cfg *config.Config) []*systemGroup {
	dirMap := make(map[string]*systemGroup)

	for key, entry := range m.Files {
		dir := path.Dir(key)
		g, ok := dirMap[dir]
		if !ok {
			g = &systemGroup{Dir: dir}
			dirMap[dir] = g
		}
		g.Files = append(g.Files, fileInfo{
			Key:      key,
			Name:     path.Base(key),
			Size:     entry.Size,
			Selected: cfg.ShouldSync(key),
		})
		g.TotalSize += entry.Size
	}

	// Sort files within each group by name
	for _, g := range dirMap {
		sort.Slice(g.Files, func(i, j int) bool {
			return g.Files[i].Name < g.Files[j].Name
		})
	}

	// Convert to sorted slice
	var groups []*systemGroup
	for _, g := range dirMap {
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Dir < groups[j].Dir
	})

	return groups
}

func printSystems(groups []*systemGroup) {
	fmt.Println()
	fmt.Println("Systems:")
	for i, g := range groups {
		state := g.groupState()
		marker := "[ ]"
		switch state {
		case "all":
			marker = "[x]"
		case "partial":
			marker = "[~]"
		}

		extra := ""
		if state == "partial" {
			extra = fmt.Sprintf("  (%d of %d selected)", g.selectedCount(), len(g.Files))
		}

		fmt.Printf("  %2d. %s %-25s %8s  (%d files)%s\n",
			i+1, marker, g.Dir, formatSize(g.TotalSize), len(g.Files), extra)
	}
}

func printTotals(groups []*systemGroup) {
	var selectedSize, totalSize int64
	for _, g := range groups {
		totalSize += g.TotalSize
		selectedSize += g.selectedSize()
	}
	fmt.Printf("Selected: %s  |  Total available: %s", formatSize(selectedSize), formatSize(totalSize))
}

// drillInto shows individual files within a system group and lets the user
// toggle them on/off.
func drillInto(reader *bufio.Reader, g *systemGroup) {
	for {
		fmt.Printf("\n%s (%d files, %s):\n", g.Dir, len(g.Files), formatSize(g.TotalSize))
		for i, f := range g.Files {
			marker := "[ ]"
			if f.Selected {
				marker = "[x]"
			}
			fmt.Printf("  %2d. %s %-45s %8s\n", i+1, marker, f.Name, formatSize(f.Size))
		}

		fmt.Println()
		fmt.Print("Toggle games (e.g., 1 3 5), 'all', 'none', or Enter to go back: ")
		input := prompt(reader, "")
		if input == "" {
			return
		}

		lower := strings.ToLower(strings.TrimSpace(input))
		if lower == "all" {
			for i := range g.Files {
				g.Files[i].Selected = true
			}
			continue
		}
		if lower == "none" {
			for i := range g.Files {
				g.Files[i].Selected = false
			}
			continue
		}

		for _, tok := range strings.Fields(input) {
			idx, err := strconv.Atoi(tok)
			if err != nil || idx < 1 || idx > len(g.Files) {
				fmt.Printf("  invalid: %s\n", tok)
				continue
			}
			g.Files[idx-1].Selected = !g.Files[idx-1].Selected
		}
	}
}

func formatSize(bytes int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.0f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.0f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func init() {
	rootCmd.AddCommand(chooseCmd)
}
