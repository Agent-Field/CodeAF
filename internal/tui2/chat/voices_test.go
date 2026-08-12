package chat

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/clip"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

// §3b's transcript, tested where it is decided: at the rows.
//
// The law is about what is NOT there — no speaker headers, no names, no glyph
// on the answer — so most of these tests are absence tests, and every one of
// them also asserts that the words themselves survived. A dressing that dropped
// the label and the sentence with it would pass the first half of every check
// here, which is why none of them stops at the first half.

// -- the three voices ---------------------------------------------------------

// The answer is the unmarked voice: no label, no glyph, no header row.
func TestTheAnswerIsUnmarked(t *testing.T) {
	app := dressedApp(t)
	out := whole(t, app, 80)

	if strings.Contains(out, "aforge") {
		t.Fatalf("the answer is still wearing a speaker name:\n%s", out)
	}
	if strings.Contains(out, tokens.GlyphPromptChat+" aforge") {
		t.Fatalf("the answer is still wearing a speaker row:\n%s", out)
	}
	// Unmarked is not unindented: the words sit at the shared left edge, which
	// is where the reader's own words sit too.
	if !strings.Contains(out, "\n"+strings.Repeat(" ", bodyIndent)+"Here is the plan:") {
		t.Fatalf("the answer left the shared left edge:\n%s", out)
	}
	// The model word rode on the header that is gone; it must not have grown a
	// row of its own on the way out.
	if strings.Contains(out, "claude-k3\n") && strings.Contains(out, "› claude-k3") {
		t.Fatalf("the model word became a row of the transcript:\n%s", out)
	}
}

// The reader's own words hang the prompt glyph they typed at in the gutter, and
// nothing names them.
func TestTheReadersTurnHangsItsPromptInTheGutter(t *testing.T) {
	app := dressedApp(t)
	out := whole(t, app, 80)

	if strings.Contains(out, tokens.GlyphPromptChat+" you") {
		t.Fatalf("the reader is still being named every turn:\n%s", out)
	}
	if !strings.Contains(out, "\n"+tokens.GlyphPromptChat+" what is the plan for navctx?") {
		t.Fatalf("the reader's turn does not hang its prompt in the gutter:\n%s", out)
	}
}

// Dim, and dimmer than the answer beside it: the reader's words are the quiet
// short ones (§3b), and the ramp has exactly three tiers on this surface (§16).
func TestTheReadersWordsAreDimmerThanTheAnswer(t *testing.T) {
	backend := &fakeBackend{}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleUser, Body: "the question"})
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent, Body: "the answer"})
	app := New(Options{
		Backend: backend, Session: testSession, Profile: tokens.TrueColor,
		Now: fixedNow, PollEvery: 1, Root: "/tmp/room", Home: "/tmp",
	})
	poll(t, app)

	style := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	user := strings.Join(app.transcript.Block(0).Rows(60), "\n")
	agent := strings.Join(app.transcript.Block(1).Rows(60), "\n")

	// Word by word, which is how the prose renderer paints.
	if !strings.Contains(user, style.PaintToken("question", tokens.TextSecondary)) {
		t.Fatalf("the reader's words are not at the dim tier: %q", user)
	}
	if strings.Contains(user, style.PaintToken("question", tokens.TextPrimary)) {
		t.Fatalf("the reader's words are at the answer's tier: %q", user)
	}
	if !strings.Contains(agent, style.PaintToken("answer", tokens.TextPrimary)) {
		t.Fatalf("the answer is not at the primary tier: %q", agent)
	}
}

// A JOB CARD WEARS ITS NAME AS A TITLE ROW: the state glyph in the gutter at
// column 0, the job's name at the content edge at column 2, and everything the
// card says about itself on the lines under it (§3, §20).
//
// THIS REPLACES THE NAME CHIP for this row, and the reason is geometry rather
// than taste. §3b's chip rides in front of a guest's first line, which was the
// right shape when a node-anchored row was a SENTENCE work had spoken. It stopped
// being one when jobcard.go folded every such row into one evolving CARD (§3:
// "one block per job … evolving IN PLACE"), and a card is not a sentence:
//
//   - The chip hung in front of whatever prose run was drawn first, so the
//     moment a status arrived the name moved DOWN the block to sit in front of
//     it and the card's prompt was left stranded above its own title.
//   - The run under the chip hung at the chip's WIDTH, so a card's second line
//     started at whatever column the job's name happened to end at — a left edge
//     that changed per job, which is exactly what §20's "the eye learns it once"
//     forbids.
//   - The delivery card this block becomes has always been a title row, so the
//     card changed grammar at the one moment §3 promises it evolves in place.
//
// §3b's chip is untouched as the law for a guest voice; what changed is that
// this row is no longer one.
func TestAJobCardWearsItsNameAsATitleRow(t *testing.T) {
	app := dressedApp(t)
	out := whole(t, app, 80)

	title, status := "", ""
	rows := strings.Split(ansi.Strip(out), "\n")
	for i, row := range rows {
		if strings.Contains(row, "wisp-parity") && !strings.Contains(row, "settled") {
			title = row
			if i+1 < len(rows) {
				status = rows[i+1]
			}
			break
		}
	}
	if title == "" {
		t.Fatalf("the card lost its name:\n%s", out)
	}
	// The marker is in the CARD's gutter and the name is at the card's content
	// edge. The card stands one depth in — §20 lets an accent edge live in the
	// gutter, and the delivery card's `▎` does — so its own two marker cells
	// begin at [cardLane].
	if !strings.HasPrefix(title, strings.Repeat(" ", cardLane)+tokens.GlyphWorking+" wisp-parity") {
		t.Fatalf("the card's glyph is not in the gutter column: %q", title)
	}
	// The title row is the title. The status is the line under it, at the edge.
	if strings.Contains(title, "reworking NavCtx") {
		t.Fatalf("the status is riding on the title row: %q", title)
	}
	if !strings.Contains(status, "reworking NavCtx") {
		t.Fatalf("the status is not the line under the title: %q", status)
	}
	if at := strings.Index(status, "reworking"); at != blocks.ContentEdge+cardLane {
		t.Fatalf("the status line starts at column %d, want the card's content edge at %d: %q",
			at, blocks.ContentEdge+cardLane, status)
	}
	// 5.14's never-shown tier survives the re-dress.
	if strings.Contains(out, "task/wisp-parity") {
		t.Fatalf("a raw node id reached the transcript:\n%s", out)
	}
}

// §15's own test, applied to the frame: one blank line between turns is the
// only separation, and every block still closes on one.
func TestOneBlankLineIsTheOnlySeparationBetweenTurns(t *testing.T) {
	app := dressedApp(t)
	for i := 0; i < app.transcript.Len(); i++ {
		rows := app.transcript.Block(i).Rows(80)
		if len(rows) == 0 || rows[len(rows)-1] != "" {
			t.Fatalf("block %d does not close with a blank row: %q", i, rows)
		}
		if len(rows) > 1 && rows[len(rows)-2] == "" {
			t.Fatalf("block %d closes on two blank rows: %q", i, rows)
		}
	}
}

// -- the long-turn fold -------------------------------------------------------

// longTurn is a reader's message with more lines than the fold shows.
func longTurn() string {
	lines := make([]string, 0, 9)
	for _, word := range []string{"one", "two", "three", "four", "five", "six", "seven", "eight"} {
		lines = append(lines, "line "+word)
	}
	return strings.Join(lines, "\n")
}

// §3b: a long turn of the reader's own stands three lines high with the rest
// behind `▸ N lines`, and opens on the same door (§10).
func TestALongReaderTurnFoldsAndOpensAgain(t *testing.T) {
	backend := &fakeBackend{}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleUser, Body: longTurn()})
	app := newTestApp(backend, nil, nil)
	poll(t, app)

	block, ok := app.transcript.Block(0).(*messageBlock)
	if !ok {
		t.Fatalf("the reader's turn is not a message block: %T", app.transcript.Block(0))
	}
	folded := whole(t, app, 80)
	if !strings.Contains(folded, "line one") {
		t.Fatalf("the fold ate the words above it:\n%s", folded)
	}
	if strings.Contains(folded, "line eight") {
		t.Fatalf("nothing was folded:\n%s", folded)
	}
	if !strings.Contains(folded, blocks.CollapsedMark+" 5 lines") {
		t.Fatalf("the fold does not say how much it is holding:\n%s", folded)
	}
	// Three lines above the fold, and the fold's own row under them.
	rows := block.Rows(80)
	if got := len(rows); got != longTurnRows+2 { // + the hint row and the blank
		t.Fatalf("a folded turn drew %d rows, want %d: %q", got, longTurnRows+2, rows)
	}

	app.toggleFold(block)
	opened := whole(t, app, 80)
	if !strings.Contains(opened, "line eight") {
		t.Fatalf("the fold would not open:\n%s", opened)
	}
	if !strings.Contains(opened, blocks.ExpandedMark) {
		t.Fatalf("an open fold shows no witness:\n%s", opened)
	}

	app.toggleFold(block)
	if reclosed := whole(t, app, 80); strings.Contains(reclosed, "line eight") {
		t.Fatalf("the fold would not close again:\n%s", reclosed)
	}
}

// §10's expand law in full: the fold row is the door, and once open ANY row of
// the turn closes it again.
func TestAnOpenedTurnClosesFromAnyRowInsideIt(t *testing.T) {
	backend := &fakeBackend{}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleUser, Body: longTurn()})
	app := newTestApp(backend, nil, nil)
	poll(t, app)
	block := app.transcript.Block(0).(*messageBlock)

	if block.isFoldRow(0, 80) {
		t.Fatal("a folded turn's first line is a door; only its fold row is")
	}
	if !block.isFoldRow(longTurnRows, 80) {
		t.Fatal("the fold's own row is not the door")
	}

	app.toggleFold(block)
	for line := 0; line <= longTurnRows; line++ {
		if !block.isFoldRow(line, 80) {
			t.Fatalf("row %d of an opened turn does not close it", line)
		}
	}
}

// §5's no-truncation law: an answer is never folded. What the assistant said is
// the record, and a record behind a chevron is a surface deciding for the reader.
func TestAnAnswerNeverFolds(t *testing.T) {
	backend := &fakeBackend{}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent, Body: longTurn()})
	app := newTestApp(backend, nil, nil)
	poll(t, app)

	block := app.transcript.Block(0).(*messageBlock)
	if block.foldsBody || block.collapsible {
		t.Fatal("a long answer was made foldable")
	}
	out := whole(t, app, 80)
	for _, word := range []string{"line one", "line eight"} {
		if !strings.Contains(out, word) {
			t.Fatalf("the answer lost %q:\n%s", word, out)
		}
	}
	if strings.Contains(out, blocks.CollapsedMark+" ") {
		t.Fatalf("an answer is offering a fold:\n%s", out)
	}
}

// -- the copy chip ------------------------------------------------------------

// The copy chip is driven the way a hand drives it: motion through the shell,
// which is what decides which pane the pointer is over and repaints, and then a
// click at a cell of the frame. Calling the pane's own Hover would skip the one
// seam that has to work — the shell's — and would leave the frame it asserts on
// cached from before the pointer arrived.

const chipFrameWidth, chipFrameHeight = 80, 20

// chipFrame is the picture, unpainted.
func chipFrame(app *App) []string {
	return strings.Split(ansi.Strip(app.Frame(chipFrameWidth, chipFrameHeight)), "\n")
}

// hoverRow finds the frame row a phrase is drawn on.
func hoverRow(t *testing.T, app *App, phrase string) int {
	t.Helper()
	rows := chipFrame(app)
	for i, line := range rows {
		if strings.Contains(line, phrase) {
			return i
		}
	}
	t.Fatalf("no row carries %q:\n%s", phrase, strings.Join(rows, "\n"))
	return -1
}

// restPointer rests the pointer on a cell, through the shell.
func restPointer(app *App, x, y int) {
	app.Update(tea.MouseMotionMsg{X: x, Y: y})
}

// clickCell clicks a cell, through the shell, and hands back what it returned.
func clickCell(app *App, x, y int) tea.Cmd {
	_, cmd := app.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y})
	return cmd
}

// clipboardText walks a command — batched or not — for the bytes it puts on the
// clipboard, read back through the package that put them there.
func clipboardText(cmd tea.Cmd) (string, bool) {
	if cmd == nil {
		return "", false
	}
	msg := cmd()
	if text, ok := clip.Written(msg); ok {
		return text, true
	}
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return "", false
	}
	for _, one := range batch {
		if text, found := clipboardText(one); found {
			return text, true
		}
	}
	return "", false
}

// §3b: hover a message, get a dim `copy` chip at its right edge; click it and
// the words leave through OSC 52.
func TestHoveringAMessageSurfacesACopyChipThatWritesOSC52(t *testing.T) {
	const said = "Here is the plan in one line."
	backend := &fakeBackend{}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent, Body: said})
	app := newTestApp(backend, nil, nil)
	poll(t, app)

	y := hoverRow(t, app, "Here is the plan")
	if strings.Contains(strings.Join(chipFrame(app), "\n"), copyWord) {
		t.Fatal("the copy chip is on the frame before anything was pointed at")
	}

	restPointer(app, 4, y)
	row := chipFrame(app)[y]
	if !strings.Contains(row, copyWord) {
		t.Fatalf("hovering surfaced no copy chip: %q", row)
	}
	if !strings.HasSuffix(strings.TrimRight(row, " "), copyWord) {
		t.Fatalf("the chip is not at the row's right edge: %q", row)
	}

	block := app.transcript.Block(0).(*messageBlock)
	text, ok := clipboardText(clickCell(app, block.chipCol, y))
	if !ok {
		t.Fatal("clicking the chip wrote nothing to the clipboard")
	}
	if text != said {
		t.Fatalf("the clipboard was given %q, want the words that were said", text)
	}
	// The sequence itself, which is what the terminal will be handed.
	if got := clip.Copy(text); !strings.HasPrefix(got, "\x1b]52;c;") {
		t.Fatalf("the write is not an OSC 52 clipboard write: %q", got)
	}
	// And the chip says so, for a frame.
	if !strings.Contains(strings.Join(chipFrame(app), "\n"), copiedWord) {
		t.Fatal("the copy left no proof in the chip's own cell")
	}
}

// The proof expires on its own tick, and the chip goes back to being an offer.
func TestTheCopiedProofRevertsOnItsTick(t *testing.T) {
	backend := &fakeBackend{}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent, Body: "one line"})
	app := newTestApp(backend, nil, nil)
	poll(t, app)

	y := hoverRow(t, app, "one line")
	restPointer(app, 4, y)
	chipFrame(app) // the chip's column is a fact about the frame it was drawn in
	block := app.transcript.Block(0).(*messageBlock)
	clickCell(app, block.chipCol, y)
	if !block.copied {
		t.Fatal("the click left no proof to expire")
	}

	app.Update(copiedMsg{id: block.ID()})
	if block.copied {
		t.Fatal("the proof outlived its tick")
	}
	if !block.copyHover {
		t.Fatal("the chip left with the proof, though the pointer is still on the message")
	}
}

// A click on the words is a click on the conversation, never on the chip.
func TestClickingAMessagesWordsCopiesNothing(t *testing.T) {
	backend := &fakeBackend{}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent, Body: "one line"})
	app := newTestApp(backend, nil, nil)
	poll(t, app)

	y := hoverRow(t, app, "one line")
	restPointer(app, 4, y)
	if _, ok := clipboardText(clickCell(app, 3, y)); ok {
		t.Fatal("clicking a message's own words copied it")
	}
}

// The chip is a layer: it leaves with the pointer, and takes its proof with it.
func TestTheChipLeavesWithThePointer(t *testing.T) {
	backend := &fakeBackend{}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent, Body: "one line"})
	app := newTestApp(backend, nil, nil)
	poll(t, app)

	y := hoverRow(t, app, "one line")
	restPointer(app, 4, y)
	// Off the transcript entirely: the last row of the frame is the contextual
	// line, which is a different pane.
	restPointer(app, 4, chipFrameHeight-1)
	if frame := strings.Join(chipFrame(app), "\n"); strings.Contains(frame, copyWord) {
		t.Fatalf("the chip stayed after the pointer left:\n%s", frame)
	}
}

// -- widths -------------------------------------------------------------------

// Every voice survives every width, and no row ever overruns the one it was
// given. The gutter costs nothing, so the reader's prompt is there even where a
// guest's name cannot be.
func TestTheVoicesSurviveEveryWidth(t *testing.T) {
	app := dressedApp(t)
	for width := 1; width <= 120; width++ {
		for i := 0; i < app.transcript.Len(); i++ {
			for _, row := range app.transcript.Block(i).Rows(width) {
				if got := blocks.Width(row); got > width {
					t.Fatalf("a row overran width %d (%d cells): %q", width, got, row)
				}
				if strings.Contains(row, "\n") {
					t.Fatalf("a row carries a newline at width %d: %q", width, row)
				}
			}
		}
		out := whole(t, app, width)
		if width >= 24 && !strings.Contains(out, tokens.GlyphPromptChat+" what is") {
			t.Fatalf("the reader's gutter is missing at width %d:\n%s", width, out)
		}
		if strings.Contains(out, "› you") || strings.Contains(out, "› aforge") {
			t.Fatalf("a speaker row came back at width %d:\n%s", width, out)
		}
	}
}

// withoutChip takes the copy chip back off a stripped row, so a test about
// something else on that row is comparing the row and not the layer over it.
//
// The chip's cells become blanks rather than disappearing, because the rail may
// be drawn to the right of the transcript on the same frame row and everything
// after the chip has to keep its column.
func withoutChip(row string) string {
	for _, chip := range []string{copiedWord, copyWord} {
		if i := strings.Index(row, chip); i >= 0 {
			return row[:i] + strings.Repeat(" ", len(chip)) + row[i+len(chip):]
		}
	}
	return row
}

// The pointer's half of the expand law, driven through the shell: the fold row
// is a door, and once open the rows inside it shut it again (§10).
func TestClickingALongTurnsFoldOpensAndClosesIt(t *testing.T) {
	backend := &fakeBackend{}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleUser, Body: longTurn()})
	app := newTestApp(backend, nil, nil)
	poll(t, app)

	y := hoverRow(t, app, blocks.CollapsedMark+" 5 lines")
	clickCell(app, 4, y)
	if frame := strings.Join(chipFrame(app), "\n"); !strings.Contains(frame, "line eight") {
		t.Fatalf("clicking the fold did not open it:\n%s", frame)
	}

	inside := hoverRow(t, app, "line four")
	clickCell(app, 4, inside)
	if frame := strings.Join(chipFrame(app), "\n"); strings.Contains(frame, "line eight") {
		t.Fatalf("clicking inside the opened turn did not close it:\n%s", frame)
	}
}
