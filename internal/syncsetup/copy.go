package syncsetup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/session"
)

type pendingPublication struct {
	root    string
	retryAt time.Time
	backoff time.Duration
	err     error
}

// SharingRetryMax bounds retry pauses for unfinished pairing publication.
const SharingRetryMax = time.Minute

// RefreshLocalChats discovers newly saved chats without forgetting completed
// publications or the retry deadlines of chats still waiting to be shared.
func (s *Sync) RefreshLocalChats() error {
	s.publicationMu.Lock()
	defer s.publicationMu.Unlock()
	return s.scanLocalChats()
}

func (s *Sync) scanLocalChats() error {
	roots, err := filepath.Glob(filepath.Join(s.Home, "v3", "projects", "*", "*"))
	if err != nil {
		return err
	}
	if s.publication == nil {
		s.publication = map[string]*pendingPublication{}
	}
	if s.publicationSeen == nil {
		s.publicationSeen = map[string]struct{}{}
	}
	for _, root := range roots {
		id := filepath.Base(root)
		if _, known := s.publicationSeen[id]; known || !cell.ValidID(id) {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, cell.MetaPath)); os.IsNotExist(err) {
			continue
		}
		s.publication[id] = &pendingPublication{root: root}
		s.publicationSeen[id] = struct{}{}
	}
	return nil
}

// PublishLocalChats scans once per binding and retries only unfinished chats.
// Completed chats need no more disk or network work until the next launch.
// Older chats retain their separate conversion policy.
func (s *Sync) PublishLocalChats(ctx context.Context, now func() time.Time, say func(cell.Cell, string)) error {
	s.publicationMu.Lock()
	defer s.publicationMu.Unlock()
	if Quiet(s.Home) {
		return nil
	}
	if s.publication == nil {
		if err := s.scanLocalChats(); err != nil {
			return err
		}
	}
	var failures error
	for id, pending := range s.publication {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if now().Before(pending.retryAt) {
			failures = errors.Join(failures, pending.err)
			continue
		}
		if err := s.publishLocalChat(ctx, pending.root, id); err != nil {
			pending.err = err
			if pending.backoff == 0 {
				pending.backoff = DefaultInterval
			} else {
				pending.backoff = min(2*pending.backoff, SharingRetryMax)
			}
			pending.retryAt = now().Add(pending.backoff)
			failures = errors.Join(failures, err)
			if say != nil {
				say(cell.Cell{ID: id, Root: pending.root}, "This chat could not be shared yet. It is still saved here; codeaf will try again.")
			}
		} else {
			delete(s.publication, id)
		}
	}
	return failures
}

func (s *Sync) publishLocalChat(ctx context.Context, root, id string) error {
	c, err := cell.OpenAt(root, id)
	if err != nil {
		return err
	}
	if head, err := cellstore.Head(c); err != nil || head == nil {
		return err
	}
	meta, err := session.LoadMeta(root)
	if err != nil {
		return err
	}
	e := cellstore.EngineFor(meta.Workspace)
	e.DataRoot = filepath.Join(s.Home, "v3", "stores")
	if meta.Owned {
		e.Workspace = filepath.Join(root, "work")
	}
	one, stop := context.WithTimeout(ctx, cellstore.SealWithin)
	defer stop()
	return s.PublishCopy(one, e, c, devname.Name(s.Home), meta.Title)
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
	// Serialize Create and its release with foreground opening on this home.
	// A foreground resume deliberately keeps its existing fence.
	lock, err := s.lockChat(c.ID, ".publication")
	if err != nil {
		return err
	}
	defer lock.Close()
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
