package session

import (
	"strings"
	"testing"
)

func journaledDivisions(t *testing.T, path string) []journalDivision {
	t.Helper()
	var divisions []journalDivision
	for _, entry := range journaledEntries(t, path, "division") {
		if entry.Division != nil {
			divisions = append(divisions, *entry.Division)
		}
	}
	return divisions
}

func TestAWorkersOwnDivisionIsWrittenDownAsTheWorkers(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)
	nest.divide(t, divideArgs(wideEvidence, 2))

	divisions := journaledDivisions(t, nest.journal)
	if len(divisions) != 1 {
		t.Fatalf("the journal holds %d division lines, want one", len(divisions))
	}
	if got := divisions[0]; got.Source != divisionByWorker || got.Decision != divisionAdmitted || got.Admitted != 2 {
		t.Fatalf("the line reads %+v, want the worker's own admitted division", got)
	}
}

func TestAWorkerThatAsksIsToldToStopAndSaySoRatherThanCarryOn(t *testing.T) {
	reviewer := &divideReviewer{answer: `{"refuse": true, "nobody": true, "why": "only a person can approve these pull requests"}`}
	nest := newDivideNestFrom(t, judgedWide, 0, reviewer, nil)

	answer := nest.divide(t, divideArgs(issueEvidence, 2))

	if !strings.HasPrefix(answer, "not split:") {
		t.Fatalf("the worker was told %q, want the same refusal shape the gates use", answer)
	}
	if !strings.Contains(answer, "it needs a person") {
		t.Fatalf("the worker is told %q and never that this needs somebody", answer)
	}
	if !strings.Contains(answer, "only a person can approve these pull requests") {
		t.Fatalf("the worker is told %q and never what the reader found", answer)
	}
	if strings.Contains(answer, "Carry on with the work in your own hands") {
		t.Fatalf("the worker is told to carry on with work nobody can do: %q", answer)
	}
	if !strings.Contains(answer, "say so in your report") {
		t.Fatalf("the worker is told %q and never to hand it back", answer)
	}
	// THE VOCABULARY LAW: a person reads this over the worker's shoulder.
	assertPlainWords(t, "what the worker is told about work only a person can do", answer)
}

func TestACautiousRefusalStillLeavesOneWorkerToDoTheWork(t *testing.T) {
	for _, test := range []struct {
		name string
		why  string
	}{
		{"it reads them as one job", "these are stages of one job"},
		{"it will not have the boundary", "parts 2 and 3 are the same file"},
		// THE WORDS OF THE OTHER ANSWER, WITHOUT THE ANSWER. A reviewer that says
		// this and does not reach for the field has refused a division, and the
		// work is still work: a road that read the sentence would stop it here.
		{"it says the shape of the other answer without giving it",
			"nobody could split this sensibly and a person should really look at it"},
	} {
		t.Run(test.name, func(t *testing.T) {
			reviewer := &divideReviewer{answer: `{"refuse": true, "why": "` + test.why + `"}`}
			nest := newDivideNestFrom(t, judgedWide, 0, reviewer, nil)
			said := nest.divide(t, divideArgs(issueEvidence, 2))
			if !strings.Contains(said, "Carry on with the work in your own hands") {
				t.Fatalf("ordinary refusal stopped doable work: %q", said)
			}
			if kids := nest.graph.children(nest.parent.id); len(kids) != 0 {
				t.Fatalf("a refused division still bore %d parts", len(kids))
			}
			divisions := journaledDivisions(t, nest.journal)
			if len(divisions) != 1 || divisions[0].Decision != divisionRefusedReview {
				t.Fatalf("the journal reads %+v, want an ordinary refused division", divisions)
			}
		})
	}
}
