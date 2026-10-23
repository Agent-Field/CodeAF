package cellstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// bindTarget follows a named chat when its workspace path changes. Path-based
// legacy stores retain the engine's original routing. The binding is private
// local state; restored metadata must not redirect another chat's timeline.
func (e Engine) bindTarget(ctx context.Context, c cell.Cell, t Target) error {
	if t.Timeline == "" {
		return nil
	}
	path := filepath.Join(e.LocalDir(c), "engine-tree")
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(raw) > 0 && string(raw) != t.Tree {
		t.CellDir = ""
		if _, err := (Spawn{Binary: e.Binary, Run: e.Run}).Do(ctx, t, rebindOp{From: string(raw)}); err != nil {
			return fmt.Errorf("follow chat folder: %w", err)
		}
	}
	if string(raw) == t.Tree {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(t.Tree), 0600)
}
