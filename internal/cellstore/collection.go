package cellstore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// CaptureForGC proves which top-level entries can be restored before gc may
// remove them (C25). Comparing a materialized snapshot also covers exclusions
// owned by the engine, rather than guessing from the harness's policy alone.
func (e Engine) CaptureForGC(ctx context.Context, c cell.Cell) (string, []string, error) {
	snapshot, err := e.Capture(ctx, c)
	if err != nil {
		return "", nil, err
	}
	scratch, err := os.MkdirTemp("", "codeaf-gc-restore-")
	if err != nil {
		return "", nil, err
	}
	defer os.RemoveAll(scratch)
	target := Target{Tree: scratch, DataDir: e.LocalDir(c)}
	if _, err := e.transport().Do(ctx, target, materializeOp{Head: snapshot}); err != nil {
		return "", nil, fmt.Errorf("check gc restore: %w", err)
	}
	entries, err := os.ReadDir(e.tree(c))
	if err != nil {
		return "", nil, err
	}
	// The restore takes an undo capture before it writes files back. Keep the
	// policy and lockfile ancestors that explain exclusions until that capture,
	// so a dependency or a user-excluded file cannot enter the undo history.
	kept := map[string]bool{policyName: true}
	for _, lock := range knownLocks(c) {
		first, _, _ := strings.Cut(filepath.ToSlash(lock), "/")
		kept[first] = true
	}
	var paths []string
	for _, entry := range entries {
		name := entry.Name()
		if controlDirs[name] || kept[name] {
			continue
		}
		same, err := sameTree(filepath.Join(e.tree(c), name), filepath.Join(scratch, name))
		if err != nil {
			return "", nil, err
		}
		if same {
			paths = append(paths, name)
		}
	}
	return snapshot, paths, nil
}

// sameTree compares names, kinds, permissions and every byte, without following
// links. A partially captured directory stays whole on this computer.
func sameTree(left, right string) (bool, error) {
	a, err := os.Lstat(left)
	if err != nil {
		return false, err
	}
	b, err := os.Lstat(right)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if a.Mode() != b.Mode() {
		return false, nil
	}
	switch {
	case a.IsDir():
		aa, err := os.ReadDir(left)
		if err != nil {
			return false, err
		}
		bb, err := os.ReadDir(right)
		if err != nil {
			return false, err
		}
		if len(aa) != len(bb) {
			return false, nil
		}
		for i := range aa {
			if aa[i].Name() != bb[i].Name() {
				return false, nil
			}
			same, err := sameTree(filepath.Join(left, aa[i].Name()), filepath.Join(right, bb[i].Name()))
			if err != nil || !same {
				return same, err
			}
		}
		return true, nil
	case a.Mode()&os.ModeSymlink != 0:
		x, err := os.Readlink(left)
		if err != nil {
			return false, err
		}
		y, err := os.Readlink(right)
		return x == y, err
	case a.Mode().IsRegular():
		if a.Size() != b.Size() {
			return false, nil
		}
		x, err := os.Open(left)
		if err != nil {
			return false, err
		}
		defer x.Close()
		y, err := os.Open(right)
		if err != nil {
			return false, err
		}
		defer y.Close()
		xb, yb := make([]byte, 64*1024), make([]byte, 64*1024)
		for {
			xn, xe := io.ReadFull(x, xb)
			yn, ye := io.ReadFull(y, yb)
			if !bytes.Equal(xb[:xn], yb[:yn]) {
				return false, nil
			}
			if xe == io.EOF || xe == io.ErrUnexpectedEOF {
				return ye == xe, nil
			}
			if xe != nil {
				return false, xe
			}
			if ye != nil {
				return false, ye
			}
		}
	}
	return false, nil
}
