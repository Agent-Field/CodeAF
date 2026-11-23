package lease_test

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

func TestOldSignalWorkerStillCollectsOnlyOnce(t *testing.T) {
	f := setup(t)
	it, held := f.running(t)
	if ok, err := f.desk.SignalIfHeld(it.ID, lease.Signal{Verb: lease.SignalSteer, Words: "once"}); !ok || err != nil {
		t.Fatal(ok, err)
	}
	first, err := f.desk.DrainLive(held.ID, held.Worker)
	if err != nil || len(first) != 1 || first[0].Words != "once" {
		t.Fatalf("legacy delivery: %+v %v", first, err)
	}
	second, err := f.open(t).DrainLive(held.ID, held.Worker)
	if err != nil || len(second) != 0 {
		t.Fatalf("old worker received a repeat: %+v %v", second, err)
	}
}

func TestOldSignalWorkerHTTPHasNoAcknowledgementOrRepeat(t *testing.T) {
	f := setup(t)
	it := f.add(t, factory.Item{State: factory.StateNew})
	handler := serve.New(factory.LocalSeam(f.store, f.now, factory.WithRunner(lease.NewDoors(f.desk))), serve.Config{Store: f.store, LeaseOptions: lease.Options{Now: func() time.Time { return f.now }}})
	defer handler.Close()
	server := httptest.NewServer(handler)
	defer server.Close()
	wc := client.Open(server.URL, "").Worker("")
	if _, err := wc.Attach(t.Context(), api.WorkerAttach{Name: "old", Handle: "sam", TakesUnassigned: map[string]bool{lease.EveryRepo: true}}); err != nil {
		t.Fatal(err)
	}
	if err := lease.NewDoors(f.desk).Launch(it.ID); err != nil {
		t.Fatal(err)
	}
	pulled, ok, err := wc.Pull(t.Context())
	if !ok || err != nil {
		t.Fatal(ok, err)
	}
	if ok, err := f.desk.SignalIfHeld(it.ID, lease.Signal{Verb: lease.SignalBudget, Budget: 8}); !ok || err != nil {
		t.Fatal(ok, err)
	}
	first, err := wc.Heartbeat(t.Context(), pulled.Lease.ID)
	if err != nil || len(first.Signals) != 1 {
		t.Fatalf("legacy HTTP delivery: %+v %v", first, err)
	}
	second, err := wc.Heartbeat(t.Context(), pulled.Lease.ID)
	if err != nil || len(second.Signals) != 0 {
		t.Fatalf("legacy HTTP repeated: %+v %v", second, err)
	}
}

func TestAcknowledgedSignalsStayWithinTheirLeaseAndStopClearsThem(t *testing.T) {
	f := setup(t)
	it, held := f.running(t)
	if ok, err := f.desk.SignalIfHeld(it.ID, lease.Signal{Verb: lease.SignalBudget, Budget: 8}); !ok || err != nil {
		t.Fatal(ok, err)
	}
	first, err := f.desk.DrainLive(held.ID, held.Worker, []string{})
	if err != nil || len(first) != 1 || first[0].Delivery == "" {
		t.Fatalf("missing delivery ID: %+v %v", first, err)
	}
	retry, err := f.open(t).DrainLive(held.ID, held.Worker, []string{})
	if err != nil || !reflect.DeepEqual(first, retry) {
		t.Fatalf("restart lost pending signal: %+v %v", retry, err)
	}
	ack := []string{first[0].Delivery}
	drained, err := f.desk.DrainLive(held.ID, held.Worker, ack)
	if err != nil || len(drained) != 0 {
		t.Fatalf("ack did not remove signal: %+v %v", drained, err)
	}
	if ok, err := f.desk.SignalIfHeld(it.ID, lease.Signal{Verb: lease.SignalSteer, Words: "old lease"}); !ok || err != nil {
		t.Fatal(ok, err)
	}
	doors := lease.NewDoors(f.desk)
	if err := doors.Stop(it.ID); err != nil {
		t.Fatal(err)
	}
	// A person's Start is what lets a stopped job run again.
	if err := f.store.Update(it.ID, func(it *factory.Item) error { factory.ClearPersonStop(it); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := doors.Launch(it.ID); err != nil {
		t.Fatal(err)
	}
	next, err := f.desk.Pull(lease.Worker{ID: held.Worker, Handle: "sam", TakesUnassigned: map[string]bool{lease.EveryRepo: true}})
	if err != nil {
		t.Fatal(err)
	}
	drained, err = f.desk.DrainLive(next.ID, next.Worker, ack)
	if err != nil || len(drained) != 0 {
		t.Fatalf("Stop leaked a signal into the new lease: %+v %v", drained, err)
	}
	if ok, err := f.desk.SignalIfHeld(it.ID, lease.Signal{Verb: lease.SignalBudget, Budget: 12}); !ok || err != nil {
		t.Fatal(ok, err)
	}
	drained, err = f.desk.DrainLive(next.ID, next.Worker, ack)
	if err != nil || len(drained) != 1 || drained[0].Delivery == first[0].Delivery {
		t.Fatalf("old ack consumed a new choice: %+v %v", drained, err)
	}
}
