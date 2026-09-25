package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE NARROW TIER ─────────────────────────────────────────────────────────
//
// Sixty columns is a split pane, an ssh session from a train, a phone. It is
// not a degraded frame that somebody will widen in a minute — it is the frame a
// developer is stuck with — and the three things below are what a person could
// not DO there: find six of the seven places, read the end of a sentence, and
// tell one conversation from another.

// THE NARROW NAV STILL SAYS WHERE ELSE YOU CAN GO.
//
// At sixty columns the places' bar used to collapse to the single word `home`,
// with nothing on the frame saying the other places existed at all. The nav
// on the wordmark's row keeps every place reachable at every width: a word it
// has no room for folds into `more ▾`, which is a door onto exactly those
// places (navmore.go), and the air between two words never changes.
func TestTheNarrowNavStillSaysWhereElseYouCanGo(t *testing.T) {
	a := placeApp(t)
	for _, width := range []int{50, 60, 80, 120, 160} {
		bar := navPlaces(a, width, false)
		for _, id := range barPages(a.navLit(), false) {
			if !strings.Contains(" "+bar+" ", " "+id.word()+" ") && !pagesHold(a.navMore.folded, id) {
				t.Fatalf("at %d columns the nav drew\n\t%q\nand a person cannot reach %q from it, on the row or behind `more`", width, bar, id.word())
			}
		}
		if got := ansi.StringWidth(plain(a.navLine(width, a.pal))); got > width {
			t.Fatalf("at %d columns the nav is %d cells wide and runs past the frame", width, got)
		}
		// AND THE AIR IS HELD: two blank cells between two words, whatever the
		// width, because what a narrow row gives up is words and never air.
		if width >= 80 && !strings.Contains(bar, "home  teams  chats  sessions  spend  settings") {
			t.Fatalf("at %d columns the nav drew\n\t%q\nand should carry the six places two cells apart", width, bar)
		}
		if strings.Contains(bar, "   ") {
			t.Fatalf("at %d columns the nav drew\n\t%q\nwith the air between two words changed", width, bar)
		}
	}
}

// pagesHold reports whether one place is in a list of places.
func pagesHold(list []page, id page) bool {
	for _, p := range list {
		if p == id {
			return true
		}
	}
	return false
}

// A NAV TOO NARROW FOR EVERY WORD FOLDS THEM INTO `more ▾`, AND IT IS A DOOR.
//
// Under the width where all six fit there is no honest way to draw them, and
// the collapse the ladder exists to prevent is not "fewer words", it is a row
// that says `home` and lets a person believe that is all there is. So what is
// left ends in `more ▾`, which stands for exactly the places it folded, and
// never in a bare count a person cannot press.
func TestANavTooNarrowForEveryWordFoldsIntoMore(t *testing.T) {
	a := placeApp(t)
	for _, width := range []int{34, 40, 48} {
		bar := navPlaces(a, width, false)
		if !strings.Contains(bar, a.page.word()) {
			t.Fatalf("at %d columns the nav drew\n\t%q\nand folded the place you are standing in (%q)", width, bar, a.page.word())
		}
		if !strings.HasSuffix(bar, a.navMoreWord(a.pal)) || !a.navMore.span.pressable() {
			t.Fatalf("at %d columns the nav drew\n\t%q\nand said nothing about the places it could not carry; it should end in `more ▾`", width, bar)
		}
		missing := 0
		for _, id := range barPages(a.page, false) {
			if !strings.Contains(" "+bar+" ", " "+id.word()+" ") {
				missing++
				if !pagesHold(a.navMore.folded, id) {
					t.Fatalf("at %d columns %q is off the row and not behind `more`", width, id.word())
				}
			}
		}
		if missing != len(a.navMore.folded) || missing == 0 {
			t.Fatalf("at %d columns the nav left %d places off and folded %v", width, missing, a.navMore.folded)
		}
		if strings.Contains(bar, "+") || strings.Contains(bar, tokens.GlyphCollapsed) {
			t.Fatalf("at %d columns the fold is a count and not a word: %q", width, bar)
		}
	}
	// AND A FRAME WITH NO ROOM EVEN FOR THE FOLD NEVER RUNS PAST ITS OWN EDGE.
	for _, width := range []int{8, 12, 16, 24} {
		if got := ansi.StringWidth(plain(a.navLine(width, a.pal))); got > width {
			t.Fatalf("at %d columns the nav is %d cells wide and runs past the frame", width, got)
		}
	}
	// AND A NAV THAT CARRIED EVERYTHING FOLDS NOTHING.
	for _, width := range []int{80, 120, 160} {
		if navPlaces(a, width, false); a.navMore.span.pressable() {
			t.Fatalf("at %d columns every place is on the row and it still folds %v", width, a.navMore.folded)
		}
	}
}

// A FOOT DROPS WHOLE HINTS AND NEVER SLICES ONE.
//
// Home's own foot is not a key list — it is a statement about a door with an
// elaboration hung off a dash and a gloss in brackets behind that — and it
// reached sixty columns as `open in another window — enter again to move it
// here (it …`, which promises a key and then eats it. It is the same defect the
// sliced key list was, in a sentence instead of a list, and it goes through the
// same fitter now.
//
// THE SENTENCE IS A LITERAL HERE AND NO LONGER THE FOOT'S OWN. That door asks
// with a card on home now (homeconfirm.go) and the foot has stopped carrying it
// — but the FITTER is what this test is about, the shape it defeated is the one
// below, and a law deleted with the string that first broke it is a law that
// comes back with the next sentence of that shape.
func TestTheNarrowFootDropsWholeHintsAndNeverSlicesOne(t *testing.T) {
	sentence := "open in another window — enter again to move it here (that window's reply stops there; its tasks resume here)"
	for _, width := range []int{60, 80, 120, 160} {
		foot := hintFit(sentence, width-2)
		if strings.Contains(foot, glyphMore) {
			t.Fatalf("at %d columns home's foot drew\n\t%q\nwith a word cut in half; it should drop the clause whole:\n\t%q",
				width, foot, "open in another window — enter again to move it here")
		}
		if !strings.HasPrefix(sentence, foot) {
			t.Fatalf("at %d columns home's foot drew\n\t%q\nwhich is not a prefix of the sentence it came from:\n\t%q", width, foot, sentence)
		}
		if got := ansi.StringWidth(foot); got > width-2 {
			t.Fatalf("at %d columns home's foot is %d cells wide:\n\t%q", width, got, foot)
		}
	}
	// THE RANK, SAID EXACTLY. The gloss in brackets is the lowest-value clause on
	// the line, because it explains a clause that is still there; the dash
	// elaboration goes second; the statement is what a person is left with.
	if got, want := hintFit(sentence, 58), "open in another window — enter again to move it here"; got != want {
		t.Fatalf("at 60 columns home's foot drew\n\t%q\nand it should have dropped the bracketed gloss whole:\n\t%q", got, want)
	}
	if got, want := hintFit(sentence, 30), "open in another window"; got != want {
		t.Fatalf("at 32 columns home's foot drew\n\t%q\nand it should be down to the statement alone:\n\t%q", got, want)
	}
	// AND A WIDE FRAME STILL SAYS THE WHOLE THING.
	if got := hintFit(sentence, 158); got != sentence {
		t.Fatalf("at 160 columns home's foot drew\n\t%q\nand there was room for all of it:\n\t%q", got, sentence)
	}
	// AND THE WAY OUT IS NEVER WHAT A CLAUSE-DROP TAKES. A hint that ends in
	// `tab next place` keeps it at every width the ladder can reach.
	tailed := "enter open — the one under the cursor · " + placeHintTail
	for _, room := range []int{58, 40, 30} {
		if got := hintFit(tailed, room); !strings.Contains(got, placeHintTail) {
			t.Fatalf("at %d cells the foot drew\n\t%q\nand lost the way out; %q is kept to the last cell there is", room, got, placeHintTail)
		}
	}
}

// hintsStillCutByCharacter is the ledger of feet this surface still slices, and
// it is here so the law below can be a law rather than an ambition. IT ONLY
// SHRINKS: fix one, delete its line.
//
// IT IS EMPTY, and the two entries it opened with are gone rather than
// forgotten: `jobpage.go` and `taskrecord.go` both went onto [hintFit] with the
// way out moved to the end of their key rows, where the ranked-clause fitter
// keeps it (docs/design/polish/audit-jobs.md's hint rows). The map stays so the
// second half of the law below — a file fixed but still named here — has
// something to guard the day somebody needs the ledger again.
var hintsStillCutByCharacter = map[string]string{}

// EVERY HINT IS FITTED BY DROPPING CLAUSES, NOT BY CUTTING CHARACTERS.
//
// [paintHint] is the painter every key line on this surface goes through, and
// what it is handed decides whether a narrow terminal loses a whole key or half
// of one. A key sheet with one fewer key on it is a smaller sheet; a key sheet
// that ends `· t…` is a sheet that named a key and then ate it, which is worse
// than never having named it. So the argument to [paintHint] may not be a bare
// [fit] call: it goes through [hintFit], which is the ranked-clause fitter.
//
// It reads the tree with go/parser, which is what puts it on the pull-request
// gate the day it lands (scripts/laws.sh).
func TestEveryHintIsFittedByDroppingClausesNotByCuttingCharacters(t *testing.T) {
	fset := token.NewFileSet()
	offenders := map[string][]string{}
	for _, name := range placeSourceFiles(t) {
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("could not read %s: %v", name, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			fn, ok := call.Fun.(*ast.Ident)
			if !ok || fn.Name != "paintHint" {
				return true
			}
			inner, ok := call.Args[0].(*ast.CallExpr)
			if !ok {
				return true
			}
			cut, ok := inner.Fun.(*ast.Ident)
			if !ok || cut.Name != "fit" {
				return true
			}
			at := fset.Position(call.Pos())
			offenders[name] = append(offenders[name], itoa(at.Line))
			return true
		})
	}
	for name, lines := range offenders {
		if _, known := hintsStillCutByCharacter[name]; known {
			continue
		}
		t.Fatalf("%s:%s paints a hint through a plain fit, which cuts it mid-word:\n\tpaintHint(fit(hint, width-2), …)\nand every hint on this surface drops whole clauses instead:\n\tpaintHint(hintFit(hint, width-2), …)",
			name, strings.Join(lines, ","))
	}
	// AND THE LEDGER ONLY SHRINKS. A file that has been fixed but is still named
	// here is a law with a hole in it that nobody can see.
	for name, why := range hintsStillCutByCharacter {
		if len(offenders[name]) == 0 {
			t.Fatalf("%s no longer cuts a hint by character (%s) — delete its line from hintsStillCutByCharacter", name, why)
		}
	}
}

// A CONVERSATION WITH NO TITLE IS CALLED SOMETHING A PERSON CAN USE.
//
// The row a person is most likely to be standing in is the one they just
// opened, and nothing has named it yet — so home drew `○ 927D303242f9d00e` in
// the one column that exists to let a person match names. At sixty columns that
// is a third of the row, and title-casing an id is worse than drawing nothing,
// because it reads as a name somebody chose.
func TestAConversationWithNoTitleIsNamedInWordsNotHex(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-alpha", "927d303242f9d00e", "", lab.workspace("alpha"), now.Add(-time.Minute))
	lab.session("-alpha", "aaaa000000000002", "porting the picker", lab.workspace("alpha"), now.Add(-2*time.Hour))
	for _, width := range []int{60, 80, 120, 160} {
		a := lab.app(mine)
		a.width, a.height = width, 30
		a.openingPrompt = "explain open addressing"
		openHomeFixtureTabs(a)
		a.openHome()
		frame := homeText(a)
		if strings.Contains(frame, "927D303242f9d00e") || strings.Contains(frame, "927d303242f9d00e") {
			t.Fatalf("at %d columns home drew\n\t%q\nwith the folder's id where the name goes; it should read\n\t%q",
				width, rowSaying(frame, "927"), "○ "+"explain open addressing"+"   alpha  here")
		}
		if !strings.Contains(frame, "explain open addressing") {
			t.Fatalf("at %d columns home drew\n%s\nand nothing on it names the conversation with no title; the row should read\n\t%q",
				width, frame, "○ "+"explain open addressing")
		}
		// AND A CONVERSATION THAT HAS A NAME STILL WEARS IT.
		if !strings.Contains(frame, "Porting the Picker") {
			t.Fatalf("at %d columns home drew\n%s\nand lost the name of a conversation that has one:\n\t%q", width, frame, "Porting the Picker")
		}
	}
	// AND THE LADDER IS STILL A LADDER: a folder that genuinely reads as words is
	// a better name than a generic one, and only an id-shaped stem is refused.
	if got, want := listName("", "/p/port-the-parser/transcript.jsonl"), "Port the Parser"; got != want {
		t.Fatalf("a folder that reads as words was named %q, and it should keep its own words: %q", got, want)
	}
	if got := listName("", "/p/01J8ZK4Q2M7X/transcript.jsonl"); got != unnamedConversationWord {
		t.Fatalf("an id-shaped folder was named %q, and a name that cannot be had is said in words: %q", got, "explain open addressing")
	}
}

// rowSaying is the one line of a frame that contains a word, for a failure
// message that shows the row rather than the screen.
func rowSaying(frame, word string) string {
	for _, line := range strings.Split(frame, "\n") {
		if strings.Contains(line, word) {
			return strings.TrimRight(line, " ")
		}
	}
	return ""
}

// ── THE TOP LINE, AND THE ORDER ITS SEGMENTS GIVE WAY IN ────────────────────

// pulseLab is a machine with something to say in all four of the pulse's
// clauses, on a clock that does not move: twelve things stopped on a person,
// four in flight, a hundred and twenty-three dollars spent of five hundred, and
// a Thursday afternoon.
//
// THE FACTS ARE PUT STRAIGHT INTO THE MEMO THE PULSE DRAWS ([app.machine]), because
// the subject here is the LADDER and not the reading. A fixture that built
// twelve waiting conversations and four running tasks on disk would be a slow
// test of the counters, and the counters have their own (pulsemoney_test.go).
func pulseLab(t *testing.T) (*app, time.Time) {
	t.Helper()
	lab := newHomeLab(t)
	now := time.Date(2026, 9, 3, 13, 11, 0, 0, time.Local)
	mine := lab.session("-alpha", "aaaa000000000001", "porting the picker", lab.workspace("alpha"), now)
	a := lab.app(mine)
	a.clock = func() time.Time { return now }
	a.machine = machineFacts{wants: 12, hands: 4, spent: 123.45, ceiling: 500}
	return a, now
}

// THE TOP LINE GIVES THINGS UP IN THE OWNER'S ORDER.
//
// The places and the pulse share the first row now, and the owner ruled the
// order a narrowing row gives things up in (2026-09-24): the clock first, with
// the machine's name, then the counts (the moving one before the one that
// wants you), then the allowance behind the day's figure, then the trailing
// places fold into `more ▾`, and the day's figure is the last clause standing.
// Every width is checked: a thing given up early may never be on the row while
// a thing given up later is gone, and every rung of the ladder is met.
func TestTheTopLineGivesThingsUpInTheOwnersOrder(t *testing.T) {
	a, now := pulseLab(t)
	a.showPage(pageNone)
	a.machine = machineFacts{wants: 12, hands: 4, spent: 123.45, ceiling: 500}
	a.host = "spark"
	clock := pulseClock(now)
	type state struct{ clock, host, hands, wants, allowance, unfolded, spend bool }
	seen := map[state]bool{}
	for width := 220; width >= 30; width-- {
		line := plain(a.navLine(width, a.pal))
		if got := ansi.StringWidth(line); got > width {
			t.Fatalf("at %d columns the top line is %d cells wide:\n\t%q", width, got, line)
		}
		if !strings.Contains(line, product) || !strings.Contains(line, " chats ") {
			t.Fatalf("at %d columns the top line lost the wordmark or the lit place:\n\t%q", width, line)
		}
		s := state{
			clock: strings.Contains(line, clock), host: strings.Contains(line, "on spark"),
			hands: strings.Contains(line, "4 moving"), wants: strings.Contains(line, "12 want you"),
			allowance: strings.Contains(line, "/ "+railFigure(500)), unfolded: !a.navMore.span.pressable(),
			spend: strings.Contains(line, "$123.45"),
		}
		order := []bool{s.clock, s.host, s.hands, s.wants, s.allowance, s.unfolded, s.spend}
		for i := range order {
			for j := i + 1; j < len(order); j++ {
				if order[i] && !order[j] {
					t.Fatalf("at %d columns the top line gave up rung %d before rung %d:\n\t%q", width, j, i, line)
				}
			}
		}
		seen[s] = true
	}
	for _, want := range []state{
		{true, true, true, true, true, true, true},
		{false, true, true, true, true, true, true},
		{false, false, true, true, true, true, true},
		{false, false, false, true, true, true, true},
		{false, false, false, false, true, true, true},
		{false, false, false, false, false, true, true},
		{false, false, false, false, false, false, true},
		{false, false, false, false, false, false, false},
	} {
		if !seen[want] {
			t.Fatalf("no width drew the rung %+v; the ladder skipped a step", want)
		}
	}
	// AND THE CLOCK IS STILL THE ONE SEGMENT A QUIET MORNING KEEPS. It is the
	// lowest-ranked thing on the line and it is never EMPTY: a machine with
	// nothing stopped, nothing moving and nothing spent draws the time.
	a.machine, a.host = machineFacts{}, ""
	if line := plain(a.navLine(120, a.pal)); !strings.HasSuffix(strings.TrimRight(line, " "), clock) {
		t.Fatalf("over a quiet morning the top line drew\n\t%q\nand it should end in the time", line)
	}
}

// A NARROW TOP LINE NEVER SAYS THE DAY COST NOTHING.
//
// The pulse is not the live status line, which keeps `$0.00` on purpose so its
// segments do not jump sideways as the first money arrives. A segment this
// ladder gives up has to leave NOTHING BEHIND that implies a figure: no `$0.00`,
// no allowance with an empty numerator in front of it, no bare `$`.
func TestANarrowTopLineNeverSaysTheDayCostNothing(t *testing.T) {
	a, _ := pulseLab(t)
	a.showPage(pageNone)
	a.machine = machineFacts{wants: 12, hands: 4, spent: 123.45, ceiling: 500}
	for width := 12; width <= 160; width++ {
		line := plain(a.navLine(width, a.pal))
		switch {
		case strings.Contains(line, "$0.00"):
			t.Fatalf("at %d columns the top line drew\n\t%q\nover a day that spent $123.45; a dropped segment may not become a zero", width, line)
		case strings.Contains(line, railFigure(500)) && !strings.Contains(line, "$123.45"):
			t.Fatalf("at %d columns the top line drew\n\t%q\nwith the allowance and no spend in front of it", width, line)
		case strings.Contains(line, "$") && !strings.Contains(line, "$123.45"):
			t.Fatalf("at %d columns the top line drew\n\t%q\nwith a figure that is not the day's own", width, line)
		case strings.Contains(line, "/") && !strings.Contains(line, railFigure(500)):
			t.Fatalf("at %d columns the top line drew\n\t%q\nwith the fraction's slash and no bound behind it", width, line)
		case strings.Contains(line, glyphMore):
			t.Fatalf("at %d columns the top line drew\n\t%q\nwith a segment cut in half; it drops them whole", width, line)
		}
	}
}

// squash is a frame's run of spaces reduced to one, for a comparison about WORDS
// and their order rather than about the right-alignment gap.
func squash(line string) string { return strings.Join(strings.Fields(line), " ") }

// ── THE OTHER FOUR LINES THAT USED TO BE CUT BY CHARACTER ───────────────────

// THE SETTINGS FOOT DROPS ITS ASIDE AND KEEPS THE ANSWER.
//
// The panel's note says `saved to your profile · a project's own
// .codeaf/config.json is a hand edit`, which is an answer followed by an
// aside, and a character ruler ended it `· a project's own .codeaf/conf…` at
// sixty columns: a line that named a path and then ate it. A note is fitted from
// the OTHER end to a key sheet ([noteFit]) — the answer is at the front — so
// what a narrow panel keeps is the half a person asked for.
func TestTheSettingsFootDropsWholeClausesNotCharacters(t *testing.T) {
	a := placeApp(t)
	walkTo(t, a, pageSettings)
	full := a.sheet.footNote()
	if !strings.Contains(full, " · ") {
		t.Fatalf("the settings foot is no longer two clauses and this test is about the wrong line: %q", full)
	}
	head := strings.SplitN(full, " · ", 2)[0]
	for _, width := range []int{60, 80, 120, 160} {
		note := plain(placeSettings{}.note(a, width)[0])
		if strings.Contains(note, glyphMore) {
			t.Fatalf("at %d columns the settings foot drew\n\t%q\nwith a word cut in half; it should drop the aside whole:\n\t%q",
				width, note, " "+head)
		}
		if !strings.Contains(note, head) {
			t.Fatalf("at %d columns the settings foot drew\n\t%q\nand lost the clause that answers the question; it should read at least\n\t%q",
				width, note, " "+head)
		}
		if got := ansi.StringWidth(note); got > width {
			t.Fatalf("at %d columns the settings foot is %d cells wide:\n\t%q", width, got, note)
		}
	}
	// THE RANK, SAID EXACTLY: at sixty the aside is gone whole, and at a hundred
	// and sixty both clauses are still there.
	if got, want := plain(placeSettings{}.note(a, 60)[0]), " "+head; got != want {
		t.Fatalf("at 60 columns the settings foot drew\n\t%q\nand it should be the answer alone:\n\t%q", got, want)
	}
	if got, want := plain(placeSettings{}.note(a, 160)[0]), " "+full; got != want {
		t.Fatalf("at 160 columns the settings foot drew\n\t%q\nand there was room for all of it:\n\t%q", got, want)
	}
}

// THE macOS CHORD NOTE DROPS ITS REMEDY WHOLE RATHER THAN SLICING IT.
//
// `your terminal sends opt as a letter — turn on "use option as meta" in Terminal:
// Profiles › Keyboard` came to sixty columns as a quoted menu item with its end
// eaten, which is the worst thing a line about a SETTING can do: it named the
// setting and then refused to finish saying it. The dash clause goes whole now
// and the diagnosis stays, which is the half that tells a person the chords are
// not broken.
func TestTheChordNoteDropsItsRemedyWholeRatherThanSlicingIt(t *testing.T) {
	a := placeApp(t)
	a.chordLost = true
	a.chords = chordSpelling{meta: chordMetaWord, terminal: "Terminal", setting: "Profiles › Keyboard › Use Option as Meta key"}
	full := a.chords.chordOptionWords()
	diagnosis := strings.SplitN(full, sentenceDash, 2)[0]
	diagnosis = strings.TrimRight(diagnosis, " ")
	for _, width := range []int{60, 80, 120, 160} {
		note := plain(a.chordNote(width))
		if strings.Contains(note, glyphMore) {
			t.Fatalf("at %d columns the chord note drew\n\t%q\nwith the setting's name cut in half; it should drop the remedy whole:\n\t%q",
				width, note, " "+diagnosis)
		}
		if !strings.Contains(note, diagnosis) {
			t.Fatalf("at %d columns the chord note drew\n\t%q\nand lost the diagnosis; it should read at least\n\t%q", width, note, " "+diagnosis)
		}
		if got := ansi.StringWidth(note); got > width {
			t.Fatalf("at %d columns the chord note is %d cells wide:\n\t%q", width, got, note)
		}
	}
	if got, want := plain(a.chordNote(60)), " "+diagnosis; got != want {
		t.Fatalf("at 60 columns the chord note drew\n\t%q\nand it should be the diagnosis alone:\n\t%q", got, want)
	}
	if got, want := plain(a.chordNote(160)), " "+full; got != want {
		t.Fatalf("at 160 columns the chord note drew\n\t%q\nand there was room for all of it:\n\t%q", got, want)
	}
}

// A FILTER BOX'S PLACEHOLDER DROPS WHOLE KEYS AND KEEPS THE WAY OUT.
//
// An overlay explains itself in the box a person is already looking at instead
// of spending a row on a legend, so that placeholder IS the key sheet — and it
// was handed to a character ruler, which ended the deliverables box
// `· ctrl+y cop…` and the connection box `· esc clo…`. A box that names the way
// out and then eats it is the defect this whole wave is about.
func TestAFilterBoxPlaceholderDropsWholeKeysNotCharacters(t *testing.T) {
	a := placeApp(t)
	for _, hint := range []string{filesHint, connectFilterHint, subListHint, resumeHint, memoryEditHint} {
		for _, width := range []int{60, 80, 120, 160} {
			var box editor
			rows, _, _ := draftBlock(&box, a.pal, width, 1, hint, "")
			row := plain(rows[0])
			if strings.Contains(row, glyphMore) {
				t.Fatalf("at %d columns the box drew\n\t%q\nwith a key cut in half; it should drop whole clauses:\n\t%q",
					width, row, prompt+hintFit(hint, width-ansi.StringWidth(prompt)))
			}
			if !strings.Contains(row, "esc") {
				t.Fatalf("at %d columns the box drew\n\t%q\nand lost the way out; `esc` is kept to the last cell there is, as in\n\t%q",
					width, row, prompt+hint)
			}
			if got := ansi.StringWidth(row); got > width {
				t.Fatalf("at %d columns the box is %d cells wide:\n\t%q", width, got, row)
			}
		}
		// AND A WIDE FRAME STILL SAYS THE WHOLE THING.
		var box editor
		rows, _, _ := draftBlock(&box, a.pal, 200, 1, hint, "")
		if got, want := plain(rows[0]), prompt+hint; got != want {
			t.Fatalf("at 200 columns the box drew\n\t%q\nand there was room for all of it:\n\t%q", got, want)
		}
	}
	// AND THE TIER UNDER SIXTY, WHICH IS WHERE THESE LINES ACTUALLY BITE. The
	// longest of them is fifty-five cells, so a sixty-column window carries every
	// one whole; a forty-eight-column split — the frame this wave keeps finding —
	// is where the ruler used to cut, and what goes there is a WHOLE key.
	for _, one := range []struct {
		width int
		hint  string
		want  string
	}{
		{48, filesHint, "filter · enter open · ctrl+r reveal · esc"},
		{40, filesHint, "filter · enter open · esc"},
		{24, filesHint, "filter · esc"},
	} {
		var box editor
		rows, _, _ := draftBlock(&box, a.pal, one.width, 1, one.hint, "")
		if got, want := plain(rows[0]), prompt+one.want; got != want {
			t.Fatalf("at %d columns the box drew\n\t%q\nand it should drop the key nearest the way out, whole:\n\t%q", one.width, got, want)
		}
	}
}

// A GATE CARD KEEPS THE KEY THAT ANSWERS IT.
//
// The pause card takes no letter keys and suspends nothing, so the one row that
// says how three quiet rows are answered is the only thing standing between a
// person and typing their answer into the composer — which somebody did. That
// row read `↑ ↓ pick · enter answers · or keep ty…` at the widths a gate is most
// likely to be met at.
//
// AND ITS LAST CLAUSE IS NOT A WAY OUT, which is the interesting half. `or keep
// typing to steer the planner` is an ALTERNATIVE — a second route to something
// `enter answers` already offers a first route to — so the ladder that protects
// the last clause on a key sheet had to be told that this one is not the clause
// worth protecting ([hintFit]). It goes first, and what a narrow card keeps is
// the pair it exists to be answered with.
func TestAGateCardKeepsTheKeyThatAnswersItAtEveryWidth(t *testing.T) {
	snap := orchRun4()
	snap.Paused = true
	a, _ := orchApp(t, snap)
	drive(t, a, streamEventMsg{gen: a.gen, ev: orchPause()})
	if a.orchOf() == nil || a.orchOf().gate == nil {
		t.Fatal("the pause raised no gate")
	}
	for _, width := range []int{60, 80, 120, 160} {
		a.width = width
		a.touch()
		row := rowSaying(roomText(a), " pick")
		if row == "" {
			t.Fatalf("at %d columns the gate card says nothing about how it is answered:\n%s", width, roomText(a))
		}
		if strings.Contains(row, glyphMore) {
			t.Fatalf("at %d columns the gate card's gestures drew\n\t%q\nwith a gesture cut in half; it should drop the alternative whole:\n\t%q",
				width, row, "↑ ↓ pick · enter answers")
		}
		if !strings.Contains(row, "enter answers") {
			t.Fatalf("at %d columns the gate card's gestures drew\n\t%q\nand lost the key that answers the card; it should read at least\n\t%q",
				width, row, "↑ ↓ pick · enter answers")
		}
		if got := ansi.StringWidth(row); got > width {
			t.Fatalf("at %d columns the gate card's gesture row is %d cells wide:\n\t%q", width, got, row)
		}
	}
	// THE RANK, SAID EXACTLY. Sixty columns is the tier the alternative goes at,
	// and it goes WHOLE — no `or keep`, no `or …`, nothing of it left on the row.
	a.width = 60
	a.touch()
	if got, want := squash(rowSaying(roomText(a), " pick")), "↑ ↓ pick · enter answers"; got != want {
		t.Fatalf("at 60 columns the gate card's gestures drew\n\t%q\nand the alternative should have gone whole:\n\t%q", got, want)
	}
	// AND EVERY WIDER TIER STILL OFFERS IT, so a fix for sixty columns did not
	// cost eighty the sentence that says what typing does instead.
	for _, width := range []int{80, 120, 160, 200} {
		a.width = width
		a.touch()
		if row := rowSaying(roomText(a), " pick"); !strings.Contains(row, "or keep typing to steer the planner") {
			t.Fatalf("at %d columns the gate card drew\n\t%q\nand there was room for the whole sentence:\n\t%q",
				width, row, "↑ ↓ pick · enter answers · or keep typing to steer the planner")
		}
	}
}
