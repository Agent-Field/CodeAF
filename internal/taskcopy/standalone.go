package taskcopy

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// prepareResultCheckout reconstructs a carried commit in an owned destination.
// A plain borrowed project remains plain: its independent mirror must carry a
// complete bundle. Missing prerequisites fail without inventing a new baseline.
func prepareResultCheckout(project, destination, from string, rec record) error {
	if decoded, err := hex.DecodeString(rec.Head); err != nil || (len(decoded) != 20 && len(decoded) != 32) {
		return errors.New("carried task commit identity is malformed")
	}
	if project != "" && isRepository(project) {
		if _, err := git(filepath.Dir(project), "clone", "-q", "--no-checkout", "--no-hardlinks", "--", project, destination); err != nil {
			return err
		}
	} else {
		if _, err := git(filepath.Dir(destination), "init", "-q", destination); err != nil {
			return err
		}
	}
	if err := unbundle(destination, filepath.Join(from, bundleName)); err != nil {
		return fmt.Errorf("restore carried task history (its original archive is preserved): %w", err)
	}
	if _, err := git(destination, "-c", "core.hooksPath=/dev/null", "checkout", "-q", "--force", "--detach", rec.Head, "--"); err != nil {
		return fmt.Errorf("carried task history lacks its recorded commit (its original archive is preserved): %w", err)
	}
	return nil
}

func restoreStandaloneCopy(project, destination, from string, rec record) error {
	if entries, err := os.ReadDir(destination); err == nil && len(entries) != 0 {
		return errors.New("restored task folder already contains files; its files and carried archive were preserved")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	scratch, err := os.MkdirTemp(filepath.Dir(destination), ".restore-task-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	checkout := filepath.Join(scratch, "copy")
	if err := prepareResultCheckout(project, checkout, from, rec); err != nil {
		return err
	}
	if rec.Branch != "" {
		if _, err := git(checkout, "check-ref-format", "--branch", rec.Branch); err != nil {
			return err
		}
		if _, err := git(checkout, "-c", "core.hooksPath=/dev/null", "checkout", "-q", "-b", rec.Branch, rec.Head); err != nil {
			return err
		}
	}
	if _, err := os.Stat(destination); err == nil {
		// Only an empty owned destination is removed, never an existing copy's files.
		if err := os.Remove(destination); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(checkout, destination)
}
