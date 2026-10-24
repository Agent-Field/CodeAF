package cell

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// copyCheckpoint is a process-death seam; production never pauses construction.
var copyCheckpoint = func(string) {}

// CopyOfferedPath is local bookkeeping inside the harness-owned state area.
const CopyOfferedPath = StateDir + "/copy-offered"

// CopyRoot gives an older conversation one stable destination outside the
// folders released builds scan. Its identity never depends on a retry (C29.6).
func CopyRoot(original string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(original)))
	var raw [16]byte
	copy(raw[:], sum[:16])
	return filepath.Join(filepath.Dir(original), ".moving", encode128(raw))
}

// Copied resolves only a fully prepared, durable copy. A crash before the
// completion marker leaves the original as the only visible conversation.
func Copied(original string) string {
	root := CopyRoot(original)
	if _, err := os.Stat(filepath.Join(root, StateDir, "copy-ready")); err == nil {
		if _, err := OpenAt(root, filepath.Base(root)); err == nil {
			return root
		}
	}
	return original
}

// CopyLegacy makes a private byte copy and invokes prepare before publishing
// it. It never links files to the original: later writes must stay independent.
// The lock lives outside the original and serializes windows and crash retries.
func CopyLegacy(ctx context.Context, original string, prepare func(context.Context, Cell) error) (Cell, error) {
	original = filepath.Clean(original)
	info, err := os.Lstat(original)
	if err != nil {
		return Cell{}, err
	}
	if !info.IsDir() {
		return Cell{}, errors.New("older chat folder is not a directory")
	}
	root := CopyRoot(original)
	if err := os.MkdirAll(filepath.Dir(root), 0700); err != nil {
		return Cell{}, err
	}
	lock, err := os.OpenFile(root+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return Cell{}, err
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, true); err != nil {
		return Cell{}, err
	}
	if Copied(original) != original {
		return OpenAt(root, filepath.Base(root))
	}
	if err := ctx.Err(); err != nil {
		return Cell{}, err
	}
	if _, err := os.Stat(filepath.Join(original, legacyTranscript)); err != nil {
		return Cell{}, err
	}
	if isCell(original) {
		return Cell{}, errors.New("this chat already moves")
	}
	if err := os.RemoveAll(root); err != nil {
		return Cell{}, err
	}
	source, err := os.Open(filepath.Join(original, legacyTranscript))
	if err != nil {
		return Cell{}, err
	}
	defer source.Close()
	if err := filelock.Lock(source, true, true); err != nil {
		return Cell{}, err
	}
	copyCheckpoint("source-locked")
	if err := copyFiles(ctx, original, root); err != nil {
		return Cell{}, err
	}
	copyCheckpoint("copied")
	// Migration operates solely on the independent copy. No source inode is
	// shared, even for files the migration itself links inside that copy.
	if err := MigrateLegacy(root); err != nil {
		return Cell{}, err
	}
	copyCheckpoint("migrated")
	c, err := OpenAt(root, filepath.Base(root))
	if err != nil {
		return Cell{}, err
	}
	if err := prepare(ctx, c); err != nil {
		return Cell{}, err
	}
	copyCheckpoint("prepared")
	if err := ctx.Err(); err != nil {
		return Cell{}, err
	}
	f, err := os.OpenFile(filepath.Join(root, StateDir, "copy-ready"), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return Cell{}, err
	}
	if err := errors.Join(f.Sync(), f.Close()); err != nil {
		return Cell{}, err
	}
	copyCheckpoint("published")
	return c, nil
}

// copyFiles copies regular files and symlinks without following links outside
// the source. File modes and times are preserved; sockets and locks are local.
func copyFiles(ctx context.Context, source, dest string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		out := filepath.Join(dest, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(out, info.Mode().Perm())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(target, out)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		f, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		copyCheckpoint("file-created")
		_, err = io.Copy(f, in)
		copyCheckpoint("file-written")
		if err = errors.Join(err, f.Sync(), f.Close()); err != nil {
			return err
		}
		return os.Chtimes(out, info.ModTime(), info.ModTime())
	})
}
