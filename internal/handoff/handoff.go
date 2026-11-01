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
	"github.com/Agent-Field/codeaf/internal/fsreplace"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// Fetch brings a head and everything it needs onto this device.
type Fetch interface {
	// Fetch completes head in the store and restores it into c's tree.
	Fetch(ctx context.Context, c cell.Cell, head string) error // *cellsync.Fetcher
	// Complete is Fetch without the restore: the store holds everything head
	// needs and no tree is touched.
	Complete(ctx context.Context, c cell.Cell, head string) error
}

// Local is what the taking device knows about a copy of the cell it already
// has on disk (A taking a chat back from B, or a workspace the person owns).
type Local interface {
	// Dirty says whether the tree differs from the local head.
	Dirty(ctx context.Context, c cell.Cell) (bool, error)
	// Unpublished returns the local head and count of sealed turns that have
	// not reached the directory, even when the working tree is clean.
	Unpublished(c cell.Cell, remoteHead string) (head string, turns uint32, err error)
	// Seal seals the tree as a local turn.
	Seal(ctx context.Context, c cell.Cell) (head string, turns uint32, err error)
	// Follow says the tree restored at from was moved to be the tree of to. The
	// engine knows a tree by its path, so a tree moved into place unannounced
	// is one it has never heard of, and cannot tell whether it holds edits.
	Follow(ctx context.Context, from, to cell.Cell) error
}

// Taker continues a chat on this device.
type Taker struct {
	// stagedHead joins a claim's value copies so a failure restores only
	// a native binding that the claim may have moved.
	stagedHead *string

	// OnPhase receives measurements of local work during this take.
	OnPhase func(Phase)

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
	// Preflight validates the authenticated staged tree before acquiring a lease
	// or replacing local files. It runs again if the acquired head moved.
	Preflight func(context.Context, cell.Cell) error
	// Prepare admits incoming objects in private scratch without changing the
	// chat's native binding. It replaces staged fetching and Preflight when set.
	Prepare func(context.Context, cell.Cell, string) error
}

// Taken is a chat this device now drives.
type Taken struct {
	Cell    cell.Cell
	Driving cellsync.Driving
	// Kept is the id of the branch holding local edits, "" when there were none.
	Kept string
}

// Take makes this device the driver of chat id. The order is fixed: the
// directory's record, then keep local edits, then fetch and admit incoming work
// in private scratch, then the lease and a second check if the head moved, then
// the staging folder becomes the root, then the After hooks, then open. Fetch
// precedes acquire, so a failed fetch never takes a lease, and the root
// changes only after the acquire, so a lost race (directory.ErrLeaseHeld)
// leaves nothing on disk beyond the kept branch. A person who chose to continue
// a chat that was running elsewhere has chosen to displace its holder, so that
// lease is taken by force; a holder that appears meanwhile is a lost race.
func (t Taker) Take(ctx context.Context, id string) (Taken, error) {
	return t.TakeSeen(ctx, id, nil)
}

// TakeSeen continues the lease the person saw on the card. Older callers
// without that reading still bind the claim to the record read before fetch.
func (t Taker) TakeSeen(ctx context.Context, id string, seen *directory.LeaseExpectation) (Taken, error) {
	directoryRead := Trace(t.OnPhase, "directory")
	view, err := t.Dir.Cell(ctx, id)
	directoryRead()
	if err != nil {
		return Taken{}, fmt.Errorf("handoff: read chat %s: %w", id, err)
	}
	if seen != nil && !seen.Matches(view.Cell) {
		return Taken{}, directory.ErrLeaseHeld
	}
	c := cell.Cell{ID: id, Root: t.RootFor(id)}
	kept, err := t.keepLocal(ctx, c, view.Cell.Head, view.Cell.Frames)
	if err != nil {
		return Taken{}, err
	}
	how := displacing(view)
	if seen != nil && seen.Head != "" {
		how.Expect.Head = seen.Head
	}
	head, fence, plan, err := t.claim(ctx, c, view.Cell.Head, how)
	if err != nil {
		return Taken{}, err
	}
	opened, err := t.open(ctx, c, fence)
	if err != nil {
		return Taken{}, err
	}
	// The plan is the acquired record's: the record read before the claim can
	// trail a publish that landed during the fetch, and the next publish
	// replaces the plan wholesale (contract §22.2).
	return Taken{Cell: opened, Driving: cellsync.Driving{Cell: opened, Fence: fence, Head: head, Frames: plan}, Kept: kept}, nil
}

// displacing is the acquire the person's choice covers: they saw the chat's
// lease held by a live holder, so they may take it from that holder, and only
// from that one.
func displacing(v directory.CellView) directory.AcquireOpts {
	return directory.AcquireOpts{
		RequireConditional: true,
		Force:              v.Cell.Lease.Expires > v.Now,
		Expect:             &directory.LeaseExpectation{Device: v.Cell.Lease.Device, Fence: v.Cell.Lease.Fence},
	}
}

// keepLocal seals unsealed local work onto a branch before any fetch. It
// answers the branch id, or "" when the root is absent or clean. The
// branch's Driving starts from the parent record's plan: the branch head's
// closure is the parent's plus the orphan turns, whose frames the branch's
// own upload adds.
func (t Taker) keepLocal(ctx context.Context, c cell.Cell, parentHead string, plan []string) (string, error) {
	if !exists(c.Root) {
		return "", nil
	}
	checked := Trace(t.OnPhase, "unpublished check")
	head, turns, err := t.Local.Unpublished(c, parentHead)
	checked()
	if err != nil {
		return "", err
	}
	checked = Trace(t.OnPhase, "dirty check")
	dirty, err := t.Local.Dirty(ctx, c)
	checked()
	if err != nil {
		return "", err
	}
	if dirty {
		sealed, added, err := t.Local.Seal(ctx, c)
		if err != nil {
			return "", fmt.Errorf("handoff: seal local edits of %s: %w", c.ID, err)
		}
		head, turns = sealed, turns+added
	}
	if head == "" {
		return "", nil
	}
	from := &cellsync.Driving{Cell: c, Head: parentHead, Frames: plan}
	keptUpload := Trace(t.OnPhase, "kept-branch upload")
	kept, err := t.Branch(ctx, from, head, turns)
	keptUpload()
	if err != nil {
		return "", fmt.Errorf("handoff: keep local edits of %s: %w", c.ID, err)
	}
	return kept, nil
}

// claim fetches head into the staging folder, takes the lease, fetches again
// if the head moved while the fetch ran, and only then puts the staging folder
// in place of c's root. It answers the head that is on disk and the fence. The
// staging folder never outlives a failed claim.
func (t Taker) claim(ctx context.Context, c cell.Cell, head string, how directory.AcquireOpts) (string, uint64, []string, error) {
	stage := cell.Cell{ID: c.ID, Root: stagingOf(c.Root)}
	t.stagedHead = new(string)
	head, fence, plan, err := t.fetchAndAcquire(ctx, stage, c.Root, head, how)
	if err == nil {
		if err = t.install(ctx, stage, c, head); err != nil {
			err = t.giveBack(ctx, c.ID, fence, fmt.Errorf("handoff: put %s in place: %w", c.ID, err))
		}
	}
	if err != nil {
		// Staged admission may have moved the engine's timeline binding even
		// though local files stayed in place. A retry must inspect that original
		// root rather than the staging folder we are about to remove.
		if (t.Preflight != nil && t.Prepare == nil || t.stagedHead != nil && *t.stagedHead != "") && exists(c.Root) && exists(stage.Root) {
			err = errors.Join(err, t.Local.Follow(ctx, stage, c))
		}
		return "", 0, nil, errors.Join(err, os.RemoveAll(stage.Root))
	}
	return head, fence, plan, nil
}

// fetchAndAcquire is the part of a claim that touches only the store, the
// staging folder and the directory. A chat in a copy is fetched into the
// staging folder, which starts as a copy of the tree this device already holds
// at from, so the fetch writes only the paths the new head changed; with
// nothing at from it starts empty. A chat in the person's own folder is only
// completed in the store: it is restored where it stands once the lease is
// ours, except when Preflight needs the staged files to validate admission
// before any local replacement. Prepare instead admits fetched objects in its
// own timeline and leaves the real restore until acquisition. It answers the
// plan of the record the device actually
// acquired: another device may have published while the fetch ran, and the
// next publish replaces the record's plan wholesale, so seeding from the
// record read before the claim would carry the stale one (contract §22.2).
func (t Taker) fetchAndAcquire(ctx context.Context, stage cell.Cell, from, head string, how directory.AcquireOpts) (string, uint64, []string, error) {
	if t.stagedHead == nil {
		t.stagedHead = new(string)
	}
	staged := Trace(t.OnPhase, "staging/preflight")
	defer func() {
		if staged != nil {
			staged()
		}
	}()
	inPlace := t.InPlace != nil && t.InPlace(cell.Cell{ID: stage.ID, Root: from})
	prepare := func(head string) error {
		if t.Prepare != nil {
			if err := freshDir(stage.Root); err != nil {
				return err
			}
			if err := t.bring(ctx, stage, true, head); err != nil {
				return fmt.Errorf("handoff: fetch %s: %w", short(head), err)
			}
			return t.Prepare(ctx, stage, head)
		}
		if !inPlace || t.Preflight != nil {
			if err := freshDir(stage.Root); err != nil {
				return err
			}
			seedFrom(from, stage.Root)
		}
		if err := t.bring(ctx, stage, inPlace && t.Preflight == nil, head); err != nil {
			return fmt.Errorf("handoff: fetch %s: %w", short(head), err)
		}
		if t.Preflight != nil {
			return t.Preflight(ctx, stage)
		}
		return nil
	}
	if err := prepare(head); err != nil {
		return "", 0, nil, err
	}
	staged()
	staged = nil
	if t.OnPhase != nil {
		t.OnPhase(Phase{Name: "first acquire", Started: time.Now()})
	}
	acquiring := Trace(t.OnPhase, "acquire")
	got, err := t.Dir.Acquire(ctx, stage.ID, how)
	acquiring()
	if err != nil {
		return "", 0, nil, fmt.Errorf("handoff: take %s: %w", stage.ID, err)
	}
	if got.Cell.Head != head {
		head = got.Cell.Head
		if err := prepare(head); err != nil {
			return "", 0, nil, t.giveBack(ctx, stage.ID, got.Cell.Lease.Fence, err)
		}
	}
	return head, got.Cell.Lease.Fence, got.Cell.Frames, nil
}

// bring puts head where the claim restores it from: in the store alone for a
// folder the person owns, else also in the staging folder.
func (t Taker) bring(ctx context.Context, stage cell.Cell, inPlace bool, head string) error {
	defer Trace(t.OnPhase, "fetch")()
	if inPlace {
		return t.Fetch.Complete(ctx, stage, head)
	}
	if t.stagedHead != nil {
		*t.stagedHead = head
	}
	return t.Fetch.Fetch(ctx, stage, head)
}

// StagingSuffix identifies a takeover's private scratch folder.
const StagingSuffix = ".taking"

// stagingOf is the folder a takeover materializes into: beside the root, so
// the final move is a rename on one filesystem.
func stagingOf(root string) string { return root + StagingSuffix }

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
func (t Taker) install(ctx context.Context, stage, c cell.Cell, head string) error {
	defer Trace(t.OnPhase, "restore")()
	// Snapshots arriving from another computer never carry this computer's local
	// copy-ready or copy-offered markers, so installing over a `.moving` copy
	// root must keep or re-stamp them lest its `.lock` sibling hide the chat or
	// revive the untouched original.
	keepCopyReady := exists(filepath.Join(c.Root, filepath.FromSlash(cell.CopyReadyPath))) ||
		(filepath.Base(filepath.Dir(c.Root)) == ".moving" && exists(c.Root+".lock"))
	offeredFile := filepath.Join(c.Root, filepath.FromSlash(cell.CopyOfferedPath))
	keepCopyOffered := exists(offeredFile)
	restoreCopyMarkers := func() error {
		if keepCopyReady {
			if err := cell.StampCopyReady(c.Root); err != nil {
				return err
			}
		}
		if keepCopyOffered && !exists(offeredFile) {
			if err := os.MkdirAll(filepath.Dir(offeredFile), 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(offeredFile, nil, 0o600); err != nil {
				return err
			}
		}
		return nil
	}
	if t.InPlace == nil || !t.InPlace(c) {
		if t.Prepare != nil {
			if err := freshDir(stage.Root); err != nil {
				return err
			}
			if t.stagedHead != nil {
				*t.stagedHead = head
			}
			if err := t.Fetch.Fetch(ctx, stage, head); err != nil {
				return err
			}
		}
		if err := t.replace(ctx, stage, c); err != nil {
			return err
		}
		return restoreCopyMarkers()
	}
	if err := t.Fetch.Fetch(ctx, c, head); err != nil {
		return err
	}
	if err := restoreCopyMarkers(); err != nil {
		return err
	}
	return os.RemoveAll(stage.Root)
}

// replace puts the staging folder in place of c's root and tells the engine the
// tree moved, so a chat that is taken again before it is ever opened is still
// known to the engine.
func (t Taker) replace(ctx context.Context, stage, c cell.Cell) error {
	if err := swap(stage.Root, c.Root); err != nil {
		return err
	}
	return t.Local.Follow(ctx, stage, c)
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
		if err := fsreplace.Rename(root, aside); err != nil {
			return err
		}
	}
	if err := fsreplace.Rename(stage, root); err != nil {
		if had {
			err = errors.Join(err, fsreplace.Rename(aside, root))
		}
		return err
	}
	return os.RemoveAll(aside)
}

// open runs the hooks and opens the cell. Once the lease is ours, a failure
// here hands it back, so the chat is takeable at once instead of after a TTL.
func (t Taker) open(ctx context.Context, c cell.Cell, fence uint64) (cell.Cell, error) {
	for _, hook := range t.After {
		done := Trace(t.OnPhase, "hooks")
		err := hook(ctx, c)
		done()
		if err != nil {
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
