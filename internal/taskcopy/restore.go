package taskcopy

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// Restore puts the task copies a takeover carried back to work on this machine
// and answers the names it restored. project is the repository the copies were
// cut from, already restored by the takeover.
//
// It first forgets every registration in the project whose folder is not on this
// machine, which is what the first machine's absolute paths are here. A copy is
// then cut again at this chat's own trees/ on the branch it was on, and the
// carried files are laid over it. A copy that cannot be cut again still gets
// its files, in a plain folder at its path, so the edits are found where the
// task worked; the error says why it is not a worktree.
func (Carry) Restore(c cell.Cell, project string) ([]string, error) {
	var errs []error
	if isRepository(project) {
		_, err := git(project, "worktree", "prune")
		errs = append(errs, err)
	}
	var restored []string
	for _, name := range Carried(c) {
		if err := restoreOne(c, project, name); err != nil {
			errs = append(errs, fmt.Errorf("task copy %s: %w", name, err))
			continue
		}
		restored = append(restored, name)
	}
	return restored, errors.Join(errs...)
}

func restoreOne(c cell.Cell, project, name string) error {
	from := filepath.Join(carriedRoot(c), name)
	rec, err := readRecord(from)
	if err != nil {
		return err
	}
	dest := filepath.Join(liveRoot(c), name)
	cutErr := cutAgain(project, dest, rec)
	return errors.Join(cutErr, overlay(filepath.Join(from, filesDir), dest, rec.Deleted))
}

// cutAgain makes dest a worktree of project at the commit the copy was at. A
// dest that already is one (the chat came back to the machine it never left)
// is left as it is.
func cutAgain(project, dest string, rec record) error {
	if isLinkedWorktree(dest) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	_, err := git(project, addArgs(project, dest, rec)...)
	return err
}

// addArgs is the worktree add that puts a copy on its own branch when this
// repository still has it, and on its commit alone when it does not.
func addArgs(project, dest string, rec record) []string {
	if rec.Branch != "" {
		if _, err := git(project, "show-ref", "--verify", "--quiet", "refs/heads/"+rec.Branch); err == nil {
			return []string{"worktree", "add", dest, rec.Branch}
		}
	}
	return []string{"worktree", "add", "--detach", dest, rec.Head}
}

// overlay lays the carried files over dest and removes the files the copy had
// deleted. A deleted path that would leave dest is refused, since a record that
// arrives over a network is not trusted with the rest of the disk.
func overlay(files, dest string, deleted []string) error {
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
		return copyFile(path, filepath.Join(dest, rel), info)
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return removeAll(dest, deleted)
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

// copyFile writes to with the bytes, permission and modified time of from, whose
// stat is info. Keeping the time is what lets [copyIfChanged] recognise a file
// it already carried.
func copyFile(from, to string, info fs.FileInfo) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return err
	}
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
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
	return os.Chtimes(to, info.ModTime(), info.ModTime())
}
