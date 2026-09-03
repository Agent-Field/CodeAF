package splitgate

import (
	"os"
	"path/filepath"
	"testing"
)

// THE UNPINNED BINARY KEEPS EVERY DIVISION IT IS SHOWN.
//
// THIS PIN MOVED, AND IT MOVED ON A MEASUREMENT. Until 2026-09-02 this file
// held the opposite test: an unpinned binary was the gate as shipped, counting
// items against [Floor], and every other reading was something a person had to
// type. Then a designed experiment ran four planner arms against four readings
// of this gate over 273 judged plan draws, and the front it drew is the stages
// planner with the gate having NO SAY — same quality as the best armed cell,
// a third of its unrecoverable draws, the lowest cost of the front
// (docs/design/plan-gate-doe/REPORT.md). So the default is off, and the count
// is what somebody opts into.
func TestTheUnpinnedBinaryKeepsEveryDivisionThePlannerDrew(t *testing.T) {
	t.Setenv("AFORGE_SPLITGATE", "")
	if got := Mode(); got != ModeOff {
		t.Fatalf("the unpinned mode is %q, want %q", got, ModeOff)
	}
	if Armed() {
		t.Error("the gate has the last word with nobody having pinned it on")
	}
	for _, probe := range []string{
		"twelve image files need captions",
		"there are 12 image files",
		"L1: rewrite the headings. L2: link the cross-references. L3: add contents.",
		"HANDBOOK.md is one file of thirty chapters",
		"keep each section under 250 words",
		"fix the one bug",
		"",
	} {
		if !Judge(probe, nil).Keep {
			t.Errorf("the unpinned gate folded %q; off means every division stands as drawn", probe)
		}
	}
	// THE COUNT IS STILL REPORTED, and it is still the shipped reading. Arming
	// reads it (internal/session's enumeratesWidth), a run's log shows what a
	// floor would have made of a division, and `1` puts it back in charge — so
	// what the experiment moved is who decides, not what the counter counts.
	for _, probe := range []struct {
		text  string
		items int
	}{
		{"twelve image files need captions", 12},
		{"there are 12 image files", 12},
		{"keep each section under 250 words", 0},
		{"HANDBOOK.md is one file of thirty chapters", 0},
	} {
		if got := Judge(probe.text, nil).Items; got != probe.items {
			t.Errorf("Judge(%q).Items = %d, want the shipped reading %d", probe.text, got, probe.items)
		}
	}
}

// THE PIN IS THE SELECTOR, AND AN UNREADABLE PIN IS OFF.
//
// The rule reversed with the default. While the gate was armed by default an
// unknown word had to leave a run on the shipped gate, because the danger was a
// typo moving somebody onto an experimental arm. The danger now runs the other
// way: off is what was measured, and a typo must not put a floor back under
// somebody's divisions. `lanes` is in the table because it was a real arm of
// the experiment and people have typed it — it lost, its code is gone, and the
// word now reads as off like any other.
func TestThePinSelectsTheModeAndAnythingUnknownIsOff(t *testing.T) {
	for _, probe := range []struct {
		pin  string
		want GateMode
	}{
		{"", ModeOff},
		{"0", ModeOff},
		{"1", ModeCount},
		{" 1 ", ModeCount},
		{"judgment", ModeJudgment},
		{" JUDGMENT ", ModeJudgment},
		{"lanes", ModeOff},
		{"on", ModeOff},
		{"true", ModeOff},
		{"judgement", ModeOff},
		{"2", ModeOff},
	} {
		t.Setenv("AFORGE_SPLITGATE", probe.pin)
		if got := Mode(); got != probe.want {
			t.Errorf("AFORGE_SPLITGATE=%q selected %q, want %q", probe.pin, got, probe.want)
		}
		if got, want := Armed(), probe.want != ModeOff; got != want {
			t.Errorf("AFORGE_SPLITGATE=%q armed=%v, want %v", probe.pin, got, want)
		}
	}
}

// OFF IS OFF IN EVERY SHAPE. `0` predates the experiment as this gate's
// rollback switch and it has to keep meaning what it meant, even now that it
// selects what an unset pin selects anyway: the gate has no say, whatever the
// brief counts and whatever the plan drew.
func TestOffKeepsEveryDivisionWhateverTheBriefOrThePlanSays(t *testing.T) {
	t.Setenv("AFORGE_SPLITGATE", "0")
	for _, probe := range []struct {
		text   string
		leaves []Leaf
	}{
		{"", nil},
		{"fix the one bug", nil},
		{"fix the one bug", strictChainOfAtomicLeaves()},
		{"fix the one bug", oneOversizedAmongIndependentLeaves()},
	} {
		if !Judge(probe.text, probe.leaves).Keep {
			t.Errorf("the disarmed gate folded %q", probe.text)
		}
	}
}

// AND `1` IS THE GATE EXACTLY AS IT SHIPPED. The mode did not change when the
// default did: a run that pins the count gets the same floor over the same
// reading it always got, down to folding #418's three-lane brief — which is
// the fault that started the experiment and the reason this is a pin now rather
// than what everybody gets.
func TestTheCountPinIsTheGateExactlyAsItShipped(t *testing.T) {
	t.Setenv("AFORGE_SPLITGATE", "1")
	for _, probe := range []string{
		"twelve image files need captions",
		"there are 12 image files",
		"L1: rewrite the headings. L2: link the cross-references. L3: add contents.",
		"HANDBOOK.md is one file of thirty chapters",
		"keep each section under 250 words",
		"",
	} {
		if got, want := Judge(probe, nil).Keep, WorthIt(probe); got != want {
			t.Errorf("Judge(%q).Keep = %v, want the shipped WorthIt answer %v", probe, got, want)
		}
	}
	if !Armed() {
		t.Error("AFORGE_SPLITGATE=1 did not arm the gate")
	}
	// And the sizing a judgment run would read is not read here: three
	// independent atomic leaves cannot rescue a brief that counts zero.
	if Judge("rewrite the handbook in three lanes", threeIndependentAtomicLeaves()).Keep {
		t.Error("the count pin kept a division on the plan's sizing; only AFORGE_SPLITGATE=judgment does that")
	}
}

// JUDGMENT ASKS THE PLAN AND NOT THE TEXT. The rows are the whole design:
// sizing says yes and the count is overruled; sizing says nothing useful and
// the count decides; and an oversized leaf is never the yes.
func TestJudgmentReadsThePlansSizingAndFallsBackToTheCount(t *testing.T) {
	t.Setenv("AFORGE_SPLITGATE", "judgment")
	// A brief with no digit in it at all, and three atomic leaves that owe each
	// other nothing: KEPT. This is #418's replication, decided the other way.
	narrow := "HANDBOOK.md is one file. Deliver lanes that share no lines: rewrite the headings, link the cross-references, insert a contents section."
	if got := Items(narrow); got != 0 {
		t.Fatalf("the replication brief counts %d items; it is supposed to count none", got)
	}
	if !Judge(narrow, threeIndependentAtomicLeaves()).Keep {
		t.Error("judgment folded a division of three independent sittings; the plan's sizing is what this mode reads")
	}
	// A STRICT CHAIN FALLS BACK TO THE COUNT, and on this brief the count is
	// zero, so it folds. It falls back rather than refusing outright because
	// that is what keeps this mode one-directional: it may only add keeps to
	// what the count would have done. A chain over a brief that DOES enumerate
	// its pile is kept, by the count.
	if Judge(narrow, strictChainOfAtomicLeaves()).Keep {
		t.Error("judgment kept a strict chain over a brief naming nothing; three sittings in a row are one sitting")
	}
	if !Judge("there are 12 image files to caption", strictChainOfAtomicLeaves()).Keep {
		t.Error("judgment folded a chain the count would have kept; the fallback may not take keeps away")
	}
	// AN OVERSIZED LEAF IS NEVER THE REASON A DIVISION IS KEPT. The planner
	// having failed to get a part down to a sitting is the fault #384 is about,
	// not evidence that the division is real.
	if Judge(narrow, oneOversizedAmongIndependentLeaves()).Keep {
		t.Error("judgment kept a division on the strength of a leaf the planner could not finish sizing")
	}
	// Nor is an unsized leaf: no sizing at all means the plan has no opinion,
	// and the count answers.
	if Judge(narrow, unsizedLeaves()).Keep {
		t.Error("judgment kept a division whose parts nothing had sized")
	}
	// And a single part is not a division at all.
	if Judge(narrow, threeIndependentAtomicLeaves()[:1]).Keep {
		t.Error("judgment kept a one-leaf graph as a division")
	}
	// THE COUNT IT FALLS BACK TO IS THE SHIPPED ONE, and it is the only
	// counting left in the package: the wider reading the experiment ran
	// against it lost and was deleted with it.
	spelled := "HANDBOOK.md is one file of thirty chapters"
	if Judge(spelled, nil).Keep {
		t.Error("judgment read a wider counting than the shipped one; its one repair is the plan's sizing")
	}
}

// THE CORPUS DECISIONS HOLD IN EVERY MODE THAT COUNTS. The eight bench tasks
// are the measurement this gate was built on — twelve image files and eight
// endpoints divided, four modules and three bugs did not — and a counting that
// quietly re-decided one of them would have thrown the measurement away.
//
// ModeOff is not in the table because it decides nothing: off keeps all eight.
// ModeJudgment is, because on text alone — which is all the corpus is, eight
// files with no plan behind them — it falls back to exactly this count.
func TestEveryCountingModeStillDecidesTheCorpusTheWayItWasMeasured(t *testing.T) {
	// The DECISION column is the measurement and may not drift; the ITEMS
	// column is what the counter read, recorded so that a drift toward the
	// floor is visible here rather than discovered as a re-decided task.
	decided := map[string]struct {
		items  int
		divide bool
	}{
		"api refactor.txt":       {8, true},
		"bugfix repo.txt":        {3, false},
		"codegen modules.txt":    {1, false},
		"doc coverage.txt":       {0, false},
		"image captions.txt":     {12, true},
		"prose report.txt":       {0, false},
		"research synthesis.txt": {4, false},
		"review diff.txt":        {0, false},
	}
	paths, err := filepath.Glob(filepath.Join("..", "..", "bench", "swarm", "tasks", "*.txt"))
	if err != nil {
		t.Fatalf("reading the corpus: %v", err)
	}
	if len(paths) != len(decided) {
		t.Fatalf("the corpus holds %d tasks and the table names %d: a task was added or removed without a decision being recorded", len(paths), len(decided))
	}
	for _, path := range paths {
		name := filepath.Base(path)
		want, known := decided[name]
		if !known {
			t.Errorf("%s is in the corpus and not in the table", name)
			continue
		}
		text, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		if got := Items(string(text)); got != want.items {
			t.Errorf("%s: the counting reads %d items, want %d", name, got, want.items)
		}
		for _, mode := range []GateMode{ModeCount, ModeJudgment} {
			t.Setenv("AFORGE_SPLITGATE", string(mode))
			if got := Judge(string(text), nil).Keep; got != want.divide {
				t.Errorf("%s under AFORGE_SPLITGATE=%s divides=%v, want the measured %v", name, mode, got, want.divide)
			}
		}
	}
}

// The three leaf shapes the judgment rows are written against. They are
// functions rather than package variables because a Decision-taking test that
// mutated one would move the ground under the others.

// threeIndependentAtomicLeaves is a real division: three sittings, no edges
// between them.
func threeIndependentAtomicLeaves() []Leaf {
	return []Leaf{
		{ID: 2, Size: "atomic", Needs: []int{1}},
		{ID: 3, Size: "atomic", Needs: []int{1}},
		{ID: 4, Size: "atomic", Needs: []int{1}},
	}
}

// strictChainOfAtomicLeaves is three sittings that each wait on the one before:
// one sitting spread over three workers, two of them idle.
func strictChainOfAtomicLeaves() []Leaf {
	return []Leaf{
		{ID: 2, Size: "atomic"},
		{ID: 3, Size: "atomic", Needs: []int{2}},
		{ID: 4, Size: "atomic", Needs: []int{3}},
	}
}

// oneOversizedAmongIndependentLeaves is the shape #384 is about: parts that owe
// each other nothing, one of which the planner never got down to a sitting.
func oneOversizedAmongIndependentLeaves() []Leaf {
	return []Leaf{
		{ID: 2, Size: "atomic"},
		{ID: 3, Size: SizeOversized},
		{ID: 4, Size: "atomic"},
	}
}

// unsizedLeaves is a graph nothing sized at all — an older document, or a
// sizing pass that never ran.
func unsizedLeaves() []Leaf {
	return []Leaf{{ID: 2}, {ID: 3}, {ID: 4}}
}
