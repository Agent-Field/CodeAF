package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/store"
)

// THE ENVELOPE SAYS WHEN THE REQUESTED WORK WAS FIRST FOUND DONE, in seconds
// from the run's start, off the journal's own gate event — and leaves the key
// out when no gate ever said so, because a fabricated instant is worse than an
// absent one.
func TestTheEnvelopeSaysWhenTheRequestedWorkWasDone(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "coredone.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	const gated, ungated = "session-gated", "session-ungated"
	spliceForErrand(t, graph, gated, []store.NodeSpec{{ID: "task-1", Brief: "add the flag"}})
	spliceForErrand(t, graph, ungated, []store.NodeSpec{{ID: "task-2", Brief: "add the other flag"}})
	if err := graph.RecordDeliveryGate("task-1", store.DeliveryGate{Pass: true}); err != nil {
		t.Fatal(err)
	}
	_, at, ok, err := graph.DeliveryGateAnchor("task-1")
	if err != nil || !ok {
		t.Fatalf("the passing gate has no anchor: ok=%t err=%v", ok, err)
	}
	started := at.Add(-7 * time.Second)

	gatedOutcome := headlessOutcome{started: started}
	priceErrand(graph, gated, 0, &gatedOutcome)
	fields := envelopeFields(t, errandEnvelope(gatedOutcome))
	got, present := fields["core_done_seconds"]
	if !present {
		t.Fatal("a gate found the work done and the envelope says nothing about when")
	}
	if seconds, ok := got.(float64); !ok || seconds < 6.9 || seconds > 7.1 {
		t.Fatalf("core_done_seconds = %v, want about 7", got)
	}

	// And absent when no gate ever said the work was done.
	ungatedOutcome := headlessOutcome{started: started}
	priceErrand(graph, ungated, 0, &ungatedOutcome)
	if _, present := envelopeFields(t, errandEnvelope(ungatedOutcome))["core_done_seconds"]; present {
		t.Fatal("a run whose gate never found the work done reported when it was")
	}
}
