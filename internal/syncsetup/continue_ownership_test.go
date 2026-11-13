package syncsetup

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/session"
)

// TestContinueChecksOwnershipBeforeRetiring proves against the native service that
// continuing never releases this computer's own hold, and that a different holder
// is refused before any local session is retired.
func TestContinueChecksOwnershipBeforeRetiring(t *testing.T) {
	for _, roots := range []bool{false, true} {
		for _, own := range []bool{true, false} {
			t.Run(map[bool]string{false: "ordinary/", true: "workspace roots/"}[roots]+map[bool]string{true: "already held here", false: "different holder won"}[own], func(t *testing.T) {
				var releases, acquires atomic.Int32
				h := newTwoHomes(t, func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/release") {
							releases.Add(1)
						}
						if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/acquire") {
							acquires.Add(1)
						}
						next.ServeHTTP(w, r)
					})
				})
				ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
				defer cancel()
				a := h.openA()
				a.mustSay("saved before continuing")
				h.durable(h.cell.ID, h.cell)
				view, err := h.a.Dir.Cell(ctx, h.cell.ID)
				mustNil(t, err)
				seen := &directory.LeaseExpectation{Device: h.b.Device.ID(), Fence: view.Cell.Lease.Fence - 1}
				if !own {
					seen = &directory.LeaseExpectation{Device: view.Cell.Lease.Device, Fence: view.Cell.Lease.Fence}
					_, err = h.b.Dir.Acquire(ctx, h.cell.ID, directory.AcquireOpts{Force: true, Expect: seen, RequireConditional: true})
					mustNil(t, err)
				}
				before, err := h.a.Dir.Cell(ctx, h.cell.ID)
				mustNil(t, err)
				journal, err := os.Open(transcriptOf(h.cell))
				mustNil(t, err)
				defer journal.Close()
				mustNil(t, filelock.Lock(journal, true, true))
				defer filelock.Unlock(journal)
				var retired atomic.Bool
				stop, done := make(chan struct{}), make(chan struct{})
				go func() {
					defer close(done)
					beat := time.NewTicker(time.Millisecond)
					defer beat.Stop()
					for {
						select {
						case <-stop:
							return
						case <-beat.C:
							if _, err := os.Stat(session.TakeoverPath(h.cell.Root)); err == nil {
								retired.Store(true)
								_ = a.drive.Close(ctx)
								_ = filelock.Unlock(journal)
								return
							}
						}
					}
				}()
				defer func() { close(stop); <-done }()
				r0, a0 := releases.Load(), acquires.Load()
				var got Continued
				if roots {
					got, err = h.continuerA().TakeSeenWithWorkspaceRoots(ctx, h.cell.ID, seen, map[string]string{"root": t.TempDir()})
				} else {
					got, err = h.continuerA().TakeSeen(ctx, h.cell.ID, seen)
				}
				if own && err != nil {
					t.Errorf("already held here refused: %v", err)
				}
				if !own && !errors.Is(err, directory.ErrLeaseHeld) {
					t.Errorf("different holder was not refused: %v", err)
				}
				after, readErr := h.a.Dir.Cell(ctx, h.cell.ID)
				mustNil(t, readErr)
				if retired.Load() || releases.Load() != r0 || acquires.Load() != a0 || !session.InUse(transcriptOf(h.cell)) {
					t.Errorf("continuation retired the session or changed ownership: retired=%v releases=%d acquires=%d inUse=%v", retired.Load(), releases.Load()-r0, acquires.Load()-a0, session.InUse(transcriptOf(h.cell)))
				}
				if after.Cell.Lease != before.Cell.Lease {
					t.Errorf("lease changed: before=%+v after=%+v", before.Cell.Lease, after.Cell.Lease)
				}
				if own {
					cell, fence, readErr := h.a.SendLease(h.cell.ID)
					if readErr != nil || cell != h.cell.ID || fence != before.Cell.Lease.Fence {
						t.Errorf("successful take did not record send lease: %s %d %v", cell, fence, readErr)
					}
				}
				if own && (got.Taken.Driving.Fence != before.Cell.Lease.Fence || got.Taken.Cell.Root != h.cell.Root) {
					t.Errorf("no-op lost the local chat: %+v", got)
				}
			})
		}
	}
}
