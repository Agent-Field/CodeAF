package taskcopy

import (
	"errors"
	"os"
	"path/filepath"
)

const independentBaselineFile = "codeaf-independent-baseline"

// MarkIndependentBaseline records the harness-created baseline of an owned
// mirror. Its original plain folder cannot supply Git prerequisites on relay.
// Callers use this only while creating their private mirror repository.
func MarkIndependentBaseline(tree string) error {
	info, err := os.Lstat(filepath.Join(tree, ".git"))
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("independent baseline requires its own repository")
	}
	return os.WriteFile(filepath.Join(tree, ".git", independentBaselineFile), []byte("independent\n"), 0600)
}

func independentBaseline(tree string) bool {
	info, err := os.Lstat(filepath.Join(tree, ".git"))
	if err != nil || !info.IsDir() {
		return false
	}
	marker, err := os.ReadFile(filepath.Join(tree, ".git", independentBaselineFile))
	return err == nil && string(marker) == "independent\n"
}
