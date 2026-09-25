package tui3

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

// THE LINE NAMING WHAT A TURN CARRIED OUTLIVES THE TURN. It sits under the
// question it belongs to and the work chip starts below it; before, the chip
// swallowed it the moment the answer landed, and an opened chip lists calls,
// not notes, so the one screen record that a skill reached the turn was gone
// for good. It still does not hold the turn open the way a sentence addressed
// to the person does: the calls fold as they always did.
func TestTheCarriedSkillsLineStaysAboveTheWorkChip(t *testing.T) {
	f := &feed{live: -1, think: -1}
	f.ingest(session.Event{Kind: session.EventNotice, Text: "skills carried: tide-almanac", Skills: []string{"tide-almanac"}})
	if len(f.entries) != 1 || !f.entries[0].carried {
		t.Fatalf("the skills note is not marked as the carried record: %+v", f.entries)
	}
	f.ingest(session.Event{Kind: session.EventNotice, Text: "request adjusted and asked again"})
	if f.entries[len(f.entries)-1].carried {
		t.Fatalf("an ordinary notice was marked as carried skills: %+v", f.entries[len(f.entries)-1])
	}

	base := time.Unix(100, 0)
	entries := []entry{
		{kind: entryUser, text: "what does the almanac say about noon", turn: 1, began: base},
		{kind: entryNote, text: "skills · tide-almanac", turn: 1, carried: true},
		{kind: entryThinking, text: "checking", turn: 1, began: base, ended: base.Add(2 * time.Second), settled: true},
		{kind: entryTool, tool: "read", turn: 1, status: toolOK, began: base.Add(2 * time.Second), ended: base.Add(3 * time.Second)},
		{kind: entryAssistant, text: "high water at noon", turn: 1, settled: true},
	}
	folds := deriveWorkfolds(entries, 0)
	if len(folds) != 1 {
		t.Fatalf("the carried line stopped the chip forming: %#v", folds)
	}
	for start := range folds {
		if start != 2 {
			t.Fatalf("the chip starts at entry %d, want 2, below the carried line", start)
		}
	}
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries, a.workMode = entries, config.WorkFold
	a.touch()
	if got := strings.Join(plainRows(a), "\n"); !strings.Contains(got, "skills · tide-almanac") || !strings.Contains(got, "worked") {
		t.Fatalf("want the carried line above a folded chip:\n%s", got)
	}

	// AND THE SAME NOTE WITHOUT THE MARK IS STILL SWALLOWED, so the test fails
	// on a build that lost the mark rather than passing on one that stopped
	// folding.
	plain := append([]entry(nil), entries...)
	plain[1].carried = false
	a.entries = plain
	a.touch()
	if got := strings.Join(plainRows(a), "\n"); strings.Contains(got, "skills · tide-almanac") {
		t.Fatalf("an unmarked note was not folded, so the control proves nothing:\n%s", got)
	}
}

func TestSkillNoticeAbsentAndEmptyAreTheSameUnknown(t *testing.T) {
	const ordinary = "request adjusted and asked again"
	fixtures := []session.Event{
		{Kind: session.EventNotice, Text: ordinary},
		{Kind: session.EventNotice, Text: ordinary, Skills: []string{}},
	}
	var got [][]entry
	for _, ev := range fixtures {
		f := &feed{live: -1, think: -1}
		f.ingest(ev)
		got = append(got, f.entries)
	}
	if !reflect.DeepEqual(got[0], got[1]) {
		t.Fatalf("absent and empty skills drew differently:\nabsent: %+v\nempty:  %+v", got[0], got[1])
	}
	if len(got[0]) != 1 || got[0][0].kind != entryNote || got[0][0].text != ordinary {
		t.Fatalf("ordinary notice did not retain its dim note: %+v", got[0])
	}
	if strings.Contains(strings.ToLower(got[0][0].text), "skill") {
		t.Fatalf("unknown skills produced a skills statement: %q", got[0][0].text)
	}
}

func TestSkillNoticeDrawsNamesFromFieldAsADimNote(t *testing.T) {
	f := &feed{live: -1, think: -1}
	f.ingest(session.Event{
		Kind:   session.EventNotice,
		Text:   "skills carried: wrong, words",
		Skills: []string{"comma, safe", "field two"},
	})

	if len(f.entries) != 1 {
		t.Fatalf("nonempty skills drew %d entries, want one: %+v", len(f.entries), f.entries)
	}
	got := f.entries[0]
	if got.kind != entryNote || got.text != "skills · comma, safe, field two" {
		t.Fatalf("skills did not draw from the field in note voice: %+v", got)
	}
	if got.told || got.block || len(got.facts) != 0 {
		t.Fatalf("skills note became actionable or attention-bearing: %+v", got)
	}
	if strings.Contains(got.text, "wrong") || strings.Contains(got.text, "words") {
		t.Fatalf("skills were derived from Event.Text: %q", got.text)
	}
}
