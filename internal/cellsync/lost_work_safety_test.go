package cellsync

import (
	"github.com/Agent-Field/codeaf/internal/blobstore"
	"testing"
)

func TestStaleHeartbeatRefusesWorkBeforePreservationCanFail(t *testing.T) {
	r := newRig(t)
	r.publishFirst(map[string]string{"file": "shared"})
	b := r.batcher()
	note(b, r.seal(map[string]string{"file": "local work"}))
	r.takeOver()
	r.store.set(blobstore.ErrUnreachable, nil)
	lost := false
	b.OnLost = func() { lost = true }
	b.beat(t.Context())
	if !lost {
		t.Fatal("stale heartbeat left new work enabled when preservation failed")
	}
	if b.Pending() == 0 {
		t.Fatal("failed preservation discarded pending work")
	}
}
