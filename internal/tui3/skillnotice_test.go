package tui3

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

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
