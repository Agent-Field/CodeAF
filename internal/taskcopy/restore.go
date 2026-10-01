package taskcopy

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// Cutter makes a task copy the way a task makes one. Restore asks it instead of
// cutting a copy itself, so a copy that comes back is made by the very road that
// made it and cannot drift from it: a fork with a repository of its own where
// the task would have got one, a linked worktree where it would not.
type Cutter interface {
	// Cut makes dest a copy of project as the spec says, and answers why it
	// could not.
	Cut(project, dest string, spec Spec) error
}

// Spec is the copy to cut: a new branch of the given name whose tip is the
// commit At, as a linked worktree when Linked and otherwise as the cutter's
// first choice, which may be a fork with a repository of its own.
type Spec struct {
	Branch, At string
	Linked     bool
}

// Restore puts the task copies a takeover carried back to work on this machine
// and answers the names it restored. project is the repository the copies were
// cut from, already restored by the takeover.
//
// It first forgets every registration in the project whose folder is not on this
// machine, which is what the first machine's absolute paths are here. A copy is
// then cut again at this chat's own trees/ on the branch it was on, and the
// carried files are laid over it. A copy that cannot be cut again still gets
// its files, in a plain folder at its path, so the edits are found where the
// task worked; the error says why it is not a working tree.
func (k Carry) Restore(c cell.Cell, project string) ([]string, error) {
	var errs []error
	if isRepository(project) {
		_, err := git(project, "worktree", "prune")
		errs = append(errs, err)
	}
	var restored []string
	for _, name := range Carried(c) {
		if err := k.restoreOne(c, project, name); err != nil {
			errs = append(errs, fmt.Errorf("task copy %s: %w", name, err))
			continue
		}
		restored = append(restored, name)
	}
	return restored, errors.Join(errs...)
}

func (k Carry) restoreOne(c cell.Cell, project, name string) error {
	from := filepath.Join(carriedRoot(c), name)
	rec, err := readRecord(from)
	if err != nil {
		return err
	}
	dest := filepath.Join(liveRoot(c), name)
	cutErr := k.cutAgain(project, dest, name, from, rec)
	return errors.Join(cutErr, overlay(filepath.Join(from, filesDir), dest, rec), applyModes(dest, rec.Modes))
}

// applyModes gives each file of the copy the bits it left with, including the
// files the checkout wrote, which took the umask of this machine. A path the
// copy does not have is skipped, and one that would leave dest is refused,
// since a record that arrives over a network is not trusted with the rest of
// the disk.
func applyModes(dest string, modes map[string]fs.FileMode) error {
	var errs []error
	for rel, mode := range modes {
		if !filepath.IsLocal(filepath.FromSlash(rel)) {
			errs = append(errs, fmt.Errorf("carried mode for %q leaves the task copy", rel))
			continue
		}
		if err := os.Chmod(filepath.Join(dest, filepath.FromSlash(rel)), mode); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// cutAgain makes dest a copy of project at the commit the copy was at. A dest
// that already is one (the chat came back to the machine it never left) is left
// as it is.
//
// The copy's own commits are put into the project's object store first, since a
// fork kept them in a repository the seal did not capture. The branch name is
// then freed if the project still holds it at that same commit, because the
// cutter makes its branch new, and a branch at any other commit is left alone
// and reported by the cutter rather than moved. A branch the project held and
// the cut did not make again in the project is put back after it.
func (k Carry) cutAgain(project, dest, name, from string, rec record) error {
	if isRepository(dest) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	if err := unbundle(project, filepath.Join(from, bundleName)); err != nil {
		return err
	}
	branch := branchFor(rec, name)
	kept := holdsBranch(project, branch, rec.Head)
	_, _ = git(project, "update-ref", "-d", "refs/heads/"+branch, rec.Head)
	cutErr := k.Cutter.Cut(project, dest, Spec{Branch: branch, At: rec.Head, Linked: rec.Linked})
	return errors.Join(cutErr, keepBranch(project, branch, rec.Head, kept))
}

// holdsBranch says whether the project has the branch at exactly the commit.
func holdsBranch(project, branch, commit string) bool {
	out, err := git(project, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil && strings.TrimSpace(out) == commit
}

// keepBranch gives the project back a branch it held before the copy was cut.
// A copy that is a fork makes its branch in a repository of its own, so freeing
// the name for the cutter would otherwise leave the project with the commit and
// no ref: a kept task's record unreferenced, for the next garbage collection to
// drop. A branch the cutter made in the project already stands at the commit
// and is left as it is.
func keepBranch(project, branch, commit string, held bool) error {
	if !held || holdsBranch(project, branch, commit) {
		return nil
	}
	_, err := git(project, "update-ref", "refs/heads/"+branch, commit)
	return err
}

// branchFor is the branch a restored copy is cut on: the one it was on, or for a
// copy that was on a detached head a branch named for the copy, because a task's
// road always cuts a branch.
func branchFor(rec record, name string) string {
	if rec.Branch != "" {
		return rec.Branch
	}
	return "restored/" + name
}

// unbundle writes the commits of a carried bundle into project's object store.
// A copy that made no commits of its own carries no bundle and needs nothing.
func unbundle(project, path string) error {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	_, err := git(project, "bundle", "unbundle", path)
	return err
}

// overlay lays the carried files over dest, each at the mode the record kept,
// and removes the files the copy had deleted. A deleted path that would leave dest is refused, since a record that
// arrives over a network is not trusted with the rest of the disk.
func overlay(files, dest string, rec record) error {
	err := filepath.WalkDir(files, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(files, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(path, filepath.Join(dest, rel), info, rec.modeOf(filepath.ToSlash(rel), info))
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return removeAll(dest, rec.Deleted)
}

func removeAll(dest string, rels []string) error {
	for _, rel := range rels {
		if !filepath.IsLocal(rel) {
			return fmt.Errorf("carried path %q leaves the task copy", rel)
		}
		if err := os.Remove(filepath.Join(dest, rel)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// copyFile writes to with the bytes and modified time of from, whose stat is
// info, and exactly the permission bits in mode. The mode is set after the write
// because a mode given to the create is cut down by the umask of the machine,
// and a copy that comes back with other bits than it left with is not the same
// copy. Keeping the time is what lets [copyIfChanged] recognise a file it
// already carried.
func copyFile(from, to string, info fs.FileInfo, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return err
	}
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return err
	}
	if err := dst.Close(); err != nil {
		return err
	}
	if err := os.Chmod(to, mode); err != nil {
		return err
	}
	return os.Chtimes(to, info.ModTime(), info.ModTime())
}
