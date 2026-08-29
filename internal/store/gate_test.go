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

// The citation list and the mechanical flag replay out of the journal exactly
// as they were written. They are what bounds the next repair round and what
// decides the run's exit code, and a ledger that lost either on a restart would
// hand a job a fresh allowance and an honest failure a success code.
func TestTheGateLedgerReplaysItsCitationsAndItsProvenance(t *testing.T) {
	graph, err := Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{
		ID: "job", Brief: "write the ladder", Stage: 1,
	}}}, Provenance{Origin: OriginUser, SessionID: "s1", Intent: "grow spacing.go and home.go"}); err != nil {
		t.Fatal(err)
	}
	want := DeliveryGate{
		Pass: false, Gap: "spacing.go, home.go", Quote: "spacing.go, home.go",
		Quotes: []string{"spacing.go", "home.go"}, Mechanical: true, Round: 1,
	}
	if err := graph.RecordDeliveryGate("job", want); err != nil {
		t.Fatal(err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	got, ok, err := graph.DeliveryGateFor("job")
	if err != nil || !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("rebuilt gate = %+v, ok %t, err %v; want %+v", got, ok, err, want)
	}
	if cited := got.Cited(); !reflect.DeepEqual(cited, []string{"spacing.go", "home.go"}) {
		t.Fatalf("cited = %q, want the list it was recorded with", cited)
	}

	// A row written before the list existed carries only the joined line, and
	// reading it as the one citation it was then is the honest reading: a
	// ledger that read it as nothing would let the same words buy a second
	// round after a restart.
	old := DeliveryGate{Pass: false, Gap: "the benchmark numbers", Quote: "the benchmark numbers", Extended: true}
	if err := graph.RecordDeliveryGate("job", old); err != nil {
		t.Fatal(err)
	}
	back, _, err := graph.DeliveryGateFor("job")
	if err != nil {
		t.Fatal(err)
	}
	if back.Quotes != nil {
		t.Fatalf("quotes = %q, want nothing invented for a row that had none", back.Quotes)
	}
	if cited := back.Cited(); !reflect.DeepEqual(cited, []string{"the benchmark numbers"}) {
		t.Fatalf("cited = %q, want the line read as the one citation it was", cited)
	}

	// Blank citations are dropped rather than stored: strings.Contains is true
	// of the empty string against any text at all, so one carried into the
	// grounding rule would ground itself against anything.
	if err := graph.RecordDeliveryGate("job", DeliveryGate{
		Pass: false, Gap: "nothing landed", Quote: "nothing landed", Quotes: []string{"  ", ""}}); err != nil {
		t.Fatal(err)
	}
	blank, _, err := graph.DeliveryGateFor("job")
	if err != nil {
		t.Fatal(err)
	}
	if blank.Quotes != nil {
		t.Fatalf("quotes = %q, want the blanks dropped", blank.Quotes)
	}
}
