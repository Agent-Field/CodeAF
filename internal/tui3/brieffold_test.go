package tui3

// THE INSTRUCTION FOLDS, AND NOTHING ELSE DOES.
//
// A person who walks into a node's page went there to watch work happen. The
// first block on that page is the brief the node was given, and on a long one it
// was the whole page: sixty lines of assignment above the first tool call. These
// tests hold the fold that answers it — what is on screen when it is shut, that
// the number on its door is true, that both hands reach it, that opening shows
// everything and shuts again — and the two refusals around it: a short
// instruction gets no door at all, and nothing about this is allowed to cost a
// person a keystroke while they are typing.

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"
)

// briefRoom opens node 7's page on one instruction and one reply.
func briefRoom(t *testing.T, instruction string) *app {
	t.Helper()
	a, fake, _ := roomApp(t)
	fake.journal = roomJournal(t,
		`{"type":"message","role":"user","content":`+strconv.Quote(instruction)+`}`,
		`{"type":"message","role":"assistant","content":"On it."}`)
	a.openRoom(7, "Fix the nil-map crash")
	a.touch()
	return a
}

// briefWords is an instruction of a given number of words, each one distinct so
// a test can say which of them survived the fold.
func briefWords(n int) string {
	words := make([]string, n)
	for i := range words {
		words[i] = "word" + strconv.Itoa(i)
	}
	return strings.Join(words, " ")
}

// briefRoom's page, as a reader sees it — and the rows behind it, because a door
// is a row with a hit on it and not a string.
func briefRows(a *app) []row { return a.roomRows(a.bodyWidth()) }

// briefDoor answers the instruction's fold line on the page, if there is one.
func briefDoor(a *app) (row, bool) {
	for _, r := range briefRows(a) {
		if r.hit == hitBrief {
			return r, true
		}
	}
	return row{}, false
}

// briefBodyRows is the instruction as it is drawn: every row of block zero above
// the door.
func briefBodyRows(a *app) []string {
	var out []string
	for _, r := range briefRows(a) {
		if r.hit == hitBrief {
			break
		}
		if r.entry == 0 && strings.TrimSpace(plain(r.text)) != "" {
			out = append(out, plain(r.text))
		}
	}
	return out
}

// briefWrapped is what the instruction wraps to at the width the page draws it —
// the truth every count on the door is measured against.
func briefWrapped(a *app, text string) []string {
	return wrap(text, a.bodyWidth()-userLeadCols)
}

// pressBriefDoor clicks the door where it is on screen. It resolves the screen
// row through [app.rowAt], which is the one resolver a real click goes through
// and the only one that knows a room is open.
func pressBriefDoor(t *testing.T, a *app) {
	t.Helper()
	top := a.bodyTop()
	for y := top; y < top+a.viewHeight(); y++ {
		if r, ok := a.rowAt(y); ok && r.hit == hitBrief {
			drive(t, a, tea.MouseClickMsg{Y: y, Button: tea.MouseLeft})
			drive(t, a, tea.MouseReleaseMsg{Y: y, Button: tea.MouseLeft})
			return
		}
	}
	t.Fatalf("no visible row is the instruction's door:\n%s", roomText(a))
}

// ── THE EMPTINESS LAW: NO DOOR FOR NOTHING ──────────────────────────────────

// AN INSTRUCTION THAT FITS IS DRAWN WHOLE AND WEARS NOTHING. A fold line under
// three lines that are all there is would be furniture claiming to be an
// affordance — the emptiness law, said about a door.
func TestAShortInstructionIsDrawnWholeWithNoDoorUnderIt(t *testing.T) {
	const said = "Fix the nil-map crash in the loader"
	a := briefRoom(t, said)

	if !strings.Contains(roomText(a), said) {
		t.Fatalf("the page does not say what the node was asked for:\n%s", roomText(a))
	}
	if r, ok := briefDoor(a); ok {
		t.Fatalf("a one-line instruction grew a fold line: %q", plain(r.text))
	}
	// And the key falls straight through, because there is nothing here for it to
	// open — which is what lets ctrl+o keep every other meaning it has.
	if a.toggleBriefFold() {
		t.Fatal("the fold key claimed a page with nothing folded on it")
	}
}

// ── SHUT: THE OPENING, AND AN HONEST NUMBER ─────────────────────────────────

// A LONG INSTRUCTION SHOWS ITS OPENING AND SAYS WHAT IT IS HOLDING BACK. Three
// lines, then one dim line counting the rest in the unit a reader can check —
// rendered lines — and naming the key that opens it.
func TestALongInstructionShowsThreeLinesAndCountsTheRest(t *testing.T) {
	said := briefWords(160)
	a := briefRoom(t, said)

	all := briefWrapped(a, said)
	if len(all) <= briefFoldLines {
		t.Fatalf("the fixture is not long enough to fold: it wraps to %d lines", len(all))
	}

	body := briefBodyRows(a)
	if len(body) != briefFoldLines {
		t.Fatalf("a folded instruction drew %d lines, want %d:\n%s",
			len(body), briefFoldLines, strings.Join(body, "\n"))
	}
	// AND THEY ARE THE FIRST LINES, in order. A fold that showed the end of a
	// brief would be showing the acceptance criteria and hiding the errand.
	// The first row wears the turn glyph, which is the mark and not the sentence.
	for i, want := range all[:briefFoldLines] {
		got := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(unindented(body[i])),
			strings.TrimSpace(plain(a.pal.youGlyph()))))
		if got != strings.TrimSpace(want) {
			t.Fatalf("visible line %d is %q, want %q", i, got, want)
		}
	}
	if strings.Contains(roomText(a), "word159") {
		t.Fatalf("the last word of a folded instruction is on screen:\n%s", roomText(a))
	}

	door, ok := briefDoor(a)
	if !ok {
		t.Fatalf("a long instruction has no door under it:\n%s", roomText(a))
	}
	// THE NUMBER IS THE ROWS BEHIND THE DOOR AND NOTHING ELSE, and the sentence
	// is home's, word for word, so a person learns one fold line on this program.
	want := bandFoldGlyph + " …" + strconv.Itoa(len(all)-briefFoldLines) + " more lines" +
		railSep + briefFoldKey
	if got := strings.TrimSpace(plain(door.text)); got != want {
		t.Fatalf("the door says %q, want %q", got, want)
	}
	if w := ansi.StringWidth(plain(door.text)); w > a.bodyWidth() {
		t.Fatalf("the door is %d cells wide in a %d-cell body", w, a.bodyWidth())
	}
}

// ── BOTH HANDS REACH IT, AND IT SHUTS AGAIN ─────────────────────────────────

// THE DOOR OPENS ON A CLICK AND SHUTS ON THE NEXT ONE, and open means ALL of it:
// a fold that stopped at some second cap would be a surface asking twice.
func TestClickingTheDoorOpensTheWholeInstructionAndFoldsItBack(t *testing.T) {
	said := briefWords(160)
	a := briefRoom(t, said)
	all := briefWrapped(a, said)

	pressBriefDoor(t, a)
	if got := len(briefBodyRows(a)); got != len(all) {
		t.Fatalf("the opened instruction is %d lines, want all %d", got, len(all))
	}
	if !strings.Contains(roomText(a), "word159") {
		t.Fatalf("the opened instruction is missing its end:\n%s", roomText(a))
	}
	// AN OPENED FOLD KEEPS ITS DOOR, wearing the open mark and the inverse
	// sentence — which is how it is shut again.
	door, ok := briefDoor(a)
	if !ok {
		t.Fatalf("the opened instruction lost its door:\n%s", roomText(a))
	}
	want := bandFoldOpenGlyph + " …" + strconv.Itoa(len(all)-briefFoldLines) + " fewer" +
		railSep + briefFoldKey
	if got := strings.TrimSpace(plain(door.text)); got != want {
		t.Fatalf("the opened door says %q, want %q", got, want)
	}

	pressBriefDoor(t, a)
	if got := len(briefBodyRows(a)); got != briefFoldLines {
		t.Fatalf("a second press left %d lines on screen, want %d", got, briefFoldLines)
	}
}

// AND THE KEY DOES EXACTLY WHAT THE POINTER DOES. ctrl+o is what the door itself
// names, so a person with no mouse reads the fold line and is told the truth.
func TestTheFoldKeyOpensAndShutsTheInstruction(t *testing.T) {
	said := briefWords(160)
	a := briefRoom(t, said)
	all := briefWrapped(a, said)

	drive(t, a, key(briefFoldKey))
	if got := len(briefBodyRows(a)); got != len(all) {
		t.Fatalf("%s opened %d lines, want all %d", briefFoldKey, got, len(all))
	}
	drive(t, a, key(briefFoldKey))
	if got := len(briefBodyRows(a)); got != briefFoldLines {
		t.Fatalf("%s left %d lines on screen, want %d", briefFoldKey, got, briefFoldLines)
	}
}

// ── THE FOLD IS COUNTED IN CELLS, NOT IN RUNES ──────────────────────────────

// AN INSTRUCTION IN WIDE GLYPHS FOLDS WHERE THE EYE SAYS IT DOES. Every line of
// this fold is measured through [wrap], which breaks on DISPLAY WIDTH — so a
// brief written in a script whose glyphs take two cells shows three lines that
// fit the frame and a count that matches the rows behind them. A fold counted in
// runes or in bytes would have cut these lines at twice the width of the page.
func TestAWideRuneInstructionFoldsByDisplayWidth(t *testing.T) {
	said := strings.TrimSpace(strings.Repeat("日本語の説明文 ", 120))
	a := briefRoom(t, said)

	all := briefWrapped(a, said)
	body := briefBodyRows(a)
	if len(body) != briefFoldLines {
		t.Fatalf("a wide-rune instruction drew %d lines, want %d", len(body), briefFoldLines)
	}
	for i, line := range body {
		plainLine := unindented(line)
		if w := ansi.StringWidth(plainLine); w > a.bodyWidth() {
			t.Fatalf("visible line %d is %d cells wide in a %d-cell body", i, w, a.bodyWidth())
		}
		// THE PROOF THAT IT IS CELLS AND NOT RUNES: these glyphs take two cells
		// each, so a row that fills the frame holds about half as many runes as it
		// does columns. A rune-counted fold would have measured this row as half
		// full and kept going.
		if w := ansi.StringWidth(plainLine); w <= len([]rune(plainLine)) {
			t.Fatalf("visible line %d measures %d cells for %d runes; the fixture is not wide",
				i, w, len([]rune(plainLine)))
		}
	}

	door, ok := briefDoor(a)
	if !ok {
		t.Fatalf("a long wide-rune instruction has no door:\n%s", roomText(a))
	}
	want := "…" + strconv.Itoa(len(all)-briefFoldLines) + " more lines"
	if got := plain(door.text); !strings.Contains(got, want) {
		t.Fatalf("the door says %q, want it to count %q", strings.TrimSpace(got), want)
	}
}

// ── WHAT THE FOLD MAY NOT COST ──────────────────────────────────────────────

// THE VISIBLE LINES ARE PAINTED EXACTLY AS THEY WOULD BE UNFOLDED, byte for
// byte, escape for escape. The cut is made on the WRAPPED lines and before the
// passes that paint them, so everything a row of a message gets — the turn
// glyph, the muted body, a slash command's chip, and whatever the path pass
// makes of it — a row above the fold still gets. Nothing here is a second
// renderer, which is the only way a fold can promise it is hiding rows rather
// than downgrading them.
//
// (What the path pass makes of it in a room is nothing: a node works in its own
// worktree and this surface holds only its own root, so a room draws no path
// doors at all — pathlink.go's [app.linker] states that law and this fold does
// not touch it.)
func TestTheVisibleLinesOfAFoldedInstructionArePaintedUnchanged(t *testing.T) {
	said := "See internal/tui3/pathlink.go first. " + briefWords(160)
	a := briefRoom(t, said)

	shut := append([]row(nil), briefRows(a)...)
	var shutText []string
	for _, r := range shut {
		if r.hit == hitBrief {
			break
		}
		if r.entry == 0 {
			shutText = append(shutText, r.text)
		}
	}
	drive(t, a, key(briefFoldKey))
	var openText []string
	for _, r := range briefRows(a) {
		if r.hit == hitBrief {
			break
		}
		if r.entry == 0 {
			openText = append(openText, r.text)
		}
	}
	if len(openText) <= len(shutText) {
		t.Fatalf("opening the fold added no rows: %d shut, %d open", len(shutText), len(openText))
	}
	for i, want := range shutText {
		if openText[i] != want {
			t.Fatalf("visible row %d changed when the fold opened:\n shut %q\n open %q",
				i, plain(want), plain(openText[i]))
		}
	}
}

// AND NOTHING IN THE CONVERSATION FOLDS. workfold.go's law read from this end:
// the one thing on this surface a fold may never hide is the person's own words,
// and an instruction is exempt only because it is the terms of reference at the
// head of a page about something else. A message out here is the conversation.
func TestNoMessageInTheConversationEverGrowsAFoldDoor(t *testing.T) {
	a, _, _ := taskApp(t)
	typeLine(t, a, briefWords(160))

	for _, r := range a.layout(a.bodyWidth()) {
		if r.hit == hitBrief {
			t.Fatalf("a message in the conversation grew a fold line: %q", plain(r.text))
		}
	}
	if a.toggleBriefFold() {
		t.Fatal("the fold key claimed a conversation")
	}
}

// A LETTER ALWAYS TYPES. The fold is on a chord for exactly this reason: a
// person steering a node types sentences into the box under the page, and a
// surface that spent a letter on a view toggle would eat the first character of
// half of them.
func TestTypingIntoARoomNeverTouchesTheFold(t *testing.T) {
	said := briefWords(160)
	a := briefRoom(t, said)

	typeText(t, a, "open the loader and read it")
	if got := len(briefBodyRows(a)); got != briefFoldLines {
		t.Fatalf("typing moved the fold: %d lines on screen, want %d", got, briefFoldLines)
	}
	if got := string(a.input.value); got != "open the loader and read it" {
		t.Fatalf("the box holds %q; a letter did not type", got)
	}
	// AND THE CHORD STILL WORKS OVER A SENTENCE, which is the other half of the
	// same bargain: a chord carries no text, so it costs the draft nothing.
	drive(t, a, key(briefFoldKey))
	if got := len(briefBodyRows(a)); got == briefFoldLines {
		t.Fatalf("%s did nothing over a full box", briefFoldKey)
	}
	if got := string(a.input.value); got != "open the loader and read it" {
		t.Fatalf("the fold key spent the draft: the box holds %q", got)
	}
}

// AND AN OVERLAY ABOVE THE PAGE KEEPS THE KEY. The project's record is the whole
// screen while it is up, so there is nothing of the room on the frame for a key
// to mean anything to — and a chord that reached through it would be moving a
// fold on a page nobody can see.
func TestAPageOverTheRoomKeepsTheFoldKey(t *testing.T) {
	said := briefWords(160)
	a := briefRoom(t, said)
	before := len(briefBodyRows(a))

	if !openTaskPlaceWithRows(a) {
		t.Skip("this session has no record page to raise")
	}
	a.touch()
	drive(t, a, key(briefFoldKey))
	if !a.taskSheet.open {
		t.Fatalf("%s closed the page it was pressed on", briefFoldKey)
	}
	if got := len(briefBodyRows(a)); got != before {
		t.Fatalf("%s reached through the record page: %d lines, want %d", briefFoldKey, got, before)
	}
}
