package factory

import (
	"strings"
	"testing"
	"time"
)

// THE PERSON TELLS AUTO TO CARRY ON PAST A FAILING CHECK: the pause clears,
// the job's history names who overrode which finding, and a finding they did
// not override pauses Auto again.
func TestAPersonsResumeOverridesTheFindingAutoPausedOn(t *testing.T) {
	now := time.Date(2026, 11, 16, 9, 30, 0, 0, time.UTC)
	it := Item{Autonomy: AutonomyAuto, Assignee: "sam", Cap: 5, State: StateLanded,
		Proof:  []Claim{{Text: "tests pass", OK: true}},
		Policy: []Claim{{Text: "FINDING: secrets in config", Medium: "policy"}}}
	if why := AutonomyBlockReason(it); !strings.HasPrefix(why, "Repository policy needs attention: FINDING: secrets in config") {
		t.Fatalf("Auto did not pause on the failing policy check: %q", why)
	}
	if !OffersAutoResume(it) {
		t.Fatal("a pause on a failing finding does not offer resume")
	}

	found, err := OverrideFindings(&it, "@Sam", "resume anyway, that file is a fixture", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Kind != OverridePolicy || found[0].Text != "FINDING: secrets in config" || found[0].By != "sam" || !found[0].At.Equal(now) {
		t.Fatalf("the override reads %+v", found)
	}
	if why := AutonomyBlockReason(it); why != "" {
		t.Fatalf("Auto is still paused after the person's override: %q", why)
	}
	if OffersAutoResume(it) {
		t.Fatal("resume is still offered with nothing left to override")
	}
	last := it.Activity[len(it.Activity)-1]
	if !last.At.Equal(now) || last.What != "Auto pause overridden by @sam · repository policy: FINDING: secrets in config" {
		t.Fatalf("the job's history reads %+v", last)
	}

	// A NEW FAILING FINDING PAUSES AGAIN; the old one stays overridden.
	it.Proof = append(it.Proof, Claim{Text: "screenshot of the form"})
	if why := AutonomyBlockReason(it); !strings.HasPrefix(why, "Proof needs attention: screenshot of the form") {
		t.Fatalf("a new failing finding did not pause Auto: %q", why)
	}
	if got := FailingFindings(it); len(got) != 1 || got[0].Text != "screenshot of the form" {
		t.Fatalf("failing findings %+v, want only the new one", got)
	}
}

// NOTHING TO OVERRIDE IS REFUSED: a job not in Auto, or paused on something
// that is not a failing finding (a budget, a question), keeps its own door.
func TestResumeIsRefusedWhenAutoIsNotPausedOnAFinding(t *testing.T) {
	failing := []Claim{{Text: "FINDING: x"}}
	for name, it := range map[string]Item{
		"manual": {Autonomy: AutonomyManual, Assignee: "sam", Cap: 5, State: StateLanded, Policy: failing},
		"budget": {Autonomy: AutonomyAuto, Assignee: "sam", Cap: 0, State: StateLanded, Policy: failing},
		"clean":  {Autonomy: AutonomyAuto, Assignee: "sam", Cap: 5, State: StateLanded, Proof: []Claim{{Text: "ok", OK: true}}},
	} {
		if _, err := OverrideFindings(&it, "sam", "resume", time.Now()); err == nil {
			t.Fatalf("%s: an override was recorded with no pause on a finding", name)
		}
		if len(it.Overrides) != 0 {
			t.Fatalf("%s: a refused override left %+v", name, it.Overrides)
		}
	}
}

// THE MANAGER IS TOLD THE PAUSE AND THE DOOR, and told nothing once it clears.
func TestTheManagersStatusOffersResumeOnlyWhilePausedOnAFinding(t *testing.T) {
	it := Item{Autonomy: AutonomyAuto, Assignee: "sam", Cap: 5, State: StateLanded,
		Proof: []Claim{{Text: "tests pass", OK: true}}, Policy: []Claim{{Text: "FINDING: x"}}}
	person, actions := managerPRDecision(it)
	if !strings.Contains(strings.Join(person, " "), "Auto is paused on a failing check: repository policy: FINDING: x") ||
		!strings.Contains(strings.Join(actions, " "), "action=resume") {
		t.Fatalf("status does not offer resume: %v / %v", person, actions)
	}
	if _, err := OverrideFindings(&it, "sam", "resume", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, actions := managerPRDecision(it); strings.Contains(strings.Join(actions, " "), "action=resume") {
		t.Fatalf("resume still offered after the override: %v", actions)
	}
}
