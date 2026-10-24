package exec

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ContributingReadLimit bounds the policy prefix read before a commit is signed.
const ContributingReadLimit = 256 << 10

// RepositoryRefusesTrailers reads a checkout root's own attribution policy.
// Only regular CONTRIBUTING files in the root, .github or docs are read, so
// the factory and chat landing paths obey the same exception to signing.
func RepositoryRefusesTrailers(root string) bool {
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
			content, err := io.ReadAll(io.LimitReader(file, ContributingReadLimit))
			file.Close()
			if err == nil && ContributingRefusesTrailers(string(content)) {
				return true
			}
		}
	}
	return false
}
