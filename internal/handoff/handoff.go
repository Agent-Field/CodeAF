// Package handoff is the takeover: one device continues a chat another device
// was driving. Its one promise is that a takeover never destroys work: local
// edits that were never sealed are sealed and kept in a branch before anything
// is fetched, a lease is taken only once everything it needs is on disk, and
// the fetched tree reaches the chat's root only once the lease is held, so a
// lost race leaves nothing behind.
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
	// Branch turns sealed local work into a cell of its own; it is
	// cellsync.Brancher.Branch, which uploads the head's objects first. Its
	// Brancher must run with Map nil: the chat being taken keeps its id, and
	// only the kept edits live in the branch, so no old-id to new-id mapping
	// may be recorded.
	Branch func(ctx context.Context, from *cellsync.Driving, head string, turns uint32) (string, error)
	// InPlace says the tree of the chat at c's root is a folder the person owns
	// (their project), which is never renamed or replaced: editors and git hold
	// it open. Such a chat gets the fetched head restored into the folder where
	// it stands, after the lease is ours. Nil, or false, is a chat that lives in
	// a copy, which is replaced whole.
	InPlace func(c cell.Cell) bool
	// RootFor is where this device keeps the cell. The takeover materializes
	// beside it and moves the result into place once the lease is held.
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
// directory's record, then keep local edits, then fetch into a staging folder,
// then the lease, then (only if the head moved meanwhile) one more fetch, then
// the staging folder becomes the root, then the After hooks, then open. Fetch
// precedes acquire, so a failed fetch never takes a lease, and the root
// changes only after the acquire, so a lost race (directory.ErrLeaseHeld)
// leaves nothing on disk beyond the kept branch. A person who chose to continue
// a chat that was running elsewhere has chosen to displace its holder, so that
// lease is taken by force; a holder that appears meanwhile is a lost race.
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
	head, fence, err := t.claim(ctx, c, view.Cell.Head, displacing(view))
	if err != nil {
		return Taken{}, err
	}
	opened, err := t.open(ctx, c, fence)
	if err != nil {
		return Taken{}, err
	}
	return Taken{Cell: opened, Driving: cellsync.Driving{Cell: opened, Fence: fence, Head: head}, Kept: kept}, nil
}

// displacing is the acquire the person's choice covers: they saw the chat's
// lease held by a live holder, so they may take it from that holder, and only
// from that one.
func displacing(v directory.CellView) directory.AcquireOpts {
	return directory.AcquireOpts{Force: v.Cell.Lease.Expires > v.Now}
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

// claim fetches head into the staging folder, takes the lease, fetches again
// if the head moved while the fetch ran, and only then puts the staging folder
// in place of c's root. It answers the head that is on disk and the fence. The
// staging folder never outlives a failed claim.
func (t Taker) claim(ctx context.Context, c cell.Cell, head string, how directory.AcquireOpts) (string, uint64, error) {
	stage := cell.Cell{ID: c.ID, Root: stagingOf(c.Root)}
	head, fence, err := t.fetchAndAcquire(ctx, stage, head, how)
	if err == nil {
		if err = t.install(ctx, stage.Root, c, head); err != nil {
			err = t.giveBack(ctx, c.ID, fence, fmt.Errorf("handoff: put %s in place: %w", c.ID, err))
		}
	}
	if err != nil {
		return "", 0, errors.Join(err, os.RemoveAll(stage.Root))
	}
	return head, fence, nil
}

// fetchAndAcquire is the part of a claim that touches only the staging folder
// and the directory.
func (t Taker) fetchAndAcquire(ctx context.Context, stage cell.Cell, head string, how directory.AcquireOpts) (string, uint64, error) {
	if err := freshDir(stage.Root); err != nil {
		return "", 0, fmt.Errorf("handoff: prepare %s: %w", stage.ID, err)
	}
	if err := t.Fetch.Fetch(ctx, stage, head); err != nil {
		return "", 0, fmt.Errorf("handoff: fetch %s: %w", short(head), err)
	}
	got, err := t.Dir.Acquire(ctx, stage.ID, how)
	if err != nil {
		return "", 0, fmt.Errorf("handoff: take %s: %w", stage.ID, err)
	}
	if got.Cell.Head != head {
		head = got.Cell.Head
		if err := t.Fetch.Fetch(ctx, stage, head); err != nil {
			return "", 0, t.giveBack(ctx, stage.ID, got.Cell.Lease.Fence, fmt.Errorf("handoff: fetch %s: %w", short(head), err))
		}
	}
	return head, got.Cell.Lease.Fence, nil
}

// stagingOf is the folder a takeover materializes into: beside the root, so
// the final move is a rename on one filesystem.
func stagingOf(root string) string { return root + ".taking" }

// freshDir makes dir exist and be empty, whatever a crashed takeover left.
func freshDir(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return os.MkdirAll(dir, 0o700)
}

// install makes the fetched head the chat's tree. A folder the person owns is
// restored in place: everything it needs is already in the store from the
// staging fetch, so this is only the engine's restore to that tree, and the
// staging folder is dropped. A copy is replaced by the staging folder.
func (t Taker) install(ctx context.Context, stage string, c cell.Cell, head string) error {
	if t.InPlace == nil || !t.InPlace(c) {
		return swap(stage, c.Root)
	}
	if err := t.Fetch.Fetch(ctx, c, head); err != nil {
		return err
	}
	return os.RemoveAll(stage)
}

// swap puts stage in place of root. What was at root is sealed in the store
// (any unsealed edit went to a branch first), so it is set aside for the move
// and removed once the new root is in place; a failed move puts it back.
func swap(stage, root string) error {
	aside := root + ".replaced"
	if err := os.RemoveAll(aside); err != nil {
		return err
	}
	had := exists(root)
	if had {
		if err := os.Rename(root, aside); err != nil {
			return err
		}
	}
	if err := os.Rename(stage, root); err != nil {
		if had {
			err = errors.Join(err, os.Rename(aside, root))
		}
		return err
	}
	return os.RemoveAll(aside)
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
