package blobstore_test

import (
	"context"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
)

type trafficTally struct {
	mu                 sync.Mutex
	begun, ended       int
	up, down           int64
	inflightAtFirstHit int
}

func (t *trafficTally) Begin() { t.mu.Lock(); t.begun++; t.mu.Unlock() }
func (t *trafficTally) End()   { t.mu.Lock(); t.ended++; t.mu.Unlock() }
func (t *trafficTally) Moved(up, down int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.up == 0 && t.down == 0 {
		t.inflightAtFirstHit = t.begun - t.ended
	}
	t.up += up
	t.down += down
}

// A move shows its bytes while they move and stops one whose bytes stopped:
// the wire reports each request's start and end and every byte both ways.
func TestTrafficWatchHearsRequestsAndBytes(t *testing.T) {
	g := newRig(t)
	c := g.client("alice")
	o, f := frame(t, "moving")
	tally := &trafficTally{}
	ctx := blobstore.WithTraffic(context.Background(), tally)
	if _, err := c.PutFrame(ctx, f); err != nil {
		t.Fatal(err)
	}
	got, err := c.GetMany(ctx, []string{o.RID})
	if err != nil || len(got) != 1 {
		t.Fatalf("GetMany = %v, %v", got, err)
	}
	tally.mu.Lock()
	defer tally.mu.Unlock()
	if tally.begun != 2 || tally.ended != 2 {
		t.Fatalf("requests begun %d ended %d, want 2 and 2", tally.begun, tally.ended)
	}
	if tally.up < int64(len(f)) {
		t.Fatalf("up = %d, want at least the frame's %d bytes", tally.up, len(f))
	}
	if tally.down < int64(len(o.Bytes)) {
		t.Fatalf("down = %d, want at least the object's %d bytes", tally.down, len(o.Bytes))
	}
	if tally.inflightAtFirstHit != 1 {
		t.Fatalf("bytes moved with %d requests in flight, want 1", tally.inflightAtFirstHit)
	}
}

// Without a watch the wire is unchanged.
func TestTrafficWatchAbsentReportsNothing(t *testing.T) {
	g := newRig(t)
	c := g.client("alice")
	_, f := frame(t, "quiet")
	if _, err := c.PutFrame(blobstore.WithTraffic(context.Background(), nil), f); err != nil {
		t.Fatal(err)
	}
}
