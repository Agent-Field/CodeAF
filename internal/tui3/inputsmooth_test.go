package tui3

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE INPUT PATH, AND WHAT IT IS ALLOWED TO COST.
//
// Four things arrive in BURSTS on this surface and only one of them carries four
// bursts' worth of meaning: a pointer sends a message per cell it crosses, a
// terminal being dragged sends a size per step of the drag, a paste arrives as
// one message holding what a person spent an afternoon producing, and a provider
// streaming a reply sends a hundred to two hundred text deltas a second. Every
// one of them used to pay for the whole transcript, or for the whole draft, per
// message — which is why a session that felt instant on a laptop felt like syrup
// over a link with a hundred milliseconds in it.
//
// These tests state the ceilings rather than the timings. A ceiling holds on a
// loaded CI box and a stopwatch does not.

// ── the pointer ─────────────────────────────────────────────────────────────

// hoverApp is a settled turn with two tool calls in it, which is two rows the
// pointer can answer to and two entries it can dirty.
func hoverApp(t *testing.T) *app {
	t.Helper()
	return toolApp(t, tokens.ANSI256,
		call("read", `{"path":"a.go"}`, "one\ntwo"),
		call("edit", `{"path":"b.go"}`, "three\nfour"))
}

// settle drops every mark the surface is carrying, so what a test does NEXT is
// the only thing that could have left one.
func settle(a *app) {
	a.visible(a.bodyWidth())
	for i := range a.entries {
		a.entries[i].stale = false
	}
	a.dirty = false
}

// stales is which entries are waiting to be drawn again.
func stales(a *app) []int {
	var out []int
	for i := range a.entries {
		if a.entries[i].stale {
			out = append(out, i)
		}
	}
	return out
}

// A POINTER THAT MOVED WITHOUT CHANGING WHAT IT IS OVER HAS NOT MOVED, as far
// as this surface is concerned. Two cells of the same tool row are the same
// answer, and the second one must leave nothing behind: no stale entry, no
// dirty flag, and so no frame.
//
// THE SECOND MOTION IS THE ONE THAT ASKS FOR THE POINTER'S OWN CLOCK, and that
// is the whole of what the fold added here (coalesce.go): a motion arriving
// after another motion is a SWEEP, so it is kept rather than answered, and one
// [pointerMsg] is asked for to answer it with. That wakeup is not a frame — it
// spends the fold and stops — which is why the claim this test exists to make
// survives it: a pointer crossing a row it is already on still costs no layout,
// no stale row, and nothing on screen.
func TestPointerMotionOverTheSameRowLeavesNothingBehind(t *testing.T) {
	a := hoverApp(t)
	toolY := screenRowOf(t, a, func(r row) bool { return r.hit == hitTool })

	drive(t, a, motionAt(toolY))
	if a.hot.kind != hoverEntry {
		t.Fatalf("the pointer over a tool row recorded %v", a.hot)
	}
	settle(a)

	// The fold takes it and answers it at the frame, which is where the claim is
	// checked: the whole round trip must leave the surface exactly as it was.
	drive(t, a, tea.MouseMotionMsg{X: 4, Y: toolY})
	if a.ptr.have {
		t.Fatal("the pointer's fold is still holding a position after it settled")
	}
	if a.dirty {
		t.Fatal("a motion that changed nothing asked for a frame")
	}
	if got := stales(a); len(got) != 0 {
		t.Fatalf("a motion that changed nothing dropped the rows of entries %v", got)
	}
}

// A POINTER CROSSING A BOUNDARY PAYS FOR THE TWO ROWS THE BOUNDARY IS BETWEEN
// and for nothing else: the entry that lost the highlight and the one that
// gained it. A third entry going stale would be the transcript being re-drawn
// because the mouse moved.
func TestPointerMotionCrossingRowsMarksOnlyTheTwoItCrossed(t *testing.T) {
	a := hoverApp(t)
	body, _ := a.window(a.bodyWidth(), a.viewHeight())
	var toolRows []int
	for i, r := range body {
		if r.hit == hitTool {
			toolRows = append(toolRows, i)
		}
	}
	if len(toolRows) < 2 {
		t.Fatalf("the turn drew %d tool rows, want two to cross between", len(toolRows))
	}
	first, second := body[toolRows[0]].entry, body[toolRows[1]].entry
	firstY := a.bodyTop() + toolRows[0]
	secondY := a.bodyTop() + toolRows[1]

	drive(t, a, motionAt(firstY))
	settle(a)
	drive(t, a, motionAt(secondY))

	got := stales(a)
	if len(got) != 2 || got[0] != first || got[1] != second {
		t.Fatalf("crossing from entry %d to entry %d dropped the rows of %v", first, second, got)
	}
	if !a.dirty {
		t.Fatal("a hover that changed did not ask for a frame")
	}
}

// ── the resize ──────────────────────────────────────────────────────────────

// THE FIRST SIZE IS LAID OUT ON THE SPOT. There is no burst to wait out at
// startup, and a surface that opened with its scroll one tick behind would be a
// surface that opened scrolled to the wrong place.
func TestTheFirstSizeIsLaidOutImmediately(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.rows, a.builds = nil, 0

	_, cmd := a.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	if cmd != nil {
		t.Fatal("the first size waited for a burst that had not started")
	}
	if a.builds != 1 {
		t.Fatalf("the first size built %d layouts, want exactly one", a.builds)
	}
	if a.width != 100 || a.height != 40 {
		t.Fatalf("the surface is %dx%d, want 100x40", a.width, a.height)
	}
}

// A DRAG IS ONE RESIZE, NOT THIRTY. Every size lands the moment it arrives — the
// frame is never laid out for a window the terminal no longer has — but the
// clamp that walks the whole transcript is spent once, at the end, against the
// size the drag stopped at.
func TestAResizeBurstLaysTheTranscriptOutOnceAtTheFinalWidth(t *testing.T) {
	a := hoverApp(t)
	a.frame()
	a.builds = 0

	var scheduled int
	final := 0
	for i := 0; i < 20; i++ {
		width := 70 + i
		final = width
		if _, cmd := a.Update(tea.WindowSizeMsg{Width: width, Height: 30}); cmd != nil {
			scheduled++
		}
	}
	if scheduled != 1 {
		t.Fatalf("a burst of twenty sizes armed %d settlements, want one", scheduled)
	}
	if a.builds != 0 {
		t.Fatalf("a burst of twenty sizes laid the transcript out %d times, want none until it is drawn", a.builds)
	}
	if a.width != final {
		t.Fatalf("the surface is %d wide, want the last size %d", a.width, final)
	}

	// The frame the person actually sees is the final size, laid out once.
	a.frame()
	if a.builds != 1 {
		t.Fatalf("the frame after the burst built %d layouts, want one", a.builds)
	}
	if a.rowsWidth != a.bodyWidth() {
		t.Fatalf("the rows are laid out for %d columns, want the final %d", a.rowsWidth, a.bodyWidth())
	}
	for _, line := range plainRows(a) {
		if len([]rune(line)) > final {
			t.Fatalf("a row is %d cells wide in a %d-column window: %q", len([]rune(line)), final, line)
		}
	}

	// And the settlement clamps once and re-arms the next drag.
	drive(t, a, resizeSettledMsg{})
	if a.sizing {
		t.Fatal("the surface is still expecting a size it has been told stopped moving")
	}
	if _, cmd := a.Update(tea.WindowSizeMsg{Width: 64, Height: 30}); cmd == nil {
		t.Fatal("the next drag armed no settlement of its own")
	}
}

// A SIZE THE TERMINAL HAS ALREADY SENT IS THE TERMINAL SAYING NOTHING, and
// multiplexers say it on every attach and every pane focus.
func TestARepeatedSizeCostsNothing(t *testing.T) {
	a := hoverApp(t)
	a.frame()
	a.builds = 0

	_, cmd := a.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	if cmd != nil || a.dirty || a.builds != 0 {
		t.Fatalf("a repeated size scheduled %v, dirty=%v, builds=%d", cmd, a.dirty, a.builds)
	}
}

// ── the paste ───────────────────────────────────────────────────────────────

// bigPaste is a document, which is what a paste into this box usually is.
func bigPaste(lines int) string {
	return strings.Repeat("goroutine 42 [running]: main.step(0x1400, 0x2)\n", lines)
}

// A PASTE IS ONE MESSAGE, ONE EDIT AND NO LAYOUT AT ALL, however long it is. The
// bracket coalesces it (app.go), the box shows the six rows it can, and nothing
// about its length reaches the transcript.
//
// NO LAYOUT AT ALL is the stronger ceiling this wave earned. Filling the box
// changes the CHROME and the chrome is rebuilt every frame anyway; the laid-out
// transcript underneath it cannot have changed, so [app.edited] no longer throws
// it away (draft.go states the law). The frame after the paste therefore draws
// the rows it already had.
func TestALargePasteIsOneEditAndNoLayout(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.frame()
	a.builds = 0
	paste := bigPaste(4000)

	drive(t, a, tea.PasteMsg{Content: paste})
	if got, want := len(a.input.value), len([]rune(paste)); got != want {
		t.Fatalf("the draft holds %d runes of a %d-rune paste", got, want)
	}
	if a.builds != 0 {
		t.Fatalf("the paste laid the transcript out %d times before it was drawn", a.builds)
	}

	a.frame()
	if a.builds != 0 {
		t.Fatalf("the frame after the paste built %d layouts, want none", a.builds)
	}
	block, _, _ := a.inputBlock(a.width - len(inputPad))
	if len(block) > draftRows {
		t.Fatalf("a four-thousand-line paste drew %d rows, want at most %d", len(block), draftRows)
	}
}

// AND NOTHING THE BOX DOES SCALES WITH IT. The rows it lays out are the rows it
// can show, so the wrap of a four-thousand-line draft is the wrap of six lines
// of it — which is the whole claim [draftWindow] exists to make.
func TestTheDraftBoxLaysOutWhatItShowsAndNotWhatItHolds(t *testing.T) {
	value := []rune(bigPaste(4000))
	segments, caretRow, top, opening := draftWindow(value, len(value), 40, draftRows)
	if len(segments) > 4*draftRows {
		t.Fatalf("a four-thousand-line draft laid out %d rows to show %d", len(segments), draftRows)
	}
	if opening {
		t.Fatal("a window at the end of a four-thousand-line draft claims to open it")
	}
	if caretRow-top != draftRows-1 {
		t.Fatalf("the caret is on row %d of the box, want the last of %d", caretRow-top, draftRows)
	}

	// And the frame it costs is the frame a twelve-line draft costs, within the
	// noise of counting allocations at all.
	small := framePerPaste(t, bigPaste(2*draftRows))
	large := framePerPaste(t, bigPaste(4000))
	if large > small*3/2 {
		t.Fatalf("a frame with a four-thousand-line draft allocates %.0f times, against %.0f with a draft that already fills the box", large, small)
	}
}

// framePerPaste is how many allocations one frame costs with this draft in the
// box. It is a proxy for "did anything walk the whole draft", and a robust one:
// a walk of four thousand lines cannot hide inside a fifty-percent margin. The
// two drafts it is asked about both FILL the box, so the rows drawn are the same
// rows and the only thing that differs is how much draft is behind them.
func framePerPaste(t *testing.T, paste string) float64 {
	t.Helper()
	a := newTestApp(&fakeAgent{model: "m"})
	a.input.setText(paste)
	a.frame()
	return testing.AllocsPerRun(10, func() {
		a.dirty = true
		a.frame()
	})
}

// ── the draft box draws what it always drew ─────────────────────────────────

// refWrap and refCaret are the box's arithmetic as it stood before [draftWindow]
// — the whole draft wrapped, the caret found by scanning it — kept here as the
// ORACLE for the windowed version. A faster answer to a question about what a
// person sees is worth nothing unless it is the same answer.
//
// It fits by CELLS, as the box does: the oracle's claim is that windowing the
// wrap changes nothing about it, and an oracle measuring a different unit would
// be asserting the opposite.
func refWrap(value []rune, room int) []segment {
	var out []segment
	line := 0
	flush := func(end int) {
		for line < end {
			cut, width := line, 0
			for cut < end {
				w := ansi.StringWidth(string(value[cut]))
				if width+w > room {
					break
				}
				width += w
				cut++
			}
			if cut >= end {
				out = append(out, segment{from: line, to: end})
				line = end
				return
			}
			if cut == line {
				cut = line + 1
			}
			for at := cut; at > line; at-- {
				if value[at-1] == ' ' {
					cut = at
					break
				}
			}
			out = append(out, segment{from: line, to: cut})
			line = cut
		}
		out = append(out, segment{from: end, to: end})
	}
	for at := 0; at < len(value); at++ {
		if value[at] == '\n' {
			flush(at)
			line = at + 1
		}
	}
	flush(len(value))
	if len(out) == 0 {
		out = append(out, segment{})
	}
	return out
}

func refCaret(e *editor, segments []segment, room int) int {
	row := 0
	for i, s := range segments {
		if e.cursor >= s.from && e.cursor <= s.to {
			row = i
			full := ansi.StringWidth(string(e.value[s.from:e.cursor])) >= room
			if e.cursor == s.to && full && i+1 < len(segments) {
				continue
			}
			break
		}
	}
	return row
}

// refBlock is [draftBlock] written against the oracle above.
func refBlock(e *editor, pal palette, width, maxRows int, hint string) ([]string, int, int) {
	room := width - ansi.StringWidth(prompt)
	if room < 4 {
		room = 4
	}
	if maxRows < 1 {
		maxRows = 1
	}
	if len(e.value) == 0 && hint != "" {
		return []string{pal.dim(prompt) + pal.dim(fit(hint, room))}, ansi.StringWidth(prompt), 0
	}
	segments := refWrap(e.value, room)
	caretRow := refCaret(e, segments, room)
	caretColumn := caretColumnIn(e, segments[caretRow])
	top := 0
	if caretRow >= maxRows {
		top = caretRow - maxRows + 1
	}
	end := min(top+maxRows, len(segments))
	out := make([]string, 0, end-top)
	for i := top; i < end; i++ {
		lead := "  "
		switch {
		case i == 0:
			lead = pal.dim(prompt)
		case i == top:
			lead = pal.dim(glyphMore + " ")
		}
		out = append(out, lead+pal.ink(string(e.value[segments[i].from:segments[i].to])))
	}
	return out, ansi.StringWidth(prompt) + caretColumn, caretRow - top
}

// THE BOX DRAWS WHAT IT ALWAYS DREW. Every draft shape this surface has ever
// been handed — an empty one, a line that soft-wraps at a space, one that has no
// space to wrap at, blank lines in the middle, a trailing newline, wide runes —
// is laid out at every width and with the caret at every position, and the
// windowed answer is compared against the whole-draft one rune for rune.
func TestTheWindowedDraftDrawsWhatTheWholeDraftDrew(t *testing.T) {
	pal := newPalette(tokens.ANSI256, false)
	drafts := []string{
		"",
		" ",
		"fix this",
		"a much longer sentence than the box is wide, which has to break somewhere",
		"abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghij",
		"one\ntwo\nthree",
		"one\n\n\nfour",
		"trailing\n",
		"\nleading",
		"one\na much longer second line than the box is wide by a good margin\nthree",
		"日本語のテキストがここにあります\nand some ascii after it",
		strings.Repeat("a line of a pasted stack trace\n", 30),
		strings.Repeat("x", 200),
	}
	for _, draft := range drafts {
		value := []rune(draft)
		for _, width := range []int{8, 12, 20, 41, 60} {
			for _, maxRows := range []int{1, 2, draftRows} {
				for cursor := 0; cursor <= len(value); cursor++ {
					e := &editor{value: value, cursor: cursor}
					gotRows, gotX, gotRow := draftBlock(e, pal, width, maxRows, "", "")
					wantRows, wantX, wantRow := refBlock(e, pal, width, maxRows, "")
					if gotX != wantX || gotRow != wantRow || !sameRows(gotRows, wantRows) {
						t.Fatalf("draft %q at width %d, %d rows, caret %d:\n got %q x=%d row=%d\nwant %q x=%d row=%d",
							draft, width, maxRows, cursor, gotRows, gotX, gotRow, wantRows, wantX, wantRow)
					}
				}
			}
		}
	}
}

// ── what the numbers were ───────────────────────────────────────────────────

// BenchmarkPointerMotion is one pointer step over a row it is already on, which
// is the commonest message this surface receives and the one that must cost
// closest to nothing. The draft in the box is the variable: it used to be the
// whole cost, because every geometric question below the conversation wrapped
// the entire draft on the way to an answer.
func BenchmarkPointerMotion(b *testing.B) {
	for _, lines := range []int{1, 4000} {
		b.Run(fmt.Sprintf("draft=%d", lines), func(b *testing.B) {
			a := benchApp(20)
			a.input.setText(bigPaste(lines))
			a.frame()
			a.Update(tea.MouseMotionMsg{X: 10, Y: 5})
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				a.Update(tea.MouseMotionMsg{X: 10, Y: 5})
			}
		})
	}
}

// BenchmarkResizeStep is one size out of a drag. What it used to cost was a full
// transcript layout, once per step, for a frame the terminal's own renderer was
// never going to paint (view.go's [app.resized]).
func BenchmarkResizeStep(b *testing.B) {
	a := benchApp(20)
	a.frame()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.Update(tea.WindowSizeMsg{Width: 90 + i%20, Height: 40})
	}
}

// ── the stream ──────────────────────────────────────────────────────────────
//
// The fourth burst, and the loudest: a provider sends a hundred to two hundred
// text deltas a second and bubbletea builds a frame per message. [waitEvent]
// folds a run that is ALREADY QUEUED into one message, by internal/session's own
// rule about which kinds are exactly their texts joined.

// A RUN OF DELTAS ALREADY QUEUED IS ONE MESSAGE.
func TestAQueuedRunOfDeltasArrivesAsOneMessage(t *testing.T) {
	ch := make(chan session.Event, 8)
	for _, part := range []string{"the answer ", "is a sentence ", "in three parts."} {
		ch <- text(session.EventTextDelta, part)
	}
	msg, ok := waitEvent(ch, 7)().(streamEventMsg)
	if !ok {
		t.Fatalf("the wait answered %T, want a stream event", msg)
	}
	if msg.gen != 7 {
		t.Fatalf("the fold came back on generation %d, want 7", msg.gen)
	}
	if msg.then != nil {
		t.Fatalf("a run with nothing behind it carried a stopper: %+v", *msg.then)
	}
	if want := "the answer is a sentence in three parts."; msg.ev.Text != want {
		t.Fatalf("the fold reads %q, want %q", msg.ev.Text, want)
	}
	if len(ch) != 0 {
		t.Fatalf("the drain left %d events on the channel", len(ch))
	}
}

// AND THE EVENT THAT ENDED IT COMES WITH IT. The drain has to take an event off
// the channel to find out whether it folds, and a channel cannot be put back —
// so the one that stopped the run rides beside the run.
func TestTheEventThatEndsAFoldTravelsWithIt(t *testing.T) {
	ch := make(chan session.Event, 8)
	ch <- text(session.EventTextDelta, "reading it ")
	ch <- text(session.EventTextDelta, "now")
	ch <- toolBegin("read", "internal/tui3/app.go")
	msg := waitEvent(ch, 1)().(streamEventMsg)
	if msg.ev.Text != "reading it now" {
		t.Fatalf("the fold reads %q", msg.ev.Text)
	}
	if msg.then == nil {
		t.Fatal("the call that ended the fold was dropped")
	}
	if msg.then.Kind != session.EventToolBegin || msg.then.Tool != "read" {
		t.Fatalf("the stopper is %+v, want the read that ended the run", *msg.then)
	}
}

// AND TWO KINDS THAT ARE BOTH FOLDABLE STILL DO NOT FOLD INTO EACH OTHER:
// reasoning and a reply are two blocks, and joining them would put the model's
// thinking inside its answer.
func TestReasoningDoesNotFoldIntoAReply(t *testing.T) {
	ch := make(chan session.Event, 8)
	ch <- text(session.EventReasoning, "checking the loop")
	ch <- text(session.EventTextDelta, "it parses.")
	msg := waitEvent(ch, 1)().(streamEventMsg)
	if msg.ev.Kind != session.EventReasoning || msg.ev.Text != "checking the loop" {
		t.Fatalf("the reasoning event was folded into something else: %+v", msg.ev)
	}
	if msg.then == nil || msg.then.Kind != session.EventTextDelta {
		t.Fatalf("the reply that ended the run is %+v", msg.then)
	}
}

// AND A FOLDED RUN DRAWS WHAT THE DELTAS WOULD HAVE DRAWN. This is the whole
// claim the fold rests on: [app.appendText] concatenates, so a joined text and
// the texts joined are the same transcript.
func TestAFoldedRunDrawsWhatTheDeltasWouldHave(t *testing.T) {
	parts := []string{"## the answer\n\n", "it parses, ", "and the loop ", "is where it lands.\n"}

	apart := newTestApp(&fakeAgent{model: "m"})
	for _, part := range parts {
		apart.apply(text(session.EventTextDelta, part))
	}
	apart.frame()

	folded := newTestApp(&fakeAgent{model: "m"})
	folded.apply(text(session.EventTextDelta, strings.Join(parts, "")))
	folded.frame()

	if got, want := strings.Join(plainRows(folded), "\n"), strings.Join(plainRows(apart), "\n"); got != want {
		t.Fatalf("the folded run drew a different transcript:\nfolded:\n%s\n\napart:\n%s", got, want)
	}
}

// AND A QUIET STREAM IS NOT MADE TO WAIT. The drain takes what is queued and
// stops the instant it would block, so one event on its own is delivered as
// promptly as it always was.
func TestAQuietStreamDeliversItsOneEventAtOnce(t *testing.T) {
	ch := make(chan session.Event, 8)
	ch <- text(session.EventTextDelta, "one word")
	done := make(chan tea.Msg, 1)
	go func() { done <- waitEvent(ch, 1)() }()
	select {
	case msg := <-done:
		if msg.(streamEventMsg).ev.Text != "one word" {
			t.Fatalf("the lone delta came back as %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("the wait held a lone delta back looking for more")
	}
}
