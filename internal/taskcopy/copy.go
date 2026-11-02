package taskcopy

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/fsreplace"
)

// CaptureCopy uses the continue-here carrier for one task without needing the
// chat or the source project on the receiving computer. The full reachable
// history makes a linked worktree independent of its original repository.
func CaptureCopy(tree, carrier string) error {
	if err := carryWithHistory(tree, carrier, true); err != nil {
		return err
	}
	dirs, err := captureWorkspaceDirectories(tree)
	if err != nil {
		return err
	}
	return writeWorkspaceJSON(filepath.Join(carrier, "directories.json"), dirs)
}

// ImportCopyHistory runs before the last conflict check because a large fetch
// must not provide a window in which a local edit can be overwritten.
func ImportCopyHistory(original, prepared string) error {
	info, err := os.Lstat(filepath.Join(original, ".git"))
	if err != nil {
		return err
	}
	if info.IsDir() {
		return nil
	}
	if !info.Mode().IsRegular() {
		return errors.New("the task copy's repository link is not a file")
	}
	_, err = git(original, "-c", "core.hooksPath=/dev/null", "fetch", "--quiet", "--no-tags", "--no-write-fetch-head", prepared, "HEAD")
	return err
}

// BindLinkedResult preserves the origin's worktree registration after the
// returned commits have been imported. Normal landing expects
// that task branch to live in the ground repository on this copy rung.
// The caller must run undo if it cannot install prepared over original.
func BindLinkedResult(original, prepared string) (undo func() error, err error) {
	info, err := os.Lstat(filepath.Join(original, ".git"))
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return func() error { return nil }, nil
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("the task copy's repository link is not a file")
	}
	pointer, err := os.ReadFile(filepath.Join(original, ".git"))
	if err != nil {
		return nil, err
	}
	oldHead, err := git(original, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	newHead, err := git(prepared, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	gitDir, err := git(original, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil, err
	}
	index := filepath.Join(strings.TrimSpace(gitDir), "index")
	oldIndex, err := os.ReadFile(index)
	if err != nil {
		return nil, err
	}
	if err := os.RemoveAll(filepath.Join(prepared, ".git")); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(prepared, ".git"), pointer, info.Mode().Perm()); err != nil {
		return nil, err
	}
	oldHead, newHead = strings.TrimSpace(oldHead), strings.TrimSpace(newHead)
	if _, err := git(original, "update-ref", "HEAD", newHead, oldHead); err != nil {
		return nil, err
	}
	undo = func() error {
		if _, err := git(original, "update-ref", "HEAD", oldHead, newHead); err != nil {
			return err
		}
		return replaceCopyIndex(index, oldIndex)
	}
	// Git builds the receiving index itself because a copied split index
	// could refer to shared-index files owned by the disposable repository.
	if _, err := git(original, "read-tree", "--reset", newHead); err != nil {
		return nil, errors.Join(err, undo())
	}
	return undo, nil
}

func replaceCopyIndex(path string, raw []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".task-index-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(raw)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	return fsreplace.Rename(file.Name(), path)
}

// RestoreCopy validates the complete carrier before creating an independent
// repository. Its destination must be absent or empty; replacing an existing
// copy is the caller's responsibility after its conflict check.
func RestoreCopy(carrier, destination string) error {
	rec, err := readWorkspaceRecord(carrier)
	if err != nil {
		return err
	}
	if !rec.FullHistory {
		return errors.New("the task copy is missing its independent history")
	}
	if err := validateWorkspaceRecord(carrier, rec); err != nil {
		return err
	}
	if err := screenFullHistory(filepath.Join(carrier, bundleName)); err != nil {
		return err
	}
	var root workspaceRoot
	root.DirectoryVersion = 2
	if rec.Symlinks == nil {
		root.DirectoryVersion = 1
	}
	raw, err := readWorkspaceFile(filepath.Join(carrier, "directories.json"), workspaceManifestLimit)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &root.Directories); err != nil {
		return err
	}
	if _, err := validateWorkspaceDirectories(root, rec); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	if err := restoreStandaloneCopy("", destination, carrier, rec); err != nil {
		return err
	}
	if err := validateWorkspaceOverlay(destination, carrier, rec); err != nil {
		return err
	}
	if err := prepareWorkspaceDirectories(destination, root.Directories); err != nil {
		return err
	}
	if err := errors.Join(applyEOL(destination, rec.EOL), overlay(filepath.Join(carrier, filesDir), destination, rec)); err != nil {
		return err
	}
	if err := applyModes(destination, rec.Modes); err != nil {
		return err
	}
	if err := restorePortableBaseline(destination, rec); err != nil {
		return err
	}
	return finishWorkspaceDirectories(destination, root.Directories)
}
