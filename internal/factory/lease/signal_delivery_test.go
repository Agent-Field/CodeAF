package lease_test

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"net/http/httptest"
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
