package tui3

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── THE MARKS THEMSELVES ────────────────────────────────────────────────────

// EVERY FAMILY HAS A MARK. The engine's vocabulary and this table are two halves
// of one thing, and a family with no mark would draw the bucket while claiming
// to be something else — silently, on the one row a person is looking at.
func TestEveryActionFamilyHasAMark(t *testing.T) {
	for _, category := range session.ActionCategories() {
		if _, ok := actionMarks[category]; !ok {
			t.Errorf("the %q family has no mark — add it to actionMarks", category)
		}
	}
	if len(actionMarks) != len(session.ActionCategories()) {
		t.Errorf("the icon table has %d marks for %d families — one of them says nothing",
			len(actionMarks), len(session.ActionCategories()))
	}
}

// ONE CELL, BOTH TIERS, BOTH RULERS. This is the shared vocabulary's own width
// law (internal/tui2/tokens' glyph.go) applied to a table that lives outside it:
// flipping to the ASCII tier must not move one column, and a mark that measured
// two cells anywhere would push the sentence beside it off the frame on the
// narrow widths this block is budgeted for.
func TestEveryActionMarkIsOneCellInBothTiers(t *testing.T) {
	for category, mark := range actionMarks {
		for _, side := range []struct {
			tier  string
			glyph string
		}{{"unicode", mark.glyph}, {"ascii", mark.ascii}} {
			if side.glyph == "" {
				t.Errorf("%s draws nothing in the %s tier", category, side.tier)
				continue
			}
			if n := utf8.RuneCountInString(side.glyph); n != 1 {
				t.Errorf("%s (%s) is %d runes, must be 1", category, side.tier, n)
				continue
			}
			if w := ansi.StringWidth(side.glyph); w != 1 {
				t.Errorf("%s (%s, %q): grapheme width %d, must be 1",
					category, side.tier, side.glyph, w)
			}
			if w := ansi.StringWidthWc(side.glyph); w != 1 {
				t.Errorf("%s (%s, %q): wcwidth %d, must be 1",
					category, side.tier, side.glyph, w)
			}
		}
		if a, b := ansi.StringWidth(mark.glyph), ansi.StringWidth(mark.ascii); a != b {
			t.Errorf("%s: %d cells unicode, %d cells ascii — the tier moved this column",
				category, a, b)
		}
	}
}

// NO EMOJI, NO VARIATION SELECTOR, AND NOTHING THE SHARED TABLE HAS ALREADY
// THROWN OUT. The ban list is the measured one in internal/tui2/tokens; reading
// it here rather than restating it is what keeps a rune that was refused over
// there from arriving over here.
func TestActionMarksRefuseTheBannedGlyphs(t *testing.T) {
	banned := map[rune]string{}
	for _, b := range tokens.BannedGlyphs {
		banned[b.Rune] = b.Reason
	}
	for category, mark := range actionMarks {
		for _, spelling := range []string{mark.glyph, mark.ascii} {
			for _, r := range spelling {
				if why, out := banned[r]; out {
					t.Errorf("%s draws %U, which the vocabulary bans: %s", category, r, why)
				}
				if r == '️' || r == '︎' {
					t.Errorf("%s carries a variation selector", category)
				}
				if r >= 0x1F000 {
					t.Errorf("%s draws %U, which is in the emoji planes", category, r)
				}
			}
		}
	}
}

// THIRTEEN MARKS MEAN THIRTEEN THINGS. Two families sharing a character is a
// gutter that cannot be read, in either tier.
func TestEveryActionMarkIsDistinct(t *testing.T) {
	for _, tier := range []struct {
		name string
		of   func(actionMark) string
	}{
		{"unicode", func(m actionMark) string { return m.glyph }},
		{"ascii", func(m actionMark) string { return m.ascii }},
	} {
		seen := map[string]session.ActionCategory{}
		for category, mark := range actionMarks {
			spelling := tier.of(mark)
			if first, taken := seen[spelling]; taken {
				t.Errorf("%s: %q says both %q and %q", tier.name, spelling, first, category)
				continue
			}
			seen[spelling] = category
		}
	}
}

// THE ACT OF CHECKING IS NOT ITS VERDICT. `test` earns an action mark like every
// other family; a tick there would be the block reporting an outcome, which it
// never does, and would read as "this step passed" on a step that is still
// running.
func TestTheTestFamilyDrawsAnActionAndNeverAVerdict(t *testing.T) {
	mark := actionMarks[session.ActionTest]
	for _, verdict := range []string{
		tokens.GlyphSettled, tokens.GlyphFailed, tokens.GlyphNeedsHuman, "✓", "✔", "x", "X",
	} {
		if mark.glyph == verdict || mark.ascii == verdict {
			t.Errorf("the test family draws the verdict mark %q", verdict)
		}
	}
}

// ── WHICH FAMILY A STEP IS ──────────────────────────────────────────────────

// THE FLOOR IS THE TOOLS AND THE NARRATOR ONLY REFINES IT.
func TestAStepTakesItsFamilyFromTheNarratorAndOtherwiseFromItsTools(t *testing.T) {
	es := []entry{
		{kind: entryTool, tool: "grep", turn: 1, status: toolOK},
		{kind: entryTool, tool: "read", turn: 1, status: toolOK},
	}
	c := caption{start: 0, end: 2, calls: 2}
	if got := stepCategory(c, es); got != session.ActionSearch {
		t.Fatalf("with no narration the family is the tools': got %q, want %q", got, session.ActionSearch)
	}
	c.category = session.ActionTest
	if got := stepCategory(c, es); got != session.ActionTest {
		t.Fatalf("the narrator's family did not win: got %q", got)
	}
}

// A REPAINT MAY NOT CHANGE A MARK. The derivation is pure over the entry list,
// which is what makes the gutter hold still while the sentence beside it is
// rewritten by a narrator that arrived late.
func TestAStepsFamilyIsTheSameOnEveryRepaint(t *testing.T) {
	es := liveStepsFixture()
	captions := deriveCaptions(es, 1)
	if len(captions) == 0 {
		t.Fatal("the fixture derived no steps")
	}
	first := make([]session.ActionCategory, len(captions))
	for i, c := range captions {
		first[i] = stepCategory(c, es)
	}
	for paint := 0; paint < 5; paint++ {
		again := deriveCaptions(es, 1)
		for i, c := range again {
			if got := stepCategory(c, es); got != first[i] {
				t.Fatalf("step %d changed family on repaint %d: %q then %q", i, paint, first[i], got)
			}
		}
	}
}

// AND A CONVERSATION READ BACK OFF DISK DRAWS THE MARKS ITS TOOLS ALWAYS
// IMPLIED. A replayed entry carries no narration — the sentence is recomposed
// from the calls — so the family has to come from the same place, and it has to
// be the same answer the live turn drew.
func TestAReopenedStepDerivesTheSameFamilyWithNoNarration(t *testing.T) {
	live := liveStepsFixture()
	for i := range live {
		if live[i].kind == entryTool {
			live[i].caption = "run | poking at something"
			live[i].captionCat = session.ActionRun
		}
	}
	// The same rows as they come back from a file: no narrator override at all.
	reopened := liveStepsFixture()
	for i := range reopened {
		reopened[i].caption, reopened[i].captionCat = "", ""
		if reopened[i].kind == entryTool && reopened[i].status == toolRunning {
			reopened[i].status = toolOK
			reopened[i].ended = reopened[i].began.Add(time.Second)
		}
	}

	liveCaps, backCaps := deriveCaptions(live, 1), deriveCaptions(reopened, 0)
	if len(liveCaps) != len(backCaps) {
		t.Fatalf("the two readings found %d and %d steps", len(liveCaps), len(backCaps))
	}
	for i := range liveCaps {
		if got := stepCategory(liveCaps[i], live); got != session.ActionRun {
			t.Fatalf("live step %d ignored the narrator's family: %q", i, got)
		}
		back := stepCategory(backCaps[i], reopened)
		want := session.ActionCategoryForTools([]string{reopened[backCaps[i].end-1].tool})
		if back != want {
			t.Fatalf("reopened step %d drew %q, want the tool-derived %q", i, back, want)
		}
		if back == "" {
			t.Fatalf("reopened step %d drew no family at all", i)
		}
	}
}

// ── THE GUTTER ON THE FRAME ─────────────────────────────────────────────────

// liveStepRowsOf is the compact block's own rows, painted, at a width.
//
// It prepares the deck exactly as [app.layout] does — folds, then the hierarchy
// stamp, then the captions — because the stamp is what demotes a narrating line
// into a step title, and a run derived without it is broken at every one of
// them.
func liveStepRowsOf(t *testing.T, a *app, width int) []row {
	t.Helper()
	d := a.bodyDeck()
	folds := a.deckFolds(d)
	stampHierarchy(d.entries, folds)
	d.captions = deriveCaptions(d.entries, d.runningTurn)
	works := deriveLiveWork(d)
	if len(works) != 1 {
		t.Fatalf("the page has %d compact blocks, want 1", len(works))
	}
	for _, w := range works {
		return a.liveStepBlock(w, width, d.entries)
	}
	return nil
}

// ONE MARK PER STEP, ON ITS FIRST LINE, AND THE SAME COLUMN FOR ALL OF THEM.
func TestTheCompactBlockDrawsOneMarkPerStepInAFixedGutter(t *testing.T) {
	a := liveStepsApp(t)
	rows := liveStepRowsOf(t, a, a.width)
	if len(rows) == 0 {
		t.Fatal("the compact block drew nothing")
	}

	marks := map[string]bool{}
	for _, mark := range actionMarks {
		marks[mark.glyph] = true
	}
	body := a.width - workIndentCols(a.width)
	found := 0
	for i, r := range rows {
		line := plain(r.text)
		if utf8.RuneCountInString(line) < actionGutter {
			t.Fatalf("row %d is shorter than the gutter: %q", i, line)
		}
		lead, _ := utf8.DecodeRuneInString(line)
		second := []rune(line)[1]
		if second != ' ' {
			t.Errorf("row %d does not spend the whole gutter: %q", i, line)
		}
		if marks[string(lead)] {
			found++
			continue
		}
		if lead != ' ' {
			t.Errorf("row %d leads with %q, which is neither a mark nor the wrap's blank", i, line)
		}
		if ansi.StringWidth(line) > body {
			t.Errorf("row %d is %d cells wide in a %d-cell body: %q",
				i, ansi.StringWidth(line), body, line)
		}
	}
	if found == 0 {
		t.Fatalf("no row carried a mark:\n%s", strings.Join(plainRows(a), "\n"))
	}
	if found > liveStepRows {
		t.Fatalf("%d marks for a %d-row budget — a step drew more than one", found, liveStepRows)
	}
}

// THE FIXTURE'S OWN STEPS, NAMED. It reads a file, greps, runs a suite and then
// runs git — so the block's marks are read, search, run, run, newest last, and
// the window's three-row budget keeps the newest three.
func TestTheCompactBlockDrawsTheFamilyEachStepActuallyIs(t *testing.T) {
	a := liveStepsApp(t)
	rows := liveStepRowsOf(t, a, a.width)
	var got []string
	for _, r := range rows {
		line := []rune(plain(r.text))
		if len(line) > 0 && string(line[0]) != " " {
			got = append(got, string(line[0]))
		}
	}
	want := []string{
		actionMarks[session.ActionSearch].glyph, // grep
		actionMarks[session.ActionRun].glyph,    // bash: the suite
		actionMarks[session.ActionRun].glyph,    // bash: git status, still running
	}
	if strings.Join(got, "") != strings.Join(want, "") {
		t.Fatalf("marks %q, want %q\n%s", got, want, strings.Join(plainRows(a), "\n"))
	}
}

// A WRAPPED CAPTION STAYS IN ITS COLUMN. This is the whole reason the gutter is
// a constant: a step whose sentence needs two lines spends the same two cells on
// the second one, as blanks, so the block does not zig-zag.
func TestAWrappedStepKeepsItsSentenceInOneColumn(t *testing.T) {
	a := liveStepsApp(t)
	a.width = 22
	rows := liveStepRowsOf(t, a, a.width)
	if len(rows) < 2 {
		t.Fatalf("a narrow frame drew %d rows, want a wrap", len(rows))
	}
	wrapped := false
	for _, r := range rows {
		line := []rune(plain(r.text))
		if len(line) < actionGutter {
			t.Fatalf("row shorter than the gutter: %q", string(line))
		}
		if line[0] == ' ' && line[1] == ' ' {
			wrapped = true
		}
		if len(line) > actionGutter && line[actionGutter] == ' ' {
			t.Errorf("the sentence does not start in the gutter's own column: %q", string(line))
		}
	}
	if !wrapped {
		t.Fatal("nothing wrapped at the narrow width, so the claim was not tested")
	}
}

// THE NARROWEST FRAME STILL DRAWS A MARK AND STILL FITS. The block's floor is a
// body of 8 columns; the gutter comes off before the wrap, never after it.
func TestTheGutterSurvivesTheNarrowestFrame(t *testing.T) {
	for _, width := range []int{24, 20, 16, 12} {
		a := liveStepsApp(t)
		a.width = width
		rows := liveStepRowsOf(t, a, width)
		if len(rows) == 0 {
			t.Fatalf("width %d drew nothing", width)
		}
		marked := false
		for _, r := range rows {
			line := []rune(plain(r.text))
			if len(line) < actionGutter {
				t.Fatalf("width %d: row shorter than the gutter: %q", width, string(line))
			}
			if line[0] != ' ' {
				marked = true
			}
		}
		if !marked {
			t.Errorf("width %d drew no mark at all", width)
		}
	}
}

// THE MARK NEVER MOVES. The live step's words shimmer; its icon holds still, so
// a frame later the gutter is byte-for-byte what it was.
func TestTheMarksDoNotAnimate(t *testing.T) {
	a := liveStepsApp(t)
	// THE SHIMMER NEEDS TRUECOLOUR TO MOVE AT ALL (captionmotion.go), so the
	// palette is raised here — a lower tier holds the whole line still, which
	// would let this test pass by drawing nothing.
	a.pal = newPalette(tokens.TrueColor, false)
	a.touch()
	first := liveStepRowsOf(t, a, a.width)
	gutters := make([]string, len(first))
	for i, r := range first {
		gutters[i] = string([]rune(plain(r.text))[:actionGutter])
	}
	moved := false
	for paint := 0; paint < shimmerPeriod; paint++ {
		a.paints++
		again := liveStepRowsOf(t, a, a.width)
		if len(again) != len(first) {
			t.Fatalf("the block changed height on paint %d", paint)
		}
		for i, r := range again {
			if got := string([]rune(plain(r.text))[:actionGutter]); got != gutters[i] {
				t.Fatalf("the mark on row %d moved on paint %d: %q then %q", i, paint, gutters[i], got)
			}
			if r.text != first[i].text {
				moved = true
			}
		}
	}
	if !moved {
		t.Fatal("nothing on the block moved at all, so the still-mark claim was not tested")
	}
}

// THE ASCII TIER DRAWS A MARK OF THE SAME WIDTH. A screen-reader terminal keeps
// the column, so the block reads the same shape with none of the shapes.
func TestTheScreenReaderTierKeepsTheGutter(t *testing.T) {
	a := liveStepsApp(t)
	a.linear = true
	rows := liveStepRowsOf(t, a, a.width)
	if len(rows) == 0 {
		t.Fatal("the linear tier drew nothing")
	}
	ascii := map[string]bool{}
	for _, mark := range actionMarks {
		ascii[mark.ascii] = true
	}
	marked := false
	for _, r := range rows {
		line := []rune(plain(r.text))
		if len(line) < actionGutter {
			t.Fatalf("row shorter than the gutter: %q", string(line))
		}
		if lead := string(line[0]); lead != " " {
			if !ascii[lead] {
				t.Errorf("the linear tier drew %q, which is not one of its own marks", lead)
			}
			marked = true
		}
	}
	if !marked {
		t.Fatal("the linear tier drew no mark")
	}
}

// ── WHAT THE ICONS MAY NOT CHANGE ───────────────────────────────────────────

// OPENING THE WORK STILL PUTS THE OUTLINE UP, and the compact block with its
// gutter goes away — the marks are the SHUT state's own vocabulary and do not
// follow the reader into the outline, where every caption already leads with the
// disclosure triangle it is opened by.
func TestOpeningTheWorkStillReplacesTheCompactBlock(t *testing.T) {
	a := liveStepsApp(t)
	before := livePage(a)
	if !strings.Contains(before, actionMarks[session.ActionRun].glyph) {
		t.Fatalf("the shut block drew no mark:\n%s", before)
	}
	showLiveWork(t, a)
	after := livePage(a)
	if !strings.Contains(after, "ctrl+e") {
		t.Fatalf("the opened work lost its way back:\n%s", after)
	}
	if !strings.Contains(after, "git status --porcelain") {
		t.Fatalf("the opened work does not show the calls:\n%s", after)
	}
	// AND THE OUTLINE'S OWN GUTTER IS THE DISCLOSURE TRIANGLE, unchanged. The
	// marks belong to the shut block; a second one in front of a row that is
	// already opened by its leading glyph would be two gutters saying two
	// different things about one row.
	for _, line := range strings.Split(after, "\n") {
		if !strings.Contains(line, "Running the suite") {
			continue
		}
		if !strings.Contains(line, "▸") && !strings.Contains(line, "▾") {
			t.Fatalf("an outline row lost its disclosure mark: %q", line)
		}
	}
}

// AND COMPLETION STILL COLLAPSES IT. The marks live inside the compact block, so
// a finished turn has none of them — the chip is the only thing left.
func TestAFinishedTurnKeepsNoMarks(t *testing.T) {
	a := liveStepsApp(t)
	showLiveWork(t, a)
	a.collapseLiveWork(a.turn)
	a.state = stateIdle
	a.entries = append(a.entries, entry{kind: entryAssistant, text: "the loader reads the tree up front.", turn: 1, settled: true})
	a.touch()
	page := livePage(a)
	for category, mark := range actionMarks {
		// The ASCII-bodied marks (`+` for create, `$` for run) are characters a
		// page carries for a hundred honest reasons — the shared vocabulary
		// gives up its claim on ASCII slots for exactly this reason — so the
		// shapes are what this asks about.
		if r, _ := utf8.DecodeRuneInString(mark.glyph); r < utf8.RuneSelf {
			continue
		}
		if strings.Contains(page, mark.glyph) {
			t.Fatalf("a settled turn still draws the %q mark:\n%s", category, page)
		}
	}
}
