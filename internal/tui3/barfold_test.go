package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

// A FOLD ON THE PLACE BAR WEARS THE FOLD MARK, LIKE EVERY OTHER FOLD HERE.
//
// The bar's remainder said `+3 more` while the command menu's own tail said
// `▸ 3 more` about the same idea — a navigation list with more items than fit —
// so a `+3` at the end of a row could not be told from a count, a badge or a
// door. `▸` is the half that says which, and it is on every rung: the word
// `more` gives way before the mark does.
func TestTheBarsRemainderWearsTheSameFoldMarkTheMenusDoes(t *testing.T) {
	// Wide enough for the whole sentence, then narrow enough that only the
	// count survives, then too narrow for either.
	long := foldLine(3, "")
	short := tokens.GlyphCollapsed + " 3"
	for _, c := range []struct {
		room int
		want string
	}{
		{tabPadCols + ansi.StringWidth(long), long},
		{tabPadCols + ansi.StringWidth(short), short},
		{tabPadCols + ansi.StringWidth(short) - 1, ""},
	} {
		got := barMoreWord(3, c.room)
		if got != c.want {
			t.Fatalf("with %d cells of room the bar drew %q, want %q", c.room, got, c.want)
		}
	}
	// AND NO RUNG OF THE LADDER IS A BARE `+`, which is what it used to be at
	// both lengths.
	for _, room := range []int{40, 20, 12, 9, 8} {
		if got := barMoreWord(4, room); got != "" && !strings.HasPrefix(got, tokens.GlyphCollapsed) {
			t.Fatalf("with %d cells of room the bar drew %q, which wears no fold mark", room, got)
		}
	}
	// AND IT IS THE SAME SPELLER THE MENU'S FOLD USES (commands.go draws
	// foldLine), which is the whole of this row: one idea, one sentence.
	if want := foldLine(3, ""); barMoreWord(3, 60) != want {
		t.Fatalf("the bar drew %q and the menu's fold spells it %q", barMoreWord(3, 60), want)
	}
}

// AND THE MAP'S CLAUSE ABOUT `→` SAYS WHAT THE KEY DOES.
//
// It read `→ verbs on this row` on a line where every other clause names an act:
// `alt+1…7 go to a place`, `alt+enter send it off as a task`, `esc close`.
// `verbs` is the machinery's name for the strip, not anybody's word for what
// pressing the key gets them.
func TestTheMapNamesWhatTheArrowDoesAndNotWhatItIsCalled(t *testing.T) {
	if strings.Contains(placeMapVerbWords, "verbs") {
		t.Fatalf("the map's arrow clause reads %q, which names the machinery's category rather than the act", placeMapVerbWords)
	}
	// Every clause on that line is a key and then a VERB — the word right after
	// the key is something a person does.
	for _, clause := range strings.Split(placeMapWords, " · ") {
		fields := strings.Fields(clause)
		if len(fields) < 2 {
			t.Fatalf("the map's line has a clause with no verb in it: %q (in %q)", clause, placeMapWords)
		}
		if verb := fields[1]; !mapVerbs[verb] {
			t.Fatalf("the clause %q opens with %q, which is not something a person does; the line reads %q",
				clause, verb, placeMapWords)
		}
	}
}

// mapVerbs is what the map's line is allowed to say a key does — every one of
// them a plain act, which is the law this file's second test states.
var mapVerbs = map[string]bool{"go": true, "send": true, "show": true, "close": true, "open": true}
