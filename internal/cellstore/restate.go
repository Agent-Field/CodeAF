package cellstore

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// chainNames are the entries of .cell/ that a restore never writes: the chain
// describes the past rather than belongs to any turn's content (L12), and
// meta.json is the harness's own, composed afresh by every seal.
var chainNames = map[string]bool{
	path.Base(TurnsPath):     true,
	path.Base(ReceiptsDir):   true,
	path.Base(BlobsDir):      true,
	path.Base(cell.MetaPath): true,
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
	if err := e.restore(ctx, c, snapshot, nil, cellDirArg(scratch)); err != nil {
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
	return os.Rename(tmp.Name(), dst)
}
