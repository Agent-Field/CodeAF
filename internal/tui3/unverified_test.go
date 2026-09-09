package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE THIRD SETTLED STATE, ON THE SURFACE.
//
// session lands a node here when the run is finished, nothing came back that
// could call the work right or call it wrong, the branch is kept, and the
// dependents wait on a person (internal/session's task_contract.go). Every
// assertion here is about the one thing that makes the state worth having — that
// a person can tell it apart from both of the other two at a glance.
//
// AND EVERY WORD OF IT IS A PERSON'S. The engine's own report arrives already
// worded for the reader, and the surface's three words for the state say what is
// true of it from the outside: it finished, and it is on you to look. What made
// it land here is not on screen anywhere, which is the law and not an omission.

// unverifiedNotice is the update session sends for one, report and all.
func unverifiedNotice(report string) session.TaskNotice {
	return session.TaskNotice{
		Elapsed: 400 * time.Second,
		Report:  report,
		Merge:   mergeWordAborted,
		Branch:  "task/parser",
		Changed: []string{"internal/parse/keys.go", "internal/parse/keys_test.go"},
	}
}

func TestAnUnverifiedLandingIsNeitherDoneNorFailed(t *testing.T) {
	a, _, _ := taskApp(t)
	said := "finished, but needs your look — nothing came back either way"
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskUnverified,
		unverifiedNotice(said+"\nthe key table is the part to read first"))})

	text := taskText(a)
	for _, want := range []string{
		// THE HEAD: the question mark, the node's own identity cell, the state's
		// own word — and the branch named in the half of the stopped sentence
		// that is true of work which ran to the end.
		glyphUnverified + " " + plain(a.taskMark(identFor(7))) + " Port the parser",
		// THE SPAN IS A FACT OF ITS OWN, joined with the row's own separator since
		// 2026-09-03: `needs your look 6m40s` fused the state word — the reason
		// this card is asking for a hand at all — into a duration (taskdone.go's
		// [app.doneTail]).
		"· " + taskUnverifiedWord + " · " + taskSpanWord(400*time.Second),
		"2 files",
		"· " + taskBranchKept + " · task/parser",
		// THE OUTCOME LINE IS THE ENGINE'S OWN SENTENCE, quoted, exactly as a
		// failure's is: it is what a person reads to decide.
		`"` + said + `"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the unverified card is missing %q:\n%s", want, text)
		}
	}
	for _, never := range []string{
		glyphDone, doneWord + " ", doneFailWord, glyphBad, taskStoppedKept, mergeWordAborted,
		"the key table is the part to read first", // the rest of the report is behind ctrl+o
	} {
		if strings.Contains(text, never) {
			t.Fatalf("the unverified card claims %q:\n%s", never, text)
		}
	}

	// THE CARD IS NOT A FAILURE, in the field the rest of the surface reads.
	card := a.doneCardAt(len(a.entries) - 1)
	if card == nil || card.failed || !card.unverified {
		t.Fatalf("the card landed as %+v, want unverified and not failed", card)
	}

	// THE RAIL SAYS THE SAME THING IN ITS OWN COLUMN: the question glyph, and a
	// row that says what it is waiting for rather than that it stopped.
	if group := a.railGroupOf(a.tasks[7]); group != railAttention {
		t.Fatalf("an unverified node filed under %q, want %q",
			railGroupWords[group], railGroupWords[railAttention])
	}
	rail := plain(strings.Join(a.railRows(12), "\n"))
	for _, want := range []string{
		// The column leads with the STATE and nothing else — the card's identity
		// cell is not spent here (task.go's [app.railLead]).
		glyphUnverified + " Port the parser",
		taskUnverifiedWaits,
	} {
		if !strings.Contains(rail, want) {
			t.Fatalf("the rail is missing %q:\n%s", want, rail)
		}
	}
	if strings.Contains(rail, taskStoppedKept) {
		t.Fatalf("the rail says an unverified node stopped:\n%s", rail)
	}
}

// A NODE THAT LANDED WITH NOTHING TO SAY still says which of the three states it
// is in — the gloss stands in for the missing sentence and never for the state.
func TestAnUnverifiedLandingWithNoReportSaysWhyItIsThere(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskUnverified,
		session.TaskNotice{Elapsed: 30 * time.Second})})

	if text := taskText(a); !strings.Contains(text, `"`+taskUnverifiedGloss+`"`) {
		t.Fatalf("a silent unverified landing does not fall back to %q:\n%s", taskUnverifiedGloss, text)
	}
}

// A BATCH WITH A QUESTION IN IT IS NOT A DONE BATCH. The rollup header must
// never report three successes when it is two and something nobody could judge.
func TestARollupWithAnUnverifiedNodeStopsSayingDone(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a,
		streamEventMsg{gen: a.gen, ev: update(1, "Collect sources", session.TaskDone, session.TaskNotice{
			Elapsed: time.Second, Merge: mergeWordMerged,
		})},
		streamEventMsg{gen: a.gen, ev: update(2, "Mix audio", session.TaskDone, session.TaskNotice{
			Elapsed: time.Second, Merge: mergeWordMerged,
		})},
		streamEventMsg{gen: a.gen, ev: update(3, "Port the parser", session.TaskUnverified,
			unverifiedNotice("finished, but needs your look — nobody could say either way"))},
	)
	text := taskText(a)
	if !strings.Contains(text, glyphUnverified+" 3"+doneRollupMix) {
		t.Fatalf("the rollup head does not carry the question:\n%s", text)
	}
	if strings.Contains(text, doneRollupWord) {
		t.Fatalf("the rollup called a batch with an unjudged node done:\n%s", text)
	}
	if strings.Contains(text, glyphBad) {
		t.Fatalf("the rollup called an unjudged node a failure:\n%s", text)
	}
}

// THE "@" LIST IS THE OTHER PLACE A STATE IS READ IN ONE CELL, and its default
// used to be the tick — which is the one answer an unverified row must not get
// (taskmention.go).
func TestTheMentionListMarksAnUnverifiedRow(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		entry := session.TaskIndexEntry{Status: string(session.TaskUnverified)}
		if got := taskStatusGlyph(entry, ascii); got != glyphUnverified {
			t.Fatalf("an unverified row is marked %q (ascii=%v), want %q", got, ascii, glyphUnverified)
		}
	}
	done := session.TaskIndexEntry{Status: string(session.TaskDone)}
	if got := taskStatusGlyph(done, false); got != glyphDone {
		t.Fatalf("a done row is marked %q, want %q", got, glyphDone)
	}
}
