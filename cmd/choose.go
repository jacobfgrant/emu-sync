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

// systemKey returns the first two path segments of a manifest key,
// grouping files by top-level system (e.g., "bios/pcsx2", "roms/snes").
// For keys with fewer than 3 segments, it falls back to path.Dir.
func systemKey(key string) string {
	parts := strings.SplitN(key, "/", 3)
	if len(parts) <= 2 {
		return path.Dir(key)
	}
	return parts[0] + "/" + parts[1]
}

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

// subGroup is a subset of files within a systemGroup that share the same
// immediate child directory under the system key. Files directly in the
// system directory have RelDir == "".
type subGroup struct {
	RelDir string
	Files  []*fileInfo // pointers into parent systemGroup.Files
}

func (sg *subGroup) groupState() string {
	selected := 0
	for _, f := range sg.Files {
		if f.Selected {
			selected++
		}
	}
	if selected == 0 {
		return "none"
	}
	if selected == len(sg.Files) {
		return "all"
	}
	return "partial"
}

func (sg *subGroup) selectedCount() int {
	n := 0
	for _, f := range sg.Files {
		if f.Selected {
			n++
		}
	}
	return n
}

func (sg *subGroup) selectedSize() int64 {
	var size int64
	for _, f := range sg.Files {
		if f.Selected {
			size += f.Size
		}
	}
	return size
}

func (sg *subGroup) totalSize() int64 {
	var size int64
	for _, f := range sg.Files {
		size += f.Size
	}
	return size
}

// fullPath returns the absolute directory path for this sub-group.
func (sg *subGroup) fullPath(systemDir string) string {
	if sg.RelDir == "" {
		return systemDir
	}
	return systemDir + "/" + sg.RelDir
}

// buildSubGroups groups files within a systemGroup by their immediate
// child directory under g.Dir. For example, if g.Dir is "bios/pcsx2",
// the file "bios/pcsx2/resources/shader.glsl" gets RelDir="resources"
// and "bios/pcsx2/file.bin" gets RelDir="".
func buildSubGroups(g *systemGroup) []*subGroup {
	sgMap := make(map[string]*subGroup)
	for i := range g.Files {
		f := &g.Files[i]
		// f.Name is relative to g.Dir, e.g. "resources/shader.glsl" or "file.bin"
		relDir := ""
		if idx := strings.Index(f.Name, "/"); idx != -1 {
			relDir = f.Name[:idx]
		}
		sg, ok := sgMap[relDir]
		if !ok {
			sg = &subGroup{RelDir: relDir}
			sgMap[relDir] = sg
		}
		sg.Files = append(sg.Files, f)
	}

	var sgs []*subGroup
	for _, sg := range sgMap {
		sgs = append(sgs, sg)
	}
	sort.Slice(sgs, func(i, j int) bool {
		// Direct files (RelDir=="") sort first
		if sgs[i].RelDir == "" {
			return true
		}
		if sgs[j].RelDir == "" {
			return false
		}
		return sgs[i].RelDir < sgs[j].RelDir
	})

	return sgs
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
// slices for the config file. It encodes at the sub-group level when possible,
// using directory paths instead of individual files, and picks the shorter
// representation between dir + exclusions vs individual inclusions.
func encodeSelections(groups []*systemGroup) (syncDirs, syncExclude []string) {
	for _, g := range groups {
		state := g.groupState()
		switch state {
		case "all":
			syncDirs = append(syncDirs, g.Dir)
		case "none":
			// skip
		case "partial":
			sgs := buildSubGroups(g)
			if len(sgs) <= 1 {
				// No sub-groups — fall back to file-level encoding
				syncDirs, syncExclude = encodeFlat(g, syncDirs, syncExclude)
				continue
			}

			// Build two candidate encodings:
			// A) system dir + exclude unselected sub-parts
			// B) include only selected sub-parts
			var aInc, aExc []string
			var bInc []string

			aInc = append(aInc, g.Dir)

			for _, sg := range sgs {
				sgState := sg.groupState()
				sgPath := sg.fullPath(g.Dir)

				switch sgState {
				case "all":
					// A: included by parent, nothing to exclude
					// B: include the sub-group dir (or individual files for direct)
					if sg.RelDir == "" {
						for _, f := range sg.Files {
							bInc = append(bInc, f.Key)
						}
					} else {
						bInc = append(bInc, sgPath)
					}
				case "none":
					// A: exclude the sub-group
					if sg.RelDir == "" {
						for _, f := range sg.Files {
							aExc = append(aExc, f.Key)
						}
					} else {
						aExc = append(aExc, sgPath)
					}
					// B: skip entirely
				case "partial":
					// Both encodings must list individual files
					for _, f := range sg.Files {
						if !f.Selected {
							aExc = append(aExc, f.Key)
						}
						if f.Selected {
							bInc = append(bInc, f.Key)
						}
					}
				}
			}

			// Pick the encoding with fewer total entries
			if len(aInc)+len(aExc) <= len(bInc) {
				syncDirs = append(syncDirs, aInc...)
				syncExclude = append(syncExclude, aExc...)
			} else {
				syncDirs = append(syncDirs, bInc...)
			}
		}
	}
	return syncDirs, syncExclude
}

// encodeFlat encodes a partial group at the file level, picking whichever
// representation is shorter.
func encodeFlat(g *systemGroup, syncDirs, syncExclude []string) ([]string, []string) {
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
	return syncDirs, syncExclude
}

// buildGroups aggregates manifest files into system-level groups and marks
// which are currently selected based on the existing config. Files are
// grouped by their first two path segments (e.g., "bios/pcsx2").
func buildGroups(m *manifest.Manifest, cfg *config.Config) []*systemGroup {
	dirMap := make(map[string]*systemGroup)

	for key, entry := range m.Files {
		sk := systemKey(key)
		g, ok := dirMap[sk]
		if !ok {
			g = &systemGroup{Dir: sk}
			dirMap[sk] = g
		}
		// Name is the path relative to the system key
		name := strings.TrimPrefix(key, sk+"/")
		g.Files = append(g.Files, fileInfo{
			Key:      key,
			Name:     name,
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

// drillInto shows sub-groups within a system group and lets the user toggle
// them. If the system has no sub-groups (all files in the root), it falls
// back to a flat file list. Entering ">N" drills into a sub-group to show
// individual files.
func drillInto(reader *bufio.Reader, g *systemGroup) {
	sgs := buildSubGroups(g)
	if len(sgs) <= 1 {
		drillIntoFiles(reader, g.Dir, g.Files)
		return
	}

	for {
		fmt.Printf("\n%s (%d files, %s):\n", g.Dir, len(g.Files), formatSize(g.TotalSize))

		// Build display items: direct files first, then sub-group rows
		type displayItem struct {
			isSubGroup bool
			fileIdx    int // index into g.Files (for direct files only)
			sgIdx      int // index into sgs
		}
		var items []displayItem
		for si, sg := range sgs {
			if sg.RelDir == "" {
				// Direct files — show individually
				for _, f := range sg.Files {
					// Find index of f in g.Files
					for fi := range g.Files {
						if &g.Files[fi] == f {
							items = append(items, displayItem{fileIdx: fi})
							break
						}
					}
				}
			} else {
				items = append(items, displayItem{isSubGroup: true, sgIdx: si})
			}
		}

		for i, item := range items {
			if item.isSubGroup {
				sg := sgs[item.sgIdx]
				marker := "[ ]"
				switch sg.groupState() {
				case "all":
					marker = "[x]"
				case "partial":
					marker = "[~]"
				}
				extra := ""
				if sg.groupState() == "partial" {
					extra = fmt.Sprintf("  (%d of %d)", sg.selectedCount(), len(sg.Files))
				}
				fmt.Printf("  %2d. %s %-35s %4d files  %8s%s\n",
					i+1, marker, sg.RelDir+"/", len(sg.Files), formatSize(sg.totalSize()), extra)
			} else {
				f := g.Files[item.fileIdx]
				marker := "[ ]"
				if f.Selected {
					marker = "[x]"
				}
				fmt.Printf("  %2d. %s %-45s %8s\n", i+1, marker, f.Name, formatSize(f.Size))
			}
		}

		fmt.Println()
		fmt.Print("Toggle (e.g., 1 3), '>N' to browse, 'all', 'none', or Enter to go back: ")
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
			if strings.HasPrefix(tok, ">") {
				idx, err := strconv.Atoi(tok[1:])
				if err != nil || idx < 1 || idx > len(items) {
					fmt.Printf("  invalid: %s\n", tok)
					continue
				}
				item := items[idx-1]
				if item.isSubGroup {
					sg := sgs[item.sgIdx]
					label := g.Dir + "/" + sg.RelDir
					drillIntoFilesPtrs(reader, label, sg.Files)
				} else {
					fmt.Printf("  %s is a file, not a directory\n", g.Files[item.fileIdx].Name)
				}
				continue
			}
			idx, err := strconv.Atoi(tok)
			if err != nil || idx < 1 || idx > len(items) {
				fmt.Printf("  invalid: %s\n", tok)
				continue
			}
			item := items[idx-1]
			if item.isSubGroup {
				sg := sgs[item.sgIdx]
				newState := sg.groupState() != "all"
				for _, f := range sg.Files {
					f.Selected = newState
				}
			} else {
				g.Files[item.fileIdx].Selected = !g.Files[item.fileIdx].Selected
			}
		}
	}
}

// drillIntoFilesPtrs shows individual files (referenced by pointer) and
// lets the user toggle them. Used when drilling into sub-groups.
func drillIntoFilesPtrs(reader *bufio.Reader, label string, files []*fileInfo) {
	for {
		fmt.Printf("\n%s (%d files):\n", label, len(files))
		for i, f := range files {
			marker := "[ ]"
			if f.Selected {
				marker = "[x]"
			}
			fmt.Printf("  %2d. %s %-45s %8s\n", i+1, marker, f.Name, formatSize(f.Size))
		}

		fmt.Println()
		fmt.Print("Toggle (e.g., 1 3 5), 'all', 'none', or Enter to go back: ")
		input := prompt(reader, "")
		if input == "" {
			return
		}

		lower := strings.ToLower(strings.TrimSpace(input))
		if lower == "all" {
			for _, f := range files {
				f.Selected = true
			}
			continue
		}
		if lower == "none" {
			for _, f := range files {
				f.Selected = false
			}
			continue
		}

		for _, tok := range strings.Fields(input) {
			idx, err := strconv.Atoi(tok)
			if err != nil || idx < 1 || idx > len(files) {
				fmt.Printf("  invalid: %s\n", tok)
				continue
			}
			files[idx-1].Selected = !files[idx-1].Selected
		}
	}
}

// drillIntoFiles shows individual files and lets the user toggle them.
func drillIntoFiles(reader *bufio.Reader, label string, files []fileInfo) {
	for {
		fmt.Printf("\n%s (%d files):\n", label, len(files))
		for i, f := range files {
			marker := "[ ]"
			if f.Selected {
				marker = "[x]"
			}
			fmt.Printf("  %2d. %s %-45s %8s\n", i+1, marker, f.Name, formatSize(f.Size))
		}

		fmt.Println()
		fmt.Print("Toggle (e.g., 1 3 5), 'all', 'none', or Enter to go back: ")
		input := prompt(reader, "")
		if input == "" {
			return
		}

		lower := strings.ToLower(strings.TrimSpace(input))
		if lower == "all" {
			for i := range files {
				files[i].Selected = true
			}
			continue
		}
		if lower == "none" {
			for i := range files {
				files[i].Selected = false
			}
			continue
		}

		for _, tok := range strings.Fields(input) {
			idx, err := strconv.Atoi(tok)
			if err != nil || idx < 1 || idx > len(files) {
				fmt.Printf("  invalid: %s\n", tok)
				continue
			}
			files[idx-1].Selected = !files[idx-1].Selected
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
