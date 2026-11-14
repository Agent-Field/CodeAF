package syncsetup

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
	"github.com/Agent-Field/codeaf/internal/handoff"
	"github.com/Agent-Field/codeaf/internal/session"
)

// fileCopy uses the origin from the sealed inventory rather than this
// computer's home directory. A first arrival stays a copy even if an unrelated
// folder with the origin's path exists here. A root known before restoration
// stays in place, including an owned copy returning from another computer.
func (c *Continuer) fileCopy(ctx context.Context, at cell.Cell, keepRoot bool) (cell.Cell, func(), error) {
	m, err := session.LoadMeta(at.Root)
	if err != nil || keepRoot || c.opt.BucketFor == nil || !m.Owned || m.Origin == "" {
		return at, nil, err
	}
	dest := filepath.Join(c.opt.BucketFor(m.Origin), at.ID)
	if filepath.Clean(dest) == filepath.Clean(at.Root) {
		return at, nil, nil
	}
	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			err = errors.New("a copy of this chat is already filed here")
		}
		return at, nil, err
	}
	// The destination becomes discoverable at rename. Keep that root fenced
	// through the caller's remaining arrival work, as the incoming root was.
	filed := handoff.Arrive(dest)
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return at, filed, err
	}
	if err := fsreplace.Rename(at.Root, dest); err != nil {
		return at, filed, err
	}
	landed := cell.Cell{ID: at.ID, Root: dest}
	if err := session.RebaseOwnedCopy(dest); err != nil {
		return landed, filed, err
	}
	if err := c.eng.Follow(ctx, at, landed); err != nil {
		return landed, filed, err
	}
	opened, err := cell.OpenAt(dest, at.ID)
	return opened, filed, err
}
