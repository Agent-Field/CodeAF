package verify

import "testing"

// THE BASELINE IS THE JOB'S, NOT THE LEAF'S. A repair round stands in a tree its
// own job has already changed, so a round that photographed what IT found took
// the broken tree as its baseline and every check an earlier round turned red
// subtracted to nothing. textual s5 walked twenty project checks down to one
// across four rounds and raised no regression at any of them.
func TestEveryRoundOfAJobIsMeasuredAgainstTheTreeItStartedWith(t *testing.T) {
	ForgetBaselines()
	t.Cleanup(ForgetBaselines)

	root := t.TempDir()
	job := JobKey("Make Log and RichLog expose is_following_end")
	green := Reading{
		Taken: true,
		Before: Result{
			Reported: []string{"test_follow_end", "test_process_line", "test_write"},
		},
	}
	if _, ok := BaselineFor(root, job); ok {
		t.Fatal("a job that has taken no reading inherited one")
	}
	RememberBaseline(root, job, "", green)

	// The second round. It arrives at a tree the first round already broke, and
	// what it must be handed is the reading of the tree BEFORE that.
	held, ok := BaselineFor(root, job)
	if !ok {
		t.Fatal("a continuation of the same job in the same tree inherited nothing")
	}
	held.After = Result{
		Reported: []string{"test_process_line", "test_write"},
		Failing:  []string{"test_process_line"},
	}
	held.AfterTaken = true
	if regressed := held.Regressed(); len(regressed) != 1 || regressed[0] != "test_process_line" {
		t.Errorf("a check the job broke in an earlier round is invisible: %#v", regressed)
	}
	if vanished := held.Vanished(); len(vanished) != 1 || vanished[0] != "test_follow_end" {
		t.Errorf("a check the job deleted in an earlier round is invisible: %#v", vanished)
	}
}

// A DIFFERENT JOB IN THE SAME DIRECTORY RE-BASELINES IT. The first job's changes
// are the second job's world, and blaming them on it would convict every job
// that followed another.
func TestASecondJobInTheSameTreeTakesItsOwnBaseline(t *testing.T) {
	ForgetBaselines()
	t.Cleanup(ForgetBaselines)

	root := t.TempDir()
	RememberBaseline(root, JobKey("the first errand"), "", Reading{Taken: true})
	if _, ok := BaselineFor(root, JobKey("a different errand entirely")); ok {
		t.Error("a new job inherited the previous job's reading of a tree it had " +
			"already changed")
	}
}

// A reading nobody took is not remembered. Remembering the zero value would make
// the next round inherit that silence instead of taking the photograph the job
// still owes.
func TestASilenceIsNotRememberedAsABaseline(t *testing.T) {
	ForgetBaselines()
	t.Cleanup(ForgetBaselines)

	root := t.TempDir()
	RememberBaseline(root, JobKey("an errand"), "", Reading{})
	if _, ok := BaselineFor(root, JobKey("an errand")); ok {
		t.Error("a photograph nobody took was remembered as one somebody did")
	}
}

// The job's identity is the person's own request, whitespace and all, because
// that is the one thing every leaf of a job holds identically and no two jobs
// share.
func TestTheJobIsIdentifiedByTheRequestItself(t *testing.T) {
	if JobKey("  add a   circuit breaker\n") != JobKey("add a circuit breaker") {
		t.Error("two spellings of one request are two jobs")
	}
	if JobKey("add a circuit breaker") == JobKey("add a retry policy") {
		t.Error("two different requests are one job")
	}
	if JobKey("   ") != "" {
		t.Error("a request that says nothing produced a job identity")
	}
}

// A READING IS REMEMBERED AGAINST THE STATE OF THE TREE IT IS A READING OF.
//
// Without it, "has anything happened since somebody looked" had no answer, and
// what stood in for one was "did this leaf inherit a reading" — a fact about a
// lookup rather than about a tree. Every leaf after the first inherits, so every
// leaf after the first bought a second whole reading of a tree nothing had
// touched: four of them in the run measured in #429, two minutes each.
func TestABaselineRemembersTheTreeItWasTakenAgainst(t *testing.T) {
	ForgetBaselines()
	t.Cleanup(ForgetBaselines)

	root := t.TempDir()
	job := JobKey("Run one package's tests and report the final line. Change no files.")
	untouched := TreeState(nil)
	if untouched != "" {
		t.Errorf("a job that has produced nothing has a tree-state of %q, want the empty one",
			untouched)
	}
	// The same account of one tree, assembled in two orders, is one state.
	if TreeState([]string{"b.go", "a.go"}) != TreeState([]string{"a.go", " b.go "}) {
		t.Error("one tree read in two orders produced two states")
	}
	if TreeState([]string{"a.go"}) == TreeState([]string{"a.go", "b.go"}) {
		t.Error("a tree that gained a file kept its state")
	}

	// Nothing was remembered, so nothing can be said about "since": a caller
	// with no moment to be since it must look.
	if TreeUnchangedSince(root, job, untouched) {
		t.Error("a tree nobody has read was called unchanged since a reading of it")
	}
	RememberBaseline(root, job, untouched, Reading{
		Taken:  true,
		Before: Result{Reported: []string{"TestCard", "TestCatalog"}},
	})
	if !TreeUnchangedSince(root, job, untouched) {
		t.Fatal("a job that has changed nothing was told its tree had moved")
	}
	if TreeUnchangedSince(root, job, TreeState([]string{"internal/subharness/card.go"})) {
		t.Error("a job that wrote a file was told its tree was the tree that was read")
	}
	// The reading itself is unchanged by any of this: the tree-state rides
	// beside it and never inside it.
	held, tree, ok := BaselineOf(root, job)
	if !ok || tree != untouched || len(held.Before.Reported) != 2 {
		t.Errorf("the remembered reading did not come back whole: %#v, tree %q", held, tree)
	}
}
