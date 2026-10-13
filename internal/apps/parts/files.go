package parts

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func validRelative(path string) bool {
	if path == "" || filepath.IsAbs(path) || strings.ContainsAny(path, "\\\x00:*?") {
		return false
	}
	for _, p := range strings.Split(path, "/") {
		if p == "" || p == "." || p == ".." {
			return false
		}
	}
	return true
}
func validPart(part string) error {
	if !validRelative(part) || filepath.Base(part) == "doc.json" {
		return invalid("part must be a relative content path, without .. or doc.json")
	}
	return nil
}

// Symlinks are rejected even when their current target stays inside the root.
func safePath(root, relative string) (string, error) {
	if !validRelative(relative) {
		return "", invalid("path must be relative and cannot contain ..")
	}
	path := filepath.Clean(root)
	if err := noSymlink(path); err != nil {
		return "", err
	}
	for _, p := range strings.Split(relative, "/") {
		path = filepath.Join(path, p)
		if err := noSymlink(path); err != nil {
			return "", err
		}
	}
	return path, nil
}
func noSymlink(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return invalid(fmt.Sprintf("symlink is not allowed: %s", path))
	}
	return nil
}
func atomicFile(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".part-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if _, err = f.Write(body); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temp, path)
}
func (c *session) change(path string, body []byte, remove bool) error {
	old, err := os.ReadFile(path)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	c.changes = append(c.changes, fileChange{path, old, exists})
	if remove {
		err = os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return atomicFile(path, body)
}
func (c *session) undo() error {
	var result error
	for i := len(c.changes) - 1; i >= 0; i-- {
		f := c.changes[i]
		if f.existed {
			result = errors.Join(result, atomicFile(f.path, f.old))
			continue
		}
		err := os.Remove(f.path)
		if !os.IsNotExist(err) {
			result = errors.Join(result, err)
		}
	}
	c.changes = nil
	return result
}
