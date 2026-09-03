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

// A READING KILLED AT ITS BUDGET IS NOT RETAKEN OVER THE SAME SCOPE. A second
// identical attempt cannot finish where the first did not, and it spends the
// same eighth of the wall to find that out.
func TestACutReadingIsNotRetakenOverTheSameScope(t *testing.T) {
	// The whole suite, cut. There is no narrower scope to fall to — the
	// selection is everything the entrypoint covers — so the answer stands.
	whole := Reading{
		CutAfter: 2 * time.Minute, Budget: 2 * time.Minute,
		Strategy: Strategy{Command: "go test -json ./...", Scope: ScopeWhole},
	}
	if whole.Retakeable() {
		t.Error("a whole reading cut at its budget was queued to be run again whole")
	}
	// And a scoped one whose measured pace affords no file at all inside the
	// budget is the same refusal one level down: two files cost two minutes
	// between them, the budget is thirty seconds, and there is no selection
	// smaller than one file to retake over.
	same := Reading{
		CutAfter: 2 * time.Minute, Budget: 30 * time.Second,
		Strategy: Strategy{
			Base:     "python3 -m pytest -rA",
			Selected: []string{"tests/test_log.py", "tests/test_widget.py"},
			Scope:    "touched packages (2 files)",
		},
	}
	if same.Retakeable() {
		t.Errorf("a cut reading was retaken over the %d files that were just killed",
			len(same.Strategy.Selected))
	}
	// And where the pace affords a strictly smaller reading, it is still taken:
	// forty files cut at 113 seconds narrow to twenty, which is a different
	// reading and a real one.
	narrower := Reading{
		CutAfter: 113 * time.Second, Budget: 113 * time.Second,
		Strategy: Strategy{
			Base:     "python3 -m pytest -rA",
			Selected: make([]string, 40),
			Scope:    "touched packages (40 files)",
		},
	}
	if !narrower.Retakeable() {
		t.Error("a cut reading with a smaller selection to fall to was inherited as settled")
	}
}

// THE READING OF A TREE NOTHING CHANGED IS THE READING SOMEBODY ALREADY TOOK OF
// IT. The suite would be run a second time over identical bytes for an identical
// roster, which is what the measured errand paid two minutes for, four times.
func TestAnUnchangedTreeInheritsTheReadingBeforeTheWork(t *testing.T) {
	before := Reading{
		Taken: true,
		Before: Result{
			Reported: []string{"TestCard", "TestCatalog"},
			Failing:  []string{"TestCatalog"},
		},
	}
	settled, ok := before.OnAnUnchangedTree()
	if !ok {
		t.Fatal("a reading of an unchanged tree could not stand as the reading of it")
	}
	if !settled.AfterTaken || len(settled.After.Reported) != 2 {
		t.Fatalf("the reading before the work did not stand as the reading after it: %#v", settled)
	}
	// It settles nothing it did not measure: a check that was red before the
	// work is not a check the work broke.
	if regressed := settled.Regressed(); len(regressed) != 0 {
		t.Errorf("an unchanged tree produced a regression: %#v", regressed)
	}
	// And there is nothing to stand where nobody looked, or where a real second
	// reading has already been taken.
	if _, ok := (Reading{Unread: "this project declares no way of checking itself"}).
		OnAnUnchangedTree(); ok {
		t.Error("a reading nobody took was offered as the reading of a finished tree")
	}
	if _, ok := settled.OnAnUnchangedTree(); ok {
		t.Error("a tree that was actually read a second time had its reading overwritten")
	}
}
