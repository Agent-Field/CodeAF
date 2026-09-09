package tui3

// ── THE NARROWEST FRAME IS NOT THE ONE THAT SAYS LEAST ──────────────────────
//
// At sixty columns a room parked on a person's decision used to be the frame
// that told them least about how to answer it: the foot's hint slot went empty
// altogether, and the body's answers row lost `[d]` with nothing to say it had.
// Every other fold on this surface counts what it hid — `▸ +1`, `holds 3 more`,
// `▸ N earlier` — and a question a person is standing in front of is the last
// row that should be allowed to hide a key silently.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// settleRowText is one row of columns as it is drawn, plainly.
func settleRowText(parts []settleChoice, hid int) string {
	said := ""
	for i, part := range parts {
		if i > 0 {
			said += settleGap
		}
		said += part.key + part.word
	}
	if hid > 0 {
		said += settleHidLead + itoa(hid)
	}
	return said
}

// THE ANSWERS ROW COUNTS THE COLUMNS IT COULD NOT FIT. The two answers are the
// question and `tell it` and the hand-over move it elsewhere, so those go first
// — and the row then ends in the count rather than in nothing.
func TestANarrowAnswersRowCountsTheChoiceItDropped(t *testing.T) {
	a, _ := roomSettleApp(t, session.TaskUnverified)
	card := a.doneCardFor(7)
	choices := a.settleChoices(card)
	if len(choices) != 4 {
		t.Fatalf("the card offers %d columns, want the four the design draws", len(choices))
	}
	all, hid := settleParts(choices, 200)
	full := settleRowText(all, hid)
	if hid != 0 || !strings.Contains(full, settleHandKey) {
		t.Fatalf("a wide answers row is\n\t%q\nand it should carry every column, %q included", full, settleHandKey)
	}
	// Two cells narrower than the whole row: exactly the frame that has to drop
	// the last column and still has room to say so.
	room := ansi.StringWidth(full) - 2
	parts, dropped := settleParts(choices, room)
	narrow := settleRowText(parts, dropped)
	if strings.Contains(narrow, settleHandKey) {
		t.Fatalf("the answers row at %d cells is\n\t%q\nand should have dropped %q", room, narrow, settleHandKey)
	}
	if !strings.HasSuffix(narrow, settleHidLead+"1") {
		t.Fatalf("the answers row at %d cells is\n\t%q\nand it dropped a column without saying so", room, narrow)
	}
	if ansi.StringWidth(narrow) > room {
		t.Fatalf("the answers row is %d cells and has %d:\n\t%q", ansi.StringWidth(narrow), room, narrow)
	}
	// AND THE TWO ANSWERS THEMSELVES ARE THE LAST THING TRADED. A row narrow
	// enough to lose `tell it` keeps the question answerable.
	tight, _ := settleParts(choices, ansi.StringWidth(narrow)-8)
	for _, want := range []string{settleYesKey, settleNoKey} {
		if !strings.Contains(settleRowText(tight, 0), want) {
			t.Fatalf("the tightest answers row is\n\t%q\nand it lost the answer %q", settleRowText(tight, 0), want)
		}
	}
}

// THE COUNT IS A COUNT AND NOT ANOTHER CHIP. Nothing on the row a person can
// press resolves to an answer they cannot read.
func TestTheDroppedChoicesCountAnswersToNoPress(t *testing.T) {
	a, _ := roomSettleApp(t, session.TaskUnverified)
	card := a.doneCardFor(7)
	all, _ := settleParts(a.settleChoices(card), 200)
	full := ansi.StringWidth(settleRowText(all, 0))
	rows := a.settleRows(nil, card, 0, full-2+2, 0)
	if len(rows) < 2 {
		t.Fatalf("the card drew %d rows and should draw the reason and the answers", len(rows))
	}
	if got := len(card.chips); got != 3 {
		t.Fatalf("a narrow answers row records %d pressable answers and should record 3:\n\t%q",
			got, plain(rows[len(rows)-1].text))
	}
	if line := plain(rows[len(rows)-1].text); !strings.HasSuffix(line, settleHidLead+"1") {
		t.Fatalf("the drawn answers row is\n\t%q\nand should end in the count", line)
	}
}

// AND THE FOOT NAMES THE ANSWERS AT EVERY WIDTH THE ROOM IS DRAWN AT. The hint
// slot takes a line whole or not at all, so a sentence four cells too long left
// the narrowest terminal naming none of the keys that answer the question it was
// standing on. Every rung is a ranked prefix of the one above it.
func TestARoomAskingForYourLookNamesItsAnswersAtEveryWidth(t *testing.T) {
	a, _ := roomSettleApp(t, session.TaskUnverified)
	card := a.roomSettleCard()
	if card == nil {
		t.Fatal("the fixture's room is not asking, and this test is about the frame that is")
	}
	// The two slots that carry these answers: the room's own, and the roster's
	// hold hint, which adds the key that gives the column back.
	for _, tail := range []string{"", railSep + "esc"} {
		for _, width := range []int{160, 120, 80, 60} {
			say := a.settleHintAt(card, width, tail)
			if !strings.HasPrefix(say, "a accept") {
				t.Fatalf("at %d columns the hint reads %q and should lead with the first answer", width, say)
			}
			if ansi.StringWidth(roomLegendWord)+ansi.StringWidth(say)+legendFurniture > width {
				t.Fatalf("at %d columns the hint %q does not fit beside %q, so the legend will throw it away",
					width, say, roomLegendWord)
			}
			if tail != "" && !strings.HasSuffix(say, tail) {
				t.Fatalf("at %d columns the roster's hint is %q and it dropped the key that gives the column back", width, say)
			}
		}
	}
	wide := a.settleHintAt(card, 160, "")
	if got := a.settleHintAt(card, 60, ""); got == wide {
		t.Fatalf("the 60-column hint is the whole sentence, which does not fit: %q", got)
	}
	// AND THE LADDER IS A LADDER: each rung says less than the one above it, and
	// what it drops it counts.
	rungs := a.roomSettleHintFor(card)
	for i := 1; i < len(rungs); i++ {
		if ansi.StringWidth(rungs[i]) >= ansi.StringWidth(rungs[i-1]) {
			t.Fatalf("rung %d (%q) is no shorter than rung %d (%q)", i, rungs[i], i-1, rungs[i-1])
		}
		if !strings.HasPrefix(rungs[i], "a accept") {
			t.Fatalf("rung %d is %q and every rung is a prefix of the whole sentence", i, rungs[i])
		}
	}
}

// AND THE ROOM ACTUALLY DRAWS IT AT SIXTY. The ladder above is arithmetic; this
// is the frame.
func TestASixtyColumnRoomDrawsTheAnswersOnItsFoot(t *testing.T) {
	a, _ := roomSettleApp(t, session.TaskUnverified)
	if !a.roomSettleAsking() {
		t.Fatal("the fixture's room is not asking, and this test is about the frame that is")
	}
	for _, width := range []int{160, 120, 80, 60} {
		a.width, a.height = width, 30
		foot := plain(a.legend(width))
		if !strings.Contains(foot, "a accept") {
			t.Fatalf("at %d columns the room's foot reads\n\t%q\nand the room is standing on a question it names no key for", width, foot)
		}
		if ansi.StringWidth(foot) > width {
			t.Fatalf("at %d columns the foot is %d cells:\n\t%q", width, ansi.StringWidth(foot), foot)
		}
		if !strings.Contains(foot, roomLegendWord) {
			t.Fatalf("at %d columns the foot lost the way out to make room for the answers:\n\t%q", width, foot)
		}
	}
}
