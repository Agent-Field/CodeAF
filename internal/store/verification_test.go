package store

import (
	"path/filepath"
	"testing"
)

// A FAIL-SAFE THAT LEAVES NO RECORD CANNOT BE AUTOPSIED. Not one of the five
// graded runs of the 2026-08-29 sweep holds a row saying a reading of the
// project's own checks had happened, so a project that declares no verification
// and a reading that ran and named nothing were the same silence in the
// journal — and those are the two opposite diagnoses.
func TestAReadingOfTheProjectsChecksIsJournaled(t *testing.T) {
	graph, err := Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{
		ID: "job", Brief: "add a circuit breaker", Stage: 1,
	}}}, Provenance{Origin: OriginUser, SessionID: "s1", Intent: "add a circuit breaker"}); err != nil {
		t.Fatal(err)
	}

	want := VerificationReading{
		When: "before the job's first change", Command: "pnpm exec vitest run --reporter=json",
		Declared: "pnpm test", Runner: "vitest", Read: "node-json",
		Source: "package.json#scripts.test", Exit: 0, Named: 28, Red: 0,
		Sample: []string{"ofetch ok", "ofetch default fetch options"},
	}
	if err := graph.RecordVerification("job", want); err != nil {
		t.Fatal(err)
	}
	readings, err := graph.VerificationsFor("job")
	if err != nil {
		t.Fatal(err)
	}
	if len(readings) != 1 {
		t.Fatalf("journaled %d readings, want 1", len(readings))
	}
	got := readings[0]
	if got.Command != want.Command || got.Declared != want.Declared {
		t.Errorf("the command that ran and the one the project declared were not "+
			"both kept: %+v", got)
	}
	if got.Runner != "vitest" || got.Read != "node-json" {
		t.Errorf("the strategy is not in the record, so the next autopsy cannot "+
			"see where the reader looked: %+v", got)
	}
	if got.Named != 28 {
		t.Errorf("the size of the roster was lost: %+v", got)
	}

	// A reading nobody took writes nothing. The absence of the event is the
	// fact, said by not saying it, and a row for it would be one every reader
	// has to learn to ignore.
	if err := graph.RecordVerification("job", VerificationReading{}); err != nil {
		t.Fatal(err)
	}
	if again, _ := graph.VerificationsFor("job"); len(again) != 1 {
		t.Errorf("a photograph nobody took was journaled: %d rows", len(again))
	}

	// The sample is bounded, because a suite with two thousand checks would
	// otherwise write a megabyte into the journal on every round of every job.
	long := make([]string, 50)
	for index := range long {
		long[index] = "check"
	}
	if err := graph.RecordVerification("job", VerificationReading{
		When: "on the finished tree", Command: "pnpm exec vitest run --reporter=json",
		Named: 2000, Sample: long,
	}); err != nil {
		t.Fatal(err)
	}
	all, _ := graph.VerificationsFor("job")
	if len(all) != 2 || len(all[1].Sample) != VerificationSample {
		t.Errorf("the journaled sample is unbounded: %d names", len(all[1].Sample))
	}
}
