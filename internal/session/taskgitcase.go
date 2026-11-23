package session

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// fsLstat is a variable so a test can stand in for a volume that ignores case.
var fsLstat = os.Lstat

var protectedMetadataEntries = [...]struct {
	name string
	tree bool
}{
	{name: "HEAD"},
	{name: "config"},
	{name: "packed-refs"},
	{name: "objects", tree: true},
	{name: "refs", tree: true},
	{name: "worktrees", tree: true},
	{name: "modules", tree: true},
}

func metadataIdentities(metadata string) []os.FileInfo {
	entry, err := fsLstat(metadata)
	if err != nil {
		return nil
	}
	if entry.Mode()&os.ModeSymlink != 0 {
		if real, err := os.Stat(metadata); err == nil && !os.SameFile(entry, real) {
			return []os.FileInfo{entry, real}
		}
	}
	return []os.FileInfo{entry}
}

// belowGroundMetadata walks only ancestors so a missing target can still name protected metadata.
func belowGroundMetadata(metadata, path string) (below []string, ok bool) {
	identities := metadataIdentities(metadata)
	if len(identities) == 0 {
		return nil, false
	}
	for current := path; ; current = filepath.Dir(current) {
		if info, err := fsLstat(current); err == nil && slices.ContainsFunc(identities, func(id os.FileInfo) bool {
			return os.SameFile(info, id)
		}) {
			slices.Reverse(below)
			return below, true
		}
		if filepath.Dir(current) == current {
			return nil, false
		}
		below = append(below, filepath.Base(current))
	}
}

func protectedEntryBelow(metadata string, below []string) bool {
	if len(below) == 0 {
		return true
	}
	for _, entry := range protectedMetadataEntries {
		if len(below) > 1 && !entry.tree {
			continue
		}
		if below[0] == entry.name {
			return true
		}
		if strings.EqualFold(below[0], entry.name) {
			return metadataFoldsCase(metadata)
		}
	}
	return false
}

func protectedByMetadataIdentity(metadata, path string) bool {
	below, found := belowGroundMetadata(metadata, path)
	return found && protectedEntryBelow(metadata, below)
}

func holdsMetadataIdentity(metadata, target string) bool {
	info, err := fsLstat(target)
	if err != nil {
		return false
	}
	for current := resolveParentSymlinks(metadata); ; current = filepath.Dir(current) {
		if ancestor, err := fsLstat(current); err == nil && os.SameFile(info, ancestor) {
			return true
		}
		if filepath.Dir(current) == current {
			return false
		}
	}
}

// metadataFoldsCase falls back to the ground when no known entry is available inside .git.
func metadataFoldsCase(metadata string) bool {
	for _, name := range [...]string{"HEAD", "config", "objects", "refs"} {
		if folds, known := foldsCase(metadata, name); known {
			return folds
		}
	}
	folds, _ := foldsCase(filepath.Dir(metadata), filepath.Base(metadata))
	return folds
}

func foldsCase(dir, name string) (folds, known bool) {
	original, err := fsLstat(filepath.Join(dir, name))
	if err != nil {
		return false, false
	}
	flipped := strings.ToUpper(name)
	if flipped == name {
		flipped = strings.ToLower(name)
	}
	variant, err := fsLstat(filepath.Join(dir, flipped))
	if err != nil {
		return false, os.IsNotExist(err)
	}
	return os.SameFile(original, variant), true
}
