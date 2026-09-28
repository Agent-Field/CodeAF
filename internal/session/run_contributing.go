package session

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/exec"
)

const contributingReadLimit = 256 << 10

// repositoryRefusesTrailers is the one repository rule every harness-written
// commit obeys. Only regular CONTRIBUTING files in the top level, .github or
// docs are read; a link or device cannot make a landing follow or block. The
// bounded prefix keeps a very large policy file from holding up the landing,
// while still reading an early ban instead of skipping the whole file.
func repositoryRefusesTrailers(dir string) bool {
	root, ok := repositoryRoot(dir)
	if !ok {
		return false
	}
	for _, folder := range []string{root, filepath.Join(root, ".github"), filepath.Join(root, "docs")} {
		entries, err := os.ReadDir(folder)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := strings.ToLower(entry.Name())
			switch name {
			case "contributing", "contributing.md", "contributing.rst", "contributing.txt":
			default:
				continue
			}
			path := filepath.Join(folder, entry.Name())
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			file, err := os.Open(path)
			if err != nil {
				continue
			}
			content, err := io.ReadAll(io.LimitReader(file, contributingReadLimit))
			file.Close()
			if err == nil && exec.ContributingRefusesTrailers(string(content)) {
				return true
			}
		}
	}
	return false
}
