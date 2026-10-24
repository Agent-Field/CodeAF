package cellsync

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// C28/C31 require the final saved head before a successful quit marker.
// A reachable directory must not advertise an incomplete upload as a clean quit.
func TestC28C31FailedFinalUploadDoesNotRelease(t *testing.T) {
	r := newRig(t)
	before := r.publishFirst(map[string]string{"work.txt": "published"})
	b := r.batcher()
	latest := r.seal(map[string]string{"work.txt": "local final turn"})
	note(b, latest)
	r.store.set(blobstore.ErrUnreachable, nil)
	if err := b.Close(context.Background()); !errors.Is(err, blobstore.ErrUnreachable) {
		t.Fatalf("close error %v", err)
	}
	v := r.head(cellID)
	if v.Head != before || v.Lease.Expires == 0 || v.Lease.ReleasedAt != 0 || b.Pending() != 1 {
		t.Fatalf("failed quit exposed a release or lost pending work: %+v pending %d", v, b.Pending())
	}
	if _, err := r.dir.For(devB).Acquire(context.Background(), cellID, directory.AcquireOpts{}); !errors.Is(err, directory.ErrLeaseHeld) {
		t.Fatalf("failed quit became available: %v", err)
	}
	r.store.set(nil, nil)
	if err := b.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	v = r.head(cellID)
	if v.Head != latest || v.Lease.Expires != 0 || v.Lease.ReleasedAt == 0 || b.Pending() != 0 {
		t.Fatalf("successful retry did not publish before release: %+v", v)
	}
}
