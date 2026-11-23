package cellsync

import (
	"context"
	"errors"
	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/syncwords"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type l4HeldUpload struct {
	blobstore.Store
	entered, release chan struct{}
	err              error
}

func (s *l4HeldUpload) PutFrame(ctx context.Context, raw []byte) (string, error) {
	close(s.entered)
	<-s.release
	return "", s.err
}

// L4-R3: Close retains a service failure's origin when its budget ends behind an in-flight upload.
func TestL4FixCloseJoinsUploadOrigin(t *testing.T) {
	r := newRig(t)
	held := &l4HeldUpload{Store: r.mem, entered: make(chan struct{}), release: make(chan struct{}), err: blobstore.ErrUnreachable}
	r.pub.Store = held
	b := r.batcher()
	head := r.seal(map[string]string{"a": "one"})
	b.Note(cellstore.Turn{ID: head})
	run, cancelRun := context.WithCancel(t.Context())
	defer cancelRun()
	done := make(chan struct{})
	go func() { defer close(done); _ = b.Run(run) }()
	<-held.entered
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	closing := make(chan error, 1)
	go func() { closing <- b.Close(ctx) }()
	close(held.release)
	err := <-closing
	cancelRun()
	<-done
	if !syncwords.UploadPending(err) || !errors.Is(err, context.Canceled) {
		t.Fatalf("Close lost upload origin: %v", err)
	}
}

// L4-R3: A newer local branch-write failure replaces an earlier failed upload's provenance.
func TestL4FixLocalResolutionReplacesUploadFailure(t *testing.T) {
	r := newRig(t)
	r.publishFirst(map[string]string{"a": "one"})
	newest := r.seal(map[string]string{"a": "two"})
	b := r.batcher()
	b.Interval = time.Hour
	ticks := make(chan chan struct{})
	resting := make(chan struct{}, 1)
	b.Sleep = func(ctx context.Context, d time.Duration) error {
		if d != directory.HeartbeatEvery {
			select {
			case resting <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return ctx.Err()
		}
		step := make(chan struct{})
		select {
		case ticks <- step:
		case <-ctx.Done():
			return ctx.Err()
		}
		select {
		case <-step:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	r.store.set(blobstore.ErrUnreachable, nil)
	b.Note(cellstore.Turn{ID: newest})
	run, cancelRun := context.WithCancel(t.Context())
	defer cancelRun()
	done := make(chan struct{})
	go func() { defer close(done); _ = b.Run(run) }()
	<-resting
	r.store.set(nil, nil)
	r.takeOver()
	blocker := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocker, []byte("blocks branch map"), 0600); err != nil {
		t.Fatal(err)
	}
	r.branch.path = filepath.Join(blocker, "branches.json")
	step := <-ticks
	close(step)
	<-ticks // The next interval acknowledges the failed heartbeat and its branch resolution.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := b.Close(ctx)
	cancelRun()
	<-done
	var local *os.PathError
	if syncwords.UploadPending(err) || !errors.As(err, &local) {
		t.Fatalf("Close reused stale upload provenance: %v", err)
	}
}
