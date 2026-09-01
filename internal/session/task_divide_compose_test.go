package session

// WHAT A PART IS TOLD, AS TESTS. The composer is the harness's half of a part's
// world (task_divide_compose.go), and what these drive is the real door: a worker
// calling `divide_work` and a harness putting a drawing to the same body, both
// read back off the node the graph actually admitted.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// familyBlock is the harness's half of one part's brief: everything above the
// heading its scope stands under. Two parts of two divisions of the same work
// must have the same one, byte for byte.
func familyBlock(brief string) string {
	block, _, _ := strings.Cut(brief, divisionThisPart)
	return block
}

// siblingSentence is the boundary alone — what this part is told somebody ELSE is
// holding, with neither the ground above it nor its own scope below it.
func siblingSentence(brief string) string {
	_, said, _ := strings.Cut(brief, divisionOtherParts)
	said, _, _ = strings.Cut(said, divisionThisPart)
	return said
}

// divideWrittenArgs is one well-formed call whose parts carry the titles,
// summaries and scopes named — the shape a worker writes when it says what each
// part owns and nothing about the work it came out of. It is [divideScopedArgs]
// with the two fields the composer reads besides the scope, which that one
// spells for every part alike.
func divideWrittenArgs(evidence string, parts ...dividePart) json.RawMessage {
	written := make([]string, 0, len(parts))
	for _, part := range parts {
		written = append(written, fmt.Sprintf(`{"title":%q,"summary":%q,"brief":%q,"acceptance":"a"}`,
			part.Title, part.Summary, part.Brief))
	}
	return json.RawMessage(fmt.Sprintf(`{"evidence":%q,"parts":[%s]}`,
		evidence, strings.Join(written, ",")))
}

// A PART A WORKER WROTE OPENS ON THE WORK IT CAME OUT OF, whether or not the
// worker restated it.
//
// This is the failure the composer exists to close: the two parts below are as
// terse as a cheap model makes them — a line of scope apiece and not one word
// about the family — and each of their workers still opens on the parent's brief
// and on what its sibling is holding. Nothing here scripts a reviewer, because a
// reviewer that fails open is exactly the case where this had to hold.
func TestAWorkerWrittenPartOpensOnTheParentsBriefAndItsSiblingsScope(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)

	nest.divide(t, divideWrittenArgs(wideEvidence,
		dividePart{Title: "the alpha adapter", Summary: "s", Brief: "alpha.go only, and the Port interface it names"},
		dividePart{Title: "the beta adapter", Summary: "s", Brief: "beta.go only"}))

	kids := nest.graph.children(nest.parent.id)
	if len(kids) != 2 {
		t.Fatalf("the division bore %d parts, want 2", len(kids))
	}
	for index, kid := range kids {
		opening := kid.instruction()
		if !strings.Contains(opening, wideBrief) {
			t.Fatalf("part %d never reads the work it came out of: %q", kid.id, opening)
		}
		// AND WHAT IT OWNS IS STILL ITS AUTHOR'S OWN WORDS, under a heading of
		// its own — the composer writes around the scope and never over it.
		own := []string{"alpha.go only, and the Port interface it names", "beta.go only"}[index]
		if !strings.Contains(opening, divisionThisPart+"\n"+own) {
			t.Fatalf("part %d does not own %q under the heading that says so: %q", kid.id, own, opening)
		}
		// AND THE BOUNDARY NAMES THE SIBLING'S MATERIAL, not merely its title:
		// a part told the words but not the files finds the boundary by writing
		// over it.
		sibling := []string{"the beta adapter: beta.go only", "the alpha adapter: alpha.go only"}[index]
		said := siblingSentence(opening)
		if said == "" || !strings.Contains(said, sibling) {
			t.Fatalf("part %d is not told that %q is in somebody else's hands: %q", kid.id, sibling, opening)
		}
		// AND IT IS NOT TOLD THAT ITS OWN JOB IS SOMEBODY ELSE'S. A map that
		// named every part would leave each worker with nothing it may touch.
		if strings.Contains(said, own) {
			t.Fatalf("part %d is told its own scope belongs to somebody else: %q", kid.id, said)
		}
		// AND THE PERSON'S ASK IS PRINTED ONCE. It rides on the spec and
		// [composeBrief] prints it under the heading that says whose words those
		// are; the family's context must not print it a second time.
		if said := strings.Count(opening, personSentence); said != 1 {
			t.Fatalf("part %d reads the person's own sentence %d times, want once: %q", kid.id, said, opening)
		}
	}
}

// AND WHAT THE WORK BEFORE THE PARENT LEARNED REACHES EVERY PART OF IT.
//
// A part is composed on what its parent INHERITED — the brief it was admitted
// with AND the reports of whatever ran ahead of it — because that is where a
// family's findings are. A part composed on the admitted brief alone would be
// sent to find out again what a prerequisite has already written down, which is
// the whole class of bug this composer closes, one level up.
func TestAPartInheritsWhatTheWorkBeforeItsParentLearned(t *testing.T) {
	const finding = "the reconciler writes through internal/ledger/absorb.go, never the store"
	nest := newDivideNest(t, wideBrief, 0)

	// The work that ran before this one, with its report in hand — which is the
	// shape the frontier assembles a brief from ([TaskGraph.briefLocked]).
	before := nest.graph.reserve()
	nest.graph.admit(before, taskSpec{title: "the survey", brief: "read the ledger", acceptance: "a", depth: 1})
	nest.graph.mu.Lock()
	nest.graph.nodes[before].report = finding
	nest.graph.nodes[before].state = TaskDone
	nest.parent.dependsOn = []uint64{before}
	nest.graph.mu.Unlock()

	nest.divide(t, divideWrittenArgs(wideEvidence,
		dividePart{Title: "the alpha adapter", Summary: "s", Brief: "alpha.go only"},
		dividePart{Title: "the beta adapter", Summary: "s", Brief: "beta.go only"}))

	kids := nest.graph.children(nest.parent.id)
	if len(kids) != 2 {
		t.Fatalf("the division bore %d parts, want 2", len(kids))
	}
	for _, kid := range kids {
		if !strings.Contains(kid.instruction(), finding) {
			t.Fatalf("part %d was sent to find out again what the work before its parent already wrote down: %q",
				kid.id, kid.instruction())
		}
	}
}

// AND THE TWO ROADS COMPOSE THE SAME FAMILY, BYTE FOR BYTE. A part drawn out of a
// sketch and a part a worker wrote are the same kind of thing, so the half of
// their world the harness writes cannot depend on which road put them in the
// graph — that difference is the whole bug (#233), and the way it stays fixed is
// that there is one composer with two callers.
func TestASketchPartAndAWorkerWrittenPartGetTheSameFamilyContext(t *testing.T) {
	spec := drawnSpec(countedSketch("A | B",
		"A is the flaking auth test, B is the release notes"))

	drawn := newDivideNestFrom(t, spec, 0, &scriptedCompleter{}, nil)
	drawn.node.divideFromSketch(context.Background())

	// The same two parts as the drawing bore, said by a worker instead: the
	// legend's words as the scope, and the name the sketch road mints from it.
	written := newDivideNestFrom(t, spec, 0, &scriptedCompleter{}, nil)
	written.divide(t, divideWrittenArgs(wideEvidence,
		dividePart{Title: "the flaking auth", Summary: "the flaking auth test", Brief: "A: the flaking auth test"},
		dividePart{Title: "the release notes", Summary: "the release notes", Brief: "B: the release notes"}))

	sketched, authored := drawn.graph.children(drawn.parent.id), written.graph.children(written.parent.id)
	if len(sketched) != 2 || len(authored) != 2 {
		t.Fatalf("the two roads bore %d and %d parts, want 2 each", len(sketched), len(authored))
	}
	for index := range sketched {
		one, other := familyBlock(sketched[index].assembledBrief()), familyBlock(authored[index].assembledBrief())
		if one == "" {
			t.Fatalf("part %d was composed with no family context at all", index+1)
		}
		if one != other {
			t.Fatalf("part %d reads a different world on the two roads:\n  drawn:  %q\n  worker: %q", index+1, one, other)
		}
	}
}

// AND NOTHING A MODEL WROTE CAN BLOW THE BOUND OR THE ARITHMETIC. Every field the
// composer reads — the title, the summary, the scope — arrived from a model, so
// each of them can arrive at any length, and the fitting has to hold for all of
// them at once.
func TestAPartsBriefHoldsItsBoundWhateverLengthTheFieldsArriveAt(t *testing.T) {
	huge := strings.Repeat("pathological ", 2000)
	for _, test := range []struct {
		name  string
		parts []dividePart
		// ground says whether the work being divided still has room to be
		// carried, which is the section that gives way first and only first.
		ground bool
	}{
		{"a title no column could hold", []dividePart{
			{Title: huge, Summary: "s", Brief: "alpha.go only"},
			{Title: "the beta adapter", Summary: "s", Brief: "beta.go only"},
		}, true},
		{"scopes that fill the whole bound", []dividePart{
			{Title: "the alpha adapter", Summary: "s", Brief: huge},
			{Title: "the beta adapter", Summary: "s", Brief: huge},
		}, false},
		{"every field at once", []dividePart{
			{Title: huge, Summary: huge, Brief: huge},
			{Title: huge, Summary: huge, Brief: huge},
		}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			nest := newDivideNest(t, wideBrief, 0)
			nest.divide(t, divideWrittenArgs(wideEvidence, test.parts...))

			kids := nest.graph.children(nest.parent.id)
			if len(kids) != 2 {
				t.Fatalf("the division bore %d parts, want 2", len(kids))
			}
			for _, kid := range kids {
				brief := kid.assembledBrief()
				if len(brief) > taskShapeBriefLimit {
					t.Fatalf("part %d was handed %d bytes, over the %d every brief on this road is held to",
						kid.id, len(brief), taskShapeBriefLimit)
				}
				// THE BOUNDARY IS NEVER THE SECTION THAT GIVES WAY, whatever
				// else had to.
				if !strings.Contains(brief, divisionOtherParts) {
					t.Fatalf("part %d lost the sentence saying what its sibling owns: %q", kid.id, brief)
				}
				if got := strings.Contains(brief, wideBrief); got != test.ground {
					t.Fatalf("part %d carries the work being divided (%t), want %t: %q",
						kid.id, got, test.ground, brief)
				}
			}
		})
	}
}
