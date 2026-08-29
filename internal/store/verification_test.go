package store

import (
	"path/filepath"
	"strings"
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
		Declared: "pnpm test", Runner: "vitest", Read: true, Format: "node-json",
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
	if got.Runner != "vitest" || got.Format != "node-json" || !got.Read {
		t.Errorf("the strategy is not in the record, so the next autopsy cannot "+
			"see where the reader looked: %+v", got)
	}
	if got.Named != 28 {
		t.Errorf("the size of the roster was lost: %+v", got)
	}

	// A READING THAT COULD NOT BE TAKEN IS STILL AN EVENT. This is the s6
	// silence: textual's bare leaf spent five and a half minutes on a suite
	// killed at its ceiling and the store held no row saying so, which read
	// from outside exactly like a project that declares no verification.
	if err := graph.RecordVerification("job", VerificationReading{
		When: "before the job's first change", Read: false,
		Why:     "`python3 -m pytest -rA` was killed at its ceiling of 5m30s without finishing",
		Command: "python3 -m pytest -rA", Declared: "make test", Runner: "pytest",
		Exit: -1, TimedOut: true,
	}); err != nil {
		t.Fatal(err)
	}
	refused, _ := graph.VerificationsFor("job")
	if len(refused) != 2 {
		t.Fatalf("a reading that could not be taken was journaled as %d rows, want 1",
			len(refused)-1)
	}
	if refused[1].Read || !strings.Contains(refused[1].Why, "killed at its ceiling") {
		t.Errorf("the refusal does not say why nothing was read: %+v", refused[1])
	}

	// Only a row that says nothing at all is not written — the zero value,
	// which is nobody having called this.
	if err := graph.RecordVerification("job", VerificationReading{}); err != nil {
		t.Fatal(err)
	}
	if again, _ := graph.VerificationsFor("job"); len(again) != 2 {
		t.Errorf("a row saying nothing was journaled: %d rows", len(again))
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
	if len(all) != 3 || len(all[2].Sample) != VerificationSample {
		t.Errorf("the journaled sample is unbounded: %d names", len(all[len(all)-1].Sample))
	}
}
