package cellstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// chainNames are the entries of .cell/ that a restore never writes: the chain
// describes the past rather than belongs to any turn's content (L12), meta.json
// is the harness's own composed afresh by every seal, and local publication,
// adoption, and legacy-copy markers are written after sealing or materializing
// so snapshots either omit them or hold an earlier turn's stale upload note.
var chainNames = map[string]bool{
	path.Base(TurnsPath):            true,
	path.Base(ReceiptsDir):          true,
	path.Base(BlobsDir):             true,
	path.Base(cell.MetaPath):        true,
	path.Base(publishedPath):        true,
	path.Base(adoptedPath):          true,
	path.Base(cell.CopyReadyPath):   true,
	path.Base(cell.CopyOfferedPath): true,
}

// restoreKeepingChain puts the workspace and the rest of .cell/ (the
// transcript, the environment) back as the snapshot held them, and leaves the
// chain untouched. The engine restores the composed .cell/ into a scratch
// directory instead of the cell's own, and only the entries that are not the
// chain are carried over. Nothing of the chain is ever held in memory or
// written back, so a crash at any point leaves it whole.
func (e Engine) restoreKeepingChain(ctx context.Context, c cell.Cell, snapshot string) error {
	scratch, err := e.scratchDir(c)
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	if err := e.restore(ctx, c, snapshot, nil, scratch); err != nil {
		return err
	}
	return syncState(scratch, stateDir(c))
}

// scratchDir is a fresh directory beside the cell's engine store: device-local,
// and never inside a tree the engine restores.
func (e Engine) scratchDir(c cell.Cell) (string, error) {
	local := e.LocalDir(c)
	if err := os.MkdirAll(local, 0o700); err != nil {
		return "", err
	}
	return os.MkdirTemp(local, "rewind-")
}

// syncState makes the non-chain files under to match those under from: each is
// replaced whole (never half written), and those from does not hold are removed.
func syncState(from, to string) error {
	want, err := stateFiles(from)
	if err != nil {
		return err
	}
	for _, name := range want {
		if err := copyFile(filepath.Join(from, name), filepath.Join(to, name)); err != nil {
			return err
		}
	}
	have, err := stateFiles(to)
	if err != nil {
		return err
	}
	return removeAbsent(to, have, want)
}

// CarriedFile is the stat of one conversation record a take carried over from
// the older copy of the chat that was on this computer, as it stood at the
// moment of the take.
type CarriedFile struct {
	Size  int64 `json:"size"`
	MTime int64 `json:"mtime"`
}

// FingerprintOf is the stat a carried file is later recognised by.
func FingerprintOf(info fs.FileInfo) CarriedFile {
	return CarriedFile{Size: info.Size(), MTime: info.ModTime().UnixNano()}
}

// carriedPath is the device-local note beside the cell's store, never inside any
// tree a restore or a seal reads.
func (e Engine) carriedPath(c cell.Cell) string { return e.LocalDir(c) + ".carried" }

// NoteCarried records which files the take just installed came from the older
// copy this computer held (name is relative to .cell/, slash separated), so the
// restore that follows can tell a file that is merely the OLD version from one
// the person or the chat wrote since the chat opened. An empty set removes the
// note.
func (e Engine) NoteCarried(c cell.Cell, files map[string]CarriedFile) error {
	path := e.carriedPath(c)
	if len(files) == 0 {
		return removeIfPresent(path)
	}
	raw, err := json.Marshal(files)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".carried-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return fsreplace.Rename(tmp.Name(), path)
}

func removeIfPresent(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// carriedStale answers, for a file already in the cell's .cell/, whether it is
// still exactly the older copy the take carried over, untouched since. A file
// the chat or the person wrote after the chat opened has another size or time
// and is live work. With no note, nothing is stale.
func (e Engine) carriedStale(c cell.Cell) func(name string, info fs.FileInfo) bool {
	raw, err := os.ReadFile(e.carriedPath(c))
	if err != nil {
		return nil
	}
	var carried map[string]CarriedFile
	if json.Unmarshal(raw, &carried) != nil || len(carried) == 0 {
		return nil
	}
	return func(name string, info fs.FileInfo) bool {
		was, ok := carried[filepath.ToSlash(name)]
		return ok && was == FingerprintOf(info)
	}
}

// mergeMissingState copies files from scratch into to when to does not yet hold
// them, leaving live state files already installed in to untouched. The one
// exception is a file stale reports as the carried-over OLDER copy, untouched
// since the take: that file is not live work, it is the version the head
// replaced, and keeping it would let the next seal publish it as current. It is
// replaced by the head's, whole, whenever the bytes differ.
func mergeMissingState(from, to string, stale func(name string, info fs.FileInfo) bool) error {
	err := filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name, relErr := filepath.Rel(from, p)
		if relErr != nil {
			return relErr
		}
		dst := filepath.Join(to, name)
		srcInfo, sErr := d.Info()
		if dstInfo, statErr := os.Lstat(dst); statErr == nil {
			if sErr != nil {
				return nil
			}
			same := dstInfo.Size() == srcInfo.Size() && sameBytes(p, dst)
			switch {
			case same:
				_ = os.Chtimes(dst, srcInfo.ModTime(), srcInfo.ModTime())
			case stale != nil && dstInfo.Mode().IsRegular() && stale(name, dstInfo):
				if err := copyFile(p, dst); err != nil {
					return err
				}
				_ = os.Chtimes(dst, srcInfo.ModTime(), srcInfo.ModTime())
			}
			return nil
		}
		if err := copyFile(p, dst); err != nil {
			return err
		}
		if sErr == nil {
			_ = os.Chtimes(dst, srcInfo.ModTime(), srcInfo.ModTime())
		}
		return nil
	})
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// sameBytes reports whether two files hold the same bytes. A file that cannot
// be read is not the same.
func sameBytes(a, b string) bool {
	x, err := os.ReadFile(a)
	if err != nil {
		return false
	}
	y, err := os.ReadFile(b)
	return err == nil && bytes.Equal(x, y)
}

func removeAbsent(dir string, have, want []string) error {
	keep := make(map[string]bool, len(want))
	for _, name := range want {
		keep[name] = true
	}
	for _, name := range have {
		if keep[name] {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	return nil
}

// stateFiles lists the files under root that are not the chain, root-relative.
func stateFiles(root string) ([]string, error) {
	var names []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name, _ := filepath.Rel(root, p)
		if !chainNames[strings.SplitN(filepath.ToSlash(name), "/", 2)[0]] {
			names = append(names, name)
		}
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return names, err
}

// copyFile replaces dst with src's bytes by writing beside it and renaming.
func copyFile(src, dst string) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".restate-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("restore state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return fsreplace.Rename(tmp.Name(), dst)
}
