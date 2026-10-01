package cellsync

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
)

// Engine is the device-local half of sync: the furrow verbs of the Stage 1
// engine contract. The real implementation is cellstore.SyncEngine; FakeEngine
// stands in for it in tests.
type Engine interface {
	// Export packs every object reachable from head that the store's ledger
	// lacks into frame files.
	Export(ctx context.Context, c cell.Cell, head string) (Export, error)
	// Published records frames as uploaded and deletes their files.
	Published(ctx context.Context, c cell.Cell, frames []string) error
	// Want names the remote ids needed next to complete head locally; empty
	// means complete.
	Want(ctx context.Context, c cell.Cell, head string) ([]string, error)
	// Import stores each object file in inbox, named by its remote id.
	Import(ctx context.Context, c cell.Cell, head, inbox string) (int, error)
	// ImportPrimed is Import with one rule changed (contract §22.7): an inbox
	// file no pass wanted is deleted, not an error. It exists for a take that
	// primed its inbox with whole frames: a frame packs one publish's objects,
	// and later turns may have orphaned some of them, so priming can deliver
	// objects the head does not want. The want loop's answers keep the strict
	// rule: what the relay names for a want is checked.
	ImportPrimed(ctx context.Context, c cell.Cell, head, inbox string) (int, error)
	// ImportPartial is ImportPrimed that deletes nothing and refuses nothing
	// (contract §22.12): it stores what the inbox holds of what the head can
	// reach so far, and leaves every other file, which may be waiting for a
	// frame that has not landed. A take calls it while frames still download.
	ImportPartial(ctx context.Context, c cell.Cell, head, inbox string) (int, error)
	// Holds reports whether this device already holds objects of c that the
	// relay holds too: its ledger for this relay is not empty. A device that
	// does has most of the chat's history and wants only a delta (contract
	// §22.5), so priming it with the chat's whole frame plan would download
	// what it has.
	Holds(c cell.Cell) bool
	// Materialize restores head into the cell's folder.
	Materialize(ctx context.Context, c cell.Cell, head string) error
}

// Export and FrameFile are cellstore's own types under the seam's names: one
// definition, so cellstore.SyncEngine satisfies Engine with no adapter.
type (
	Export    = cellstore.Export
	FrameFile = cellstore.FrameFile
)
