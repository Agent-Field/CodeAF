package taskcopy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/mirror"
)

// Carry is the mechanism: [Carry.Compose] on the seal side, [Carry.Restore] on
// the take side. Compose needs nothing, so the zero value composes; Restore
// needs the [Cutter] that makes copies.
type Carry struct{ Cutter Cutter }

// Compose writes what every live task copy of c holds beyond its last commit
// into the cell's .cell/trees/. It is one of the things a seal composes before
// the engine captures the tree, and it is exact: a copy that is gone (its task
// finished and its folder was removed) leaves nothing behind, and a copy with no
// edits leaves only its record.
func (Carry) Compose(c cell.Cell) error {
	live := liveCopies(liveRoot(c))
	if err := dropCarriedExcept(carriedRoot(c), live); err != nil {
		return err
	}
	for _, tree := range live {
		if err := carry(tree, filepath.Join(carriedRoot(c), filepath.Base(tree))); err != nil {
			return fmt.Errorf("carry task copy %s: %w", filepath.Base(tree), err)
		}
	}
	return noteLeftOut(c, live)
}

// liveCopies is the folders under root that are git working trees, which is what
// a task's copy is whether it was cut as a worktree or forked with a repository
// of its own. A folder that is neither is not a task's copy: it has no branch to
// cut again.
func liveCopies(root string) []string {
	entries, _ := os.ReadDir(root)
	var out []string
	for _, e := range entries {
		if dir := filepath.Join(root, e.Name()); e.IsDir() && isRepository(dir) {
			out = append(out, dir)
		}
	}
	return out
}

// dropCarriedExcept removes the carried copies whose task copy is no longer
// live, so a finished task's edits do not travel forever.
func dropCarriedExcept(root string, live []string) error {
	keep := map[string]bool{}
	for _, tree := range live {
		keep[filepath.Base(tree)] = true
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !keep[e.Name()] {
			if err := os.RemoveAll(filepath.Join(root, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// carry writes one copy's record and changed files into dest.
func carry(tree, dest string) error {
	head, err := git(tree, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	changed, err := changedPaths(tree)
	if err != nil {
		return err
	}
	present, deleted := partition(tree, withoutSecrets(tree, changed))
	if _, err := mirror.Sync(tree, filepath.Join(dest, filesDir), present); err != nil {
		return err
	}
	branch := branchOf(tree)
	if err := carryCommits(tree, branch, strings.TrimSpace(head), dest); err != nil {
		return err
	}
	return writeRecord(dest, record{Branch: branch, Head: strings.TrimSpace(head), Deleted: deleted, Linked: isLinkedWorktree(tree), Modes: modesOfTree(tree)})
}

// carryCommits keeps dest's bundle of the commits only this copy holds in step
// with the copy's head. A head that has not moved since the last seal leaves the
// bundle untouched, so an idle task adds nothing to what the seal uploads.
func carryCommits(tree, branch, head, dest string) error {
	if prior, err := readRecord(dest); err == nil && prior.Head == head {
		return nil
	}
	path := filepath.Join(dest, bundleName)
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	_, err := bundleOwnCommits(tree, branch, path)
	return err
}

// branchOf is the branch a copy is on, and empty for a detached head, which a
// restore then cuts at the commit alone.
func branchOf(tree string) string {
	out, err := git(tree, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// changedPaths is every path whose content differs from the copy's last commit:
// modified, added, deleted and untracked. What the copy's own .gitignore names
// is never listed, which is what keeps build output and .env files out. Renames
// are reported as a delete and an add, so each path stands alone. The harness's
// own droppings (job logs and the like) are machinery and never listed.
func changedPaths(tree string) ([]string, error) {
	out, err := git(tree, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range strings.Split(out, "\x00") {
		// An entry is two status letters, a space and the path.
		if len(entry) > 3 && !cell.IsTaskDropping(entry[3:]) {
			paths = append(paths, entry[3:])
		}
	}
	return paths, nil
}

// withoutSecrets drops the paths whose files look like they hold a secret. The
// seal's own screen reads the workspace, not the task copies, so a copy's files
// are screened here by the same scanner before they enter the cell.
func withoutSecrets(tree string, paths []string) []string {
	held := map[string]bool{}
	for _, f := range (keys.Scanner{}).Paths(tree, paths) {
		held[f.Path] = true
	}
	var out []string
	for _, p := range paths {
		if !held[p] {
			out = append(out, p)
		}
	}
	return out
}

// partition splits paths into the regular files the copy holds and the paths it
// no longer has. Anything else at a path (a symlink, a folder) is neither: it
// is left where it is, because a file cannot stand for it.
func partition(tree string, paths []string) (present, deleted []string) {
	for _, p := range paths {
		info, err := os.Lstat(filepath.Join(tree, p))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			deleted = append(deleted, p)
		case err == nil && info.Mode().IsRegular():
			present = append(present, p)
		}
	}
	return present, deleted
}

// modesOfTree is the permission bits of every regular file of the copy, whichever
// step put it there: a checkout, the carried edits, or the copy of an ignored
// file. Git records only whether a file is executable, so a checkout leaves the
// rest to the umask of the machine, and a copy is only the same copy when each
// file has the bits it left with. The repository's own folder is not part of it.
func modesOfTree(tree string) map[string]fs.FileMode {
	modes := map[string]fs.FileMode{}
	_ = filepath.WalkDir(tree, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Name() == ".git" {
			return skipEntry(d)
		}
		if info, err := d.Info(); err == nil && info.Mode().IsRegular() {
			rel, _ := filepath.Rel(tree, path)
			modes[filepath.ToSlash(rel)] = info.Mode().Perm()
		}
		return nil
	})
	return modes
}

// skipEntry leaves out a folder with everything in it, and a file alone.
func skipEntry(d fs.DirEntry) error {
	if d.IsDir() {
		return filepath.SkipDir
	}
	return nil
}
