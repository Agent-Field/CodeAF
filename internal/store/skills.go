package store

import (
	"fmt"
	"os"
	"path/filepath"
)

// SkillsRoot is the user-owned shelf where promoted artifacts live. Keeping
// this path independent of any one database lets every resident thread offer
// the same learned commands without changing the headless no-store path.
func SkillsRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve skill home: %w", err)
	}
	if home == "" {
		return "", fmt.Errorf("resolve skill home: empty home directory")
	}
	return filepath.Join(home, ".aforge", "skills"), nil
}

func SkillsBinDir() (string, error) {
	root, err := SkillsRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "bin"), nil
}
