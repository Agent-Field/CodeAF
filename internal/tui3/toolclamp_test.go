package tui3

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── A TOOL CALL IS ONE ROW UNTIL SOMEBODY OPENS IT ──────────────────────────
//
// The line was already fitted to the width it was handed; what these pin is that
// the width it was handed is the width it is DRAWN in, and that the text it
// measured is the text the terminal will draw.
//
// Two things broke that, and both of them made a call eat four rows where it had
// asked for one:
//
//   - THE INDENT LAW WAS PAID BY THE LINE AND NOT BY THE BLOCK. render.go shoves
//     every work row two columns right after layout, and [app.toolLine] subtracts
//     those two cells before it lays itself out. The block hanging under it did
//     not, so every row of an open call's evidence was drawn two cells wider than
//     the frame — and a row two cells over does not get an ellipsis from the
//     terminal, it gets a second visual row.
//   - THE TEXT WAS SOMEBODY ELSE'S BYTES, MEASURED AS IF IT WERE OURS. A tab
//     measures nothing and draws up to eight cells; a carriage return measures
//     nothing and sends the cursor home; an escape sequence measures nothing and
//     repaints rows this surface owns. [drawableLine] is the rule, and it was
//     being applied to a background job's log and to nothing else.

// clampArgs is one tool's arguments as JSON, so a command with quotes, tabs and
// escapes in it reaches the surface the way session sends it rather than the way
// a hand-written literal survives.
func clampArgs(field, value string) string {
	b, err := json.Marshal(map[string]string{field: value})
	if err != nil {
		panic(err)
	}
	return string(b)
}

// The command from the owner's report, which is the length this is all about.
const clampCommand = `gh api "search/issues?q=repo:Agent-Field/aforge-v2+commenter:AbirAbbas&sort=created&order=desc&per_page=30" -q '.items[] | .number'`

// callRowsOf is every row the transcript draws for a tool call — the line and
// whatever it hangs — plain.
func callRowsOf(a *app) []string {
	var out []string
	for _, r := range rows(a) {
		if r.hit == hitTool || r.hit == hitMore {
			out = append(out, plain(r.text))
		}
	}
	return out
}

// openEveryCall expands every call in the transcript, whatever it opened as.
func openEveryCall(a *app) {
	for i := range a.entries {
		if a.entries[i].kind == entryTool && !a.entries[i].open {
			a.openTool(i)
		}
	}
	a.touch()
}

// A CALL LONGER THAN THE FRAME IS ONE ROW THAT ENDS IN THE ELLIPSIS. Not two
// rows, not a row that wraps, and not a row cut without saying so — the ellipsis
// is the surface's own [glyphMore] and it is the whole of what tells a person
// there is more command than they can see.
func TestALongCallLineIsOneRowThatEndsInTheEllipsis(t *testing.T) {
	// Every one of these is narrower than the command, which is the case under
	// test: a command that FITS is drawn whole and has nothing to say with an
	// ellipsis (the emptiness law over a glyph).
	for _, width := range []int{60, 80, 100, 120} {
		a := toolAppAt(t, width, call("bash", clampArgs("command", clampCommand), "ok\n"))
		lines := callRowsOf(a)
		if len(lines) != 1 {
			t.Fatalf("width %d: a closed call drew %d rows, not one:\n%s",
				width, len(lines), strings.Join(lines, "\n"))
		}
		line := lines[0]
		if got := ansi.StringWidth(line); got > width {
			t.Errorf("width %d: the call line is %d cells: %q", width, got, line)
		}
		if !strings.HasSuffix(strings.TrimRight(line, " "), glyphMore) {
			t.Errorf("width %d: the clipped call line does not end in %q: %q", width, glyphMore, line)
		}
		if strings.Contains(line, "\n") {
			t.Errorf("width %d: the call line carries a newline: %q", width, line)
		}
	}
}

// AND NOTHING AN OPEN CALL HANGS OVERHANGS THE FRAME EITHER. This is the indent
// law's two cells, and it is asserted over the WHOLE transcript rather than over
// the tool rows alone, because the pass that applies the indent does not know
// which renderer laid a row out.
func TestNothingATranscriptDrawsOverhangsTheFrame(t *testing.T) {
	long := "x" + strings.Repeat("abcdefghij", 30)
	batches := [][]session.Event{
		call("bash", clampArgs("command", clampCommand), "a long line of output "+long+"\nshort\n"),
		call("read", clampArgs("path", long+".go"), long+"\n"),
		call("grep", clampArgs("pattern", long), long+"\n"),
		call("web_fetch", clampArgs("url", "https://example.com/"+long), long),
		call("write", `{"path":"`+long+`.go","content":"`+long+`\nsecond line\n"}`, "ok"),
	}
	for _, width := range []int{phoneWidth, 60, 80, 100, 120, 200} {
		a := toolAppAt(t, width, batches...)
		a.height = 80
		openEveryCall(a)
		for _, line := range plainRows(a) {
			if got := ansi.StringWidth(line); got > width {
				t.Errorf("width %d: a row is %d cells wide: %q", width, got, line)
			}
		}
	}
}

// SOMEBODY ELSE'S CONTROL BYTES NEVER REACH THE FRAME. A tab, a carriage return
// and an escape sequence each measure nothing and each draw something, so a row
// carrying one is a row whose clamp is a fiction — and the escape is worse than
// a fiction, because it repaints rows this surface owns.
func TestNoToolRowCarriesSomebodyElsesControlBytes(t *testing.T) {
	command := "go test\t./internal/tui3\tand\tmore\ttabs\there\tand\there\ttoo\tyes\tindeed"
	output := "ok\t\x1b[31mFAIL\x1b[0m a coloured line\nprogress 10%\rprogress 100% and a long tail\n"
	for _, width := range []int{phoneWidth, 60, 100} {
		a := toolAppAt(t, width, call("bash", clampArgs("command", command), output))
		a.height = 80
		openEveryCall(a)
		for _, r := range rows(a) {
			bare := plain(r.text)
			if at := strings.IndexAny(bare, "\t\r\n\x00\x07\x1b"); at >= 0 {
				t.Errorf("width %d: a control byte reached the frame at %d: %q", width, at, bare)
			}
			if got := ansi.StringWidth(bare); got > width {
				t.Errorf("width %d: a row is %d cells wide: %q", width, got, bare)
			}
		}
	}
}

// THE GESTURE THAT OPENS ONE CALL IS THE GESTURE THAT CLOSES IT, and it is
// reachable with no pointer at all: ↑ picks a call out of the transcript and
// enter over an empty box opens what ↑ picked (input.go). What comes back is the
// command WHOLE — that is the one thing the line clipped — and pressing enter
// again returns the call to its single row.
func TestAKeyboardAloneOpensOneCallAndClosesItAgain(t *testing.T) {
	a := toolAppAt(t, 100, call("bash", clampArgs("command", clampCommand), "ok\n"))
	a.height = 40

	drive(t, a, key("up"))
	if a.sel < 0 || a.entries[a.sel].kind != entryTool {
		t.Fatalf("↑ did not reach the call: sel=%d", a.sel)
	}
	drive(t, a, key("enter"))
	opened := strings.Join(callRowsOf(a), "\n")
	if len(callRowsOf(a)) < 2 {
		t.Fatalf("enter did not open the call:\n%s", opened)
	}
	// The command is shown whole, which means the tail the line could not carry.
	if !strings.Contains(strings.ReplaceAll(opened, "\n", ""), ".items[] | .number'") {
		t.Errorf("the open call does not show the whole command:\n%s", opened)
	}

	drive(t, a, key("enter"))
	if lines := callRowsOf(a); len(lines) != 1 {
		t.Fatalf("enter did not close the call again, it drew %d rows:\n%s",
			len(lines), strings.Join(lines, "\n"))
	}
}

// A CLICK ANYWHERE ON THE LINE OPENS THAT ONE CALL, because the whole line is
// the door — hover.go's law is that the set which lights is the set the press
// acts on, and what lights here is the row.
func TestAClickAnywhereOnTheCallLineOpensThatOneCall(t *testing.T) {
	a := toolAppAt(t, 100, call("bash", clampArgs("command", clampCommand), "ok\n"))
	a.height = 40
	clickHit(t, a, hitTool)
	if lines := callRowsOf(a); len(lines) < 2 {
		t.Fatalf("a click on the call line opened nothing:\n%s", strings.Join(lines, "\n"))
	}
}

// AND IT LIGHTS AS ONE, END TO END. The pointer at the line's first cell and the
// pointer at its last both record the same call, and the row bands the whole
// frame — a line that lit for half its width would be promising a door at the
// cells it went dark on.
func TestTheWholeCallLineIsTheLitSetAndThePressedSet(t *testing.T) {
	a := toolAppAt(t, 100, call("bash", clampArgs("command", clampCommand), "ok\n"))
	a.height = 40
	a.pal = newPalette(tokens.ANSI256, false)

	// The BODY's width and not the frame's: the roster keeps a column of its own
	// at the right edge, and a pointer in there is on the roster (task.go).
	width := a.bodyWidth()
	var at, entry int
	for i, r := range a.visible(width) {
		if r.hit == hitTool {
			at, entry = i, r.entry
			break
		}
	}
	y := a.bodyTop() + at
	for _, x := range []int{0, width / 2, width - 1} {
		drive(t, a, tea.MouseMotionMsg{X: x, Y: y})
		if a.hot.kind != hoverEntry || a.hot.entry != entry {
			t.Fatalf("the pointer at column %d recorded %+v, not the call at %d", x, a.hot, entry)
		}
	}
	// The band is the row's whole width, which is what "span-sized" means for a
	// target whose span is the line.
	lit := a.visible(width)[at].text
	if !strings.Contains(lit, hoverBg()) {
		t.Fatalf("the call line under the pointer draws no ground: %q", lit)
	}
	if got := ansi.StringWidth(plain(lit)); got != width {
		t.Fatalf("the lit line bands %d cells of a %d-cell body: %q", got, width, plain(lit))
	}
}

// THE CLAMP IS RE-FITTED ON RESIZE, and it is re-fitted from the frame the
// person is now looking at rather than from the one the call arrived in. Wide
// enough and the command is drawn whole; narrow again and the ellipsis is back.
func TestTheCallLineRefitsWhenTheFrameResizes(t *testing.T) {
	a := toolAppAt(t, 60, call("bash", clampArgs("command", clampCommand), "ok\n"))
	a.height = 40

	drive(t, a, tea.WindowSizeMsg{Width: 240, Height: 40})
	if line := callRowsOf(a)[0]; !strings.Contains(line, ".items[] | .number'") {
		t.Fatalf("at a frame wider than the command it is still not drawn whole: %q", line)
	}

	for _, narrow := range []int{100, 60, phoneWidth} {
		drive(t, a, tea.WindowSizeMsg{Width: narrow, Height: 40})
		line := callRowsOf(a)[0]
		if got := ansi.StringWidth(line); got > narrow {
			t.Fatalf("at %d columns the call line is %d cells: %q", narrow, got, line)
		}
		if !strings.Contains(line, glyphMore) {
			t.Fatalf("at %d columns the call line lost its ellipsis: %q", narrow, line)
		}
	}
}
