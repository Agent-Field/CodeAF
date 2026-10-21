package cell

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// Tree carries whole directories, one file at a time, on the same terms as
// [Files]: each file is hard-linked into .cell so both names are one file, and
// the legacy name goes once .cell holds it. A file a window creates in the
// legacy directory after the link is adopted by rename when the folder is
// cleared, so no crash point loses one. The legacy directory goes when it is
// empty. A directory the folder does not have is skipped.
func Tree(names ...string) Carrier { return trees(names) }

type trees []string

func (t trees) Stage(dir string, c Cell) error {
	return t.each(dir, func(rel string) error {
		to := filepath.Join(c.Root, StateDir, rel)
		if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
			return err
		}
		return os.Link(filepath.Join(dir, rel), to)
	})
}

func (t trees) Clear(dir string) error {
	if err := t.each(dir, func(rel string) error { return dropLegacyName(dir, rel) }); err != nil {
		return err
	}
	for _, name := range t {
		if err := removeEmptyTree(filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	return nil
}

// each calls visit with the path, relative to dir, of every regular file below
// each named directory.
func (t trees) each(dir string, visit func(rel string) error) error {
	for _, name := range t {
		err := filepath.WalkDir(filepath.Join(dir, name), func(path string, d fs.DirEntry, err error) error {
			if err != nil || !d.Type().IsRegular() {
				return err
			}
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			return visit(rel)
		})
		if err := ignoreMissing(err); err != nil {
			return err
		}
	}
	return nil
}

// removeEmptyTree removes root and every directory below it that holds nothing,
// deepest first, and leaves the rest.
func removeEmptyTree(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return ignoreMissing(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			if err := removeEmptyTree(filepath.Join(root, e.Name())); err != nil {
				return err
			}
		}
	}
	err = os.Remove(root)
	if errors.Is(err, syscall.ENOTEMPTY) {
		return nil
	}
	return ignoreMissing(err)
}
