package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// TestTheCardsPlaceLineSpendsTheFactsBeforeTheAddress is rowfit.go's law on the
// one line whose whole job is saying WHICH CHECKOUT.
//
// The line was fitted as one joined string and cut from the LEFT, so the card
// drew `…e-v2 · master, 3 files dirty · here` — the address is the half that
// went, and it is the only half the title one row above does not already carry.
func TestTheCardsPlaceLineSpendsTheFactsBeforeTheAddress(t *testing.T) {
	a := &app{pal: newPalette(tokens.NoColor, false)}
	const where = "/Users/somebody/code/aforge-v2"
	a.home.repos = map[string]homeRepoReading{where: {line: "master · 3 files dirty"}}
	// A locally held conversation offers "open here"; an open presence alone
	// belongs to another window and reserves that longer, truthful door label.
	file := "/tmp/card-place-held.jsonl"
	a.behind = map[string]*kept{a.convKey(file): {conv: Conversation{Agent: newAsyncAgent(), SessionFile: file}}}
	row := session.SessionRow{ID: "one", Transcript: file, Title: "Porting the picker", Workspace: where, Open: true, Live: true}

	// The widths a card actually gets, down to the narrowest that still holds
	// the address and the door word this line reserves for it.
	for _, width := range []int{60, 48, 44} {
		lines := a.homeCardPlace(row, width, a.pal)
		if len(lines) != 1 {
			t.Fatalf("the place line is %d rows at %d columns", len(lines), width)
		}
		drawn := ansi.Strip(lines[0])
		if ansi.StringWidth(drawn) > width {
			t.Fatalf("the place line overran a %d-column card:\n\t%q", width, drawn)
		}
		if !strings.Contains(drawn, where) {
			t.Fatalf("a %d-column card drew\n\t%q\nand the address it exists to say is %q — the facts about a checkout give way before the checkout does", width, drawn, where)
		}
	}
	// AND THE FACTS ARE STILL THERE WHERE THEY FIT. Dropping more than the line
	// had to would be the same defect with the halves swapped.
	wide := ansi.Strip(a.homeCardPlace(row, 80, a.pal)[0])
	for _, want := range []string{where, "master, 3 files dirty"} {
		if !strings.Contains(wide, want) {
			t.Fatalf("an 80-column card drew\n\t%q\nand lost %q, which the line had room for", wide, want)
		}
	}
	// AND THEY GO WHOLE, FROM THE END. A clause half on the line is a fact a
	// person cannot read and cannot tell is incomplete.
	narrow := ansi.Strip(a.homeCardPlace(row, 44, a.pal)[0])
	if strings.Contains(narrow, glyphMore) {
		t.Fatalf("a 44-column card cut a clause instead of dropping one whole:\n\t%q", narrow)
	}
	if !strings.Contains(narrow, "open here") {
		t.Fatalf("a 44-column card dropped what enter will do to the row:\n\t%q", narrow)
	}
}

// TestARefusalAboutTheAddressTravelsWithTheAddress is the one clause the address
// makes room for rather than spends: `folder gone` is not a fact ABOUT this
// checkout, it is the statement that there is no checkout.
func TestARefusalAboutTheAddressTravelsWithTheAddress(t *testing.T) {
	a := &app{pal: newPalette(tokens.NoColor, false)}
	where := "/private/var/folders/x9/TestHomeMarksARowWhoseFolderIsGone/001/no-such-repository"
	a.home.gone = switcherGone{where: true}
	row := session.SessionRow{ID: "one", Title: "A project that moved", Workspace: where}
	for _, width := range []int{60, 40} {
		drawn := ansi.Strip(a.homeCardPlace(row, width, a.pal)[0])
		if !strings.Contains(drawn, homeGoneWord) {
			t.Fatalf("a %d-column card drew\n\t%q\nand did not say %q — a path too long for the card is cut around the refusal, never instead of it", width, drawn, homeGoneWord)
		}
		if ansi.StringWidth(drawn) > width {
			t.Fatalf("the place line overran a %d-column card:\n\t%q", width, drawn)
		}
	}
}
