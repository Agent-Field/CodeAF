package syncsetup

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// ReadChat visits a saved conversation in private scratch. It neither acquires
// ownership nor restores a workspace, vault, task runtime or local journal.
// The scratch and any plaintext records are removed before returning.
func (s *Sync) ReadChat(ctx context.Context, id string, view directory.CellView, read func(cell.Cell) error) error {
	if !cell.ValidID(id) || view.Cell.Head == "" || view.Cell.Archived || view.Cell.ParentCell != "" {
		return directory.ErrNotFound
	}
	if s.ChatStorageOff() {
		return wireauth.ErrStorageOff
	}
	if read == nil {
		return errors.New("reading a chat needs a reader")
	}
	root, err := os.MkdirTemp("", "codeaf-chat-preview-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	at := cell.Cell{ID: id, Root: root}
	eng := cellstore.EngineFor("")
	eng.DataRoot = filepath.Join(root, "store")
	sync := eng.Sync(cellstore.SyncKeys{CellKey: s.Identity.CellKey(), Dedup: s.Identity.DedupSecret()}, "")
	scope := s.chatScope(id)
	defer scope.Settle()
	scope.Holder(view.Cell.Lease.Device)
	fetch := cellsync.Fetcher{Engine: sync, Store: scope.Store, Inbox: sync.Inbox, ManyMax: cellsync.ManyMaxFor(s.Relay)}
	if err := fetch.StageTranscript(ctx, at, view.Cell.Head); err != nil {
		return err
	}
	return read(at)
}
