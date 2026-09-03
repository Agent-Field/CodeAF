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

// THE ANSWERS ROW COUNTS THE CHOICE IT COULD NOT FIT. The three answers are the
// question and the fourth is a preference, so the preference is what goes — and
// the row then ends in the count rather than in nothing.
func TestANarrowAnswersRowCountsTheChoiceItDropped(t *testing.T) {
	full := strings.Join(settleParts(200), "")
	if !strings.Contains(full, settleAlwaysKey) {
		t.Fatalf("a wide answers row is\n\t%q\nand it should carry every choice, %q included", full, settleAlwaysKey)
	}
	// Two cells narrower than the whole row: exactly the frame that has to drop
	// the fourth choice and still has room to say so.
	room := ansi.StringWidth(full) - 2
	narrow := strings.Join(settleParts(room), "")
	if strings.Contains(narrow, settleAlwaysKey) {
		t.Fatalf("the answers row at %d cells is\n\t%q\nand should have dropped %q", room, narrow, settleAlwaysKey)
	}
	if !strings.HasSuffix(narrow, settleHidWord) {
		t.Fatalf("the answers row at %d cells is\n\t%q\nand it dropped a choice without saying so; it should end in %q",
			room, narrow, settleHidWord)
	}
	if ansi.StringWidth(narrow) > room {
		t.Fatalf("the answers row is %d cells and has %d:\n\t%q", ansi.StringWidth(narrow), room, narrow)
	}
	// AND THE THREE ANSWERS THEMSELVES ARE NEVER TRADED FOR THE COUNT. A row too
	// narrow even for the count keeps the question and drops the count instead.
	tight := strings.Join(settleParts(ansi.StringWidth(narrow)-ansi.StringWidth(settleHidWord)), "")
	for _, want := range []string{settleTakeKey, settleAgainKey, settleNotRightKey} {
		if !strings.Contains(tight, want) {
			t.Fatalf("the tightest answers row is\n\t%q\nand it lost the answer %q", tight, want)
		}
	}
	if strings.Contains(tight, settleHidWord) {
		t.Fatalf("the tightest answers row ran past its own edge to draw a count:\n\t%q", tight)
	}
}

// THE COUNT IS A COUNT AND NOT A FOURTH CHIP. Nothing on the row a person can
// press resolves to an answer they cannot read.
func TestTheDroppedChoicesCountAnswersToNoPress(t *testing.T) {
	a, _ := settleApp(t)
	card := landUnverified(t, a)
	full := ansi.StringWidth(strings.Join(settleParts(200), ""))
	rows := a.settleRows(nil, card, 0, full-2+2, 0)
	if len(rows) < 2 {
		t.Fatalf("the card drew %d rows and should draw the ask and the answers", len(rows))
	}
	if got := len(card.chips); got != 3 {
		t.Fatalf("a narrow answers row records %d pressable answers and should record 3:\n\t%q",
			got, plain(rows[len(rows)-1].text))
	}
	if line := plain(rows[len(rows)-1].text); !strings.HasSuffix(line, settleHidWord) {
		t.Fatalf("the drawn answers row is\n\t%q\nand should end in the count %q", line, settleHidWord)
	}
}

// AND THE FOOT NAMES THE ANSWERS AT EVERY WIDTH THE ROOM IS DRAWN AT. The hint
// slot takes a line whole or not at all, so a sentence four cells too long left
// the narrowest terminal naming none of the keys that answer the question it was
// standing on. Every rung is a ranked prefix of the one above it.
func TestARoomAskingForYourLookNamesItsAnswersAtEveryWidth(t *testing.T) {
	a, _ := roomSettleApp(t, session.TaskUnverified)
	// The two slots that carry these answers: the room's own, and the roster's
	// hold hint, which adds the key that gives the column back.
	for _, tail := range []string{"", railSep + "esc"} {
		for _, width := range []int{160, 120, 80, 60} {
			say := a.settleHintAt(width, tail)
			if !strings.HasPrefix(say, "a "+strings.TrimPrefix(settleTakeWord, " ")) {
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
	if got := a.settleHintAt(160, ""); got != roomSettleHint {
		t.Fatalf("a wide frame reads %q and should carry the whole sentence %q", got, roomSettleHint)
	}
	if got := a.settleHintAt(60, ""); got == roomSettleHint {
		t.Fatalf("the 60-column hint is the whole sentence, which does not fit: %q", got)
	}
	// AND THE LADDER IS A LADDER: each rung says less than the one above it, and
	// what it drops it counts.
	for i := 1; i < len(roomSettleHints); i++ {
		if ansi.StringWidth(roomSettleHints[i]) >= ansi.StringWidth(roomSettleHints[i-1]) {
			t.Fatalf("rung %d (%q) is no shorter than rung %d (%q)",
				i, roomSettleHints[i], i-1, roomSettleHints[i-1])
		}
		if !strings.HasPrefix(roomSettleHints[i], "a accept") {
			t.Fatalf("rung %d is %q and every rung is a prefix of the whole sentence", i, roomSettleHints[i])
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
