package cellsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
)

// Fetcher brings a head and everything it needs from the store onto this device.
type Fetcher struct {
	Engine     Engine
	Store      blobstore.Store
	Inbox      func(c cell.Cell) string
	OnProgress func(have, want int) // optional; want grows as the graph is learned
}

// Fetch asks the engine what it lacks, gets those objects into the inbox and
// imports them, until nothing is lacking, then materializes head. A rid the
// store does not hold is an error that names it.
func (f *Fetcher) Fetch(ctx context.Context, c cell.Cell, head string) error {
	inbox := f.Inbox(c)
	if err := os.MkdirAll(inbox, 0o700); err != nil {
		return err
	}
	have, known := 0, 0
	for {
		want, err := f.Engine.Want(ctx, c, head)
		if err != nil {
			return err
		}
		if len(want) == 0 {
			return f.Engine.Materialize(ctx, c, head)
		}
		known += len(want)
		n, err := f.round(ctx, c, head, inbox, want)
		if err != nil {
			return err
		}
		have += n
		f.progress(have, known)
	}
}

// round gets every wanted rid and imports them. An import that takes nothing
// would repeat forever, so it is an error.
func (f *Fetcher) round(ctx context.Context, c cell.Cell, head, inbox string, want []string) (int, error) {
	for _, rid := range want {
		if err := f.get(ctx, inbox, rid); err != nil {
			return 0, err
		}
	}
	n, err := f.Engine.Import(ctx, c, head, inbox)
	if err == nil && n == 0 {
		err = errors.New("cellsync: import took none of the wanted objects")
	}
	return n, err
}

func (f *Fetcher) get(ctx context.Context, inbox, rid string) error {
	raw, err := f.Store.Get(ctx, rid)
	if err != nil {
		return fmt.Errorf("cellsync: fetch object %s: %w", rid, err)
	}
	return os.WriteFile(filepath.Join(inbox, rid), raw, 0o600)
}

func (f *Fetcher) progress(have, want int) {
	if f.OnProgress != nil {
		f.OnProgress(have, want)
	}
}
