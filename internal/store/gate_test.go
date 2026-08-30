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

// The checklist is journaled against the work it will be used to judge, and it
// is the only durable answer to "what was this work actually asked for" that
// survives the process that planned it. An autopsy of a passed run has to be
// able to tell a delivery that satisfied every point from one that was measured
// against nothing.
func TestTheAcceptanceChecklistIsJournaledAgainstTheWorkItJudges(t *testing.T) {
	graph, err := Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{
		ID: "task-2", Brief: "add a per-origin circuit breaker", Stage: 1,
	}}}, Provenance{Origin: OriginUser, SessionID: "s1",
		Intent: "Implement an opt-in per-origin circuit breaker"}); err != nil {
		t.Fatal(err)
	}

	want := Acceptance{Points: []AcceptancePoint{
		{Behaviour: "A rejected non-listed status does not close half-open state",
			Quote: "must not close half-open state"},
		{Behaviour: "A half-open probe holds its slot across internal retries",
			Quote: "keeps its slot for the full logical request"},
	}}
	if err := graph.RecordAcceptance("task-2", want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := graph.AcceptanceFor("task-2")
	if err != nil || !ok {
		t.Fatalf("AcceptanceFor: ok=%v err=%v", ok, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the journal returned %#v, want %#v", got, want)
	}

	// An empty checklist writes nothing: a request that states no checkable
	// behaviour has no checklist, and a row saying so is a row every reader has
	// to learn to ignore.
	if err := graph.RecordAcceptance("task-2", Acceptance{}); err != nil {
		t.Fatal(err)
	}
	again, _, err := graph.AcceptanceFor("task-2")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again, want) {
		t.Errorf("an empty checklist overwrote a real one: %#v", again)
	}

	// And the mapping the gate settled rides the gate event, because the
	// mapping is the evidence and the verdict is only its conclusion.
	gate := DeliveryGate{Pass: true, Exercises: []ExercisedPoint{
		{Point: "A half-open probe holds its slot across internal retries",
			Check: "keeps half-open quota reserved while a probe is internally retrying"},
		{Point: "A rejected non-listed status does not close half-open state"},
	}}
	if err := graph.RecordDeliveryGate("task-2", gate); err != nil {
		t.Fatal(err)
	}
	back, ok, err := graph.DeliveryGateFor("task-2")
	if err != nil || !ok {
		t.Fatalf("DeliveryGateFor: ok=%v err=%v", ok, err)
	}
	if !reflect.DeepEqual(back.Exercises, gate.Exercises) {
		t.Errorf("the mapping did not survive the journal: %#v", back.Exercises)
	}
}

// AN ACQUITTAL IS OF ONE FINDING. textual s10 held a single gate whose
// `unexercised` named two groups of behaviours the request states and nothing
// checks, whose verdict was refused as `what it asked for is already on disk
// under the name the request used`, and whose Overturned was true — and it
// settled WHOLE. Exit 0 at 5 of 20 hidden checks, over the run's own measurement
// of thirteen behaviours it had just found exercised by nothing.
//
// Overturning a refusal says the review was wrong about ONE thing: the file it
// called missing is on disk. It says nothing whatever about a set measured by a
// different mechanism on different evidence, and closed only by a check existing.
func TestAnOverturnDoesNotCloseTheCoverageSet(t *testing.T) {
	standing := DeliveryGate{
		Pass:        false,
		Refused:     "what it asked for is already on disk under the name the request used",
		Overturned:  true,
		Unexercised: []string{"Log and RichLog expose is_following_end", "RichLog honours expand=True"},
	}
	if standing.Whole() {
		t.Error("a gate with two behaviours nothing checks settled whole on an overturn")
	}
	// The same overturn, with nothing open, still acquits — that is what it is
	// for, and charging it a non-zero code would teach a harness to distrust the
	// gate's own corrections.
	closed := standing
	closed.Unexercised = nil
	if !closed.Whole() {
		t.Error("an overturn with nothing open stopped acquitting")
	}
	// And it is the SET that stands, not the verdict: a pass and a repaired
	// gate are held to it too.
	for _, settled := range []DeliveryGate{
		{Pass: true, Unexercised: standing.Unexercised},
		{PolishClosed: true, Unexercised: standing.Unexercised},
	} {
		if settled.Whole() {
			t.Errorf("a settled gate carried an open coverage set into whole: %#v", settled)
		}
	}
}

// EVERY COMPARISON IS JOURNALED, INCLUDING ONE THAT FOUND NOTHING. "Sixteen
// files were compared and no public name was lost" and "nobody compared
// anything" are two facts, and the absence of the row was the only spelling
// either of them had.
func TestTheSymbolLevelComparisonIsJournaled(t *testing.T) {
	graph, err := Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{
		ID: "job", Brief: "persist the feature schema", Stage: 1,
	}}}, Provenance{Origin: OriginUser, SessionID: "s1",
		Intent: "persist the feature schema"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordSurface("job", SurfaceReading{
		Compared: 3, Lost: 8, Names: []string{"Igel.results_path", "Igel.description_file"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordSurface("job", SurfaceReading{Compared: 3}); err != nil {
		t.Fatal(err)
	}
	readings, err := graph.SurfacesFor("job")
	if err != nil {
		t.Fatal(err)
	}
	if len(readings) != 2 {
		t.Fatalf("journaled %d comparisons, want 2", len(readings))
	}
	if readings[0].Lost != 8 || readings[0].Names[0] != "Igel.results_path" {
		t.Errorf("the loss was not journaled as measured: %#v", readings[0])
	}
	if readings[1].Lost != 0 || readings[1].Compared != 3 {
		t.Errorf("a comparison that found nothing was not journaled as such: %#v", readings[1])
	}
	// The sample is bounded the way every roster in this journal is.
	long := make([]string, VerificationSample+4)
	for index := range long {
		long[index] = "Thing.name"
	}
	if err := graph.RecordSurface("job", SurfaceReading{Compared: 1, Lost: len(long), Names: long}); err != nil {
		t.Fatal(err)
	}
	readings, _ = graph.SurfacesFor("job")
	if got := len(readings[2].Names); got != VerificationSample {
		t.Errorf("the journal kept %d names, want the sample bound of %d", got, VerificationSample)
	}
}

// What a gate JUDGED has to read back out of the journal under the name a
// reader looks it up by. ofetch s12 journaled a refusal whose `subject` was
// absent, and the one question an autopsy of that mechanism asks — did the
// review read the world or a sentence — had no answer on the run that needed
// it. The field is checked on the wire, in the key it is spelled with, because
// a struct field that never reaches the payload is a field that does not exist.
func TestTheGateJournalsWhatItJudged(t *testing.T) {
	graph, err := Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{
		ID: "task-2", Brief: "add the circuit breaker", Stage: 1,
	}}}, Provenance{Origin: OriginUser, SessionID: "s12", Intent: "add a circuit breaker"}); err != nil {
		t.Fatal(err)
	}
	written := DeliveryGate{Pass: false, Gap: "src/circuit-breaker.ts — it never opens the circuit",
		Subject: "tree (6 files)"}
	if err := graph.RecordDeliveryGate("task-2", written); err != nil {
		t.Fatal(err)
	}
	read, ok, err := graph.DeliveryGateFor("task-2")
	if err != nil || !ok {
		t.Fatalf("the gate did not read back: ok=%v err=%v", ok, err)
	}
	if read.Subject != written.Subject {
		t.Fatalf("what the gate judged did not survive the journal: %q", read.Subject)
	}
	events, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind != EventDeliveryGate {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["subject"] != "tree (6 files)" {
			t.Fatalf("the payload does not spell the subject where a reader looks: %v", payload)
		}
	}
}
