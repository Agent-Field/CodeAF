package syncsetup

import (
	"context"
	"errors"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// PublishCopy offers a background copy once, without ever acquiring an existing
// chat. The foreground may have opened it while it was copied; only Create's
// atomic absence check can decide who first publishes it (C29.2/C31.3).
func (s *Sync) PublishCopy(ctx context.Context, e cellstore.Engine, c cell.Cell, name, title string) error {
	if Quiet(s.Home) {
		return ErrQuiet
	}
	if _, err := s.Dir.Cell(ctx, c.ID); !errors.Is(err, directory.ErrNotFound) {
		return err
	}
	if err := s.putDevice(ctx, name); err != nil {
		return err
	}
	if err := cellstore.Await(ctx, c); err != nil {
		return err
	}
	head, err := cellstore.Head(c)
	if err != nil || head == nil {
		return err
	}
	scope := s.scope(c.ID)
	defer scope.Settle()
	engine := e.Sync(cellstore.SyncKeys{CellKey: s.Identity.CellKey(), Dedup: s.Identity.DedupSecret()}, s.Ledger)
	publisher := cellsync.Publisher{Engine: engine, Store: scope.Store, Dir: s.Dir}
	if _, err := publisher.Upload(ctx, c, head.Turn.ID); err != nil {
		return err
	}
	info := s.publishInfo(c, title)
	view, err := s.Dir.Create(ctx, c.ID, directory.CellInit{Head: head.Turn.ID, SavedAt: head.Turn.SealedAtMs, Class: info.Class, Title: info.Title, Size: info.Size, Keys: info.Keys})
	if errors.Is(err, directory.ErrExists) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := cellstore.NotePublished(c, head.Turn.ID); err != nil {
		return err
	}
	err = directory.ReleaseQuiet(ctx, s.Dir, c.ID, view.Cell.Lease.Fence)
	if errors.Is(err, directory.ErrFenceStale) {
		return nil
	}
	return err
}
