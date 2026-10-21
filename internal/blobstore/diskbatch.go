package blobstore

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// commitBatch is how many files one group commit holds open at once. It is
// small enough to stay far below any file-descriptor limit, and large enough
// that the disk sees one burst of writes followed by one burst of syncs.
const commitBatch = 256

// entry is one file to create: its final path and its whole content.
type entry struct {
	path string
	data []byte
}

// staged is an entry written to a temporary file that is not yet in place.
type staged struct {
	tmp  *os.File
	path string
}

// writeAllOnce durably creates every entry whose path does not exist yet.
// Durability is by group commit: each batch is written to temporary files,
// then all of them are synced, then renamed into place, and each directory
// that gained a name is synced once. A disk that pays a full journal commit
// per fsync therefore pays it once per batch and not once per object, and the
// final names still never hold a partial file.
func (d *Disk) writeAllOnce(entries []entry) error {
	var todo []entry
	for _, e := range entries {
		if _, err := os.Stat(e.path); err != nil {
			todo = append(todo, e)
		}
	}
	for len(todo) > 0 {
		n := min(commitBatch, len(todo))
		if err := d.commit(todo[:n]); err != nil {
			return asFull(err)
		}
		todo = todo[n:]
	}
	return nil
}

// commit makes one batch durable and visible, in that order.
func (d *Disk) commit(batch []entry) error {
	files, err := d.stageAll(batch)
	if err == nil {
		err = d.syncAll(files)
	}
	if err != nil {
		discard(files)
		return err
	}
	if err := place(files); err != nil {
		return err
	}
	return d.syncDirs(files)
}

// stageAll writes every entry to a temporary file beside its final path.
func (d *Disk) stageAll(batch []entry) ([]staged, error) {
	files := make([]staged, 0, len(batch))
	for _, e := range batch {
		f, err := stageOne(e)
		if err != nil {
			return files, err
		}
		files = append(files, f)
	}
	return files, nil
}

func stageOne(e entry) (staged, error) {
	dir := filepath.Dir(e.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return staged{}, fmt.Errorf("blobstore: create directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return staged{}, fmt.Errorf("blobstore: create temporary file: %w", err)
	}
	s := staged{tmp: tmp, path: e.path}
	if _, err := tmp.Write(e.data); err != nil {
		discard([]staged{s})
		return staged{}, fmt.Errorf("blobstore: write %s: %w", filepath.Base(e.path), err)
	}
	return s, nil
}

// syncAll makes every staged file durable before any of them gets its name.
func (d *Disk) syncAll(files []staged) error {
	fds := make([]*os.File, len(files))
	for i, f := range files {
		fds[i] = f.tmp
	}
	if err := d.sync(fds); err != nil {
		return fmt.Errorf("blobstore: sync batch: %w", err)
	}
	return nil
}

// place closes each temporary file and renames it to its final name.
func place(files []staged) error {
	for i, f := range files {
		err := f.tmp.Close()
		if err == nil {
			err = os.Rename(f.tmp.Name(), f.path)
		}
		if err != nil {
			discard(files[i:])
			return fmt.Errorf("blobstore: place %s: %w", filepath.Base(f.path), err)
		}
	}
	return nil
}

// syncDirs makes the renames durable, one sync per distinct directory.
func (d *Disk) syncDirs(files []staged) error {
	dirs, err := openDirs(files)
	defer closeAll(dirs)
	if err != nil {
		return err
	}
	if err := d.sync(dirs); err != nil && !errors.Is(err, fs.ErrInvalid) {
		return fmt.Errorf("blobstore: sync directory: %w", err)
	}
	return nil
}

// openDirs opens each distinct directory that a batch placed a file in.
func openDirs(files []staged) ([]*os.File, error) {
	var dirs []*os.File
	seen := map[string]bool{}
	for _, f := range files {
		name := filepath.Dir(f.path)
		if seen[name] {
			continue
		}
		seen[name] = true
		dir, err := os.Open(name)
		if err != nil {
			return dirs, fmt.Errorf("blobstore: open directory: %w", err)
		}
		dirs = append(dirs, dir)
	}
	return dirs, nil
}

func closeAll(files []*os.File) {
	for _, f := range files {
		f.Close()
	}
}

// discard closes and removes temporary files that will not be used.
func discard(files []staged) {
	for _, f := range files {
		f.tmp.Close()
		os.Remove(f.tmp.Name())
	}
}
