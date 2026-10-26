package syncsetup

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// C7/R2 keeps ownership checks on a reopened copy's retained branch. The
// original chat may still belong to another computer without fencing the copy.
func TestC7R2ReopenedBranchChecksItsOwnLease(t *testing.T) {
	r := newDriveRig(t)
	ctx := t.Context()
	sealed, err := r.engine.Seal(ctx, r.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	_, err = r.b.Dir.Create(ctx, r.cell.ID, directory.CellInit{Head: sealed.Turn.ID})
	mustNil(t, err)
	const branch = "01J0000000000000000000000B"
	_, err = r.a.Dir.Create(ctx, branch, directory.CellInit{Head: sealed.Turn.ID, ParentCell: r.cell.ID, OrphanTurns: 1})
	mustNil(t, err)
	branches, err := branchMapOf(r.homeA)
	mustNil(t, err)
	mustNil(t, branches.Set(r.cell.ID, branch))
	drive, err := r.a.Drive(ctx, r.engine, r.cell, DriveOptions{DeviceName: "tower"})
	mustNil(t, err)
	defer drive.Close(ctx)
	mustNil(t, drive.CheckLease(ctx))
	if err := drive.Gate(); err != nil || !drive.HoldsLease(ctx) {
		t.Fatalf("a retained copy was refused because its parent moved: %v", err)
	}
	view, err := r.b.Dir.Cell(ctx, branch)
	mustNil(t, err)
	_, err = r.b.Dir.Acquire(ctx, branch, directory.AcquireOpts{Force: true, Expect: &directory.LeaseExpectation{Device: view.Cell.Lease.Device, Fence: view.Cell.Lease.Fence}})
	mustNil(t, err)
	mustNil(t, drive.CheckLease(ctx))
	if drive.Gate() == nil || drive.HoldsLease(ctx) {
		t.Fatal("a taken branch still permits tool work")
	}
}

// C7/R2 lets a chat keep working when its first publication completes while
// an ownership read is in flight. Its new lease belongs to the same window.
func TestC7R2OwnershipReadIncludesFirstPublish(t *testing.T) {
	for _, method := range []string{"check", "holds"} {
		t.Run(method, func(t *testing.T) {
			var observe atomic.Bool
			reading, published, served := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var parent string
			r := newDriveRig(t, func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					blocked := req.Method == http.MethodGet && req.URL.Path == "/v1/dir/cells/"+parent && observe.CompareAndSwap(true, false)
					if blocked {
						close(reading)
						select {
						case <-published:
						case <-req.Context().Done():
							t.Error("ownership read ended before the first publication")
						}
					}
					next.ServeHTTP(w, req)
					if blocked {
						close(served)
					}
				})
			})
			parent = r.cell.ID
			c := r.startChat(&noticeLog{})
			flushed := c.drive.batcher.OnFlush
			c.drive.batcher.OnFlush = func(f cellsync.Flush) {
				flushed(f)
				close(published)
			}
			observe.Store(true)
			result := make(chan bool, 1)
			go func() {
				if method == "holds" {
					result <- c.drive.HoldsLease(t.Context())
					return
				}
				if err := c.drive.CheckLease(t.Context()); err != nil {
					t.Error(err)
				}
				result <- c.drive.Gate() == nil
			}()
			select {
			case <-reading:
			case <-time.After(5 * time.Second):
				t.Fatal("ownership read did not start")
			}
			c.mustSay()
			select {
			case <-served:
			case <-time.After(cellstore.SealWithin):
				t.Fatal("first publication did not finish the ownership read")
			}
			if !<-result {
				t.Fatal("first publication made its own window a viewer")
			}
		})
	}
}

// C7/R2 keeps a displaced window fenced while its pending work becomes a copy,
// even when looking up the new holder's display name has not finished.
func TestC7R2DisplacedWindowDoesNotOwnItsPreservedBranch(t *testing.T) {
	var branching atomic.Bool
	var once, started sync.Once
	entered, release := make(chan struct{}), make(chan struct{})
	branchStarted := make(chan struct{})
	defer close(release)
	var parent string
	r := newDriveRig(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			id := strings.TrimPrefix(req.URL.Path, "/v1/dir/cells/")
			createsBranch := req.Method == http.MethodPost && id != req.URL.Path && id != parent && !strings.Contains(id, "/")
			if createsBranch {
				branching.Store(true)
			}
			if req.Method == http.MethodGet && req.URL.Path == "/v1/dir/list" && branching.Load() {
				once.Do(func() { close(entered) })
				<-release
			}
			next.ServeHTTP(w, req)
			if createsBranch {
				started.Do(func() { close(branchStarted) })
			}
		})
	})
	ctx := t.Context()
	parent = r.cell.ID
	sealed, err := r.engine.Seal(ctx, r.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	_, err = r.a.Dir.Create(ctx, parent, directory.CellInit{Head: sealed.Turn.ID})
	mustNil(t, err)
	c := r.startChat(&noticeLog{})
	view, err := r.b.Dir.Cell(ctx, parent)
	mustNil(t, err)
	_, err = r.b.Dir.Acquire(ctx, parent, directory.AcquireOpts{Force: true, Expect: &directory.LeaseExpectation{Device: view.Cell.Lease.Device, Fence: view.Cell.Lease.Fence}})
	mustNil(t, err)
	c.mustSay()
	mustNil(t, cellstore.Await(ctx, r.cell))
	// Upload the captured files before observing the ownership callback; its
	// metadata lookup, rather than cold engine startup, is the blocked boundary.
	save, cancel := context.WithTimeout(ctx, cellstore.SealWithin)
	defer cancel()
	select {
	case <-branchStarted:
	case <-save.Done():
		t.Fatal("pending work did not reach branch creation")
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("pending work did not reach the preserved branch")
	}
	if c.drive.HoldsLease(ctx) {
		t.Fatal("a displaced window claimed ownership of its preserved copy")
	}
}
