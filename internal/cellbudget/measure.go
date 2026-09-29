package cellbudget

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// kept is what eviction leaves in the cell's folder: the cell's own state and
// the markers the engine needs to find its store again.
var kept = map[string]bool{cell.StateDir: true, ".git": true, ".furrow": true}

// removable lists the top-level entries eviction would remove.
func removable(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !kept[e.Name()] {
			out = append(out, filepath.Join(root, e.Name()))
		}
	}
	return out, nil
}

// workingSize is the bytes eviction would free.
func workingSize(root string) (int64, error) {
	paths, err := removable(root)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, p := range paths {
		n, err := treeSize(p)
		if err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}

func treeSize(root string) (n int64, err error) {
	err = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err == nil {
			n += info.Size()
		}
		return err
	})
	return n, err
}
