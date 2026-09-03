package revision

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// A stamp of the world moves when the world moves and not otherwise.
func TestTheTreeStampMovesOnlyWhenTheTreeDoes(t *testing.T) {
	dir := t.TempDir()
	one := filepath.Join(dir, "a.ts")
	two := filepath.Join(dir, "b.ts")
	for _, path := range []string{one, two} {
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	record := []string{one, two}
	before := TreeStamp(record)
	if TreeStamp(record) != before {
		t.Fatal("a tree nobody touched stamped differently twice")
	}
	// The order the record happens to be in is not a change.
	if TreeStamp([]string{two, one}) != before {
		t.Fatal("reordering the record read as a moved tree")
	}
	// A composition runs nothing, so this is the shape it leaves behind.
	if err := os.WriteFile(one, []byte("xy"), 0o644); err != nil {
		t.Fatal(err)
	}
	if TreeStamp(record) == before {
		t.Fatal("a file that grew did not move the stamp")
	}
	// A file that changed size back but was written later is still a change.
	grown := TreeStamp(record)
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(one, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if TreeStamp(record) == grown {
		t.Fatal("a file rewritten to its old size did not move the stamp")
	}
	// A file that has gone is the loudest change there is.
	if err := os.Remove(two); err != nil {
		t.Fatal(err)
	}
	if TreeStamp(record) == grown {
		t.Fatal("a deleted file did not move the stamp")
	}
	// And nothing at all stamps as nothing, twice the same: a run with no
	// record has no tree for a repair to move.
	if TreeStamp(nil) != TreeStamp([]string{}) {
		t.Fatal("an empty record stamped two ways")
	}
}

// A repair that rewrote the account and ran nothing may not close a finding
// about the work; it may still close one about the account.
//
// The two findings below are verbatim from bench/deepswe s5 — ink `task-2` and
// ofetch `task-2-x1` — where a composition closed each of them and the runs
// settled whole at 7 of 25 and 44 of 47 hidden checks. The third is the shape
// Compose exists for, and the fourth is the prose answer whose message is its
// own artifact. See docs/design/gate/SETTLEMENT.md §8.
func TestAnUnmovedRepairClosesOnlyWhatWritingCanClose(t *testing.T) {
	const ink = "The deliverable is a listing of files, not the answer itself. The request " +
		"asked for grid display mode to be implemented — the code changes, the branch, and " +
		"the confirmation that it compiles and works. The missing element is the substance " +
		"of the work: the implemented code, the branch created, and the confirmation that " +
		"it compiles and the new grid properties parse and apply correctly."
	const ofetch = "The deliverable reports that tests pass and the implementation is " +
		"complete, but it does not contain the test output, the verdict on whether tests " +
		"pass, or any evidence that the work was exercised. The missing element is the test " +
		"results and the confirmation that pnpm test exits 0."
	const account = "The deliverable is a list of file paths, not the implementation itself."
	const profiles = "the deliverable does not contain the 12 profiles together in one final message"

	left := []string{"/app/src/grid-layout.ts", "/app/src/styles.ts"}
	red := verify.Reading{Taken: true, AfterTaken: true, After: verify.Result{
		Exit: 1, Entrypoint: verify.Entrypoint{Command: "pnpm test"}}}
	green := verify.Reading{Taken: true, AfterTaken: true, After: verify.Result{
		Exit: 0, Reported: []string{"circuit breaker opens"}}}
	hung := verify.Reading{Taken: true, AfterTaken: true, After: verify.Result{
		Exit: 1, TimedOut: true}}

	for _, c := range []struct {
		name    string
		finding Judgment
		records Evidence
		world   bool
	}{
		// ink: a change on disk, and no reading at all says the change is good.
		// Nobody looked is not nothing wrong.
		{"ink, nothing measured the tree", Judgment{Gaps: ink},
			Evidence{Artifacts: left, Observed: true}, true},
		{"ink, the suite is red", Judgment{Gaps: ink},
			Evidence{Artifacts: left, Verification: red, Observed: true}, true},
		{"ink, the suite hung and named nothing", Judgment{Gaps: ink},
			Evidence{Artifacts: left, Verification: hung, Observed: true}, true},
		// ofetch names the command the record says was run, so it is a finding
		// about a check however the reading came back.
		{"ofetch names the check the run ran", Judgment{Gaps: ofetch},
			Evidence{Artifacts: left, Ran: []string{"pnpm test"}, Verification: green, Observed: true}, true},
		{"ofetch, the suite is red", Judgment{Gaps: ofetch},
			Evidence{Artifacts: left, Verification: red, Observed: true}, true},
		// What a composition may still close: the account was wrong and the
		// project's own checks say the change under it is good.
		{"the account alone, over a green suite", Judgment{Gaps: account},
			Evidence{Artifacts: left, Verification: green, Observed: true}, false},
		// And the prose answer, whose message is the whole of what it produced.
		{"a prose answer that left no files", Judgment{Gaps: profiles},
			Evidence{Observed: true}, false},
		// The two findings the gate settled against the world itself never
		// reach any of that.
		{"a promised file the disk does not hold", Judgment{Gaps: "report.md", Mechanical: true},
			Evidence{Observed: true}, true},
		{"a check this work turned red", Judgment{Gaps: "it broke three checks", Sourced: true},
			Evidence{Observed: true}, true},
	} {
		if got := GroundedInTheWorld(c.finding, c.records); got != c.world {
			t.Errorf("%s: grounded in the world = %v, want %v", c.name, got, c.world)
		}
		// And the rule the settlement spends: an unmoved repair closes it only
		// where the ground was the text.
		passed := Judgment{Pass: true, Checked: true}
		if got := RepairClosed(passed, c.finding, c.records, true); got == c.world {
			t.Errorf("%s: an unmoved repair closed=%v over a finding grounded in the world=%v",
				c.name, got, c.world)
		}
		// A repair that DID move the tree closes it either way — that is a
		// worker that ran and a judge that read what it did.
		if !RepairClosed(passed, c.finding, c.records, false) {
			t.Errorf("%s: a repair that moved the tree was not allowed to close its gate", c.name)
		}
	}

	// A second verdict that never arrived is not a pass, whatever the tree did.
	// An unreadable gate is a gate that did not run.
	for _, rejudged := range []Judgment{{}, {Pass: true}, {Checked: true}, {Checked: true, Fault: "no verdict"}} {
		if RepairClosed(rejudged, Judgment{Gaps: account}, Evidence{}, false) {
			t.Errorf("a repair closed its gate on a re-judgement that did not happen: %+v", rejudged)
		}
	}
}

// A PROMISED FILE STILL NEEDS THE DISK TO MOVE, WHATEVER THE PERSON FORBADE.
//
// "The run was told to change nothing" makes an unmoved repair neutral about
// findings a repair could only close by working — that is the rule a run under
// `no_writes` needs, and #427's stream is what it is written against. It must
// not become a blanket amnesty. A MECHANICAL gap is a file the plan or the
// person PROMISED and the disk does not hold, and no rule about what a run may
// not write makes an absent deliverable appear: without the guard, one rule on
// the record would let a repair that moved nothing close every world-grounded
// finding a second judge happened to pass, which is §8 switched off by a
// sentence about something else.
func TestAPromisedFileStillNeedsTheDiskToMoveUnderNoWrites(t *testing.T) {
	toldNothing := Evidence{Observed: true,
		Constraints: []plan.Constraint{{Text: "Change no files.", Kind: plan.ConstraintNoWrites}}}
	rejudged := Judgment{Pass: true, Checked: true}

	promised := Judgment{Pass: false, Checked: true, Mechanical: true,
		Gaps: "the plan promised report.md and the disk does not hold it"}
	if RepairClosed(rejudged, promised, toldNothing, true) {
		t.Fatal("a repair that moved nothing closed a finding about a file that is not on disk")
	}
	// And the finding the neutrality exists for is still neutral: a reading of
	// the work, on a run whose contract was to change nothing.
	reading := Judgment{Pass: false, Checked: true, Sourced: true,
		Gaps: "the final line reported is not the one the command printed"}
	if !RepairClosed(rejudged, reading, toldNothing, true) {
		t.Fatal("a run keeping the rule it was given was read as a repair that did nothing")
	}
}
