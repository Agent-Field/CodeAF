package store

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDeliveryGateIsAppendOnlyAndRebuildSafe(t *testing.T) {
	graph, err := Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{
		ID: "job", Brief: "ship the complete answer", Stage: 1,
	}}}, Provenance{Origin: OriginUser, SessionID: "s1", Intent: "answer every part"}); err != nil {
		t.Fatal(err)
	}

	want := DeliveryGate{Pass: false, Gap: "the benchmark result is missing", PolishClosed: true}
	if err := graph.RecordDeliveryGate("job", want); err != nil {
		t.Fatal(err)
	}
	before, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	gateEvents := 0
	for _, event := range before {
		if event.Kind != EventDeliveryGate {
			continue
		}
		gateEvents++
		var payload DeliveryGate
		if err := json.Unmarshal(event.Payload, &payload); err != nil || !reflect.DeepEqual(payload, want) {
			t.Fatalf("gate payload = %+v, err %v; want %+v", payload, err, want)
		}
	}
	if gateEvents != 1 {
		t.Fatalf("gate events = %d, want one", gateEvents)
	}

	if err := graph.Rebuild(); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	after, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("Rebuild changed the append-only journal")
	}
	got, ok, err := graph.DeliveryGateFor("job")
	if err != nil || !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("rebuilt gate = %+v, ok %t, err %v; want %+v", got, ok, err, want)
	}
}

// The gap ledger is a read over one job's whole run of judgements, repair
// rounds included, and it has to survive a rebuild for the same reason the
// verdict does: what bounds new work must be replayable, or a restart hands the
// job a fresh unbounded allowance.
func TestDeliveryGateLineageReadsEveryRoundAndSurvivesRebuild(t *testing.T) {
	graph, err := Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	provenance := Provenance{Origin: OriginUser, SessionID: "s1", Intent: "answer every part"}
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{
		ID: "job", Brief: "ship the complete answer", Stage: 1,
	}}}, provenance); err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{
		ID: "job-x1", Brief: "close the gap", Stage: 1,
	}}}, provenance); err != nil {
		t.Fatal(err)
	}
	// A different job whose id begins with other bytes must not leak in.
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{
		ID: "jobless", Brief: "unrelated", Stage: 1,
	}}}, provenance); err != nil {
		t.Fatal(err)
	}

	first := DeliveryGate{Gap: "the benchmark is missing", Quote: "every part", Round: 1, Extended: true}
	second := DeliveryGate{Gap: "still missing", Quote: "every part", Round: 2, Refused: "the same words were already worked on once"}
	if err := graph.RecordDeliveryGate("job", first); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordDeliveryGate("job-x1", second); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordDeliveryGate("jobless", DeliveryGate{Gap: "someone else's", Quote: "every part", Extended: true}); err != nil {
		t.Fatal(err)
	}

	want := []DeliveryGate{first, second}
	for _, stage := range []string{"live", "rebuilt"} {
		got, err := graph.DeliveryGateLineage("job")
		if err != nil {
			t.Fatalf("%s lineage: %v", stage, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s lineage = %+v, want %+v", stage, got, want)
		}
		if err := graph.Rebuild(); err != nil {
			t.Fatalf("Rebuild: %v", err)
		}
	}
	// A failed gate still has to name the gap; nothing about the ledger relaxes
	// that, because a gap nobody can state is not a verdict.
	if err := graph.RecordDeliveryGate("job", DeliveryGate{Quote: "every part"}); err == nil {
		t.Fatal("a failed gate with a quote and no gap was recorded")
	}
}
