package syncsetup

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestIdleRelease_93d4_SelfReleasedRetakeIsANewFenceNeverARevival(t *testing.T) {
	type wireEvent struct {
		op      string
		fence   uint64
		outside bool
	}
	var (
		mu          sync.Mutex
		events      []wireEvent
		outsideBeat atomic.Bool
	)
	recordFence := func(r *http.Request) uint64 {
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var body struct {
			Fence uint64 `json:"fence"`
		}
		_ = json.Unmarshal(raw, &body)
		return body.Fence
	}
	h := newTwoHomes(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/release"):
				fence := recordFence(r)
				next.ServeHTTP(w, r)
				mu.Lock()
				events = append(events, wireEvent{op: "release", fence: fence})
				mu.Unlock()
			case strings.HasSuffix(r.URL.Path, "/heartbeat"):
				fence := recordFence(r)
				outside := outsideBeat.Load()
				next.ServeHTTP(w, r)
				mu.Lock()
				events = append(events, wireEvent{op: "heartbeat", fence: fence, outside: outside})
				mu.Unlock()
			default:
				next.ServeHTTP(w, r)
			}
		})
	})

	c := idleContractChat(t, h)
	d := c.drive

	// (a) Self-release closes Gate(), sends no heartbeat at the released fence,
	// and an outside heartbeat at that fence does not reopen Gate().
	released := idleContractReleased(t, h, d, uploadedIdleReleaseWithin)
	releasedFence := released.Cell.Lease.Fence
	if d.Gate() == nil {
		t.Fatal("self-released drive still admits work")
	}
	if _, _, err := h.a.SendLease(h.cell.ID); !os.IsNotExist(err) {
		t.Fatalf("self-released drive kept its send receipt: %v", err)
	}
	// Wake the batcher's beat loop while released to prove it stays silent.
	d.Working(true)
	d.Working(false)
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	sawRelease := false
	for _, ev := range events {
		if ev.op == "release" && ev.fence == releasedFence {
			sawRelease = true
			continue
		}
		if sawRelease && ev.op == "heartbeat" && !ev.outside && ev.fence == releasedFence {
			mu.Unlock()
			t.Fatalf("drive sent a heartbeat at released fence %d", releasedFence)
		}
	}
	mu.Unlock()
	if !sawRelease {
		t.Fatalf("did not observe idle release at fence %d", releasedFence)
	}

	outsideBeat.Store(true)
	_, err := h.a.Dir.Heartbeat(t.Context(), h.cell.ID, directory.Beat{Fence: releasedFence})
	outsideBeat.Store(false)
	mustNil(t, err)
	mustNil(t, d.CheckLease(t.Context()))
	if d.Gate() == nil {
		t.Fatal("outside heartbeat at released fence reopened Gate()")
	}
	if _, _, err := h.a.SendLease(h.cell.ID); !os.IsNotExist(err) {
		t.Fatalf("outside heartbeat at released fence restored send receipt: %v", err)
	}

	// (b) New work on the self-released drive acquires a strictly greater fence
	// and reopens Gate() rather than reviving the released fence.
	if !d.BeginWork() {
		t.Fatal("BeginWork did not report the released window")
	}
	confirmed, err := d.ConfirmWork(t.Context())
	if err != nil || !confirmed {
		t.Fatalf("self-released retake = %v, %v; want true, nil", confirmed, err)
	}
	retaken, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	if retaken.Cell.Lease.Device != h.a.Device.ID() || retaken.Cell.Lease.Expires <= retaken.Now {
		t.Fatalf("retaken lease is not live for this device: %+v at %d", retaken.Cell.Lease, retaken.Now)
	}
	if retaken.Cell.Lease.Fence <= releasedFence {
		t.Fatalf("retaken fence = %d, want strictly greater than released fence %d", retaken.Cell.Lease.Fence, releasedFence)
	}
	if err := d.Gate(); err != nil {
		t.Fatalf("retaken drive refuses work: %v", err)
	}
	if _, receiptFence, err := h.a.SendLease(h.cell.ID); err != nil || receiptFence != retaken.Cell.Lease.Fence {
		t.Fatalf("retaken send receipt = %d, %v; want %d, nil", receiptFence, err, retaken.Cell.Lease.Fence)
	}

	// (c) With the same self-released drive, if another device acquires first,
	// ConfirmWork returns false and Gate() refuses with held-elsewhere.
	secondRelease := idleContractReleased(t, h, d, uploadedIdleReleaseWithin)
	if secondRelease.Cell.Lease.Fence != retaken.Cell.Lease.Fence {
		t.Fatalf("second release fence = %d, want %d", secondRelease.Cell.Lease.Fence, retaken.Cell.Lease.Fence)
	}
	_, err = h.b.Dir.Acquire(t.Context(), h.cell.ID, directory.AcquireOpts{
		RequireConditional: true,
		Expect:             &directory.LeaseExpectation{Device: secondRelease.Cell.Lease.Device, Fence: secondRelease.Cell.Lease.Fence},
	})
	mustNil(t, err)
	d.BeginWork()
	confirmed, err = d.ConfirmWork(t.Context())
	if err != nil || confirmed {
		t.Fatalf("displaced self-released ConfirmWork = %v, %v; want false, nil", confirmed, err)
	}
	gateErr := d.Gate()
	if gateErr == nil || !strings.Contains(gateErr.Error(), chatlist.Superseded(nameB)) {
		t.Fatalf("displaced self-released Gate() = %v; want %q", gateErr, chatlist.Superseded(nameB))
	}
	if _, _, err := h.a.SendLease(h.cell.ID); !os.IsNotExist(err) {
		t.Fatalf("displaced self-released drive kept a send receipt: %v", err)
	}

	// (d) A drive whose hold merely lapsed or was released from outside never
	// retakes on ConfirmWork, matching 93d4b7fed.
	t.Run("merely lapsed", func(t *testing.T) {
		lh := newTwoHomes(t)
		lc := idleContractChat(t, lh)
		lh.clock.advance(directory.LeaseTTL + time.Second)
		lc.drive.BeginWork()
		confirmed, err := lc.drive.ConfirmWork(t.Context())
		if err != nil || confirmed {
			t.Fatalf("lapsed ConfirmWork = %v, %v; want false, nil", confirmed, err)
		}
		if lc.drive.Gate() == nil {
			t.Fatal("lapsed drive admits work without continuation")
		}
	})
	t.Run("released from outside", func(t *testing.T) {
		oh := newTwoHomes(t)
		oc := idleContractChat(t, oh)
		held := oc.drive.batcher.Fence()
		mustNil(t, oc.drive.sync.Dir.Release(t.Context(), oh.cell.ID, held))
		mustNil(t, oc.drive.CheckLease(t.Context()))
		if oc.drive.Gate() == nil {
			t.Fatal("outside-released drive admits work")
		}
		_, err := oc.drive.sync.Dir.Heartbeat(t.Context(), oh.cell.ID, directory.Beat{Fence: held})
		mustNil(t, err)
		mustNil(t, oc.drive.CheckLease(t.Context()))
		if oc.drive.Gate() == nil {
			t.Fatal("heartbeat revived outside-released drive")
		}
		oc.drive.BeginWork()
		confirmed, err := oc.drive.ConfirmWork(t.Context())
		if err != nil || confirmed {
			t.Fatalf("outside-released ConfirmWork = %v, %v; want false, nil", confirmed, err)
		}
		if oc.drive.Gate() == nil {
			t.Fatal("outside-released drive admits work after ConfirmWork")
		}
	})
}
