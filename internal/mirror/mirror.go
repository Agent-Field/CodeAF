// Package mirror keeps one folder holding exactly the files chosen from
// another, cheaply enough to run at every seal. A seal carries what lives
// outside the sealed tree (a task's copy, a chat's logs) by mirroring it into
// the cell's own .cell/ folder, and both carriers do it by this one road.
package mirror

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Sync makes dest hold exactly the files rels name, taken from root. A file
// whose size and time already match is left alone, so a seal that changed
// nothing rewrites nothing and the engine's own stat cache stays warm. It
// answers the paths it wrote.
func Sync(root, dest string, rels []string) ([]string, error) {
	if err := dropStale(dest, rels); err != nil {
		return nil, err
	}
	var wrote []string
	for _, rel := range rels {
		changed, err := CopyIfChanged(filepath.Join(root, rel), filepath.Join(dest, rel))
		if err != nil {
			return wrote, err
		}
		if changed {
			wrote = append(wrote, rel)
		}
	}
	return wrote, nil
}

// dropStale removes the files under dest that are not in rels.
func dropStale(dest string, rels []string) error {
	want := map[string]bool{}
	for _, rel := range rels {
		want[filepath.Join(dest, rel)] = true
	}
	err := filepath.WalkDir(dest, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || want[path] {
			return err
		}
		return os.Remove(path)
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// CopyIfChanged copies from over to unless to already has its size and time,
// and says whether it wrote.
func CopyIfChanged(from, to string) (bool, error) {
	src, err := os.Stat(from)
	if err != nil {
		return false, err
	}
	if dst, err := os.Stat(to); err == nil && dst.Size() == src.Size() && dst.ModTime().Equal(src.ModTime()) {
		return false, nil
	}
	return true, Copy(from, to, src, src.Mode().Perm())
}

// Copy writes to with the bytes and modified time of from, whose stat is info,
// and exactly the permission bits in mode. The mode is set after the write
// because a mode given to the create is cut down by the umask of the machine,
// and a copy that comes back with other bits than it left with is not the same
// copy. Keeping the time is what lets [CopyIfChanged] recognise a file it
// already carried.
func Copy(from, to string, info fs.FileInfo, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return err
	}
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return err
	}
	if err := dst.Close(); err != nil {
		return err
	}
	if err := os.Chmod(to, mode); err != nil {
		return err
	}
	return os.Chtimes(to, info.ModTime(), info.ModTime())
}
