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
	// Materialize restores head into the cell's folder.
	Materialize(ctx context.Context, c cell.Cell, head string) error
}

// Export and FrameFile are cellstore's own types under the seam's names: one
// definition, so cellstore.SyncEngine satisfies Engine with no adapter.
type (
	Export    = cellstore.Export
	FrameFile = cellstore.FrameFile
)
