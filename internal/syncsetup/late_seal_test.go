package syncsetup

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestLateSealOnExpiredUnacquiredHoldDoesNotRenewLease(t *testing.T) {
	r := newDriveRig(t)
	ctx := t.Context()
	r.spokenFixture()
	first, err := r.engine.Seal(ctx, r.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	_, err = r.a.Dir.Create(ctx, r.cell.ID, directory.CellInit{Head: first.Turn.ID})
	mustNil(t, err)
	mustNil(t, cellstore.NotePublished(r.cell, first.Turn.ID))
	d, err := r.a.Drive(ctx, r.engine, r.cell, DriveOptions{})
	mustNil(t, err)
	t.Cleanup(func() { mustNil(t, d.Close(context.Background())) })
	// Stop renewal traffic so expiry leaves the original fence unacquired.
	d.cancel()
	<-d.done
	before, err := r.a.Dir.Cell(ctx, r.cell.ID)
	mustNil(t, err)
	r.clock.Advance(directory.LeaseTTL + time.Second)
	confirmed, err := d.ConfirmWork(ctx)
	mustNil(t, err)
	if confirmed || d.Gate() == nil {
		t.Fatal("expired hold admitted work")
	}
	sealed, err := d.Store(r.engine).Seal(ctx, r.cell, cellstore.TurnInfo{Calls: []cellstore.Executed{{Call: cellstore.Call{Tool: "external", Started: 1, Ended: 2}, Stdout: []byte("finished once")}}})
	mustNil(t, err)
	// The ownership check above retired this computer's hold, so closing drains
	// the late seal into a kept copy and never publishes it onto the original.
	// The expiry is compared as well as the head, because a publish onto the
	// original would renew the expired hold even when the head stayed put.
	mustNil(t, d.Close(ctx))
	after, err := r.b.Dir.Cell(ctx, r.cell.ID)
	mustNil(t, err)
	if after.Cell.Head != first.Turn.ID {
		t.Fatalf("late seal moved the original head to %s", after.Cell.Head)
	}
	if after.Cell.Lease.Fence != before.Cell.Lease.Fence || after.Cell.Lease.Expires > before.Cell.Lease.Expires {
		t.Fatalf("late seal renewed the expired hold: fence %d -> %d, expiry %d -> %d", before.Cell.Lease.Fence, after.Cell.Lease.Fence, before.Cell.Lease.Expires, after.Cell.Lease.Expires)
	}
	branches, err := branchMapOf(r.homeA)
	mustNil(t, err)
	id := branches.Resolve(r.cell.ID)
	if id == r.cell.ID {
		t.Fatal("late seal was not preserved in a branch")
	}
	kept, err := r.a.Dir.Cell(ctx, id)
	mustNil(t, err)
	if kept.Cell.Head != sealed.Turn.ID || kept.Cell.ParentCell != r.cell.ID || d.batcher.Pending() != 0 {
		t.Fatal("branch did not preserve the late seal")
	}
	taken, err := r.b.Dir.Acquire(ctx, r.cell.ID, directory.AcquireOpts{})
	mustNil(t, err)
	if taken.Cell.Lease.Fence != before.Cell.Lease.Fence+1 {
		t.Fatal("continuation did not increment the original fence exactly once")
	}
}

// TestLateSealAfterLostHoldOrders walks a call that finishes after its hold is
// gone through each order of events. The call stays in local history or a kept
// branch, and it never moves a head past a newer holder's fence.
func TestLateSealAfterLostHoldOrders(t *testing.T) {
	for _, order := range []string{"take-seal-displace", "take-displace-seal", "expire-check-seal", "expire-confirm-seal", "take-seal-reopen", "displace-in-flight", "branch-seal"} {
		t.Run(order, func(t *testing.T) {
			r := newDriveRig(t)
			ctx := t.Context()
			r.spokenFixture()
			first, err := r.engine.Seal(ctx, r.cell, cellstore.TurnInfo{})
			mustNil(t, err)
			_, err = r.a.Dir.Create(ctx, r.cell.ID, directory.CellInit{Head: first.Turn.ID})
			mustNil(t, err)
			mustNil(t, cellstore.NotePublished(r.cell, first.Turn.ID))
			d, err := r.a.Drive(ctx, r.engine, r.cell, DriveOptions{})
			mustNil(t, err)
			t.Cleanup(func() { mustNil(t, d.Close(context.Background())) })
			// Freeze background traffic to select the event order deterministically.
			d.cancel()
			<-d.done
			before, err := r.a.Dir.Cell(ctx, r.cell.ID)
			mustNil(t, err)
			expiry := order == "expire-check-seal" || order == "expire-confirm-seal"
			if expiry {
				r.clock.Advance(directory.LeaseTTL + time.Second)
			} else {
				_, err = r.b.Dir.Acquire(ctx, r.cell.ID, directory.AcquireOpts{Force: true, Expect: &directory.LeaseExpectation{Device: before.Cell.Lease.Device, Fence: before.Cell.Lease.Fence}})
				mustNil(t, err)
			}
			if order == "take-displace-seal" || order == "expire-check-seal" {
				mustNil(t, d.CheckLease(ctx))
			}
			if order == "expire-confirm-seal" {
				ok, err := d.ConfirmWork(ctx)
				mustNil(t, err)
				if ok || d.Gate() == nil {
					t.Fatal("expired hold admitted work")
				}
			}
			wantFence := before.Cell.Lease.Fence
			if order == "branch-seal" {
				_, err = d.Store(r.engine).Seal(ctx, r.cell, cellstore.TurnInfo{Calls: []cellstore.Executed{{Call: cellstore.Call{Tool: "earlier", Started: 0}}}})
				mustNil(t, err)
				mustNil(t, d.CheckLease(ctx))
				wantFence = 1
				ok, err := d.ConfirmWork(ctx)
				mustNil(t, err)
				if ok || d.Gate() == nil {
					t.Fatal("branch revived the displaced window")
				}
			}
			sealed, err := d.Store(r.engine).Seal(ctx, r.cell, cellstore.TurnInfo{Calls: []cellstore.Executed{{Call: cellstore.Call{Tool: "external", ArgsHash: "finished", Started: 1, Ended: 2, SideEffect: "external"}, Stdout: []byte("finished once")}}})
			mustNil(t, err)
			if sealed.Turn.Fence != wantFence || d.batcher.Pending() != 1 {
				t.Fatal("late seal did not use the expected fence and one noted turn")
			}
			if order == "take-seal-reopen" {
				// Reconstruct startup without letting the old process close or resolve.
				reopened, err := openSync(t, r.homeA).Drive(ctx, r.engine, r.cell, DriveOptions{})
				mustNil(t, err)
				defer reopened.Close(context.Background())
				if reopened.Gate() == nil || reopened.batcher != nil {
					t.Fatal("reopen took ownership from the current holder")
				}
				head, err := cellstore.Head(r.cell)
				mustNil(t, err)
				if head == nil || head.Turn.ID != sealed.Turn.ID || len(head.Receipt.Calls) != 1 {
					t.Fatal("reopen lost the local sealed call")
				}
				return
			}
			if order == "take-seal-displace" {
				mustNil(t, d.CheckLease(ctx))
			}
			if order == "displace-in-flight" {
				entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
				var releaseOnce sync.Once
				t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
				newID := d.batcher.Brancher.NewID
				d.batcher.Brancher.NewID = func() (string, error) {
					close(entered)
					<-release
					return newID()
				}
				go func() { d.batcher.Displaced(ctx); close(done) }()
				<-entered
				checked := make(chan error, 1)
				go func() { checked <- d.CheckLease(ctx) }()
				select {
				case err := <-checked:
					t.Fatalf("CheckLease returned before the held call reached its branch: %v", err)
				case <-time.After(200 * time.Millisecond):
				}
				releaseOnce.Do(func() { close(release) })
				mustNil(t, <-checked)
				<-done
				branches, err := branchMapOf(r.homeA)
				mustNil(t, err)
				if branches.Resolve(r.cell.ID) == r.cell.ID {
					t.Fatal("CheckLease returned without the held call in a branch")
				}
			}
			// Close drains noted work even when Displaced already ran with no work.
			mustNil(t, d.Close(ctx))
			view, err := r.a.Dir.Cell(ctx, r.cell.ID)
			mustNil(t, err)
			branches, err := branchMapOf(r.homeA)
			mustNil(t, err)
			id := branches.Resolve(r.cell.ID)
			if expiry && (view.Cell.Lease.Fence != before.Cell.Lease.Fence || view.Cell.Lease.Expires != before.Cell.Lease.Expires) {
				t.Fatal("late seal changed the expired hold")
			}
			if id == r.cell.ID || view.Cell.Head != first.Turn.ID {
				t.Fatal("late call overwrote the original head or was not branched")
			}
			kept, err := r.a.Dir.Cell(ctx, id)
			mustNil(t, err)
			if kept.Cell.Head != sealed.Turn.ID || kept.Cell.ParentCell != r.cell.ID || d.batcher.Pending() != 0 {
				t.Fatal("branch did not preserve exactly the noted head")
			}
			if !expiry && view.Cell.Lease.Device != r.b.Device.ID() {
				t.Fatal("branch changed the current holder")
			}
		})
	}
}
