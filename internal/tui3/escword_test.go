package tui3

import (
	"strings"
	"testing"
)

// ── THE SHEET A PERSON OPENS TO LEARN THE KEYS SPELLS ONE GESTURE ONE WAY ───
//
// `?` over an empty box draws this list, and it named the escape key four
// different ways on the one screen: `esc comes back` on the places row and on
// `space space`, `esc goes back` on the switcher, `esc leaves` on the roster —
// against the `esc back` every card, picker, rewind sheet and hop strip already
// says. Four phrasings of one keystroke read as four gestures, at exactly the
// moment somebody is trying to work out how many gestures there are.

// escSpellings are the wrong ones, and each is checked whole so that `esc back`
// — which is a prefix of nothing here — cannot satisfy the check by accident.
var escSpellings = []string{"esc comes back", "esc goes back", "esc leaves"}

func TestTheKeySheetSpellsTheEscapeGestureOneWay(t *testing.T) {
	for _, chords := range []chordSpelling{{meta: chordAltWord}, {meta: chordMetaWord}} {
		sheet := helpText("", chords)
		for _, wrong := range escSpellings {
			if !strings.Contains(sheet, wrong) {
				continue
			}
			for _, line := range strings.Split(sheet, "\n") {
				if strings.Contains(line, wrong) {
					t.Errorf("the key sheet says %q where every card, picker and strip on this "+
						"surface says %q, so one keystroke reads as two gestures:\n  %s",
						wrong, "esc back", line)
				}
			}
		}
	}

	// AND IT STILL NAMES THE KEY. The switcher now previews a choice, so
	// Escape cancels that pending choice rather than navigating back.
	// A row that dropped the clause instead of
	// respelling it would pass every check above and teach nobody anything, so
	// the four rows that carried a wrong spelling are named here by the key they
	// belong to and must each still say what esc does.
	sheet := helpText("", chordSpelling{meta: chordAltWord})
	for _, row := range []struct {
		what string
		// lead is enough of the row to find it and nothing more.
		lead string
		want string
	}{
		{"the places row's continuation", "on a place, tab is the next place", "esc back"},
		{"the task roster", railHoldChord + " ", "esc back"},
		{"the new chat", newChatChord + " ", "esc back"},
		{"the conversation switcher", hopOpenKey + " ", "esc cancel"},
		{"space space, over an empty box", "space space", "esc back"},
	} {
		found := ""
		for _, line := range strings.Split(sheet, "\n") {
			if strings.Contains(line, row.lead) {
				found = line
				break
			}
		}
		if found == "" {
			t.Fatalf("%s is not on the key sheet at all (looked for %q)", row.what, row.lead)
		}
		if !strings.Contains(found, row.want) {
			t.Errorf("%s stopped naming the escape key instead of respelling it:\n  %s\n  want a clause reading %q",
				row.what, found, row.want)
		}
	}
}

// ── A KEY ROW SAYS WHAT THE KEY DOES ───────────────────────────────────────
//
// Two rows on the sheet were labels among instructions: `ctrl+,` was answered
// with the bare noun `settings`, and `/standing`'s `n` with `not here`, sitting
// between neighbours that say open, switch, copy, delete, pause, stop and run.
// A person reading a column of verbs and meeting a noun has to guess whether the
// key opens the thing, closes it, or is where it is.
func TestTheKeySheetSaysWhatTheSettingsAndStandingKeysDo(t *testing.T) {
	sheet := helpText("", chordSpelling{meta: chordAltWord})
	for _, row := range []struct {
		what string
		lead string
		// want is the clause the row must carry, whole.
		want string
		// stale is the row's whole tail as it used to read, so this cannot be
		// satisfied by a rewording that dropped the key's meaning again.
		stale string
	}{
		{"ctrl+, ", "ctrl+,", "open settings", "settings"},
		{"n, on the /standing row", "p s n", "keep it out of here",
			"in /standing: pause one · stop it · not here"},
	} {
		found := ""
		for _, line := range strings.Split(sheet, "\n") {
			if strings.HasPrefix(line, row.lead) {
				found = line
				break
			}
		}
		if found == "" {
			t.Fatalf("no row on the key sheet starts with %q", row.lead)
		}
		if !strings.Contains(found, row.want) {
			t.Errorf("%s is a label on a sheet of instructions:\n  %s\n  want the clause %q, "+
				"because every neighbour leads with a verb", row.what, found, row.want)
		}
		if tail := strings.TrimSpace(strings.TrimPrefix(found, row.lead)); tail == row.stale {
			t.Errorf("%s still reads %q, which names the thing instead of the action:\n  %s",
				row.what, row.stale, found)
		}
	}
}
