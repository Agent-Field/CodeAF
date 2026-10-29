package syncsetup

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// PublishLocalChats offers existing cell histories under this set's keys and
// publication ledger. Repeating a pass only creates absent directory records;
// a failed upload leaves the local history intact for the next pass or start.
// Older chats retain their separate conversion policy.
func (s *Sync) PublishLocalChats(ctx context.Context, say func(cell.Cell, string)) error {
	if Quiet(s.Home) {
		return nil
	}
	roots, err := filepath.Glob(filepath.Join(s.Home, "v3", "projects", "*", "*"))
	if err != nil {
		return err
	}
	var failures error
	failed := func(c cell.Cell, err error) {
		if err == nil {
			return
		}
		failures = errors.Join(failures, err)
		if say != nil {
			say(c, "This chat could not be shared yet. It is still saved here; codeaf will try again.")
		}
	}
	for _, root := range roots {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		id := filepath.Base(root)
		if !cell.ValidID(id) {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, cell.MetaPath)); os.IsNotExist(err) {
			continue
		}
		c, err := cell.OpenAt(root, id)
		if err != nil {
			failed(cell.Cell{ID: id, Root: root}, err)
			continue
		}
		if head, err := cellstore.Head(c); err != nil || head == nil {
			failed(cell.Cell{ID: id, Root: root}, err)
			continue
		}
		meta, err := session.LoadMeta(root)
		if err != nil {
			failed(cell.Cell{ID: id, Root: root}, err)
			continue
		}
		e := cellstore.EngineFor(meta.Workspace)
		e.DataRoot = filepath.Join(s.Home, "v3", "stores")
		if meta.Owned {
			e.Workspace = filepath.Join(root, "work")
		}
		one, stop := context.WithTimeout(ctx, cellstore.SealWithin)
		err = s.PublishCopy(one, e, c, devname.Name(s.Home), meta.Title)
		stop()
		failed(c, err)
	}
	return failures
}

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
