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

// divideScopedArgs is one well-formed call whose parts carry the titles,
// summaries and scopes named — the shape a worker writes when it says what each
// part owns and nothing about the work it came out of.
func divideScopedArgs(evidence string, parts ...dividePart) json.RawMessage {
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

	nest.divide(t, divideScopedArgs(wideEvidence,
		dividePart{Title: "the alpha adapter", Summary: "move alpha onto the new interface", Brief: "alpha.go only"},
		dividePart{Title: "the beta adapter", Summary: "move beta onto the new interface", Brief: "beta.go only"}))

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
		own := []string{"alpha.go only", "beta.go only"}[index]
		if !strings.Contains(opening, divisionThisPart+"\n"+own) {
			t.Fatalf("part %d does not own %q under the heading that says so: %q", kid.id, own, opening)
		}
		sibling := []string{"move beta onto the new interface", "move alpha onto the new interface"}[index]
		if !strings.Contains(opening, divisionOtherParts) || !strings.Contains(opening, sibling) {
			t.Fatalf("part %d is not told that %q is in somebody else's hands: %q", kid.id, sibling, opening)
		}
		// AND IT IS NOT TOLD THAT ITS OWN JOB IS SOMEBODY ELSE'S. A map that
		// named every part would leave each worker with nothing it may touch.
		mine := []string{"move alpha onto the new interface", "move beta onto the new interface"}[index]
		if _, others, _ := strings.Cut(opening, divisionOtherParts); strings.Contains(others, mine) {
			t.Fatalf("part %d is told its own scope belongs to somebody else: %q", kid.id, opening)
		}
		// AND THE PERSON'S ASK IS PRINTED ONCE. It rides on the spec and
		// [composeBrief] prints it under the heading that says whose words those
		// are; the family's context must not print it a second time.
		if said := strings.Count(opening, personSentence); said != 1 {
			t.Fatalf("part %d reads the person's own sentence %d times, want once: %q", kid.id, said, opening)
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
	// legend's words as the summary, and the name the sketch road mints from it.
	written := newDivideNestFrom(t, spec, 0, &scriptedCompleter{}, nil)
	written.divide(t, divideScopedArgs(wideEvidence,
		dividePart{Title: "the flaking auth", Summary: "the flaking auth test", Brief: "the fixture races"},
		dividePart{Title: "the release notes", Summary: "the release notes", Brief: "RELEASE-2.4.md"}))

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
