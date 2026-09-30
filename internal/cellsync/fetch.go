package cellsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// FetchWindow is the most frame fetches a take has in flight at once
// (contract §22.6).
const FetchWindow = 8

// Fetcher brings a head and everything it needs from the store onto this device.
type Fetcher struct {
	Engine Engine
	Store  blobstore.Store
	// Dir reads the cell's record for the plan (contract §22.2). Nil means no
	// priming: the fetch is exactly the want loop (rotation uses it that way).
	Dir        directory.Client
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
	if err := f.prime(ctx, c, head, inbox); err != nil {
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

// prime reads the plan the record holds for the cell and fetches the frames
// it names, then runs one ImportPrimed over what they carried (contract §22.5).
// It is an optimisation only: a record that names no frames, a frame that is
// absent or undecodable, or a store that cannot be asked all degrade into the
// want loop, which is the completeness proof of every take. Only a local
// failure — the inbox, or the engine refusing verified objects — is an error.
func (f *Fetcher) prime(ctx context.Context, c cell.Cell, head, inbox string) error {
	if f.Dir == nil {
		return nil
	}
	view, err := f.Dir.Cell(ctx, c.ID)
	if err != nil || len(view.Cell.Frames) == 0 {
		return nil
	}
	if err := f.fetchFrames(ctx, inbox, view.Cell.Frames); err != nil {
		return err
	}
	_, err = f.Engine.ImportPrimed(ctx, c, head, inbox)
	return err
}

// fetchFrames writes the objects of every frame the store holds to the inbox,
// FetchWindow fetches in flight at once. A frame the store does not hold, or
// one that fails Decode, is skipped: the want loop asks for what it carried.
// Downloads race each other but writes go through sink: two frames can carry
// the same object, and one rid is written once, by one goroutine.
func (f *Fetcher) fetchFrames(ctx context.Context, inbox string, frames []string) error {
	sink := &frameSink{written: map[string]bool{}}
	window := make(chan struct{}, FetchWindow)
	var wg sync.WaitGroup
	for _, id := range frames {
		select {
		case window <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-window }()
			frame, err := f.Store.GetFrame(ctx, id)
			if err != nil {
				return
			}
			_, objects, err := blobstore.Decode(frame)
			if err != nil {
				return
			}
			sink.keep(inbox, objects)
		}()
	}
	wg.Wait()
	return sink.err
}

// frameSink serializes priming writes: one rid lands once, and the first local
// failure is kept for the caller.
type frameSink struct {
	mu      sync.Mutex
	written map[string]bool
	err     error
}

func (s *frameSink) keep(inbox string, objects []blobstore.Object) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return
	}
	for _, o := range objects {
		if s.written[o.RID] {
			continue
		}
		if err := os.WriteFile(filepath.Join(inbox, o.RID), o.Bytes, 0o600); err != nil {
			s.err = err
			return
		}
		s.written[o.RID] = true
	}
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
