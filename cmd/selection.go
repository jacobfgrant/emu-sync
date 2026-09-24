package cmd

import (
	"sort"
	"strings"
)

// selectionNode is a directory or file in the tree of manifest keys,
// with counts of how many files at or under it are selected.
type selectionNode struct {
	children map[string]*selectionNode
	total    int
	selected int
}

func newSelectionNode() *selectionNode {
	return &selectionNode{children: make(map[string]*selectionNode)}
}

func (n *selectionNode) add(parts []string, selected bool) {
	n.total++
	if selected {
		n.selected++
	}
	if len(parts) == 0 {
		return
	}
	child, ok := n.children[parts[0]]
	if !ok {
		child = newSelectionNode()
		n.children[parts[0]] = child
	}
	child.add(parts[1:], selected)
}

func (n *selectionNode) sortedChildren() []string {
	names := make([]string, 0, len(n.children))
	for name := range n.children {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// encodeSelections converts group selections into sync_dirs and sync_exclude
// entries such that config.ShouldSync matches the selection for every file.
//
// It walks the directory tree of all keys. A fully selected directory is
// saved as the directory, so files and systems added to the bucket later
// are synced too. For a partially selected directory it picks the shorter
// of "include the directory, exclude the unselected parts" and "list the
// selected parts", preferring the former on a tie.
func encodeSelections(groups []*systemGroup) (syncDirs, syncExclude []string) {
	root := newSelectionNode()
	for _, g := range groups {
		for _, f := range g.Files {
			root.add(strings.Split(f.Key, "/"), f.Selected)
		}
	}

	if root.total > 0 && root.selected == 0 {
		// An empty sync_dirs loads back as the default (roms, bios), so
		// name the top-level dirs in both lists to keep "nothing" explicit.
		for _, name := range root.sortedChildren() {
			syncDirs = append(syncDirs, name)
			syncExclude = append(syncExclude, name)
		}
		return syncDirs, syncExclude
	}

	return root.encodeChildren("", false)
}

// encode returns the sync_dirs and sync_exclude entries for the node at
// path. included means an ancestor is already in sync_dirs. Because
// ShouldSync checks excludes first, nothing under an excluded path can be
// re-included, so a partially selected node under an included one must
// be encoded through its children.
func (n *selectionNode) encode(path string, included bool) (inc, exc []string) {
	switch {
	case n.selected == n.total:
		if included {
			return nil, nil
		}
		return []string{path}, nil
	case n.selected == 0:
		if included {
			return nil, []string{path}
		}
		return nil, nil
	case included:
		return n.encodeChildren(path, true)
	}

	withDirInc, withDirExc := n.encodeChildren(path, true)
	withDirInc = append([]string{path}, withDirInc...)
	listedInc, listedExc := n.encodeChildren(path, false)
	if len(withDirInc)+len(withDirExc) <= len(listedInc)+len(listedExc) {
		return withDirInc, withDirExc
	}
	return listedInc, listedExc
}

func (n *selectionNode) encodeChildren(path string, included bool) (inc, exc []string) {
	for _, name := range n.sortedChildren() {
		childPath := name
		if path != "" {
			childPath = path + "/" + name
		}
		childInc, childExc := n.children[name].encode(childPath, included)
		inc = append(inc, childInc...)
		exc = append(exc, childExc...)
	}
	return inc, exc
}
