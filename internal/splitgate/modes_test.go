package splitgate

import (
	"os"
	"path/filepath"
	"testing"
)

// THE UNPINNED BINARY IS THE SHIPPED BINARY. Everything else in this file
// describes an experiment; this describes the thing the experiment must not
// disturb. Every mode is reachable only by somebody typing the pin, and with
// nobody having typed anything the answers here are the ones the gate gave
// before the modes existed — the same count, the same floor, the same yes.
func TestTheUnpinnedBinaryDecidesExactlyAsItShipped(t *testing.T) {
	t.Setenv("AFORGE_SPLITGATE", "")
	if got := Mode(); got != ModeCount {
		t.Fatalf("the unpinned mode is %q, want %q", got, ModeCount)
	}
	for _, probe := range []string{
		"twelve image files need captions",
		"there are 12 image files",
		"L1: rewrite the headings. L2: link the cross-references. L3: add contents.",
		"HANDBOOK.md is one file of thirty chapters",
		"keep each section under 250 words",
		"",
	} {
		if got, want := Count(probe), Items(probe); got != want {
			t.Errorf("Count(%q) = %d, want the shipped Items reading %d", probe, got, want)
		}
		if got, want := Judge(probe, nil).Keep, WorthIt(probe); got != want {
			t.Errorf("Judge(%q).Keep = %v, want the shipped WorthIt answer %v", probe, got, want)
		}
	}
	// And the sizing a judgment run would read is not read at all here: three
	// independent atomic leaves cannot rescue a brief that counts zero.
	if Judge("rewrite the handbook in three lanes", threeIndependentAtomicLeaves()).Keep {
		t.Error("an unpinned binary kept a division on the plan's sizing; only AFORGE_SPLITGATE=judgment does that")
	}
}

// THE PIN IS THE SELECTOR, AND AN UNREADABLE PIN IS NOT AN ARM. A typo must
// leave a run on the shipped gate rather than quietly moving it onto an
// experimental one, which is the same rule the escape hatch has always had in
// the other direction: `0` and nothing else turns the gate off.
func TestThePinSelectsTheModeAndAnythingUnknownIsTheShippedOne(t *testing.T) {
	for _, probe := range []struct {
		pin  string
		want GateMode
	}{
		{"", ModeCount},
		{"1", ModeCount},
		{"0", ModeOff},
		{"lanes", ModeLanes},
		{"judgment", ModeJudgment},
		{" JUDGMENT ", ModeJudgment},
		{"Lanes", ModeLanes},
		{"off", ModeCount},
		{"false", ModeCount},
		{"judgement", ModeCount},
		{"2", ModeCount},
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

// OFF IS OFF IN EVERY SHAPE. The rollback switch is the one thing here that
// predates the experiment and it has to keep meaning what it meant: the gate
// has no say, whatever the brief counts and whatever the plan drew.
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

// LANES READS THE SHAPES A DIVISION IS WRITTEN DOWN IN. Each row is a phrasing
// #418 measured reading as nothing, or one of the parameter shapes that must go
// on reading as nothing however the counting widens.
func TestLanesCountsNamedPartsAndStillRefusesParameters(t *testing.T) {
	for _, probe := range []struct {
		text  string
		items int
		lanes int
	}{
		// #418's held-out briefs, every one of them counted as zero today.
		{"HANDBOOK.md is one file of thirty chapters. Deliver three lanes that share no lines.", 0, 30},
		{"L1: rewrite the headings. L2: link the cross-references. L3: insert a contents section.", 0, 3},
		{"four lanes over a log of about 4,000 lines", 0, 4},
		{"rewrite server.py, client.py, parser.py, cache.py and index.py", 0, 5},
		{"1. rewrite the header\n2. link the index\n3. add a summary\n", 0, 3},
		{"- the parser\n- the cache\n- the units\n- the loader\n", 0, 4},
		// the shapes the narrower counter already reads, unchanged
		{"twelve image files need captions", 12, 12},
		{"there are 12 image files", 12, 12},
		{"9 endpoints, 4 tables", 9, 9},
		// measures, budgets and parameters stay refused in the wider reading
		{"keep each section under 250 words", 0, 0},
		{"the run took 90 seconds", 0, 0},
		{"give it 3 retries and 200 steps", 0, 0},
		{"port 8080 is taken", 0, 0},
		{"see e.g. the note above, i.e. the one at the top", 0, 0},
		{"write REPORT.md and nothing else", 0, 0},
		{"do not modify any file under tests/ or under app/", 0, 0},
	} {
		if got := Items(probe.text); got != probe.items {
			t.Errorf("Items(%q) = %d, want %d", probe.text, got, probe.items)
		}
		if got := Lanes(probe.text); got != probe.lanes {
			t.Errorf("Lanes(%q) = %d, want %d", probe.text, got, probe.lanes)
		}
		if Lanes(probe.text) < Items(probe.text) {
			t.Errorf("Lanes(%q) read fewer items than Items did; the wider reading may only widen", probe.text)
		}
	}
	t.Setenv("AFORGE_SPLITGATE", "lanes")
	if !Judge("HANDBOOK.md is one file of thirty chapters. Deliver three lanes.", nil).Keep {
		t.Error("lanes folded a brief naming thirty chapters; that is the reading the shipped counter got wrong")
	}
}

// A DIVISION SOMEBODY WROTE OUT IS NOT A PILE TO BE COUNTED — the mode's one
// law, and the only reading in the package that skips the floor. Each keep row
// is a brief in which the person named the workers rather than the material;
// each fold row is material, counted against the floor exactly as before.
func TestLanesTakesAWrittenOutDivisionAtItsWordAndStillCountsEverythingElse(t *testing.T) {
	for _, probe := range []struct {
		text     string
		explicit bool
		why      string
	}{
		// #418's replication, and the case the mode exists to answer.
		{"HANDBOOK.md is one file of thirty chapters. Deliver three lanes that share no lines. L1: rewrite every heading. L2: link every cross-reference. L3: insert a contents section.", true,
			"three labelled lanes over one file are three people's work, however few the lanes"},
		{"lane 1 takes the parser, lane 2 takes the cache", true, "a lane word and a designator, twice"},
		{"part A is the schema and part B is the migration", true, "letter designators count the same as numbers"},
		{"split it into p1, p2 and p3", true, "the label family is what makes it a division"},
		// Material, not labour: still a count, still against the floor.
		{"Create four separate, independent Python utility modules", false,
			"four modules is the bench measurement that four does not pay"},
		{"1. rewrite the header\n2. link the index\n3. add a summary\n", false,
			"a numbered list is how people write down items; the corpus numbers its three bugs that way"},
		{"- the parser\n- the cache\n- the units\n", false, "a bulleted list is the same shape as a numbered one"},
		{"there are 12 image files", false, "a count is a count however large"},
		{"rewrite server.py, client.py and parser.py", false, "naming the files says how much there is, not who does what"},
		// One label is a version, a port or an identifier — never a lane.
		{"upgrade to v2 before shipping", false, "one label is not a family"},
		{"part of the report needs a rewrite", false, "`part of` names no part"},
		{"the run took 90 seconds and used 3 retries", false, "parameters stay parameters"},
	} {
		if got := ExplicitDivision(probe.text); got != probe.explicit {
			t.Errorf("ExplicitDivision(%q) = %v, want %v — %s", probe.text, got, probe.explicit, probe.why)
		}
	}

	// AND THE LAW IS WHAT SEPARATES THIS ARM FROM THE SHIPPED ONE. The brief
	// #418 opens with is folded by the gate as it ships and kept here, which is
	// the whole of what the experiment is asking about.
	const threeLanes = "HANDBOOK.md is one file. Deliver three lanes that share no lines. L1: rewrite every heading. L2: link every cross-reference. L3: insert a contents section."
	t.Setenv("AFORGE_SPLITGATE", "")
	if Judge(threeLanes, nil).Keep {
		t.Error("the shipped gate kept the three-lane brief; #418 reports that it folds it")
	}
	t.Setenv("AFORGE_SPLITGATE", "lanes")
	if !Judge(threeLanes, nil).Keep {
		t.Error("lanes folded a brief that names its three lanes; a division somebody wrote out is not a pile to be counted")
	}
	// Four modules is the bench corpus's own measurement and it may not move.
	if Judge("Create four separate, independent Python utility modules, one file each", nil).Keep {
		t.Error("lanes kept four modules; four modules is the measurement that four does not pay")
	}
	// The bypass is the lanes mode's alone. It is not a second counter and it
	// is not something the judgment arm inherits, or the experiment could not
	// say which repair moved a result.
	t.Setenv("AFORGE_SPLITGATE", "judgment")
	if Judge(threeLanes, nil).Keep {
		t.Error("judgment took a written-out division at its word; its one repair is the plan's sizing")
	}
}

// JUDGMENT ASKS THE PLAN AND NOT THE TEXT. The three rows are the whole
// design: sizing says yes and the count is overruled; sizing says nothing
// useful and the count decides; and an oversized leaf is never the yes.
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
	// what the shipped gate would have done. A chain over a brief that DOES
	// enumerate its pile is kept, by the count, exactly as it is today.
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
	// THE COUNT IT FALLS BACK TO IS THE SHIPPED ONE. Judgment's repair is the
	// sizing; giving it the wider counting as well would leave the experiment
	// unable to say which of the two moved a result.
	spelled := "HANDBOOK.md is one file of thirty chapters"
	if Judge(spelled, nil).Keep {
		t.Error("judgment read the wider lane counting; its one repair is the plan's sizing")
	}
}

// THE CORPUS DECISIONS HOLD IN EVERY MODE THAT COUNTS. The eight bench tasks
// are the measurement this gate was built on — twelve image files and eight
// endpoints divided, four modules and three bugs did not — and a widened
// counting that quietly re-decided one of them would have thrown the
// measurement away to fix a phrasing.
//
// ModeJudgment is not in the table because it reads the same count on the same
// text; what it adds needs a plan, and the corpus is eight text files.
func TestEveryCountingModeStillDecidesTheCorpusTheWayItWasMeasured(t *testing.T) {
	// The DECISION column is the measurement and may not drift. The lanes
	// column is the wider counter's own reading, recorded so that a change to
	// it is visible here rather than discovered as a re-decided task: the two
	// closest to the floor are the five markdown notes and the five separate
	// files of the API refactor, both of which must stay under six.
	decided := map[string]struct {
		items  int
		lanes  int
		divide bool
	}{
		"api refactor.txt":       {8, 8, true},
		"bugfix repo.txt":        {3, 3, false},
		"codegen modules.txt":    {1, 4, false},
		"doc coverage.txt":       {0, 0, false},
		"image captions.txt":     {12, 12, true},
		"prose report.txt":       {0, 4, false},
		"research synthesis.txt": {4, 5, false},
		"review diff.txt":        {0, 0, false},
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
		if got := Lanes(string(text)); got != want.lanes {
			t.Errorf("%s: the wider counting reads %d lanes, want %d", name, got, want.lanes)
		}
		for _, mode := range []GateMode{ModeCount, ModeLanes, ModeJudgment} {
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
