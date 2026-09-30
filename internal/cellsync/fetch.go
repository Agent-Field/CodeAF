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

// Fetch completes head in the store and then materializes it. A rid the
// store does not hold is an error that names it.
func (f *Fetcher) Fetch(ctx context.Context, c cell.Cell, head string) error {
	if err := f.Complete(ctx, c, head); err != nil {
		return err
	}
	return f.Engine.Materialize(ctx, c, head)
}

// Complete is Fetch without the restore: everything head needs is in the
// device's store afterwards and the folder the person works in is not touched.
// It is how a device holds a chat's objects without taking the chat over.
func (f *Fetcher) Complete(ctx context.Context, c cell.Cell, head string) error {
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
			return nil
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
	if err := f.gather(ctx, inbox, want); err != nil {
		return 0, err
	}
	n, err := f.Engine.Import(ctx, c, head, inbox)
	if err == nil && n == 0 {
		err = errors.New("cellsync: import took none of the wanted objects")
	}
	return n, err
}

// gather brings every wanted object into the inbox, a frame's worth per request:
// the store answers a prefix of what is asked, and the rest is asked again. An
// object the store does not hold is an error that names it.
func (f *Fetcher) gather(ctx context.Context, inbox string, want []string) error {
	for len(want) > 0 {
		batch := want[:min(len(want), blobstore.MaxGetMany)]
		objects, err := f.Store.GetMany(ctx, batch)
		if err != nil {
			return fmt.Errorf("cellsync: fetch object %s: %w", batch[0], err)
		}
		if len(objects) == 0 {
			return fmt.Errorf("cellsync: the store answered no object for %s", batch[0])
		}
		if err := keep(inbox, objects); err != nil {
			return err
		}
		want = want[len(objects):]
	}
	return nil
}

// keep writes each object to the inbox under its remote id, the name Import reads it by.
func keep(inbox string, objects []blobstore.Object) error {
	for _, o := range objects {
		if err := os.WriteFile(filepath.Join(inbox, o.RID), o.Bytes, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func (f *Fetcher) progress(have, want int) {
	if f.OnProgress != nil {
		f.OnProgress(have, want)
	}
}
