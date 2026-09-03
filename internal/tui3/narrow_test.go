package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// ── THE NARROW TIER ─────────────────────────────────────────────────────────
//
// Sixty columns is a split pane, an ssh session from a train, a phone. It is
// not a degraded frame that somebody will widen in a minute — it is the frame a
// developer is stuck with — and the three things below are what a person could
// not DO there: find six of the seven places, read the end of a sentence, and
// tell one conversation from another.

// THE NARROW BAR STILL SAYS WHERE ELSE YOU CAN GO.
//
// At sixty columns the bar used to collapse to the single word `home`, with
// nothing on the frame saying that tasks, standing, memory, spend, search and
// settings existed at all: seven words and their padding are fifty-seven cells,
// the air between them is six more, and sixty overshot by three — straight past
// the middle rung of the ladder, because on a quiet machine no place wears a
// count. The air is what goes now.
func TestTheNarrowBarStillSaysWhereElseYouCanGo(t *testing.T) {
	a := placeApp(t)
	for _, width := range []int{60, 80, 120, 160} {
		bar := plain(a.placeTabBar(width, false, a.pal))
		for _, id := range pages() {
			if !strings.Contains(bar, id.word()) {
				t.Fatalf("at %d columns the bar drew\n\t%q\nand a person cannot reach %q from it; every one of the seven places should be on the row:\n\t%q",
					width, bar, id.word(), plain(a.placeTabBar(200, false, a.pal)))
			}
		}
		if got := ansi.StringWidth(bar); got > width {
			t.Fatalf("at %d columns the bar is %d cells wide and runs past the frame:\n\t%q", width, got, bar)
		}
	}
	// AND THE WIDE TIER DID NOT MOVE. The air between the chips is what a narrow
	// frame gives up, so a frame with room for it still has it — a fix for sixty
	// columns that re-spaced a hundred and sixty would be a fix that cost every
	// other terminal something.
	wide := plain(a.placeTabBar(120, false, a.pal))
	if !strings.Contains(wide, "home   tasks") {
		t.Fatalf("at 120 columns the bar drew\n\t%q\nand the air between two chips is gone; it should read\n\t%q",
			wide, "  home   tasks   standing …")
	}
	if narrow := plain(a.placeTabBar(60, false, a.pal)); !strings.Contains(narrow, "home  tasks") {
		t.Fatalf("at 60 columns the bar drew\n\t%q\nand it should carry every word with the air between the chips given up:\n\t%q",
			narrow, "  home  tasks  standing  memory  spend  search  settings")
	}
}

// A BAR TOO NARROW FOR EVERY WORD SAYS HOW MANY IT DROPPED.
//
// Under the width where all seven fit there is no honest way to draw them, and
// the collapse this ladder exists to prevent is not "fewer words" — it is a row
// that says `home` and lets a person believe that is all there is. So what is
// left ends in a count of the places that are not on it, and the key that
// reaches them is on the foot of every place.
func TestABarTooNarrowForEveryWordSaysHowManyItDropped(t *testing.T) {
	a := placeApp(t)
	for _, tc := range []struct{ width int }{{40}, {24}} {
		bar := plain(a.placeTabBar(tc.width, false, a.pal))
		if !strings.Contains(bar, a.page.word()) {
			t.Fatalf("at %d columns the bar drew\n\t%q\nand dropped the place you are standing in (%q)", tc.width, bar, a.page.word())
		}
		if !strings.Contains(bar, "+") {
			t.Fatalf("at %d columns the bar drew\n\t%q\nand said nothing about the places it could not carry; it should end in a count, as in\n\t%q",
				tc.width, bar, "  home  tasks  +5")
		}
		if got := ansi.StringWidth(bar); got > tc.width {
			t.Fatalf("at %d columns the bar is %d cells wide and runs past the frame:\n\t%q", tc.width, got, bar)
		}
		// AND THE COUNT IS THE TRUTH. Every place that is not spelled on the row
		// is one the count has to stand for, or the row is a second way of
		// hiding them.
		missing := 0
		for _, id := range pages() {
			if !strings.Contains(bar, id.word()) {
				missing++
			}
		}
		if !strings.Contains(bar, "+"+itoa(missing)) {
			t.Fatalf("at %d columns the bar drew\n\t%q\nwhich leaves %d places off the row; the count should read %q",
				tc.width, bar, missing, "+"+itoa(missing))
		}
	}
	// AND A FRAME WITH NO ROOM FOR THE COUNT SAYS NOTHING RATHER THAN RUNNING
	// PAST ITS OWN EDGE, which is the fault this whole ladder exists to prevent.
	for _, width := range []int{8, 12, 16} {
		if bar := plain(a.placeTabBar(width, false, a.pal)); ansi.StringWidth(bar) > width {
			t.Fatalf("at %d columns the bar is %d cells wide and runs past the frame:\n\t%q", width, ansi.StringWidth(bar), bar)
		}
	}
	// AND A COUNT NEVER APPEARS ON A BAR THAT CARRIED EVERYTHING. A `+0` beside
	// seven words would be furniture, and furniture is what people stop seeing.
	for _, width := range []int{60, 80, 120, 160} {
		if bar := plain(a.placeTabBar(width, false, a.pal)); strings.Contains(bar, "+") {
			t.Fatalf("at %d columns every place is on the bar and it still counts something:\n\t%q", width, bar)
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
func TestTheNarrowFootDropsWholeHintsAndNeverSlicesOne(t *testing.T) {
	sentence := takeoverArmedWord("open in another window")
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
// SHRINKS: fix one, delete its line. Both entries are held by other lanes of
// this wave and are written up as rows of docs/design/polish/audit-narrow.md.
var hintsStillCutByCharacter = map[string]string{
	"jobpage.go":    "the job page's key row — the jobs lane holds this file",
	"taskrecord.go": "the task card's key row — the task lane holds this file",
}

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
		a.openHome()
		frame := homeText(a)
		if strings.Contains(frame, "927D303242f9d00e") || strings.Contains(frame, "927d303242f9d00e") {
			t.Fatalf("at %d columns home drew\n\t%q\nwith the folder's id where the name goes; it should read\n\t%q",
				width, rowSaying(frame, "927"), "○ "+unnamedConversationWord+"   alpha  here")
		}
		if !strings.Contains(frame, unnamedConversationWord) {
			t.Fatalf("at %d columns home drew\n%s\nand nothing on it names the conversation with no title; the row should read\n\t%q",
				width, frame, "○ "+unnamedConversationWord)
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
		t.Fatalf("an id-shaped folder was named %q, and a name that cannot be had is said in words: %q", got, unnamedConversationWord)
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
