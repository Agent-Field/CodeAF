package cellstore

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// heldStore is a store whose seal does not end until release is closed, the
// way a fresh home's first capture of a large workspace runs for many seconds.
type heldStore struct {
	once    *sync.Once
	started chan struct{}
	release chan struct{}
}

func (s heldStore) Seal(ctx context.Context, _ cell.Cell, _ TurnInfo) (Sealed, error) {
	s.once.Do(func() { close(s.started) })
	<-s.release
	return Sealed{}, nil
}

// THE LIST OF UNFINISHED CALLS IS READ WITHOUT WAITING FOR A SEAL. Opening a
// conversation reads it (the entry notice names what a crash cut off) while
// the seat's warm capture is sealing the whole workspace; under the seal's
// lock, the open waited for that capture, which on a fresh home of a large
// folder was the whole of the time before the first turn could start.
func TestIncompleteIsReadWhileTheFirstCaptureSeals(t *testing.T) {
	c := newCell(t)
	store := heldStore{once: &sync.Once{}, started: make(chan struct{}), release: make(chan struct{})}
	r, err := NewRecorder(&stubExec{}, store, c, filepath.Join(t.TempDir(), "wal"), Options{Async: true})
	if err != nil {
		t.Fatal(err)
	}
	r.Warm(context.Background())
	select {
	case <-store.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the warm capture never began sealing")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = r.Incomplete()
		_ = r.Interrupted().Lines()
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		close(store.release)
		t.Fatal("reading the unfinished calls waited for the capture to end")
	}
	// The capture finishes before the test's folders go.
	close(store.release)
	if err := r.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
