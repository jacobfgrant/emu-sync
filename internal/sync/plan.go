package sync

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/jacobfgrant/emu-sync/internal/config"
	"github.com/jacobfgrant/emu-sync/internal/manifest"
)

// Plan describes what a sync would change. It is shared by sync, status,
// and the web UI so their previews match what sync actually does.
type Plan struct {
	Selected  *manifest.Manifest // remote entries that pass the config's selection
	Added     []string           // selected and not yet synced, including Missing
	Modified  []string           // selected and changed in the bucket
	Deleted   []string           // synced before, but no longer selected or in the bucket
	Missing   []string           // in the local manifest but gone from disk
	Unchanged int
}

// NewPlan compares the remote manifest, filtered by cfg's selection,
// against the local manifest and the files on disk. It does not modify
// either manifest. All key lists are sorted.
func NewPlan(remote, local *manifest.Manifest, cfg *config.Config) *Plan {
	selected := manifest.New()
	selected.GeneratedAt = remote.GeneratedAt
	for key, entry := range remote.Files {
		if cfg.ShouldSync(key) {
			selected.Files[key] = entry
		}
	}

	diff := manifest.Diff(selected, local)
	plan := &Plan{
		Selected: selected,
		Added:    diff.Added,
		Modified: diff.Modified,
		Deleted:  diff.Deleted,
	}

	// Files the local manifest says are synced but that were removed from
	// disk (e.g. deleted by the user) need to be downloaded again.
	changed := make(map[string]bool, len(diff.Modified))
	for _, key := range diff.Modified {
		changed[key] = true
	}
	for key := range selected.Files {
		if _, inLocal := local.Files[key]; !inLocal || changed[key] {
			continue
		}
		localPath := filepath.Join(cfg.Sync.EmulationPath, filepath.FromSlash(key))
		if _, err := os.Stat(localPath); os.IsNotExist(err) {
			plan.Missing = append(plan.Missing, key)
			plan.Added = append(plan.Added, key)
		}
	}

	plan.Unchanged = len(selected.Files) - len(plan.Added) - len(plan.Modified)

	sort.Strings(plan.Added)
	sort.Strings(plan.Modified)
	sort.Strings(plan.Deleted)
	sort.Strings(plan.Missing)
	return plan
}
