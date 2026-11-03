package syncsetup

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestIdleRelease_R5_4_IdleNoticeCannotOvertakeReacquire(t *testing.T)   { idleR5Notice(t, true) }
func TestIdleRelease_R5_4_UnchangedFreeObservationKeepsNotice(t *testing.T) { idleR5Notice(t, false) }
func idleR5Notice(t *testing.T, reacquire bool) {
	h := newTwoHomes(t)
	mustNil(t, h.b.putDevice(t.Context(), nameB))
	var current atomic.Pointer[Drive]
	var armed atomic.Bool
	at, resume := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	release := func() { unblock.Do(func() { close(resume) }) }
	defer release()
	var mu sync.Mutex
	var events []string
	record := func(event string) { mu.Lock(); defer mu.Unlock(); events = append(events, event) }
	d, err := h.a.Drive(t.Context(), h.engine, h.cell, DriveOptions{
		DeviceName: nameA,
		OnDriving:  func() { record("driving") },
		OnViewer:   func(v Viewer) { record(v.Line) },
		OnRenewalChange: func() {
			drive := current.Load()
			if !armed.Load() || drive == nil || !drive.IdleReleased() {
				return
			}
			if !drive.workMu.TryLock() {
				return
			}
			drive.workMu.Unlock()
			if armed.Swap(false) {
				close(at)
				<-resume
			}
		},
	})
	mustNil(t, err)
	current.Store(d)
	t.Cleanup(func() { release(); _ = d.Close(context.Background()) })
	appendTo(t, transcriptOf(h.cell), "{\"type\":\"message\",\"role\":\"user\",\"content\":\"notice ordering\"}\n")
	_, err = d.Store(h.engine).Seal(t.Context(), h.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	h.durable(h.cell.ID, h.cell)
	settleIdleFixture(t, h, d)
	func() { mu.Lock(); defer mu.Unlock(); events = nil }()
	armed.Store(true)
	d.Idle()
	select {
	case <-at:
	case <-time.After(15 * time.Second):
		t.Fatal("deferred release notification did not start")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if reacquire {
		confirmed, err := d.ConfirmWork(ctx)
		if err != nil || !confirmed {
			t.Fatalf("reacquire=%v,%v", confirmed, err)
		}
	} else {
		mustNil(t, d.CheckLease(ctx))
	}
	release()
	if !reacquire {
		// This assertion concerns the deferred notice, not a timed-out release's
		// later retry. Cancel preparation without changing the ownership version.
		d.BeginWork()
	}
	done := make(chan struct{})
	go func() { d.idleWorkers.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("release worker did not finish")
	}
	got, last := func() (string, string) {
		mu.Lock()
		defer mu.Unlock()
		last := ""
		if len(events) > 0 {
			last = events[len(events)-1]
		}
		return strings.Join(events, " -> "), last
	}()
	t.Logf("surface events=%s; gate=%v; released=%v", got, d.Gate(), d.IdleReleased())
	if reacquire && last == chatlist.Available {
		t.Error("stale idle notification overwrote the successful reacquisition")
	}
	if !reacquire && last != chatlist.Available {
		t.Error("unchanged free observation canceled the pending release notice")
	}
}
