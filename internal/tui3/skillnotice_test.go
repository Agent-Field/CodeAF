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

// THE SKILLS ROW OUTLIVES THE RUN THAT PRODUCED IT. A plain note lives inside
// the work chip's fold, so the one line saying which skills a turn used would be
// visible while the turn ran and gone the moment it settled — and expanding the
// chip could not bring it back, because the open fold draws its own caption and
// step rows and skips the entries underneath. A person asking "did it use my
// skill?" asks after the answer arrives, not during.
func TestTheSkillsRowIsNotSwallowedByTheWorkChip(t *testing.T) {
	f := &feed{live: -1, think: -1}
	f.ingest(session.Event{Kind: session.EventNotice, Text: "skills carried: release-notes", Skills: []string{"release-notes"}})
	if len(f.entries) == 0 {
		t.Fatal("the skills notice wrote no entry")
	}
	last := f.entries[len(f.entries)-1]
	if last.kind != entryNote {
		t.Fatalf("the skills notice landed as %v, want a note", last.kind)
	}
	if !last.told {
		t.Fatal("the skills row is a plain note, so the work chip swallows it when the turn settles")
	}
	// AND AN ORDINARY NOTICE IS UNCHANGED, because the adapter's own retry line
	// belongs inside the fold with the rest of the machinery.
	f.ingest(session.Event{Kind: session.EventNotice, Text: "Retry 1/3: removed max_tokens"})
	plain := f.entries[len(f.entries)-1]
	if plain.told {
		t.Fatal("an ordinary notice became a told note, so the fold now keeps machinery on screen")
	}
}
