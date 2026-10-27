package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

type idleAdmissionFixture struct {
	b          *appBackend
	rec        *appRecorder
	agent      appwire.Agent
	cellID     string
	owner      *syncsetup.Sync
	reads      atomic.Int64
	modelCalls atomic.Int64
	slow       atomic.Bool
	timeouts   atomic.Int64
	slowest    atomic.Int64
	waited     atomic.Int64
}

// Durable fixture setup can share a busy disk with other lanes. The contract's
// admission deadline is measured separately, after the chat is ready.
func idleAdmissionWait(t *testing.T, what string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	for !ready() {
		if time.Now().After(deadline) {
			t.Fatal("timed out preparing " + what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func idleAdmissionChat(t *testing.T) *idleAdmissionFixture {
	t.Helper()
	celltest.RequireEngine(t)
	f := &idleAdmissionFixture{}
	svc := relayserve.New(relayserve.Config{Store: t.TempDir()})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/cells/") {
			f.reads.Add(1)
			if f.slow.Load() {
				start := time.Now()
				select {
				case <-r.Context().Done():
					duration := time.Since(start).Nanoseconds()
					f.waited.Add(duration)
					for old := f.slowest.Load(); duration > old; old = f.slowest.Load() {
						if f.slowest.CompareAndSwap(old, duration) {
							break
						}
					}
					f.timeouts.Add(1)
				case <-time.After(3 * time.Second):
				}
				return
			}
		}
		svc.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { srv.Close(); _ = svc.Close() })
	b, rec, model, workspace := appSyncBackend(t)
	f.b, f.rec = b, rec
	handler := model.server.Config.Handler
	model.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			f.modelCalls.Add(1)
		}
		handler.ServeHTTP(w, r)
	})
	t.Setenv(syncsetup.URLVar, srv.URL)
	t.Setenv(syncsetup.IntervalVar, "600000")
	t.Setenv("CODEAF_MODEL", "test/model")
	if _, err := identity.Ensure(home.Dir()); err != nil {
		t.Fatal(err)
	}
	var err error
	f.agent, err = b.CreateAgent(appwire.CreateAsk{Workspace: workspace, Model: "test/model"})
	if err != nil {
		t.Fatal(err)
	}
	idleAdmissionWait(t, "the conversation", func() bool { return b.liveOf(b.agent(f.agent.ID)) != nil })
	turn, err := b.Send(f.agent.ID, "seed the idle chat", "seed")
	if err != nil {
		t.Fatal(err)
	}
	appTurnDoneFor(t, rec, turn)
	f.owner = appSyncOwn(t)
	f.cellID = v3PlaceOf(appTranscriptOf(b, f.agent.ID), workspace).ID()
	nextRetry := time.Now().Add(40 * time.Second)
	idleAdmissionWait(t, "the seed to publish", func() bool {
		v, err := f.owner.Dir.Cell(context.Background(), f.cellID)
		if err == nil && v.Cell.Head != "" {
			return true
		}
		// A busy shared disk can exhaust an initial capture's own budget.
		// Retry fixture work before measuring the admission contract.
		if time.Now().After(nextRetry) {
			turn, err := b.Send(f.agent.ID, "retry the fixture seed", "")
			if err != nil {
				t.Fatal(err)
			}
			appTurnDoneFor(t, rec, turn)
			nextRetry = time.Now().Add(40 * time.Second)
		}
		return false
	})
	// The contract's short freshness window must have expired after five idle seconds.
	time.Sleep(5 * time.Second)
	return f
}

// I1/I2: an idle app learns a real relay takeover before any user-work door
// submits, keeps the journal/model untouched, and emits the warm viewer facts.
func TestI1I2IdleAppRefusesWorkAfterTakeover(t *testing.T) {
	f := idleAdmissionChat(t)
	otherHome := t.TempDir()
	if _, err := identity.Adopt(otherHome, f.owner.Identity, false); err != nil {
		t.Fatal(err)
	}
	other, ok, err := syncsetup.Open(otherHome)
	if err != nil || !ok {
		t.Fatalf("other computer = %v, %v", ok, err)
	}
	if err := syncsetup.PutOwnDevice(t.Context(), other.Dir, other.Identity, other.Device, "laptop"); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Dir.Acquire(t.Context(), f.cellID, directory.AcquireOpts{Force: true}); err != nil {
		t.Fatal(err)
	}
	journal := appTranscriptOf(f.b, f.agent.ID)
	before, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	calls, reads := f.modelCalls.Load(), f.reads.Load()
	wire := newActionWire(t, f.b)
	for _, method := range []string{"agents.send", "agents.queue", "agents.steer", "agents.retry"} {
		_, refusal := wire.call(method, map[string]any{"id": f.agent.ID, "text": "must stay out of the journal"})
		if refusal == nil || refusal.Code != appwire.CodeConflict {
			t.Fatalf("%s = %+v, want held_elsewhere", method, refusal)
		}
		raw, _ := json.Marshal(refusal.Detail)
		var detail appwire.HeldElsewhere
		if err := json.Unmarshal(raw, &detail); err != nil || detail.Kind != "held_elsewhere" || detail.Holder == nil || detail.Holder.ID != other.Device.ID() || detail.Holder.Name != "laptop" {
			t.Fatalf("%s detail = %s, %v", method, raw, err)
		}
	}
	if got := f.reads.Load() - reads; got != 1 {
		t.Fatalf("ownership reads = %d, want one then cached refusals", got)
	}
	entryWith(f.rec, t, func(e appwire.Entry) bool {
		return e.Agent == f.agent.ID && e.NoticeKind == appwire.NoticeMoved && !e.Quiet
	})
	after, err := os.ReadFile(journal)
	if err != nil || !bytes.Equal(before, after) || f.modelCalls.Load() != calls {
		t.Fatalf("refusal changed journal or model: journal=%v calls=%d -> %d err=%v", bytes.Equal(before, after), calls, f.modelCalls.Load(), err)
	}
}

// I1/I3: after idle, the app's admission boundary confirms a current holder
// with one single-chat read; an immediate second admission reuses it. The
// admitted send then reaches the real model, independently of disk latency.
func TestI1I3IdleAppRefreshesOwnershipOnce(t *testing.T) {
	f := idleAdmissionChat(t)
	reads := f.reads.Load()
	if err := f.b.workAllowed(f.b.agent(f.agent.ID)); err != nil {
		t.Fatal(err)
	}
	if got := f.reads.Load() - reads; got != 1 {
		t.Fatalf("idle admission ownership reads = %d, want 1", got)
	}
	reads = f.reads.Load()
	if err := f.b.workAllowed(f.b.agent(f.agent.ID)); err != nil {
		t.Fatal(err)
	}
	if got := f.reads.Load() - reads; got != 0 {
		t.Fatalf("fresh admission ownership reads = %d, want 0", got)
	}
	calls := f.modelCalls.Load()
	turn, err := f.b.Send(f.agent.ID, "still mine", "idle-send")
	if err != nil {
		t.Fatal(err)
	}
	appTurnDoneFor(t, f.rec, turn)
	if f.modelCalls.Load() <= calls {
		t.Fatal("admitted work never reached the model")
	}
}

// I4: a slow relay is actually queried, but cannot keep an idle app from
// submitting a local turn within a short budget or from reaching its model.
func TestI4IdleAppAdmitsWhenOwnershipReadTimesOut(t *testing.T) {
	f := idleAdmissionChat(t)
	f.slow.Store(true)
	t.Cleanup(func() { f.slow.Store(false) })
	reads, calls := f.reads.Load(), f.modelCalls.Load()
	var logged bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(previous) })
	start := time.Now()
	err := f.b.workAllowed(f.b.agent(f.agent.ID))
	elapsed := time.Since(start)
	log.SetOutput(previous)
	// I1/I4: the person waits at most the read budget, well under a second.
	if err != nil || elapsed > driveWorkWithin+200*time.Millisecond {
		t.Fatalf("offline admission = %v in %s", err, elapsed)
	}
	if f.reads.Load() == reads {
		t.Fatal("idle admission never attempted its relay ownership check")
	}
	if !strings.Contains(logged.String(), "sync: checking chat ownership; admitting work:") {
		t.Fatalf("ownership failure was not logged: %s", logged.String())
	}
	idleAdmissionWait(t, "the cancelled ownership reads", func() bool { return f.timeouts.Load() >= f.reads.Load()-reads })
	if longest := time.Duration(f.slowest.Load()); longest > driveWorkWithin+100*time.Millisecond {
		t.Fatalf("the relay was allowed to delay ownership admission for %s", longest)
	}
	readsBeforeSend, waitsBeforeSend, timeoutsBeforeSend := f.reads.Load(), f.waited.Load(), f.timeouts.Load()
	turn, err := f.b.Send(f.agent.ID, "work despite a slow relay", "slow-relay")
	if err != nil {
		t.Fatal(err)
	}
	appTurnDoneFor(t, f.rec, turn)
	idleAdmissionWait(t, "all submission ownership checks to cancel", func() bool {
		return f.timeouts.Load()-timeoutsBeforeSend >= f.reads.Load()-readsBeforeSend
	})
	// I4: right after a failed read, a send stays local without asking again,
	// at any of its app and session doors, so an offline stretch costs nothing.
	if reads := f.reads.Load() - readsBeforeSend; reads != 0 {
		t.Fatalf("a send right after a failed ownership read asked the relay %d more times", reads)
	}
	if waited := time.Duration(f.waited.Load() - waitsBeforeSend); waited != 0 {
		t.Fatalf("a send right after a failed ownership read waited %s for the relay", waited)
	}
	if f.modelCalls.Load() <= calls {
		t.Fatal("admitted turn did not reach its local model")
	}
}
