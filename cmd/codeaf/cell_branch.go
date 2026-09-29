package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/handoff"
)

// A death branch (L12) is a cell that holds the turns a chat lost its lease
// with. `codeaf cell merge` folds it back into the chat and `codeaf cell
// discard` lets it go. Both run on the device that drives the chat, and both
// prove it still holds the lease before they change anything.

// treeMerger is the engine's merge of one head into another.
type treeMerger interface {
	// Merge merges the branch head, materialized in branch, into the parent's
	// head, seals the result as a new turn of parent and answers its head.
	// Conflicts are an error and change nothing.
	Merge(ctx context.Context, parent, branch cell.Cell, branchHead string) (string, error)
}

// headPublisher makes a sealed head durable (*cellsync.Publisher).
type headPublisher interface {
	Publish(ctx context.Context, d *cellsync.Driving, head string, in cellsync.PublishInfo) error
}

// driverOf answers how this device drives a chat: its fence and last durable head.
type driverOf interface {
	Driving(ctx context.Context, c cell.Cell) (*cellsync.Driving, error)
}

// branchDoor is everything the two verbs need, behind interfaces so tests use fakes.
type branchDoor struct {
	Dir     directory.Client
	Fetch   handoff.Fetch
	Driver  driverOf
	Merger  treeMerger
	Pub     headPublisher
	Scratch func(id string) cell.Cell // where a branch head lands, away from the parent's tree
}

// openBranchDoor builds the door for a device. The sync wiring [int-1] sets it;
// until a device has sync set up the verbs say so instead of guessing.
var openBranchDoor = func(cell.Cell) (branchDoor, error) {
	return branchDoor{}, errors.New("sync is not set up on this device")
}

func cellMerge(c cell.Cell, args []string, out io.Writer) error {
	door, err := openBranchDoor(c)
	if err != nil {
		return err
	}
	return door.merge(context.Background(), c, args[0], out)
}

func cellDiscard(c cell.Cell, args []string, out io.Writer) error {
	door, err := openBranchDoor(c)
	if err != nil {
		return err
	}
	return door.discard(context.Background(), c, args[0], out)
}

// branchWork is what a verb learns before it may touch anything.
type branchWork struct {
	driving *cellsync.Driving
	parent  directory.Cell
	branch  directory.Cell
}

// merge folds the branch into the chat: fetch its head, merge, seal, publish,
// and only then archive it, so a failure anywhere leaves the branch to retry.
func (b branchDoor) merge(ctx context.Context, c cell.Cell, branchID string, out io.Writer) error {
	w, err := b.prove(ctx, c, branchID)
	if err != nil {
		return err
	}
	scratch := b.Scratch(branchID)
	if err := b.Fetch.Fetch(ctx, scratch, w.branch.Head); err != nil {
		return fmt.Errorf("fetch branch %s: %w", branchID, err)
	}
	head, err := b.Merger.Merge(ctx, c, scratch, w.branch.Head)
	if err != nil {
		return fmt.Errorf("merge branch %s: %w", branchID, err)
	}
	info := cellsync.PublishInfo{Class: w.parent.Class, Size: w.parent.Size, Keys: w.parent.Keys}
	if err := b.Pub.Publish(ctx, w.driving, head, info); err != nil {
		return fmt.Errorf("publish merge of %s: %w", branchID, err)
	}
	if err := b.Dir.Archive(ctx, branchID); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "merged %s into %s as %s\n", short(branchID), short(c.ID), short(head))
	return err
}

// discard archives the branch and nothing else.
func (b branchDoor) discard(ctx context.Context, c cell.Cell, branchID string, out io.Writer) error {
	if _, err := b.prove(ctx, c, branchID); err != nil {
		return err
	}
	if err := b.Dir.Archive(ctx, branchID); err != nil {
		return err
	}
	_, err := fmt.Fprintf(out, "discarded %s\n", short(branchID))
	return err
}

// prove checks that this device holds the chat's lease (a heartbeat with its
// fence, which the directory refuses for a superseded one) and that branchID is
// a live branch of that chat.
func (b branchDoor) prove(ctx context.Context, c cell.Cell, branchID string) (branchWork, error) {
	d, err := b.Driver.Driving(ctx, c)
	if err != nil {
		return branchWork{}, err
	}
	parent, err := b.Dir.Cell(ctx, d.ID())
	if err != nil {
		return branchWork{}, err
	}
	if _, err := b.Dir.Heartbeat(ctx, d.ID(), directory.Beat{Fence: d.Fence, Pending: parent.Cell.Lease.Pending}); err != nil {
		return branchWork{}, fmt.Errorf("this device does not hold the lease of %s: %w", short(d.ID()), err)
	}
	branch, err := b.Dir.Cell(ctx, branchID)
	if err != nil {
		return branchWork{}, err
	}
	if branch.Cell.ParentCell != d.ID() || branch.Cell.Archived {
		return branchWork{}, fmt.Errorf("%s is not a live branch of %s", short(branchID), short(d.ID()))
	}
	return branchWork{driving: d, parent: parent.Cell, branch: branch.Cell}, nil
}
