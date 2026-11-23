package syncsetup

import (
	"context"
	"testing"
	"time"
)

func TestIdleRelease_R8_CanceledCloseStopsOrdinaryWriter(t *testing.T) {
	h := newTwoHomes(t)
	c := h.openA()
	c.mustSay("ordinary never-released chat")
	h.durable(h.cell.ID, h.cell)
	d := c.drive
	// This test deliberately exercises the expired shared shutdown budget. Clean
	// up the test's own surviving run even if Close consumed its once too soon.
	defer func() { d.cancel(); <-d.done }()
	listing, err := h.a.Dir.List(t.Context())
	mustNil(t, err)
	t.Logf("registered devices=%d; never called Idle", len(listing.Devices))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	closeErr := d.Close(ctx)
	done := false
	select {
	case <-d.done:
		done = true
	case <-time.After(200 * time.Millisecond):
	}
	t.Logf("Close(canceled)=%v; gate=%v; drive_done=%v", closeErr, d.Gate(), done)
	if d.Gate() == nil {
		t.Error("closed ordinary writer still admits work")
	}
	if !done {
		t.Error("Close left the ordinary publisher and heartbeat run alive")
	}
	again := d.Close(t.Context())
	t.Logf("second Close=%v; gate=%v", again, d.Gate())
}
