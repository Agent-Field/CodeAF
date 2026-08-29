package revision

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// ofetchRequest is the request ofetch s4 was given, read rather than retyped:
// bench/deepswe-bench task ofetch-per-origin-circuit-breaker, instruction.md.
// The run passed its delivery gate at ten minutes on "All 56 tests pass" and
// scored 41 of 47 hidden fail-to-pass tests.
func ofetchRequest(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile("testdata/acceptance/ofetch-request.md")
	if err != nil {
		t.Fatalf("the sweep's own request is the fixture: %v", err)
	}
	return string(body)
}

// ofetchPoints are behaviours that request states, each quoting it. The first
// four are the family the six hidden failures belong to — the ones the leaf's
// own twenty-eight tests never exercised.
func ofetchPoints() []plan.Point {
	return []plan.Point{
		{
			Behaviour: "A rejected non-listed status does not close half-open state",
			Quote: "Rejected non-listed statuses must not be treated as success: " +
				"they must not reset failure streaks and must not close half-open state.",
		},
		{
			Behaviour: "A half-open probe holds its slot across internal retries",
			Quote: "A half-open probe keeps its slot for the full logical request, " +
				"including internal retries.",
		},
		{
			Behaviour: "A parse or hook failure is not retried by status-based retry logic",
			Quote:     "Parse/hook failures are not retried by status-based retry logic.",
		},
		{
			Behaviour: "A failed half-open probe restarts the cooldown from that failure time",
			Quote:     "`half-open` -> `open` on failed probe, restarting cooldown from that failure time",
		},
		{
			Behaviour: "A successful logical request resets consecutive failures to zero",
			Quote:     "A successful logical request resets consecutive failures to `0`.",
		},
	}
}

func ofetchGrounds(t *testing.T) Grounds {
	t.Helper()
	return Grounds{Intent: ofetchRequest(t)}
}

// EVERY POINT IS THE PERSON'S OWN WORDS OR IT IS NOT HELD. The checklist is a
// standard the gate will fail a delivery against, so it goes through the same
// door a review's finding does — and a point this system wrote for itself after
// reading its own prompt is exactly what that door is for.
func TestOnlyThePointsTheRequestCarriesAreHeld(t *testing.T) {
	grounds := ofetchGrounds(t)
	held := Held(ofetchPoints(), grounds)
	if len(held) != len(ofetchPoints()) {
		t.Fatalf("held %d of %d points the request states verbatim: %#v",
			len(held), len(ofetchPoints()), held)
	}
	invented := append(ofetchPoints(), plan.Point{
		Behaviour: "The circuit breaker emits an OpenTelemetry span for every transition",
		Quote:     "the circuit breaker emits an OpenTelemetry span for every transition",
	})
	if held := Held(invented, grounds); len(held) != len(ofetchPoints()) {
		t.Errorf("a requirement nobody asked for survived the grounding door: %#v", held)
	}
	if held := Held(ofetchPoints(), Grounds{}); len(held) != 0 {
		t.Error("a checklist weighed against nothing was held; a bound that admits " +
			"everything when it knows nothing is not a bound")
	}
}

// "ALL 56 TESTS PASS" IS A SENTENCE ABOUT TESTS THE SAME WORKER WROTE. This is
// the happy-dom shape: the deliverable claims 31 of 31 pass, the project
// declares no verification this run could read, and the worker derived no diff.
// The claim must buy no coverage at all — and the pass must say out loud that
// nothing was measured, or a checked delivery and an unchecked one are the same
// event in the journal.
func TestAClaimThatEveryTestPassesIsWorthNoCoverage(t *testing.T) {
	evidence := Evidence{
		Observed: true,
		Accept:   ofetchPoints(),
	}
	if checks := CheckEvidence(evidence); len(checks) != 0 {
		t.Fatalf("coverage was found where nothing was measured: %#v", checks)
	}
	pass := Judgment{Pass: true, Checked: true}
	settled := settleAcceptance(context.Background(), config.Config{}, nil,
		store.Node{ID: "task-2"}, evidence, ofetchGrounds(t), "worker/model", pass)
	if !settled.Pass {
		t.Fatal("a delivery was failed for a measurement nobody could take")
	}
	if strings.TrimSpace(settled.Unmeasured) == "" {
		t.Error("the pass does not say that no check could be read, so a checked " +
			"delivery and an unchecked one are the same record")
	}
	// And the claim itself is read by nothing: the same evidence carrying the
	// deliverable's own sentence produces the identical answer.
	if again := CheckEvidence(evidence); len(again) != 0 {
		t.Errorf("prose became evidence: %#v", again)
	}
}

// The finding this whole mechanism exists to raise. The mapping is what the
// judge settled — here, the ofetch family that nothing exercises — and the gap
// must name each unexercised behaviour, be SOURCED so no citation rule can
// refuse it, and be a fail rather than a pass with a note.
func TestABehaviourNoCheckExercisesIsAFinding(t *testing.T) {
	mapping := []store.ExercisedPoint{
		{Point: "A rejected non-listed status does not close half-open state"},
		{Point: "A half-open probe holds its slot across internal retries"},
		{Point: "A parse or hook failure is not retried by status-based retry logic"},
		{
			Point: "A successful logical request resets consecutive failures to zero",
			Check: "resets consecutive failures on success",
		},
	}
	finding, raised := Unexercised(mapping)
	if !raised {
		t.Fatal("three behaviours nothing exercises raised no finding")
	}
	if finding.Pass {
		t.Error("the finding passed the delivery it was raised against")
	}
	if !finding.Sourced {
		t.Error("the finding is not Sourced, so the citation invariant will refuse " +
			"it and the repair round will never be bought")
	}
	if !finding.Checked {
		t.Error("the finding is not Checked, so the gate reads as having said nothing")
	}
	for _, want := range []string{
		"no check exercises: A rejected non-listed status does not close half-open state",
		"no check exercises: A half-open probe holds its slot across internal retries",
		"no check exercises: A parse or hook failure is not retried by status-based retry logic",
	} {
		if !strings.Contains(finding.Gaps, want) {
			t.Errorf("the gap does not say %q:\n%s", want, finding.Gaps)
		}
	}
	if strings.Contains(finding.Gaps, "resets consecutive failures to zero") {
		t.Error("a behaviour a check exercises was raised as one nothing does")
	}
	if len(finding.Exercises) != len(mapping) {
		t.Error("the mapping was not carried onto the judgement, so the evidence " +
			"behind the verdict cannot be journaled")
	}
	// Every point mapped is the case that must stay silent, or the gate would
	// fail every delivery whose checklist it could settle.
	whole := []store.ExercisedPoint{{Point: "a", Check: "one"}, {Point: "b", Check: "two"}}
	if _, raised := Unexercised(whole); raised {
		t.Error("a fully exercised checklist raised a finding")
	}
}

// A LEAF MAY NOT DELETE THE CHECK THAT WAS FAILING IT. Either source convicts:
// the removal read out of the worker's own diff, and the name the runner
// reported before the work and not after it.
func TestACheckThatStoppedExistingIsAFinding(t *testing.T) {
	finding, raised := WeakenedChecks(
		[]string{"keeps the half-open slot across internal retries"},
		[]string{"tests/test_igel.py::test_results_path"})
	if !raised {
		t.Fatal("two checks that stopped existing raised no finding")
	}
	if finding.Pass || !finding.Sourced {
		t.Errorf("the finding is not a sourced failure: pass=%v sourced=%v",
			finding.Pass, finding.Sourced)
	}
	for _, want := range []string{
		"keeps the half-open slot across internal retries",
		"tests/test_igel.py::test_results_path",
	} {
		if !strings.Contains(finding.Gaps, want) {
			t.Errorf("the gap does not name %q:\n%s", want, finding.Gaps)
		}
	}
	if _, raised := WeakenedChecks(nil, nil); raised {
		t.Error("a change that removed nothing raised a finding")
	}
}

// The roster the project's own runner printed is coverage evidence, and the
// before roster is the fallback that can only ever say a behaviour was ALREADY
// covered. A photograph nobody took contributes nothing.
func TestCoverageEvidenceComesFromTheRunnerAndTheDiff(t *testing.T) {
	reading := verify.Reading{
		Taken: true, AfterTaken: true,
		Before: verify.Result{Reported: []string{"existing check"}},
		After:  verify.Result{Reported: []string{"existing check", "opens after five failures"}},
	}
	checks := CheckEvidence(Evidence{Verification: reading})
	if !contains(checks, "opens after five failures") || !contains(checks, "existing check") {
		t.Errorf("the finished tree's roster is not coverage evidence: %#v", checks)
	}
	unread := CheckEvidence(Evidence{Verification: verify.Reading{
		After: verify.Result{Reported: []string{"opens after five failures"}},
	}})
	if len(unread) != 0 {
		t.Errorf("a photograph nobody took supplied %#v", unread)
	}
}

func contains(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}
