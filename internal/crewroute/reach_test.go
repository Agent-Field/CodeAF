package crewroute

import (
	"os"
	"testing"
)

// THE KORNIA CASE: an issue labelled `bug :bug:` — the tracker's own spelling —
// whose title names the misbehaving call and says what it never does was read
// as open-ended, because no rule knew the label's spelling and no title rule
// fired. Either alone now reads it as a bugfix.
func TestAKorniaStyleDefectIsABugfix(t *testing.T) {
	title := "augmentation: RandomResizedCrop(scale=(1, 1)) never keeps a square or portrait image: strict candidate test and an inverted-ratio fallback"
	report, err := os.ReadFile("testdata/kornia-4814.md")
	if err != nil {
		t.Fatal(err)
	}
	labels := []string{"bug :bug:", "help wanted", "good first issue"}
	for name, task := range map[string]Task{
		"the title alone":           {Text: title},
		"the labels on a bare text": {Text: "the crop looks wrong", Labels: labels},
		"the issue as filed":        {Text: string(report), Labels: labels},
	} {
		if got := Classify(task); got.Class != Bugfix || !got.Sure {
			t.Errorf("%s: %s (sure %v, %q), want a sure bugfix", name, got.Class, got.Sure, got.Why)
		}
	}
	for _, text := range []string{"The export does not close the file", "parse_date fails to read ISO weeks", "Settings should reject a negative timeout"} {
		if got := Classify(Task{Text: text}); got.Class != Bugfix {
			t.Errorf("%q: %s (%q), want bugfix", text, got.Class, got.Why)
		}
	}
	if got := Classify(Task{Text: "The exporter should support YAML"}); got.Class == Bugfix {
		t.Errorf("a request for something new read as a bugfix (%q)", got.Why)
	}
}

// A FIX WITH REACH IS COMPLEX, A ONE-LINE FIX IS SIMPLE, and only the worker
// moves: one rung, at the same λ, never over a pin.
func TestAComplexFixGetsAStrongerWorker(t *testing.T) {
	report, err := os.ReadFile("testdata/hermes-7680.md")
	if err != nil {
		t.Fatal(err)
	}
	complexTask := Task{Text: string(report)}
	simpleTask := Task{Text: "fix: typo in the loop bound of paginate() skips the last page"}
	if got := Classify(complexTask); got.Class != Bugfix || got.Complex == "" {
		t.Fatalf("the hermes report: %s, reach %q, want a complex bugfix", got.Class, got.Complex)
	}
	if got := Classify(simpleTask); got.Class != Bugfix || got.Complex != "" {
		t.Fatalf("a one-line typo: %s, reach %q, want a simple bugfix", got.Class, got.Complex)
	}
	cands := append(catalogCandidates(), candidateOf(glm53))
	simple, err := Decide(Request{Task: simpleTask, Candidates: cands})
	if err != nil {
		t.Fatal(err)
	}
	hard, err := Decide(Request{Task: complexTask, Candidates: cands})
	if err != nil {
		t.Fatal(err)
	}
	if simple.Subclass != "simple" || hard.Subclass != "complex" || hard.Class != Bugfix {
		t.Errorf("subclasses %q and %q (class %s)", simple.Subclass, hard.Subclass, hard.Class)
	}
	if hard.Lambda != simple.Lambda {
		t.Errorf("λ moved: %v against %v", hard.Lambda, simple.Lambda)
	}
	sw, hw := simple.Seat(Worker), hard.Seat(Worker)
	if hw.Quality <= sw.Quality || Lineage(hw.Model) == Lineage(sw.Model) {
		t.Errorf("complex worker %s (%.2f), simple worker %s (%.2f): want a stronger one", hw.Model, hw.Quality, sw.Model, sw.Quality)
	}
	for _, seat := range []Seat{Planner, Checker} {
		if hard.Seat(seat).Model != simple.Seat(seat).Model {
			t.Errorf("%s moved: %s against %s", seat, hard.Seat(seat).Model, simple.Seat(seat).Model)
		}
	}
	pinned, err := Decide(Request{Task: complexTask, Candidates: cands, Pins: map[Seat]Pin{Worker: {Model: sw.Model}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := pinned.Seat(Worker).Model; Lineage(got) != Lineage(sw.Model) {
		t.Errorf("a pinned worker became %s", got)
	}
}

// A CLASS GIVEN IS NOT A REACH FORGOTTEN: a caller that classified first and
// hands on only the class — or the whole reading — still gets the complex
// fix's worker, read off the same words.
func TestAGivenBugfixClassStillReadsReach(t *testing.T) {
	report, err := os.ReadFile("testdata/hermes-7680.md")
	if err != nil {
		t.Fatal(err)
	}
	task := Task{Text: string(report)}
	cands := catalogCandidates()
	plain, err := Decide(Request{Class: Bugfix, Task: Task{Text: "fix: typo in the loop bound of paginate() skips the last page"}, Candidates: cands})
	if err != nil {
		t.Fatal(err)
	}
	reading := Classify(task)
	for name, r := range map[string]Request{
		"the class alone":   {Class: Bugfix, Task: task, Candidates: cands},
		"the whole reading": {Class: Bugfix, Reading: &reading, Candidates: cands},
	} {
		d, err := Decide(r)
		if err != nil {
			t.Fatal(err)
		}
		if d.Subclass != "complex" || d.Reach == "" {
			t.Errorf("%s: subclass %q reach %q, want complex", name, d.Subclass, d.Reach)
		}
		if d.Seat(Worker).Quality <= plain.Seat(Worker).Quality {
			t.Errorf("%s: worker %s, no stronger than the simple fix's %s", name, d.Seat(Worker).Model, plain.Seat(Worker).Model)
		}
	}
}
