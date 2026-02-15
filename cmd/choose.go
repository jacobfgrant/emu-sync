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
	Included  bool // whole directory is in sync_dirs
}

type fileInfo struct {
	Key      string
	Name     string
	Size     int64
	Excluded bool
}

var chooseCmd = &cobra.Command{
	Use:   "choose",
	Short: "Interactively select which systems and games to sync",
	Long: `Downloads the remote manifest and shows available systems with their
sizes. Toggle systems on/off, then optionally drill into individual
systems to include or exclude specific games. Saves selections to
your config file.`,
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

		// System selection loop
		for {
			printSystems(groups)
			fmt.Println()
			printTotals(groups)
			fmt.Println()
			fmt.Print("Toggle systems (e.g., 1 3 4), d<N> to drill in, or Enter to save: ")
			input := readLine(reader)
			if input == "" {
				break
			}

			for _, tok := range strings.Fields(input) {
				if strings.HasPrefix(tok, "d") || strings.HasPrefix(tok, "D") {
					numStr := strings.TrimPrefix(strings.TrimPrefix(tok, "d"), "D")
					idx, err := strconv.Atoi(numStr)
					if err != nil || idx < 1 || idx > len(groups) {
						fmt.Printf("  invalid: %s\n", tok)
						continue
					}
					drillInto(reader, groups[idx-1])
				} else {
					idx, err := strconv.Atoi(tok)
					if err != nil || idx < 1 || idx > len(groups) {
						fmt.Printf("  invalid: %s\n", tok)
						continue
					}
					g := groups[idx-1]
					g.Included = !g.Included
					if g.Included {
						// Toggled on — include all files
						for i := range g.Files {
							g.Files[i].Excluded = false
						}
					} else {
						// Toggled off — exclude all files
						for i := range g.Files {
							g.Files[i].Excluded = true
						}
					}
				}
			}
		}

		// Build sync_dirs and sync_exclude from selections
		var syncDirs []string
		var syncExclude []string

		for _, g := range groups {
			if !g.Included {
				// Count individually selected files in non-included groups
				var individual []string
				for _, f := range g.Files {
					if !f.Excluded {
						individual = append(individual, f.Key)
					}
				}
				if len(individual) == len(g.Files) {
					// All files selected — just include the directory
					syncDirs = append(syncDirs, g.Dir)
				} else if len(individual) > 0 {
					// Only some files selected — add them individually
					syncDirs = append(syncDirs, individual...)
				}
				continue
			}

			syncDirs = append(syncDirs, g.Dir)
			for _, f := range g.Files {
				if f.Excluded {
					syncExclude = append(syncExclude, f.Key)
				}
			}
		}

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

// buildGroups aggregates manifest files into system-level groups and marks
// which are currently included/excluded based on the existing config.
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
			Key:  key,
			Name: path.Base(key),
			Size: entry.Size,
		})
		g.TotalSize += entry.Size
	}

	// Sort files within each group by name
	for _, g := range dirMap {
		sort.Slice(g.Files, func(i, j int) bool {
			return g.Files[i].Name < g.Files[j].Name
		})
	}

	// Determine inclusion state from current config
	// Build an exclude set for quick lookup
	excludeSet := make(map[string]bool)
	for _, ex := range cfg.Sync.SyncExclude {
		excludeSet[ex] = true
	}

	// Build set of individual file includes (sync_dirs entries that match a specific file)
	fileIncludes := make(map[string]bool)
	dirIncludes := make(map[string]bool)
	for _, sd := range cfg.Sync.SyncDirs {
		// Check if this sync_dir matches any file key exactly
		if _, ok := m.Files[sd]; ok {
			fileIncludes[sd] = true
		} else {
			dirIncludes[sd] = true
		}
	}

	for _, g := range dirMap {
		// Check if this group's directory is included via sync_dirs prefix
		g.Included = false
		for dir := range dirIncludes {
			if g.Dir == dir || strings.HasPrefix(g.Dir, dir+"/") {
				g.Included = true
				break
			}
		}

		if g.Included {
			// Mark individually excluded files
			for i := range g.Files {
				g.Files[i].Excluded = excludeSet[g.Files[i].Key]
			}
		} else {
			// Group not included — mark files as excluded by default,
			// except those individually included via sync_dirs
			for i := range g.Files {
				g.Files[i].Excluded = !fileIncludes[g.Files[i].Key]
			}
		}
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
		marker := "[ ]"
		if g.Included {
			marker = "[x]"
		} else {
			// Check if any individual files are selected
			for _, f := range g.Files {
				if !f.Excluded {
					marker = "[~]"
					break
				}
			}
		}

		excluded := 0
		if g.Included {
			for _, f := range g.Files {
				if f.Excluded {
					excluded++
				}
			}
		}

		extra := ""
		if excluded > 0 {
			extra = fmt.Sprintf("  (%d excluded)", excluded)
		}
		if !g.Included && marker == "[~]" {
			selected := 0
			for _, f := range g.Files {
				if !f.Excluded {
					selected++
				}
			}
			extra = fmt.Sprintf("  (%d of %d selected)", selected, len(g.Files))
		}

		fmt.Printf("  %2d. %s %-25s %8s  (%d files)%s\n",
			i+1, marker, g.Dir, formatSize(g.TotalSize), len(g.Files), extra)
	}
}

func printTotals(groups []*systemGroup) {
	var selectedSize, totalSize int64
	for _, g := range groups {
		for _, f := range g.Files {
			totalSize += f.Size
			if (g.Included && !f.Excluded) || (!g.Included && !f.Excluded) {
				selectedSize += f.Size
			}
		}
	}
	fmt.Printf("Selected: %s  |  Total available: %s", formatSize(selectedSize), formatSize(totalSize))
}

// drillInto shows individual files within a system group and lets the user
// toggle them on/off.
func drillInto(reader *bufio.Reader, g *systemGroup) {
	for {
		fmt.Printf("\n%s (%d files, %s):\n", g.Dir, len(g.Files), formatSize(g.TotalSize))
		for i, f := range g.Files {
			marker := "[x]"
			if f.Excluded {
				marker = "[ ]"
			}
			// For non-included groups, invert the sense: !Excluded means selected
			if !g.Included {
				if f.Excluded {
					marker = "[ ]"
				} else {
					marker = "[x]"
				}
			}
			fmt.Printf("  %2d. %s %-45s %8s\n", i+1, marker, f.Name, formatSize(f.Size))
		}

		fmt.Println()
		fmt.Print("Toggle games (e.g., 1 3 5), 'all', 'none', or Enter to go back: ")
		input := readLine(reader)
		if input == "" {
			return
		}

		lower := strings.ToLower(strings.TrimSpace(input))
		if lower == "all" {
			for i := range g.Files {
				g.Files[i].Excluded = false
			}
			if !g.Included {
				g.Included = true
			}
			continue
		}
		if lower == "none" {
			for i := range g.Files {
				g.Files[i].Excluded = true
			}
			continue
		}

		for _, tok := range strings.Fields(input) {
			idx, err := strconv.Atoi(tok)
			if err != nil || idx < 1 || idx > len(g.Files) {
				fmt.Printf("  invalid: %s\n", tok)
				continue
			}
			g.Files[idx-1].Excluded = !g.Files[idx-1].Excluded
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

func readLine(reader *bufio.Reader) string {
	text, _ := reader.ReadString('\n')
	return strings.TrimSpace(text)
}

func init() {
	rootCmd.AddCommand(chooseCmd)
}
