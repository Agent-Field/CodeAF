package main

import (
	"context"
	"fmt"
	"io"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// A death branch (L12) is a cell that holds the turns a chat lost its lease
// with. `codeaf cell discard` lets it go: the branch is archived, not deleted.
// It runs on the device that drives the chat, and proves it still holds the
// lease before it changes anything.
//
// THERE IS NO `cell merge` IN STAGE 1, and none is offered anywhere. Folding a
// branch back needs a merge of two sealed trees, and the engine's merge works
// on a live workspace and a snapshot it holds, not on a branch head that is
// still only in the store; a merge that sealed a wrong head would be worse than
// no merge. The row says `discard` alone, and the manual says the same.

// driverOf answers how this device drives a chat: its fence and last durable head.
type driverOf interface {
	Driving(ctx context.Context, c cell.Cell) (*cellsync.Driving, error)
}

// branchDoor is everything the verb needs, behind interfaces so tests use fakes.
type branchDoor struct {
	Dir    directory.Client
	Driver driverOf
}

// openBranchDoor builds the door for a device. Until a device has sync set up
// the verb says so instead of guessing.
var openBranchDoor = func() (branchDoor, error) {
	s, err := syncOf()
	if err != nil {
		return branchDoor{}, err
	}
	return branchDoor{Dir: s.Dir, Driver: s}, nil
}

func cellDiscard(c cell.Cell, args []string, out io.Writer) error {
	door, err := openBranchDoor()
	if err != nil {
		return err
	}
	return door.discard(context.Background(), c, args[0], out)
}

// discard archives the branch and nothing else.
func (b branchDoor) discard(ctx context.Context, c cell.Cell, branchID string, out io.Writer) error {
	if err := b.prove(ctx, c, branchID); err != nil {
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
func (b branchDoor) prove(ctx context.Context, c cell.Cell, branchID string) error {
	d, err := b.Driver.Driving(ctx, c)
	if err != nil {
		return err
	}
	parent, err := b.Dir.Cell(ctx, d.ID())
	if err != nil {
		return err
	}
	if _, err := b.Dir.Heartbeat(ctx, d.ID(), directory.Beat{Fence: d.Fence, Pending: parent.Cell.Lease.Pending}); err != nil {
		return fmt.Errorf("this device does not hold the lease of %s: %w", short(d.ID()), err)
	}
	branch, err := b.Dir.Cell(ctx, branchID)
	if err != nil {
		return err
	}
	if branch.Cell.ParentCell != d.ID() || branch.Cell.Archived {
		return fmt.Errorf("%s is not a live branch of %s", short(branchID), short(d.ID()))
	}
	return nil
}
