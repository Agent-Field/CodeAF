package syncsetup

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestDriveOwnReleaseDoesNotBecomeViewer(t *testing.T) {
	released, checked := make(chan struct{}), make(chan struct{})
	r := newDriveRig(t)
	r.spokenFixture()
	var notices noticeLog
	c := r.startChat(&notices)
	c.mustSay()
	waitFor(t, "published head", func() bool { return r.directoryHead(r.cell.ID) == r.head() })
	entered := make(chan struct{})
	var once sync.Once
	c.drive.BeforeOwnershipCheckInTests(func(ctx context.Context) func() {
		var after func()
		once.Do(func() {
			close(entered)
			select {
			case <-released:
			case <-ctx.Done():
			}
			after = func() { close(checked) }
		})
		return after
	}, func() { close(released); <-checked })
	// An unrelated directory change places the watcher at the read barrier.
	if err := r.b.Dir.PutDevice(t.Context(), r.b.Device.ID(), directory.Device{V: 1, Name: "barrier"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	if err := c.drive.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if line, viewer := c.drive.Viewer(); viewer {
		t.Fatalf("own clean release became viewer: %q", line)
	}
	if got := r.directoryHead(r.cell.ID); got != r.head() {
		t.Fatalf("close head = %q, want %q", got, r.head())
	}
}

// Cancelling the run must stop its child watcher even without a Close call.
func TestDriveRunCancelStopsOwnershipWatcher(t *testing.T) {
	r := newDriveRig(t)
	r.spokenFixture()
	var notices noticeLog
	c := r.startChat(&notices)
	c.mustSay()
	waitFor(t, "published head", func() bool { return r.directoryHead(r.cell.ID) == r.head() })
	entered := make(chan struct{})
	var once sync.Once
	c.drive.BeforeOwnershipCheckInTests(func(ctx context.Context) func() {
		once.Do(func() {
			close(entered)
			<-ctx.Done()
		})
		return nil
	}, nil)
	if err := r.b.Dir.PutDevice(t.Context(), r.b.Device.ID(), directory.Device{V: 1, Name: "barrier"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("ownership watcher did not enter its reading")
	}
	c.drive.cancel()
	select {
	case <-c.drive.watcherDone:
	case <-time.After(5 * time.Second):
		t.Fatal("run cancellation did not join the ownership watcher")
	}
	select {
	case <-c.drive.done:
	case <-time.After(5 * time.Second):
		t.Fatal("run cancellation did not join the batcher")
	}
}
