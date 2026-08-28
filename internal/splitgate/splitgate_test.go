package splitgate

import (
	"os"
	"path/filepath"
	"testing"
)

// THE CORPUS DECISIONS, PINNED.
//
// The eight tasks in bench/swarm/tasks are what this gate was measured against,
// and the table below is the decision it made on each of them — the same
// decision the empirically best arm of the A/B made (bench/swarm/AB-REPORT.md).
// It used to be logged and not asserted, which meant the counting could be
// changed by anybody and nothing would go red; now that a second product asks
// this same question (internal/session's task_divide.go) an unasserted table is
// a measurement two roads can silently drift away from.
func TestTheGateStillDecidesTheCorpusTheWayItWasMeasured(t *testing.T) {
	decided := map[string]struct {
		items  int
		divide bool
	}{
		// The DECISION column is the measurement and may not drift. The items
		// column is the counter's own reading and moved once, when the noun
		// allowlist became the measures denylist: list markers beside a
		// plural-shaped word now register ("1. `textstats.py`", "(4) ends
		// with") and "note-1 … note-5" no longer does — small counts either
		// way, every decision identical.
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
			t.Errorf("%s enumerates %d items, want %d", name, got, want.items)
		}
		if got := WorthIt(string(text)); got != want.divide {
			t.Errorf("%s divides=%v, want %v", name, got, want.divide)
		}
	}
}

// A NUMBER IS NOT AN ITEM UNLESS IT STANDS BESIDE ONE. This is the rule that
// keeps the gate from dividing work over a parameter somebody wrote down, and
// it is the one thing most likely to be "improved" into a bug.
func TestOnlyNumbersBesideAnItemNounAreCounted(t *testing.T) {
	for _, probe := range []struct {
		text string
		want int
	}{
		{"twelve image files need captions", 12},
		{"there are 12 image files", 12},
		{"bugs: 3", 3},
		{"set limit=100 and keep it under 250 words", 0},
		{"flatten the tree", 0},
		{"rewrite eight sections of the report", 8},
		{"eight", 0},
		{"", 0},
		{"9 endpoints, 4 tables", 9},
	} {
		if got := Items(probe.text); got != probe.want {
			t.Errorf("Items(%q) = %d, want %d", probe.text, got, probe.want)
		}
	}
}

// AND AN ITEM IS ANYTHING SOMEBODY HAS A PILE OF, in whatever domain they work
// in. The first counter knew eighteen nouns and read a live OSINT task's "34
// person-rows" as zero items twice — on honest evidence — because contact
// hunters do not write "files". The pile is open-ended, so the gate counts any
// plural word that is not a measure, and this table pins the domains the old
// list refused alongside the parameters that must stay refused.
func TestAnyEnumeratedPileCountsAndMeasuresNeverDo(t *testing.T) {
	for _, probe := range []struct {
		text string
		want int
	}{
		// the incident, verbatim shapes
		{"ranked-real-users.md holds 34 person-rows across 6 tiers", 34},
		{"34 independent research targets", 34},
		{"34 people missing a verified email", 34},
		// piles from domains no noun list anticipated
		{"the sweep found 11 contacts", 11},
		{"eight repos have never been triaged", 8},
		{"there are 7 spreadsheets to reconcile", 7},
		// measures, budgets and parameters stay refused
		{"keep each section under 250 words", 0},
		{"the run took 90 seconds", 0},
		{"give it 3 retries and 200 steps", 0},
		{"the server returned status 500", 0},
		{"port 8080 is taken", 0},
		{"order id 458812 was refunded twice", 0},
		// a verb before a number is not a pile
		{"the ledger holds 34", 0},
		{"it covers 250", 0},
	} {
		if got := Items(probe.text); got != probe.want {
			t.Errorf("Items(%q) = %d, want %d", probe.text, got, probe.want)
		}
	}
}

func TestTheFloorIsWhereDivisionStartedPaying(t *testing.T) {
	if WorthIt("5 files") {
		t.Error("five items divided; the floor is where division started paying and five is under it")
	}
	if !WorthIt("6 files") {
		t.Errorf("six items did not divide; the floor is %d", Floor)
	}
}

// THE ESCAPE HATCH IS ONE LITERAL. It is `0` and nothing else — not "false",
// not "off" — because that is what the switch has always meant and a second
// spelling would be a rollback somebody thought they had taken.
func TestTheGateIsArmedUnlessSomebodyWroteTheZero(t *testing.T) {
	t.Setenv("AFORGE_SPLITGATE", "")
	if !Armed() {
		t.Error("the gate is off with nobody having said anything")
	}
	t.Setenv("AFORGE_SPLITGATE", "0")
	if Armed() {
		t.Error("AFORGE_SPLITGATE=0 did not take the gate away")
	}
	t.Setenv("AFORGE_SPLITGATE", "1")
	if !Armed() {
		t.Error("AFORGE_SPLITGATE=1 turned the gate off")
	}
}
