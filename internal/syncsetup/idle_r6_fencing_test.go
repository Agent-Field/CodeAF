package syncsetup

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/session"
)

type r6AcquireDirectory struct {
	directory.Client
	acquires []directory.AcquireOpts
}

func (d *r6AcquireDirectory) Acquire(ctx context.Context, id string, opts directory.AcquireOpts) (directory.CellView, error) {
	d.acquires = append(d.acquires, opts)
	return d.Client.Acquire(ctx, id, opts)
}

func TestIdleRelease_R6_LapsedHolderIsFencedAfterAnOpenElsewhere(t *testing.T) {
	h := newTwoHomes(t)
	c := idleContractChat(t, h)
	d := c.drive
	a, _ := idleContractSession(t, h, c, func(cfg *session.Config) {
		// Match foreground admission: a failed confirmation leaves the drive's
		// typed refusal intact, including the holder's name.
		cfg.Seat = executor.Gated(c.seat, d.Gate, func() error {
			if _, err := d.ConfirmWork(t.Context()); err != nil {
				return err
			}
			return d.Gate()
		})
	})
	d.SetIdleCheck(func() bool { return false })
	d.Working(true)
	// Freeze A's renewal and publication loops while its unfinished work stays
	// on disk, then lapse the relay clock without waiting ninety real seconds.
	d.cancel()
	<-d.done
	old, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	_, err = h.a.Dir.Heartbeat(t.Context(), h.cell.ID, directory.Beat{Fence: old.Cell.Lease.Fence, Working: true})
	mustNil(t, err)
	h.block.set(true)
	t.Cleanup(func() { h.block.set(false) })
	c.mustSay("unfinished local turn during the outage")
	localHead := h.head()
	localWords, err := os.ReadFile(transcriptOf(h.cell))
	mustNil(t, err)
	h.clock.advance(directory.LeaseTTL + time.Second)
	mustNil(t, d.CheckLease(t.Context()))
	if confirmed, err := d.ConfirmWork(t.Context()); err != nil || confirmed {
		t.Fatalf("expired holder retook automatically: confirmed=%v err=%v", confirmed, err)
	}
	// (a) The real tool seat and durable-save door both reject new work.
	ran := false
	err = c.seat.Around(t.Context(), executor.Call{Tool: "bash"}, func() ([]byte, bool) {
		ran = true
		return nil, false
	})
	if err == nil || ran {
		t.Fatalf("expired holder admitted a new tool: ran=%v err=%v", ran, err)
	}
	if _, err := d.Store(h.engine).Seal(t.Context(), h.cell, cellstore.TurnInfo{}); !errors.Is(err, cellstore.ErrReadOnly) {
		t.Fatalf("expired holder admitted a new durable write: %v", err)
	}
	// (b) Under R6-5/R6-6, B opens this lapsed chat after confirming its card.
	// That consent still uses the conditional acquire and a strictly new fence.
	seen, err := h.b.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	if seen.Cell.Lease.Expires == 0 || seen.Cell.Lease.Expires > seen.Now || !seen.Cell.Lease.Working {
		t.Fatalf("fixture did not lapse while working: %+v", seen)
	}
	record := &r6AcquireDirectory{Client: h.b.Dir}
	h.b.Dir = record
	continuer := h.continuerB()
	expect := &directory.LeaseExpectation{Device: seen.Cell.Lease.Device, Fence: seen.Cell.Lease.Fence}
	if _, err := continuer.OpenAvailable(t.Context(), h.cell.ID, expect); !errors.Is(err, directory.ErrLeaseHeld) || len(record.acquires) != 0 {
		t.Fatalf("B bypassed the lapsed holder's card: err=%v acquires=%d", err, len(record.acquires))
	}
	got, err := continuer.TakeSeen(t.Context(), h.cell.ID, expect)
	mustNil(t, err)
	if len(record.acquires) != 1 {
		t.Fatalf("B acquired %d times", len(record.acquires))
	}
	claim := record.acquires[0]
	if !claim.RequireConditional || claim.Force || claim.Expect == nil || claim.Expect.Device != old.Cell.Lease.Device || claim.Expect.Fence != old.Cell.Lease.Fence || got.Taken.Driving.Fence <= old.Cell.Lease.Fence {
		t.Fatalf("B's open was not conditional at a new fence: %+v, fence=%d", claim, got.Taken.Driving.Fence)
	}
	bHead := got.Taken.Driving.Head
	// (c) A's delayed publish reaches the directory with its original fence.
	_, err = h.a.Dir.Publish(t.Context(), h.cell.ID, directory.Publish{Fence: old.Cell.Lease.Fence, OldHead: old.Cell.Head, Head: localHead})
	if !errors.Is(err, directory.ErrFenceStale) {
		t.Fatalf("directory accepted A's stale publish: %v", err)
	}
	fresh, err := h.b.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	if fresh.Cell.Head != bHead || fresh.Cell.Lease.Fence != got.Taken.Driving.Fence || fresh.Cell.Lease.Device != h.b.Device.ID() {
		t.Fatalf("stale A publish changed B's head or hold: %+v", fresh)
	}
	// (d) Neither the local journal nor its sealed head is lost when A learns
	// about B. Its background loop remains frozen, so pending branch upload is
	// left for the normal publication retry after this check.
	h.block.set(false)
	mustNil(t, d.CheckLease(t.Context()))
	after, err := os.ReadFile(transcriptOf(h.cell))
	mustNil(t, err)
	if !bytes.Equal(localWords, after) || h.head() != localHead || localHead == old.Cell.Head || !bytes.Contains(after, []byte("unfinished local turn during the outage")) {
		t.Fatal("A's unfinished local turn was lost")
	}
	// (e) The next real session send refuses with B's name before journaling.
	events, err := a.Submit(t.Context(), "new work must stay unsent")
	var refusal *executor.WorkRefusal
	if events != nil || !errors.As(err, &refusal) || refusal.HolderID != h.b.Device.ID() || !strings.Contains(refusal.Line, chatlist.Superseded(nameB)) {
		t.Fatalf("A's next send did not say held elsewhere once: stream=%v err=%v gate=%v", events != nil, err, d.Gate())
	}
	after, err = os.ReadFile(transcriptOf(h.cell))
	mustNil(t, err)
	if !bytes.Equal(localWords, after) {
		t.Fatal("A's refused send wrote to its journal")
	}
}
