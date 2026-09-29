// Package handoff is the takeover: one device continues a chat another device
// was driving. Its one promise is that a takeover never destroys work: local
// edits that were never sealed are sealed and kept in a branch before anything
// is fetched, and a lease is taken only once everything it needs is on disk.
package handoff

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// Fetch brings a head and everything it needs onto this device.
type Fetch interface {
	Fetch(ctx context.Context, c cell.Cell, head string) error // *cellsync.Fetcher
}

// Local is what the taking device knows about a copy of the cell it already
// has on disk (A taking a chat back from B, or a workspace the person owns).
type Local interface {
	// Dirty says whether the tree differs from the local head.
	Dirty(ctx context.Context, c cell.Cell) (bool, error)
	// Seal seals the tree as a local turn.
	Seal(ctx context.Context, c cell.Cell) (head string, turns uint32, err error)
}

// Taker continues a chat on this device.
type Taker struct {
	Dir   directory.Client
	Fetch Fetch
	Local Local
	// Branch turns sealed local work into a cell of its own. The objects of
	// head must be in the store before the branch exists (a branch whose head
	// nobody can fetch is worse than none), so the function given here uploads
	// first. It should record no old-id to new-id mapping: the chat being
	// taken keeps its id, and only the kept edits live in the branch.
	Branch func(ctx context.Context, from *cellsync.Driving, head string, turns uint32) (string, error)
	// RootFor is where this device materializes the cell.
	RootFor func(id string) string
	// After hooks run in order once the tree is on disk, for example
	// vaultsync.Inject. Each may refuse the takeover.
	After []func(ctx context.Context, c cell.Cell) error
}

// Taken is a chat this device now drives.
type Taken struct {
	Cell    cell.Cell
	Driving cellsync.Driving
	// Kept is the id of the branch holding local edits, "" when there were none.
	Kept string
}

// Take makes this device the driver of chat id. The order is fixed: the
// directory's record, then keep local edits, then fetch, then the lease, then
// (only if the head moved meanwhile) one more fetch, then the After hooks,
// then open. Fetch precedes acquire, so a failed fetch never takes a lease.
// A refused lease is directory.ErrLeaseHeld, and nothing on disk has changed
// beyond the kept branch.
func (t Taker) Take(ctx context.Context, id string) (Taken, error) {
	view, err := t.Dir.Cell(ctx, id)
	if err != nil {
		return Taken{}, fmt.Errorf("handoff: read chat %s: %w", id, err)
	}
	c := cell.Cell{ID: id, Root: t.RootFor(id)}
	kept, err := t.keepLocal(ctx, c, view.Cell.Head)
	if err != nil {
		return Taken{}, err
	}
	head, fence, err := t.claim(ctx, c, view.Cell.Head)
	if err != nil {
		return Taken{}, err
	}
	opened, err := t.open(ctx, c, fence)
	if err != nil {
		return Taken{}, err
	}
	return Taken{Cell: opened, Driving: cellsync.Driving{Cell: opened, Fence: fence, Head: head}, Kept: kept}, nil
}

// keepLocal seals unsealed work in an existing root into a branch of the chat.
// It answers the branch id, or "" when the root is absent or clean.
func (t Taker) keepLocal(ctx context.Context, c cell.Cell, parentHead string) (string, error) {
	if !exists(c.Root) {
		return "", nil
	}
	dirty, err := t.Local.Dirty(ctx, c)
	if err != nil || !dirty {
		return "", err
	}
	head, turns, err := t.Local.Seal(ctx, c)
	if err != nil {
		return "", fmt.Errorf("handoff: seal local edits of %s: %w", c.ID, err)
	}
	from := &cellsync.Driving{Cell: c, Head: parentHead}
	kept, err := t.Branch(ctx, from, head, turns)
	if err != nil {
		return "", fmt.Errorf("handoff: keep local edits of %s: %w", c.ID, err)
	}
	return kept, nil
}

// claim fetches head, takes the lease, and fetches again if the head moved
// while the fetch ran. It answers the head that is on disk and the fence.
func (t Taker) claim(ctx context.Context, c cell.Cell, head string) (string, uint64, error) {
	if err := t.Fetch.Fetch(ctx, c, head); err != nil {
		return "", 0, fmt.Errorf("handoff: fetch %s: %w", short(head), err)
	}
	got, err := t.Dir.Acquire(ctx, c.ID)
	if err != nil {
		return "", 0, fmt.Errorf("handoff: take %s: %w", c.ID, err)
	}
	if got.Cell.Head != head {
		head = got.Cell.Head
		if err := t.Fetch.Fetch(ctx, c, head); err != nil {
			return "", 0, t.giveBack(ctx, c.ID, got.Cell.Lease.Fence, fmt.Errorf("handoff: fetch %s: %w", short(head), err))
		}
	}
	return head, got.Cell.Lease.Fence, nil
}

// open runs the hooks and opens the cell. Once the lease is ours, a failure
// here hands it back, so the chat is takeable at once instead of after a TTL.
func (t Taker) open(ctx context.Context, c cell.Cell, fence uint64) (cell.Cell, error) {
	for _, hook := range t.After {
		if err := hook(ctx, c); err != nil {
			return cell.Cell{}, t.giveBack(ctx, c.ID, fence, fmt.Errorf("handoff: %w", err))
		}
	}
	opened, err := cell.OpenAt(c.Root, c.ID)
	if err != nil {
		return cell.Cell{}, t.giveBack(ctx, c.ID, fence, err)
	}
	return opened, nil
}

// giveBack releases the lease and answers cause, joined with a release failure
// if there was one (the lease then lapses by itself).
func (t Taker) giveBack(ctx context.Context, id string, fence uint64, cause error) error {
	return errors.Join(cause, t.Dir.Release(ctx, id, fence))
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func short(head string) string { return head[:min(len(head), 12)] }
