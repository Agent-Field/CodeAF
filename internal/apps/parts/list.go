package parts

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

func (c *session) documentIDs() ([]string, error) {
	roots := []string{c.root}
	if c.realRoot != "" {
		roots = append(roots, c.realRoot)
	}
	seen := map[string]bool{}
	for _, root := range roots {
		if err := scanDocuments(root, seen); err != nil {
			return nil, err
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids, nil
}

func scanDocuments(root string, seen map[string]bool) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if os.IsNotExist(err) && path == root {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return invalid("symlinks are not allowed in content")
		}
		if entry.IsDir() || entry.Name() != "doc.json" {
			return nil
		}
		relative, err := filepath.Rel(root, filepath.Dir(path))
		if err == nil {
			seen[filepath.ToSlash(relative)] = true
		}
		return err
	})
}
