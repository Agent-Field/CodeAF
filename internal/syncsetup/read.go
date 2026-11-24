package syncsetup

import (
	"context"
	"errors"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
)

// ErrNoChat is a read of a chat the relay does not list as one a person reads:
// unknown, a branch, or archived.
var ErrNoChat = errors.New("that chat is not available")

// ReadPage is one page of a remote chat's transcript and what reading it cost.
type ReadPage struct {
	cellsync.TranscriptPage
	// SealedAtMs is when the page's head was sealed, when the relay knows it and
	// the page was read at the chat's current head; zero otherwise.
	SealedAtMs int64
	// Fetched is the bytes pulled from the relay for this page.
	Fetched int64
}

// ReadChatPage reads one page of chat c's transcript as another computer last
// sealed it, without taking the chat.
//
// IT IS BUILT FROM THE SAME PARTS AS A TAKE, ON PURPOSE. The engine is the one
// a take of the chat uses (eng.Sync over this identity's keys and ledger), so
// the cell's store, timeline and import are the take's and every object a read
// brings is one the take will find held. The relay is reached through the
// chat's own Scope, so the bytes a read costs are filed under the chat with the
// rest of its traffic.
//
// IT IS NOT A TAKE, AND MUST NOT BECOME ONE. Nothing is leased, nothing is
// materialized, no agent is registered and c.Root is never created: the holder
// keeps the chat, and this computer keeps only objects in its store. See
// cellsync/read.go for why the cost is a few chunks and not the project folder.
//
// before is "" for the newest page (read at the head the relay names now) or a
// cursor a previous page answered, which pages back through the head that
// paging began at. count is how many entries a segment shows a person.
func (s *Sync) ReadChatPage(ctx context.Context, eng cellstore.Engine, c cell.Cell, before string, limit int, count func([]byte) int) (ReadPage, error) {
	var head string
	var sealed int64
	if before != "" {
		h, err := cellsync.CursorHead(before)
		if err != nil {
			return ReadPage{}, err
		}
		head = h
	}
	view, err := s.Dir.Cell(ctx, c.ID)
	if err != nil {
		return ReadPage{}, err
	}
	if view.Cell.Archived || view.Cell.ParentCell != "" {
		return ReadPage{}, ErrNoChat
	}
	if head == "" {
		head = view.Cell.Head
	}
	if head == "" {
		return ReadPage{}, nil // nothing sealed yet: nothing on the relay to read
	}
	if head == view.Cell.Head {
		sealed = view.Cell.SavedAt
	}
	sc := s.scope(c.ID)
	defer sc.Settle()
	engine := eng.Sync(cellstore.SyncKeys{CellKey: s.Identity.CellKey(), Dedup: s.Identity.DedupSecret()}, s.Ledger)
	reader := &cellsync.Reader{Engine: engine, Store: sc.Store, Inbox: engine.Inbox, ManyMax: cellsync.ManyMaxFor(s.Relay)}
	pager := &cellsync.Pager{Reader: reader, Count: count}
	page, err := pager.Page(ctx, c, head, before, limit)
	if err != nil {
		return ReadPage{}, err
	}
	return ReadPage{TranscriptPage: page, SealedAtMs: sealed, Fetched: reader.Fetched}, nil
}
