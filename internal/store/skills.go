package store

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// SkillsRoot is the user-owned shelf where promoted artifacts live. Keeping
// this path independent of any one database lets every resident thread offer
// the same learned commands without changing the headless no-store path.
func SkillsRoot() (string, error) {
	root := home.Join("skills")
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("resolve skill home: empty home directory")
	}
	return root, nil
}

func SkillsBinDir() (string, error) {
	root, err := SkillsRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "bin"), nil
}
