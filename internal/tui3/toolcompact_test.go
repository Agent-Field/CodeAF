package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE PHONE TIER'S TOOL COLUMN (toolview.go, expand.go).
//
// Two claims, and every test below is one of them: at tierPhone a tool call is
// ONE compact row and its expansion is the WHOLE FRAME, and at every other tier
// nothing whatsoever moved.

// The frame these tests are written against — a phone in a terminal, the width
// the tier was drawn for — is [phoneWidth], declared once for the package in
// palettephone_test.go.

// toolAppAt is [toolApp] on a frame of a stated width. The width is set BEFORE
// the turn runs so every row is built at it, the way a real session's is.
func toolAppAt(t *testing.T, width int, batches ...[]session.Event) *app {
	t.Helper()
	var events []session.Event
	for _, batch := range batches {
		events = append(events, batch...)
	}
	events = append(events, session.Event{Kind: session.EventTurnDone})
	agent := &fakeAgent{model: "m", turns: [][]session.Event{events}}
	a := newTestApp(agent)
	a.width = width
	a.pal = newPalette(tokens.ANSI256, false)
	runTurn(t, a, agent, "go on then")
	return a
}

// toolRowAt is the first tool row on a frame, plain.
func toolRowAt(t *testing.T, a *app) string {
	t.Helper()
	for _, r := range plainRows(a) {
		if strings.HasPrefix(r, railMid) || strings.HasPrefix(r, railLast) ||
			strings.HasPrefix(r, railASCII) {
			return r
		}
	}
	t.Fatalf("no tool line was drawn:\n%s", strings.Join(plainRows(a), "\n"))
	return ""
}

// deepEdit is one edit call against a path nothing narrow can hold whole.
func deepEdit() []session.Event {
	return call("edit",
		`{"path":"internal/session/transport/loop.go","edits":[{"oldText":"const argsLimit = 400","newText":"const argsLimit = 8192"}]}`,
		"Successfully replaced 1 block(s) in internal/session/transport/loop.go.")
}

// ── the compact row ─────────────────────────────────────────────────────────

// A forty-four column row says the tool, the BASENAME and the stat, on one
// line, inside the frame. The path it was pointed at is four times the width
// of the column that would have to hold it, and a row that overflowed would
// wrap into a second row — which is the one thing this tier forbids.
func TestThePhoneRowIsOneCompactLine(t *testing.T) {
	a := toolAppAt(t, phoneWidth, deepEdit())
	line := toolRowAt(t, a)

	if want := railLast + "  edit loop.go  +1 −1"; line != want {
		t.Fatalf("the compact row is\n\t%q\nwant\n\t%q", line, want)
	}
	if got := ansi.StringWidth(line); got > phoneWidth {
		t.Fatalf("the row is %d cells wide on a %d-cell frame: %q", got, phoneWidth, line)
	}
	if strings.Contains(line, "internal/session") {
		t.Fatalf("the path was not elided to its tail: %q", line)
	}
}

// The row never becomes two rows. A call with nothing under it draws exactly
// one line whatever the length of what it was pointed at.
func TestThePhoneRowNeverWrapsToASecondRow(t *testing.T) {
	long := strings.Repeat("very/deeply/nested/", 6) + "module.go"
	a := toolAppAt(t, phoneWidth, call("read",
		`{"path":"`+long+`"}`, "one\ntwo\nthree"))

	tools := 0
	for _, r := range plainRows(a) {
		if ansi.StringWidth(r) > phoneWidth {
			t.Fatalf("a row overflowed the frame (%d cells): %q", ansi.StringWidth(r), r)
		}
		if strings.HasPrefix(r, railMid) || strings.HasPrefix(r, railLast) {
			tools++
		}
	}
	if tools != 1 {
		t.Fatalf("one call drew %d rows:\n%s", tools, strings.Join(plainRows(a), "\n"))
	}
}

// bash keeps a BUDGETED FRAGMENT of its command: the directory it runs in is
// context, and at this width context is dropped rather than dimmed.
func TestThePhoneRowDropsTheCommandsContext(t *testing.T) {
	a := toolAppAt(t, phoneWidth, call("bash",
		`{"command":"cd internal/session && go test ./..."}`, "ok\n"))
	line := toolRowAt(t, a)

	if strings.Contains(line, "cd internal/session") {
		t.Fatalf("the cd context survived a phone row: %q", line)
	}
	if !strings.Contains(line, "go test") {
		t.Fatalf("the command itself was lost: %q", line)
	}
}

// A search keeps the PATTERN and drops where it looked, which is the same rule
// one tool over: substance leads, the qualifier is what a narrow row gives up.
func TestThePhoneRowKeepsThePatternAndDropsThePlace(t *testing.T) {
	a := toolAppAt(t, phoneWidth, call("grep",
		`{"pattern":"argsLimit","path":"internal/session"}`,
		"internal/session/loop.go:12: const argsLimit = 400"))
	line := toolRowAt(t, a)

	if !strings.Contains(line, "argsLimit") {
		t.Fatalf("the pattern is not on the row: %q", line)
	}
	if strings.Contains(line, "internal/session") {
		t.Fatalf("the search path survived a phone row: %q", line)
	}
}

// THE STATE MACHINE IS UNCHANGED — the same four marks [app.mark] draws at
// every other tier, and the same silence on success. All that moved is the
// column it is drawn in: at this width the mark leads the row instead of
// trailing it, because the right end is exactly where a narrow row runs out.
func TestThePhoneRowKeepsTheStateMachine(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status toolState
		want   string
	}{
		{"queued", toolQueued, glyphQueued},
		{"waiting on a person", toolConsent, glyphAsk},
		{"failed", toolFailed, glyphBad},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp(&fakeAgent{model: "m"})
			a.width = phoneWidth
			a.state = stateWorking
			a.entries = []entry{{kind: entryTool, tool: "edit", text: "edit", status: tc.status,
				detail: toolDetail{Args: `{"path":"a/b/loop.go"}`}}}
			a.touch()

			line := toolRowAt(t, a)
			if !strings.HasPrefix(line, railLast+tc.want+" ") {
				t.Fatalf("the state cell does not lead the row: %q (want %q)", line, tc.want)
			}
		})
	}

	// The running mark is the spinner, and only there.
	a := newTestApp(&fakeAgent{model: "m"})
	a.width = phoneWidth
	a.state = stateWorking
	a.entries = []entry{{kind: entryTool, tool: "bash", text: "bash", status: toolRunning,
		began: time.Now(), detail: toolDetail{Args: `{"command":"go test ./..."}`}}}
	a.touch()
	spinner := tokens.Spinner(a.paints / spinnerStep)
	if line := toolRowAt(t, a); !strings.HasPrefix(line, railLast+spinner+" ") {
		t.Fatalf("a running call does not spin in the state cell: %q", line)
	}

	// AND A SUCCESS IS STILL SILENT. The cell is blank rather than a tick: a
	// column of ✓ is a column read to learn nothing, at any width (D11).
	done := toolAppAt(t, phoneWidth, deepEdit())
	line := toolRowAt(t, done)
	if !strings.HasPrefix(line, railLast+"  edit") {
		t.Fatalf("a finished call drew a success glyph: %q", line)
	}
}

// ── the other tiers ─────────────────────────────────────────────────────────

// EVERY WIDER FRAME IS UNTOUCHED. The same call at tierNarrow, tierStandard and
// tierWide draws the row it drew before this tier existed: the full path in the
// target column, the mark trailing, nothing elided and no state cell.
func TestTheWiderTiersAreUnchanged(t *testing.T) {
	const path = "internal/session/transport/loop.go"
	for _, width := range []int{60, 80, 120} {
		a := toolAppAt(t, width, deepEdit())
		line := toolRowAt(t, a)

		if want := railLast + "edit " + path + "  +1 −1"; line != want {
			t.Fatalf("the row at %d cells is\n\t%q\nwant\n\t%q", width, line, want)
		}
	}
}

// And a wider frame still expands INLINE: the sheet is the phone's answer and
// nothing else's.
func TestTheWiderTiersExpandInline(t *testing.T) {
	a := toolAppAt(t, 100, deepEdit())
	body := openFirst(t, a)

	if a.expand.open {
		t.Fatal("a desktop frame opened the phone's sheet")
	}
	if !strings.Contains(strings.Join(body, "\n"), railCont+"@@ -1,1 +1,1 @@") {
		t.Fatalf("the diff is not under the rail:\n%s", strings.Join(body, "\n"))
	}
}

// ── the sheet ───────────────────────────────────────────────────────────────

// openPhoneTool opens the first tool call on a phone frame and hands back the
// sheet as a reader sees it.
func openPhoneTool(t *testing.T, a *app) []string {
	t.Helper()
	for i := range a.entries {
		if a.entries[i].kind == entryTool {
			a.openTool(i)
			if !a.expand.open {
				t.Fatal("the phone frame did not raise the sheet")
			}
			return strings.Split(plain(frame(a)), "\n")
		}
	}
	t.Fatal("no tool entry")
	return nil
}

// A tap on a phone row opens the call over the WHOLE FRAME: the tool, the full
// path the row elided, the whole diff, the way out — and nothing of the
// conversation or the draft showing past the edges.
func TestThePhoneExpandTakesTheWholeFrame(t *testing.T) {
	a := toolAppAt(t, phoneWidth, deepEdit())
	a.height = 20
	lines := openPhoneTool(t, a)

	if len(lines) != a.height {
		t.Fatalf("the sheet drew %d rows on a %d-row terminal", len(lines), a.height)
	}
	body := strings.Join(lines, "\n")
	for _, want := range []string{
		"edit",                               // what this is
		"internal/session/transport/loop.go", // the path the row elided
		"@@ -1,1 +1,1 @@",                    // the diff, whole
		"-const argsLimit = 400",             //
		"+const argsLimit = 8192",            //
		"esc close",                          // the way out, said twice
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the sheet is missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, prompt) {
		t.Fatalf("the draft is drawn under the sheet:\n%s", body)
	}
	if strings.Contains(body, "go on then") {
		t.Fatalf("the conversation is drawn under the sheet:\n%s", body)
	}
	for _, line := range lines {
		if ansi.StringWidth(line) > phoneWidth {
			t.Fatalf("a sheet row overflowed the frame: %q", line)
		}
	}
}

// esc closes it and leaves the conversation exactly where it was.
func TestThePhoneSheetClosesOnEsc(t *testing.T) {
	a := toolAppAt(t, phoneWidth, deepEdit())
	openPhoneTool(t, a)

	drive(t, a, key("esc"))
	if a.expand.open {
		t.Fatal("esc did not close the sheet")
	}
	if !strings.Contains(plain(frame(a)), "go on then") {
		t.Fatal("the conversation did not come back")
	}
}

// AND SO DOES A TAP, on the head or on the foot: a phone has no esc key, so
// every keyboard way out of this sheet has a target of its own — and both of
// them are a whole row rather than a glyph somebody has to aim at.
func TestThePhoneSheetClosesOnATap(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  func(a *app) int
	}{
		{"the head", func(*app) int { return 0 }},
		{"the foot", func(a *app) int { return a.height - 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := toolAppAt(t, phoneWidth, deepEdit())
			a.height = 20
			openPhoneTool(t, a)

			drive(t, a, tea.MouseClickMsg{Y: tc.row(a), Button: tea.MouseLeft})
			if a.expand.open {
				t.Fatalf("a tap on %s did not close the sheet", tc.name)
			}
		})
	}
}

// A tap on the sheet's PADDING does nothing at all. It must not fall through to
// the conversation underneath, which is where the row that opened this sheet
// still is: a gap that expanded a call nobody can see is the worst thing a
// touch surface can do.
func TestThePhoneSheetSwallowsATapOnNothing(t *testing.T) {
	a := toolAppAt(t, phoneWidth, call("read", `{"path":"a/b/loop.go"}`, "one line"))
	a.height = 24
	openPhoneTool(t, a)

	// The row above the foot's rule on a sheet with one line of body: padding.
	drive(t, a, tea.MouseClickMsg{Y: a.height - 4, Button: tea.MouseLeft})
	if !a.expand.open {
		t.Fatal("a tap on the sheet's padding closed it")
	}
}

// The sheet scrolls, and it scrolls on its own offset: the transcript under it
// has not moved.
func TestThePhoneSheetScrolls(t *testing.T) {
	var lines []string
	for i := 0; i < 40; i++ {
		lines = append(lines, "line "+itoa(i))
	}
	a := toolAppAt(t, phoneWidth, call("read",
		`{"path":"a/b/loop.go"}`, strings.Join(lines, "\n")))
	a.height = 20
	openPhoneTool(t, a)

	before := a.offset
	drive(t, a, key("down"), key("down"), key("down"))
	if a.expand.offset != 3 {
		t.Fatalf("the sheet's offset is %d after three downs", a.expand.offset)
	}
	if a.offset != before {
		t.Fatal("scrolling the sheet moved the conversation under it")
	}
	if body := plain(frame(a)); !strings.Contains(body, "line 5") {
		t.Fatalf("the sheet did not scroll:\n%s", body)
	}
}

// A call's cap still lifts from inside the sheet, by the row that names it.
func TestThePhoneSheetLiftsTheCap(t *testing.T) {
	var lines []string
	for i := 0; i < 60; i++ {
		lines = append(lines, "line "+itoa(i))
	}
	a := toolAppAt(t, phoneWidth, call("read",
		`{"path":"a/b/loop.go"}`, strings.Join(lines, "\n")))
	a.height = 20
	openPhoneTool(t, a)

	// The foot of a capped read is the last row of the body, so it is reached
	// the way anything below a screenful is reached: by scrolling to it.
	for i := 0; i < 20; i++ {
		drive(t, a, key("down"))
	}
	sheet := strings.Split(plain(frame(a)), "\n")
	at := -1
	for i, line := range sheet {
		if strings.Contains(line, "more lines") {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("a capped read drew no foot:\n%s", strings.Join(sheet, "\n"))
	}
	drive(t, a, tea.MouseClickMsg{Y: at, Button: tea.MouseLeft})
	if !a.entries[first(t, a)].full {
		t.Fatal("a tap on the foot did not lift the cap")
	}
	if !a.expand.open {
		t.Fatal("lifting the cap closed the sheet")
	}
	for i := 0; i < 60; i++ {
		drive(t, a, key("down"))
	}
	if !strings.Contains(plain(frame(a)), "line 59") {
		t.Fatal("the rest of the read is still hidden")
	}
}

// first is the index of the conversation's first tool call.
func first(t *testing.T, a *app) int {
	t.Helper()
	for i := range a.entries {
		if a.entries[i].kind == entryTool {
			return i
		}
	}
	t.Fatal("no tool entry")
	return -1
}

// A FRAME DRAGGED WIDER STANDS THE SHEET DOWN. Past tierPhone the call is
// expanded inline, exactly as it would have been had it been opened there — and
// narrowing again brings the sheet back rather than having closed the call.
func TestThePhoneSheetStandsDownOnAWiderFrame(t *testing.T) {
	a := toolAppAt(t, phoneWidth, deepEdit())
	a.height = 20
	openPhoneTool(t, a)

	a.width = 100
	a.touch()
	if a.expandShowing() {
		t.Fatal("the sheet is still on a hundred-column frame")
	}
	body := plain(frame(a))
	if !strings.Contains(body, "@@ -1,1 +1,1 @@") {
		t.Fatalf("the call did not expand inline instead:\n%s", body)
	}
	if !strings.Contains(body, "go on then") {
		t.Fatalf("the conversation is not back:\n%s", body)
	}

	a.width = phoneWidth
	a.touch()
	if !a.expandShowing() {
		t.Fatal("narrowing again did not bring the sheet back")
	}
}

// A question the SESSION is blocked on outranks the sheet, the way it outranks
// the settings panel: an approval parked behind a fullscreen overlay is a turn
// waiting on a key nobody can reach.
func TestAConsentQuestionStandsTheSheetDown(t *testing.T) {
	a := toolAppAt(t, phoneWidth, deepEdit())
	a.height = 20
	openPhoneTool(t, a)

	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind: session.EventConsentRequest, Tool: "bash", Hint: "rm -rf build",
		Args: `{"command":"rm -rf build"}`,
	}})
	if a.expand.open {
		t.Fatal("a consent question was asked behind the sheet")
	}
}

// A running call's sheet is live: it is re-resolved from the deck every frame
// rather than snapshot at the tap, so the output arriving under it arrives on
// screen.
func TestThePhoneSheetFollowsALiveCall(t *testing.T) {
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)
	a.width, a.height = phoneWidth, 20
	a.state = stateWorking
	a.entries = []entry{{kind: entryTool, tool: "bash", text: "bash", status: toolRunning,
		began: time.Now(), detail: toolDetail{Args: `{"command":"go test ./..."}`}}}
	a.openTool(0)
	if !a.expand.open {
		t.Fatal("the sheet did not open over a running call")
	}
	if body := plain(frame(a)); !strings.Contains(body, "go test ./...") {
		t.Fatalf("a running call's command is not on its sheet:\n%s", body)
	}

	a.entries[0].status = toolOK
	a.entries[0].detail.Output = "ok  \tgithub.com/Agent-Field/aforge-v2\t0.4s"
	a.touch()
	if body := plain(frame(a)); !strings.Contains(body, "ok  ") {
		t.Fatalf("the result did not reach the open sheet:\n%s", body)
	}
}
