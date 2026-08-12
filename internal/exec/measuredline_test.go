package exec

import (
	"strings"
	"testing"
)

// W6: the measured line reached every selection prompt as decoration the model
// was never told what to do with. It is now an instruction, and it appears only
// beside figures that exist — a rule about numbers nobody printed is prompt the
// caller pays for and the model cannot use, and its absence is what keeps the
// pre-evidence menu exactly what it always was.
func TestTheMeasuredLineCarriesItsReadingInstructionOnlyWhenThereAreFigures(t *testing.T) {
	defer ForgetSubharnesses()
	RegisterSubharness(SubharnessInfo{Name: "swe", Purpose: "software engineering taken whole", PriorAnchors: "swe ruler"})

	const instruction = "The figures beside each worker are what work of this kind has really cost here."

	UseSubharnessKnowledge(nil)
	if menu := MenuText(); strings.Contains(menu, "The figures beside each worker") {
		t.Fatalf("the menu instructs a reading of figures it never printed:\n%s", menu)
	}

	// Below the evidence gate the hook returns nothing for this worker, and the
	// instruction must stay away for exactly the same reason.
	UseSubharnessKnowledge(func(string) string { return "" })
	if menu := MenuText(); strings.Contains(menu, "The figures beside each worker") {
		t.Fatalf("the menu instructs a reading of a line the gate withheld:\n%s", menu)
	}

	UseSubharnessKnowledge(func(name string) string {
		if name == "swe" {
			return "median 12 turns"
		}
		return ""
	})
	menu := MenuText()
	if !strings.Contains(menu, "measured here so far: median 12 turns") {
		t.Fatalf("the measured line itself is missing:\n%s", menu)
	}
	if !strings.Contains(menu, instruction) {
		t.Fatalf("the measured line is still decoration; no reading was instructed:\n%s", menu)
	}
	// Evidence about this machine, never a target to optimise, and never a
	// reason to take a worker whose purpose does not fit.
	for _, want := range []string{
		"not as a target",
		"prefer the worker whose purpose fits",
	} {
		if !strings.Contains(menu, want) {
			t.Errorf("the reading instruction is missing %q:\n%s", want, menu)
		}
	}
	// It sits above the choosing rule, which stays the last word.
	if strings.Index(menu, instruction) > strings.Index(menu, "Choose a specialist subharness only") {
		t.Error("the reading instruction displaced the choosing rule from the end of the menu")
	}
	// The retry menu is built from the same registrations, so a second-attempt
	// judgement reads the evidence the same way the first one did.
	if retry := MenuTextExcept(LinearSubharness); strings.Contains(retry, "measured here so far") &&
		!strings.Contains(retry, instruction) {
		t.Errorf("the retry menu prints the figures without the reading:\n%s", retry)
	}
}
