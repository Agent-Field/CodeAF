package syncsetup

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestIdleRelease_R10_LateCommitWaitsForLapseAfterEarlyRetry(t *testing.T) {
	var requests atomic.Int32
	allow, committed := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(allow) })
	defer release()
	h := newTwoHomes(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/watch") && r.URL.Query().Get("events") == "1" {
				<-r.Context().Done()
				return
			}
			if strings.HasSuffix(r.URL.Path, "/acquire") && requests.Load() > 0 {
				switch requests.Add(1) {
				case 2:
					raw, _ := io.ReadAll(r.Body)
					<-r.Context().Done()
					<-allow
					late := r.Clone(context.WithoutCancel(r.Context()))
					late.Body = io.NopCloser(bytes.NewReader(raw))
					next.ServeHTTP(httptest.NewRecorder(), late)
					close(committed)
					return
				case 3:
					// The retry read the old free fence. Commit the first request
					// before the retry reaches the relay's conditional acquire.
					release()
					<-committed
				}
			}
			next.ServeHTTP(w, r)
		})
	})
	c := idleContractChat(t, h)
	before := idleContractReleased(t, h, c.drive)
	requests.Store(1)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if confirmed, err := c.drive.ConfirmWork(ctx); err == nil || confirmed {
		t.Fatalf("first acquire=%v error=%v", confirmed, err)
	}
	confirmed, err := c.drive.ConfirmWork(t.Context())
	if err == nil || confirmed || c.drive.Gate() == nil || !strings.Contains(c.drive.Gate().Error(), "try again") {
		t.Fatalf("late commit must remain uncertain: confirmed=%v error=%v gate=%v", confirmed, err, c.drive.Gate())
	}
	h.clock.advance(directory.LeaseTTL + time.Millisecond)
	confirmed, err = c.drive.ConfirmWork(t.Context())
	if err != nil || !confirmed || c.drive.Gate() != nil {
		t.Fatalf("retry after lapse stuck: confirmed=%v error=%v gate=%v", confirmed, err, c.drive.Gate())
	}
	at, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	if at.Cell.Lease.Fence != before.Cell.Lease.Fence+2 {
		t.Fatalf("lapsed acquire was not replaced: before=%d after=%d", before.Cell.Lease.Fence, at.Cell.Lease.Fence)
	}
}

// The relay commits the real signed acquire, but its reply misses the caller's
// deadline. No ownership or readiness result is substituted by this wrapper.
func TestIdleRelease_R10_CommittedAcquireLostReplyRecoversAfterLapse(t *testing.T) {
	var armed atomic.Bool
	committed := make(chan struct{}, 1)
	h := newTwoHomes(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/watch") && r.URL.Query().Get("events") == "1" {
				<-r.Context().Done()
				return
			}
			if strings.HasSuffix(r.URL.Path, "/acquire") && armed.CompareAndSwap(true, false) {
				reply := httptest.NewRecorder()
				next.ServeHTTP(reply, r.WithContext(context.WithoutCancel(r.Context())))
				if reply.Code == http.StatusOK {
					committed <- struct{}{}
				}
				<-r.Context().Done()
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	c := idleContractChat(t, h)
	a, _ := idleContractSession(t, h, c)
	before := idleContractReleased(t, h, c.drive)
	armed.Store(true)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	confirmed, err := c.drive.ConfirmWork(ctx)
	if err == nil || confirmed {
		t.Fatalf("lost acquire reply: confirmed=%v error=%v", confirmed, err)
	}
	select {
	case <-committed:
	default:
		t.Fatal("fixture did not commit the acquire before the deadline")
	}
	if gate := c.drive.Gate(); gate == nil || strings.Contains(gate.Error(), chatlist.Available) || !strings.Contains(gate.Error(), "try again") {
		t.Fatalf("uncertain acquire needs an honest retryable refusal, got %v", gate)
	}
	mustNil(t, c.drive.CheckLease(t.Context()))
	if gate := c.drive.Gate(); gate == nil || !strings.Contains(gate.Error(), "try again") {
		t.Fatalf("ownership observation guessed the uncertain writer: %v", gate)
	}
	confirmed, err = c.drive.ConfirmWork(t.Context())
	if err == nil || confirmed {
		t.Fatalf("live uncertain fence was adopted: confirmed=%v error=%v", confirmed, err)
	}
	at, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	if at.Cell.Lease.Fence != before.Cell.Lease.Fence+1 {
		t.Fatalf("uncertain retry acquired again: before=%d after=%d", before.Cell.Lease.Fence, at.Cell.Lease.Fence)
	}
	if _, _, holding := c.drive.RenewalStatus(); holding {
		t.Fatal("uncertain writer must not renew")
	}
	h.clock.advance(directory.LeaseTTL + time.Millisecond)
	lapsed, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	if lapsed.Cell.Lease.Expires != at.Cell.Lease.Expires || lapsed.Cell.Lease.Expires > lapsed.Now {
		t.Fatalf("uncertain lease did not lapse unchanged: before=%+v after=%+v", at, lapsed)
	}
	confirmed, err = c.drive.ConfirmWork(t.Context())
	if err != nil || !confirmed || c.drive.Gate() != nil {
		t.Fatalf("retry after lapse stuck: confirmed=%v error=%v gate=%v", confirmed, err, c.drive.Gate())
	}
	at, err = h.a.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	if at.Cell.Lease.Fence != before.Cell.Lease.Fence+2 {
		t.Fatalf("retry did not conditionally acquire a new fence: %+v", at.Cell.Lease)
	}
	events, err := a.SubmitBash(t.Context(), "! printf ran > r10-late-acquire.txt")
	mustNil(t, err)
	for range events {
	}
	raw, err := os.ReadFile(filepath.Join(h.work, "r10-late-acquire.txt"))
	mustNil(t, err)
	if string(raw) != "ran" {
		t.Fatalf("retried send did not run: %q", raw)
	}
}
