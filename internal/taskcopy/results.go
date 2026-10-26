package taskcopy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/mirror"
)

// ResultSelection names a settled task's recorded outputs. Empty Paths means no
// identified output files; its original archive remains available independently.
type ResultSelection struct {
	ID    string
	Paths []string
}

// RestoreCompletedResults opens recorded outputs as ordinary historical files,
// never working copies. The caller selects only settled tasks. Git reconstruction
// is temporary; unchanged project inputs and execution metadata are not retained.
func RestoreCompletedResults(c cell.Cell, project, destination string, selections []ResultSelection) (map[string]string, error) {
	return RestoreCompletedResultsWithParents(c, project, destination, selections, nil)
}

// RestoreCompletedResultsWithParents reconstructs prerequisite commit history
// only in temporary scratch. It creates no runnable parent or child worktree.
func RestoreCompletedResultsWithParents(c cell.Cell, project, destination string, selections []ResultSelection, parents map[string]string) (map[string]string, error) {
	if _, err := restorationOrder(Carried(c), parents); err != nil {
		return nil, err
	}
	results := make(map[string]string)
	for _, selection := range selections {
		if selection.ID == "." || !filepath.IsLocal(selection.ID) || filepath.Base(selection.ID) != selection.ID {
			return nil, errors.New("historical task ID leaves its folder")
		}
		if len(selection.Paths) == 0 {
			continue
		}
		from := filepath.Join(carriedRoot(c), selection.ID)
		record, err := readRecord(from)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		path, err := restoreSelectedResult(c, project, destination, selection, from, record, parents)
		if err != nil {
			return nil, fmt.Errorf("historical task %s: %w", selection.ID, err)
		}
		results[selection.ID] = path
	}
	return results, nil
}

func restoreSelectedResult(c cell.Cell, project, destination string, selection ResultSelection, from string, record record, parents map[string]string) (string, error) {
	if err := os.MkdirAll(destination, 0700); err != nil {
		return "", err
	}
	scratch, err := os.MkdirTemp(destination, ".restore-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(scratch)
	project, err = historicalParentCheckout(c, project, selection.ID, parents, scratch)
	if err != nil {
		return "", err
	}
	checkout := filepath.Join(scratch, "checkout")
	if err := prepareResultCheckout(project, checkout, from, record); err != nil {
		return "", err
	}
	if err := applyEOL(checkout, record.EOL); err != nil {
		return "", err
	}
	output := filepath.Join(scratch, "output")
	if err := os.Mkdir(output, 0700); err != nil {
		return "", err
	}
	for _, relative := range selection.Paths {
		if err := copySelectedResult(checkout, from, output, relative, record); err != nil {
			return "", err
		}
	}
	final := filepath.Join(destination, selection.ID)
	if _, err := os.Lstat(final); err == nil {
		return final, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if err := os.Rename(output, final); err != nil {
		return "", err
	}
	return final, nil
}

func copySelectedResult(checkout, from, output, relative string, record record) error {
	if !filepath.IsLocal(relative) {
		return fmt.Errorf("historical output %q leaves its folder", relative)
	}
	if relative == ".git" || strings.HasPrefix(filepath.ToSlash(relative), ".git/") {
		return errors.New("Git metadata is not an output")
	}
	for _, deleted := range record.Deleted {
		if relative == deleted {
			return nil
		}
	}
	if record.Symlinks != nil {
		if entry, ok := record.Symlinks.Entries[filepath.ToSlash(relative)]; ok {
			return restorePortableLinks(output, &PortableState{Version: 1, Entries: map[string]PortableEntry{filepath.ToSlash(relative): entry}})
		}
	}
	source := filepath.Join(from, filesDir, relative)
	if _, err := os.Lstat(source); errors.Is(err, fs.ErrNotExist) {
		source = filepath.Join(checkout, relative)
	} else if err != nil {
		return err
	}
	info, err := os.Lstat(source)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if _, err := workspacePath(checkout, filepath.Dir(relative)); err != nil {
			return err
		}
		entry, err := inspectPortableEntry(source, info)
		if err != nil {
			return err
		}
		return restorePortableLinks(output, &PortableState{Version: 1, Entries: map[string]PortableEntry{filepath.ToSlash(relative): entry}})
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("historical output %q is not a regular file; its archive is preserved", relative)
	}
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil {
		return err
	}
	root := checkout
	if rel, err := filepath.Rel(filepath.Join(from, filesDir), source); err == nil && filepath.IsLocal(rel) {
		root = filepath.Join(from, filesDir)
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || !filepath.IsLocal(rel) {
		return errors.New("historical output redirects outside its archive")
	}
	return mirror.Copy(source, filepath.Join(output, relative), info, record.modeOf(filepath.ToSlash(relative), info))
}

func historicalParentCheckout(c cell.Cell, project, id string, parents map[string]string, scratch string) (string, error) {
	parent := parents[id]
	if parent == "" {
		return project, nil
	}
	base, err := historicalParentCheckout(c, project, parent, parents, scratch)
	if err != nil {
		return "", err
	}
	from := filepath.Join(carriedRoot(c), parent)
	rec, err := readRecord(from)
	if err != nil {
		return "", err
	}
	checkout := filepath.Join(scratch, "parent-"+parent)
	if err := prepareResultCheckout(base, checkout, from, rec); err != nil {
		return "", err
	}
	return checkout, nil
}
