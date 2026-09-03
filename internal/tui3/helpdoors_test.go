package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// ── /help IS THE ONE KEY SHEET AND IT NAMED NO WAY INTO ANY PLACE ───────────
//
// The hand-written key block listed thirty chords and not one of `alt+1`…`alt+7`,
// `alt+.` or `tab`'s meaning on a place — so a developer who typed /help from a
// cold start finished it not knowing the seven places exist. The map is this
// surface's own chord list and was reachable only from inside a place you
// already had to know how to open.
func TestTheKeySheetNamesTheWayToEveryPlace(t *testing.T) {
	sheet := helpText("", chordSpelling{meta: chordAltWord})
	for _, want := range []string{chordJumpWords, placeMapKey} {
		if !strings.Contains(sheet, want) {
			t.Errorf("/help never names %q — a person who reads the whole sheet still does not know the places exist:\n%s", want, sheet)
		}
	}
	// AND IT SAYS WHICH DIGIT IS WHICH, in the tab bar's own order and read off
	// [placeOrder] rather than typed out a second time.
	if !strings.Contains(sheet, placeWordList()) {
		t.Errorf("/help names %s but never says which place each digit opens (want the list %q):\n%s",
			chordJumpWords, placeWordList(), sheet)
	}
	// AND WHAT `tab` MEANS ONCE YOU ARE ON ONE. The sheet's own `tab` row is
	// about the last conversation, which is true where it is read and says
	// nothing about the seven.
	if !strings.Contains(sheet, "on a place, tab is the next place") {
		t.Errorf("/help never says what tab does on a place:\n%s", sheet)
	}
	// THE CHORDS ARE SPELLED THROUGH THE ONE DOOR, so a Mac sheet says ⌥ and this
	// one does not (chords.go).
	mac := helpText("", chordSpelling{meta: "⌥"})
	if strings.Contains(mac, chordJumpWords) || !strings.Contains(mac, "⌥1…7") {
		t.Errorf("the place rows are not spelled through chords.say — a Mac sheet still reads alt+:\n%s", mac)
	}
}

// ── THE SEARCH PLACE HAD NO TYPED DOOR, AND /spend OPENED SOMETHING ELSE ────
//
// Every other place has a word — /home, /memory, /standing, /history,
// /settings — and search had none, so the only ways in were `alt+6`, `tab`, the
// bar, or typing on home, all of which have to be learned elsewhere first.
// /spend existed and was an alias of /cost, which prints THIS CONVERSATION's
// bill: the one guess a developer makes landed on a different question without
// saying so.
func TestSearchAndSpendHaveTypedDoorsOfTheirOwn(t *testing.T) {
	doors := map[string]page{"search": pageSearch, "spend": pageSpend}
	for word, want := range doors {
		named := false
		for _, c := range commands {
			if c.name == word {
				named = true
			}
		}
		if !named {
			t.Errorf("the command table has no /%s row, so the palette and /help cannot offer it", word)
		}
		a := newTestApp(nil)
		a.width, a.height = 120, 40
		a.slash("/" + word)
		if a.page != want {
			t.Errorf("/%s opened page %d, want %d — the typed door does not reach the place", word, a.page, want)
		}
	}

	// AND `spend` IS NO LONGER A WORD FOR THIS CONVERSATION'S BILL. An alias that
	// still pointed at /cost would resolve there and the place would be
	// unreachable by the one word a person guesses.
	for _, c := range commands {
		for _, word := range c.alias {
			if word == "spend" {
				t.Errorf("/spend is still an alias of /%s — it must open the spend place", c.name)
			}
		}
	}
	// /cost keeps its own words and says on its own row which question it answers.
	cost := ""
	for _, c := range commands {
		if c.name == "cost" {
			cost = c.desc
		}
	}
	if !strings.Contains(cost, "/spend") {
		t.Errorf("/cost's row does not say the other reading exists: %q", cost)
	}
}

// ── THE PALETTE SHOWED 8 OF 52 UNDER 36 BLANK ROWS ──────────────────────────
//
// `menuRows` was a bare ceiling, so `/` — the one door the greeting advertises —
// showed a developer /model through /compact and stopped, with /help and
// /manual both below the fold and nothing on the screen saying forty-four more
// existed.
func TestThePaletteFillsTheFrameAndSaysWhatIsHidden(t *testing.T) {
	open := func(width, room int) *menu {
		m := &menu{}
		m.sync(&editor{value: []rune("/"), cursor: 1})
		if !m.open {
			t.Fatalf("the list did not open on a bare slash")
		}
		return m
	}

	// A TALL FRAME SHOWS MORE THAN EIGHT. Fifty rows of terminal with thirty of
	// them free is not a frame that should draw eight commands and thirty-six
	// blanks.
	m := open(120, 30)
	if want := m.height(120, 30); want <= menuRows {
		t.Fatalf("with 30 rows of room the list still wants %d (the old ceiling was %d) — it is not filling the frame", want, menuRows)
	}
	tall := m.height(120, 30)
	rows := m.rows(120, tall, newPalette(0, false), -1)
	if len(rows) != tall {
		t.Fatalf("the list promised %d lines and drew %d", tall, len(rows))
	}

	// AND A FRAME WITH ROOM FOR THE WHOLE TABLE SHOWS THE WHOLE TABLE, which is
	// what puts /help and /manual on the screen at all: `/` is the one door the
	// greeting advertises, and both of them used to be below an eight-row fold.
	roomy := len(m.hits) + 4
	all := m.rows(120, m.height(120, roomy), newPalette(0, false), -1)
	drawn := strings.Join(all, "\n")
	for _, want := range []string{"/help", "/manual"} {
		if !strings.Contains(drawn, want) {
			t.Errorf("%s is still below the fold on a frame with room for every row:\n%s", want, drawn)
		}
	}

	// A SHORT FRAME KEEPS THE FLOOR. Eight is the least it ever shows, whatever
	// the room says, and [app.overlayHeight]'s own clamp is what protects the
	// status line.
	if want := m.height(120, 2); want < menuRows {
		t.Errorf("with 2 rows of room the list wants %d — the floor of %d was lost", want, menuRows)
	}

	// AND WHERE ROWS ARE STILL HIDDEN, THE COUNT IS SAID. Every other list on
	// this surface draws that line and this one drew nothing at all.
	short := m.rows(120, menuRows, newPalette(0, false), -1)
	last := ansi.Strip(short[len(short)-1])
	hidden := len(m.hits) - (menuRows - 1)
	if !strings.Contains(last, "more") {
		t.Fatalf("the palette hides %d of %d commands and says nothing about them — its last line is %q",
			hidden, len(m.hits), last)
	}
	if !strings.Contains(last, foldLine(hidden, "")) {
		t.Errorf("the fold line says %q, want it to name the %d commands that are hidden (%q)",
			last, hidden, foldLine(hidden, ""))
	}

	// AND A LIST THAT FITS WHOLE SAYS NOTHING ABOUT A REMAINDER THAT IS NOT THERE.
	whole := &menu{}
	whole.sync(&editor{value: []rune("/quit"), cursor: 5})
	lines := whole.rows(120, whole.height(120, 20), newPalette(0, false), -1)
	if strings.Contains(strings.Join(lines, "\n"), "more") {
		t.Errorf("a list with nothing hidden drew a fold line:\n%s", strings.Join(lines, "\n"))
	}
}

// ── AN EMPTY STATE MUST SAY WHAT TO DO NEXT, IN A VERB ──────────────────────
//
// Search was three declarative statements with no example query; spend never
// said that nothing had been spent, so a developer could not tell "no rows yet"
// from "the ledger could not be read"; memory explained what memory is over a
// page with no shelves, and its foot then promised `enter open a shelf · alt+s
// walk the shelves` over nothing to open, filter or walk. The tasks place, one
// `tab` away, already had the shape all three lacked.
func TestAnEmptyPlaceSaysWhatToDoNext(t *testing.T) {
	pal := newPalette(0, false)

	// SEARCH — the asker's own words, and what a searchable thing looks like.
	teach := strings.Join(searchTeach(pal), "\n")
	if !strings.Contains(ansi.Strip(teach), searchExampleWord) {
		t.Errorf("the search teaching page never says what to type:\n%s", ansi.Strip(teach))
	}

	// SPEND — the sentence that tells an empty ledger from an unread one.
	if !strings.Contains(spendTeach, spendTeachEmptyWord) {
		t.Errorf("the spend teaching prose never says nothing has been spent:\n%s", spendTeach)
	}

	// MEMORY — the verb, and a foot that promises only what is bound.
	bare := readMemory(store.MemoryShelves{}, nil, "", time.Time{})
	body := ""
	for _, line := range bare.rows(120, pal) {
		body += ansi.Strip(line) + "\n"
	}
	if !strings.Contains(body, memoryEmptyWord) {
		t.Errorf("the memory page with nothing on it never says what to do next:\n%s", body)
	}
	a := newTestApp(nil)
	a.width, a.height = 120, 40
	a.mem.reading = bare
	foot := placeTailed((placeMemory{}).hint(a))
	for _, wrong := range []string{"open a shelf", "walk the shelves", "type to filter"} {
		if strings.Contains(foot, wrong) {
			t.Errorf("the memory foot promises %q over a page with no shelves: %q", wrong, foot)
		}
	}
	if foot != placeHintTail+" · "+memoryBareHint {
		t.Errorf("the memory foot on a bare page drew %q, want %q — the way out is said last",
			foot, placeHintTail+" · "+memoryBareHint)
	}

	// AND A PAGE THAT IS ONLY NEARLY EMPTY STILL SAYS ITS SHELF KEYS. The
	// teaching prose stands until there are eight lines, and "nothing learned
	// yet" over three visible shelves would be the page contradicting its body.
	some := readMemory(store.MemoryShelves{Total: 3, Held: 3,
		Shelves: []store.MemoryShelf{{Scope: store.MemoryScopeUser, Held: 3}}}, nil, "", time.Time{})
	shown := ""
	for _, line := range some.rows(120, pal) {
		shown += ansi.Strip(line) + "\n"
	}
	if strings.Contains(shown, memoryEmptyWord) {
		t.Errorf("a page with three memories on it says nothing is learned yet:\n%s", shown)
	}
}

// ── THE WORDMARK JUMPED SIX COLUMNS WHEN SETUP ENDED ────────────────────────
//
// The setup block and the greeting that replaces it were centred by two
// different rules — an exact half of a sixty-four-cell measure against two
// fifths of the slack over a seventy-six-cell one — so at 160x50 the setup's
// wordmark stood at column 49 and the identical letterform, one keypress later,
// at column 43. They are the only two screens this wordmark is ever drawn on.
func TestTheWordmarkDoesNotMoveWhenSetupEnds(t *testing.T) {
	for _, size := range []struct{ w, h int }{{160, 50}, {120, 40}, {80, 24}, {60, 30}} {
		a := newTestApp(nil)
		a.width, a.height = size.w, size.h
		a.pal = newPalette(0, false)
		a.setup = setupFlow{steps: []setupStep{setupKey}}

		block, _, _ := a.setupFrame(size.w, size.h)
		setupCol, setupRow := wordmarkAt(block)
		if setupCol < 0 {
			t.Fatalf("at %dx%d the setup screen drew no wordmark at all", size.w, size.h)
		}

		a.welcome.open = true
		a.welcome.step = welcomeSlide
		unit, _, _, _ := a.welcomeUnit(size.w)
		if len(unit) == 0 {
			// A frame too small to be greeted in draws no unit, and there is then
			// nothing for the setup screen to agree with (welcome.go's minimums).
			continue
		}
		greetCol, _ := wordmarkAt(unit)
		if greetCol != setupCol {
			t.Errorf("at %dx%d the wordmark stands at column %d during setup and column %d in the greeting — "+
				"one keypress apart, on the two screens it is the only thing on",
				size.w, size.h, setupCol+1, greetCol+1)
		}
		_ = setupRow
	}

	// AND THE LIFT IS THE GREETING'S OWN RULE rather than an exact half. Two
	// fifths of the slack is written down and argued for ([welcomeAbove]); the
	// half was not.
	a := newTestApp(nil)
	a.width, a.height = 160, 50
	a.pal = newPalette(0, false)
	a.setup = setupFlow{steps: []setupStep{setupKey}}
	block, _, _ := a.setupFrame(160, 50)
	_, row := wordmarkAt(block)
	// The block runs from the wordmark to the last thing it draws, blanks inside
	// it included — that span is what the greeting's rule is a fraction of.
	last := row
	for at, line := range block {
		if strings.TrimSpace(ansi.Strip(line)) != "" {
			last = at
		}
	}
	body := last - row + 1
	if want := welcomeAbove(body, 50-body); row != want {
		t.Errorf("the setup block sits %d rows down; the greeting's own rule puts a %d-row block at %d",
			row, body, want)
	}
}

// wordmarkAt is the row and the column the wordmark's first glyph stands at in a
// block of drawn lines, and (-1, -1) when it is not there.
func wordmarkAt(lines []string) (col, row int) {
	head := wordmarkRows(false)[0]
	for at, line := range lines {
		plain := ansi.Strip(line)
		if i := strings.Index(plain, head); i >= 0 {
			return ansi.StringWidth(plain[:i]), at
		}
	}
	return -1, -1
}
