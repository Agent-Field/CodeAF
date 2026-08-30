package verify

import (
	"testing"
	"time"
)

// The budget is a share of the caller's own wall with a floor under it, and it
// lives here rather than inside one worker because two things read it now: the
// worker that photographs before the work, and the delivery gate that takes the
// reading of the tree it is about to judge when nobody else did. PERF.md, "The
// verification photograph's budget", states the same arithmetic for a person.
func TestOneReadingSpendsAnEighthOfTheWallAndNeverLessThanAMinute(t *testing.T) {
	if budget, ok := ReadingBudget(90 * time.Minute); !ok || budget != 11*time.Minute+15*time.Second {
		t.Errorf("a ninety-minute wall affords %v (ok=%v), want 11m15s", budget, ok)
	}
	if _, ok := ReadingBudget(time.Minute); ok {
		t.Error("a sixty-second wall affords 7.5s, which is under the floor: it must photograph nothing")
	}
	if _, ok := ReadingBudget(8 * time.Minute); !ok {
		t.Error("eight minutes is the shortest wall that photographs at all, and it did not")
	}
	if _, ok := ReadingBudget(0); ok {
		t.Error("a wall nobody set is not a wall a reading may be taken against")
	}
}

// A CHECK MISSING FROM ONE EMPTY ROSTER IS NOT A CHECK THAT DISAPPEARED. Plenty
// of runners print nothing at all on success, so an empty roster is a quiet
// runner and never a suite that lost everything — and a rule that read it the
// other way would raise a finding against every project whose test command says
// only "ok".
func TestADisappearanceIsOnlyReadWhenBothRostersNamedSomething(t *testing.T) {
	full := Reading{
		Taken: true, AfterTaken: true,
		Before: Result{Reported: []string{"a", "b", "c"}},
		After:  Result{Reported: []string{"a", "c"}},
	}
	if got := full.Vanished(); len(got) != 1 || got[0] != "b" {
		t.Errorf("Vanished() = %#v, want [b]", got)
	}
	quiet := Reading{
		Taken: true, AfterTaken: true,
		Before: Result{Reported: []string{"a", "b"}},
		After:  Result{},
	}
	if got := quiet.Vanished(); len(got) != 0 {
		t.Errorf("a quiet second reading reported %#v as disappeared", got)
	}
	unread := Reading{Taken: true, Before: Result{Reported: []string{"a"}}}
	if got := unread.Vanished(); len(got) != 0 {
		t.Errorf("a photograph with no second half reported %#v as disappeared", got)
	}
}

// The subtraction the whole photograph is for, asked from the other direction.
// Red before and red after is the repository's own state and not this work's
// doing; red only after is the change's.
func TestOnlyTheChecksThisWorkTurnedRedAreRegressed(t *testing.T) {
	reading := Reading{
		Taken: true, AfterTaken: true,
		Before: Result{Failing: []string{"tests/test_env.py::test_needs_root"}},
		After: Result{Failing: []string{
			"tests/test_env.py::test_needs_root",
			"tests/test_igel.py::test_results_path",
		}},
	}
	got := reading.Regressed()
	if len(got) != 1 || got[0] != "tests/test_igel.py::test_results_path" {
		t.Errorf("Regressed() = %#v, want only the check this work turned red", got)
	}
	if half := (Reading{Taken: true, Before: reading.Before}).Regressed(); half != nil {
		t.Errorf("a photograph nobody finished claimed %#v; nobody looked is not no regression", half)
	}
}
