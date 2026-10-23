package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func deliveryJob(t *testing.T, handler http.Handler) (*job, *time.Time) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	mirror, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	it, err := mirror.Create(factory.Item{Kind: factory.KindIssue, Repo: "acme/api", State: factory.StateRunning, Cap: 10, Stream: &factory.Stream{Phases: []factory.Phase{{Name: "test", State: factory.PhaseRunning, Round: 1}}}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)
	j := &job{o: Options{Mirror: mirror, Runner: run.New(run.Options{Store: mirror}), Now: func() time.Time { return now }, DeliveryRoot: t.TempDir()}, w: client.Open(server.URL, "").Worker("worker-key"), lease: "lease_test", id: it.ID, sent: it, accepted: map[string]bool{}, acknowledged: map[string]bool{}}
	return j, &now
}

func setDeliverySpend(t *testing.T, j *job, spent float64) {
	t.Helper()
	if err := j.o.Mirror.Update(j.id, func(it *factory.Item) error { it.Stream.Spent = spent; return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerDeliveryRetriesImmutableDeltaBeforeNewWork(t *testing.T) {
	var keys, bodies []string
	var costs []float64
	j, now := deliveryJob(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var report api.WorkerReport
		_ = json.NewDecoder(r.Body).Decode(&report)
		raw, _ := json.Marshal(report)
		bodies = append(bodies, string(raw))
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		costs = append(costs, report.Cost)
		if len(keys) == 1 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
	}))
	setDeliverySpend(t, j, .25)
	if !j.flush(context.Background()) || j.pending == nil || j.sent.Stream.Spent != 0 {
		t.Fatal("unconfirmed request advanced baseline")
	}
	retained, err := os.ReadFile(j.deliveryPath())
	if err != nil || !strings.Contains(string(retained), keys[0]) {
		t.Fatal("pending request not retained", err)
	}
	setDeliverySpend(t, j, .5)
	*now = now.Add(3 * time.Second)
	if !j.flush(context.Background()) || j.pending != nil || j.sent.Stream.Spent != .25 {
		t.Fatal("ack did not advance exact snapshot")
	}
	if !j.flush(context.Background()) || j.sent.Stream.Spent != .5 {
		t.Fatal("new work not reported after acknowledgment")
	}
	if len(keys) != 3 || keys[0] == "" || keys[0] != keys[1] || keys[1] == keys[2] || bodies[0] != bodies[1] || !reflect.DeepEqual(costs, []float64{.25, .25, .25}) {
		t.Fatalf("requests changed across retry: keys=%v costs=%v", keys, costs)
	}
}

func TestWorkerRejectedDeliveryParksAndRetainsOriginal(t *testing.T) {
	for _, status := range []int{413, 422} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			results, controls := 0, 0
			j, _ := deliveryJob(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var report api.WorkerReport
				_ = json.NewDecoder(r.Body).Decode(&report)
				if report.Question != nil {
					controls++
					if report.Cost != 0 || len(report.Steps) != 0 || report.Question.Kind != "results_not_saved" {
						t.Error("control asserted rejected work")
					}
					w.WriteHeader(200)
					return
				}
				results++
				w.WriteHeader(status)
			}))
			setDeliverySpend(t, j, .25)
			for range 5 {
				if !j.flush(context.Background()) {
					t.Fatal("rejection released lease")
				}
			}
			if results != 1 || controls != 1 || j.blocked == "" || j.pending == nil || j.sent.Stream.Spent != 0 {
				t.Fatalf("lost/unbounded rejected delivery: results=%d controls=%d", results, controls)
			}
			var retained pendingDelivery
			raw, err := os.ReadFile(j.deliveryPath())
			if err != nil || json.Unmarshal(raw, &retained) != nil || retained.Reason == "" {
				t.Fatal("rejection artifact missing", err)
			}
			if j.flushFinal(context.Background()) {
				t.Fatal("rejected results allowed Done")
			}
		})
	}
}

func TestWorkerDeliveryRecoveryWindowEndsWithoutDiscard(t *testing.T) {
	results := 0
	j, now := deliveryJob(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var report api.WorkerReport
		_ = json.NewDecoder(r.Body).Decode(&report)
		if report.Question == nil {
			results++
		}
		w.WriteHeader(503)
	}))
	setDeliverySpend(t, j, .25)
	_ = j.flush(context.Background())
	*now = now.Add(factory.RecoveryWindow)
	for range 8 {
		_ = j.flush(context.Background())
	}
	if results != 1 || j.blocked == "" || j.controlTries != deliveryControlAttempts || j.sent.Stream.Spent != 0 {
		t.Fatal("unbounded delivery recovery or baseline lost")
	}
	if _, err := os.Stat(j.deliveryPath()); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerBlockedCleanupRetainsNewerWorkWithoutChangingRequest(t *testing.T) {
	j, now := deliveryJob(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	setDeliverySpend(t, j, .25)
	if !j.flush(context.Background()) || j.pending == nil {
		t.Fatal("retryable request was not retained")
	}
	original, err := json.Marshal(j.pending.Request)
	if err != nil {
		t.Fatal(err)
	}
	digest := j.pending.Digest
	setDeliverySpend(t, j, .5)
	j.blockDelivery("delivery refused")
	var retained pendingDelivery
	read := func() {
		t.Helper()
		raw, err := os.ReadFile(j.deliveryPath())
		if err != nil || json.Unmarshal(raw, &retained) != nil {
			t.Fatal("reconciliation artifact missing", err)
		}
	}
	read()
	if retained.Reconciliation == nil || retained.Reconciliation.Stream.Spent != .5 || retained.Snapshot.Stream.Spent != .25 {
		t.Fatal("block lost newer work or changed frozen baseline")
	}
	// A final usage/cleanup write can arrive after cancellation was requested.
	setDeliverySpend(t, j, .75)
	*now = now.Add(time.Second)
	j.clearMirror()
	if _, err := os.Stat(j.o.Mirror.ItemPath(j.id)); !os.IsNotExist(err) {
		t.Fatal("mirror cleanup did not occur", err)
	}
	read()
	if retained.Reconciliation == nil || retained.Reconciliation.Stream.Spent != .75 || !retained.ReconciliationAt.Equal(*now) || retained.Snapshot.Stream.Spent != .25 || j.sent.Stream.Spent != 0 {
		t.Fatal("cleanup lost late recorded usage or advanced an unconfirmed baseline")
	}
	frozen, _ := json.Marshal(retained.Request)
	if string(frozen) != string(original) || retained.Digest != digest {
		t.Fatal("reconciliation changed immutable retry bytes or digest")
	}
}

func TestWorkerBlockedDeliveryDoesNotAdmitSignals(t *testing.T) {
	j, _ := deliveryJob(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	called := make(chan struct{}, 1)
	j.o.Reply = func(context.Context, factory.Item, string) error { called <- struct{}{}; return nil }
	j.blockDelivery("results need reconciliation")
	if !j.signals(context.Background(), []api.WorkerSignal{
		{Verb: lease.SignalReply, ID: "reply", Words: "continue"},
		{Verb: lease.SignalHold, Yes: false},
		{Verb: lease.SignalAnswer, Yes: true},
		{Verb: lease.SignalSteer, Words: "continue"},
		{Verb: lease.SignalBudget, Budget: 20, Yes: true},
	}) {
		t.Fatal("blocked signals released lease")
	}
	if j.replyingNow() || j.o.Runner.Running(j.id) {
		t.Fatal("blocked delivery admitted work")
	}
	it, err := j.o.Mirror.Get(j.id)
	if err != nil || it.Cap != 10 {
		t.Fatal("blocked execution accepted budget/resume", err)
	}
	select {
	case <-called:
		t.Fatal("blocked delivery admitted a manager turn")
	default:
	}
	if j.signals(context.Background(), []api.WorkerSignal{{Verb: lease.SignalStop}}) {
		t.Fatal("Stop must still release blocked work")
	}
}

func TestWorkerHeartbeatDoesNotCancelManagerReply(t *testing.T) {
	j, _ := deliveryJob(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(api.WorkerHeartbeat{Signals: []api.WorkerSignal{{Verb: lease.SignalReply, ID: "reply", Words: "status"}}})
	}))
	started := make(chan context.Context, 1)
	ended := make(chan struct{})
	j.o.Reply = func(ctx context.Context, _ factory.Item, _ string) error {
		started <- ctx
		<-ctx.Done()
		close(ended)
		return ctx.Err()
	}
	if !j.beat(context.Background()) {
		t.Fatal("heartbeat lost lease")
	}
	var replyCtx context.Context
	select {
	case replyCtx = <-started:
	case <-time.After(time.Second):
		t.Fatal("manager reply did not start")
	}
	if replyCtx.Err() != nil {
		t.Fatal("heartbeat RPC timeout canceled asynchronous reply")
	}
	j.blockDelivery("results need reconciliation")
	select {
	case <-ended:
	case <-time.After(time.Second):
		t.Fatal("parked delivery left a paid manager turn running")
	}
}
