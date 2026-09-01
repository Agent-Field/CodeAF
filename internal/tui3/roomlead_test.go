package tui3

// THE BOX SAYS WHERE THE WORDS GO.
//
// A room used to stop saying anything the moment somebody typed: the box's
// placeholder named the node, and a placeholder is the one thing on a surface
// that disappears exactly when it is being used. Everything else that knew — the
// pinned header, the legend, the status line — was somewhere the eye was not
// while a sentence was being written.
//
// So the composer carries the room itself, as one tinted segment in front of its
// own prompt: the node's state cell and its name, in the hue the roster paints
// the same node's glyph with. These hold that it is there while typing, that it
// is the right colour, that it gives way on a narrow frame, and that there is
// nothing there at all out in the conversation.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// composerRows is the box's own rows, painted, exactly as the frame lays them
// out — placeholder and all, which is why it goes through the chrome rather than
// through [app.inputBlock] alone.
func composerRows(a *app) []string {
	rows, _, _, caretRow := a.chrome(a.width)
	// The caret is on the block's own caret row, so the block starts however many
	// rows above the frame's caret that its own caret sits.
	block, _, within := a.inputBlock(a.width - len(inputPad))
	top := caretRow - within
	if top < 0 || top+len(block) > len(rows) {
		return nil
	}
	return rows[top : top+len(block)]
}

// composerLine is the row the caret is on, painted, and the caret's column.
func composerLine(a *app) (string, int) {
	rows, _, caretX, caretRow := a.chrome(a.width)
	if caretRow < 0 || caretRow >= len(rows) {
		return "", caretX
	}
	return rows[caretRow], caretX
}

// THE SEGMENT IS THERE WHILE A SENTENCE IS BEING TYPED, which is the whole
// defect: the placeholder that used to say this is gone by then.
func TestTheComposerNamesTheRoomWhileYouType(t *testing.T) {
	a, _, _ := roomApp(t)
	railRun(a)
	clickRailNode(t, a, 1)
	if !a.roomOpen() {
		t.Fatal("the rail did not open a room to type in")
	}

	a.input.setText("fix the flake in the loader")
	line, caretX := composerLine(a)
	if !strings.Contains(plain(line), "Ship the port") {
		t.Fatalf("the box does not name the room it is typing into:\n%q", plain(line))
	}
	if !strings.Contains(plain(line), "fix the flake in the loader") {
		t.Fatalf("the sentence is not on the row the segment is on:\n%q", plain(line))
	}
	// THE CARET IS COUNTED THROUGH THE SEGMENT. A lead the layout drew and the
	// arithmetic did not know about would put the terminal's cursor several cells
	// left of the letter it is on — which is the one way this can be worse than
	// not drawing it at all.
	lead := plain(a.roomLead(a.width - len(inputPad)))
	want := len(inputPad) + ansi.StringWidth(lead) + ansi.StringWidth(prompt) +
		ansi.StringWidth("fix the flake in the loader")
	if caretX != want {
		t.Fatalf("the caret sits at %d, want %d — the segment is %q", caretX, want, lead)
	}
}

// AND IT IS GONE IN THE CONVERSATION. The emptiness law about a mark: there is
// no dim segment and no empty one out here, because there is nowhere else the
// words could be going.
func TestTheComposerCarriesNoSegmentOutsideARoom(t *testing.T) {
	a, _, _ := roomApp(t)
	railRun(a)
	if got := a.roomLead(a.width); got != "" {
		t.Fatalf("a conversation with no room open drew a segment: %q", plain(got))
	}
	clickRailNode(t, a, 1)
	if a.roomLead(a.width) == "" {
		t.Fatal("the open room drew no segment to lose")
	}
	drive(t, a, key("esc"))
	if a.roomOpen() {
		t.Fatal("esc did not leave the room")
	}
	if got := a.roomLead(a.width); got != "" {
		t.Fatalf("the segment outlived the room: %q", plain(got))
	}
	a.input.setText("what is in this repository")
	for _, row := range composerRows(a) {
		if strings.Contains(plain(row), "Ship the port") {
			t.Fatalf("the conversation's box still names a room:\n%q", plain(row))
		}
	}
}

// THE HUE IS THE STATE'S, and it is the roster's own — one table, asked twice
// ([app.taskStateInk]). A segment in a colour of its own would be a colour that
// meant nothing.
func TestTheComposersSegmentWearsTheTasksStateHue(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state session.TaskState
		hue   hue
	}{
		{"running", session.TaskRunning, hueAccent},
		{"failed", session.TaskFailed, hueBad},
		{"needs your look", session.TaskUnverified, hueWarn},
		{"done", session.TaskDone, hueMuted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _, _ := roomApp(t)
			railRun(a)
			clickRailNode(t, a, 1)
			a.taskUpdate(update(1, "Ship the port", tc.state, session.TaskNotice{}))
			lead := a.roomLead(a.width - len(inputPad))
			if lead == "" {
				t.Fatal("a room with a node drew no segment")
			}
			if !strings.Contains(lead, sgr256(tc.hue)) {
				t.Fatalf("the segment is not in the %s hue:\n%q", tc.name, lead)
			}
			// AND THE ROSTER'S GLYPH FOR THE SAME NODE IS THE SAME COLOUR, which is
			// the pairing this test exists to keep.
			if glyph := a.railGlyph(a.tasks[1]); !strings.Contains(glyph, sgr256(tc.hue)) {
				t.Fatalf("the roster's glyph disagrees with the segment: %q", glyph)
			}
		})
	}
}

// IT IS LIFTED OFF THE PAGE ON THE SELECTION TINT, the same background the
// roster's row and the strip's chip wear for the same node — so the three marks
// of "you are in here" are one mark said three times.
func TestTheComposersSegmentIsTintedLikeEveryOtherSelection(t *testing.T) {
	a, _, _ := roomApp(t)
	railRun(a)
	clickRailNode(t, a, 1)
	lead := a.roomLead(a.width - len(inputPad))
	if !strings.Contains(lead, bandSeq()) {
		t.Fatalf("the segment is not lifted off the page:\n%q", lead)
	}
}

// A NARROW FRAME TRIMS THE NAME AND THEN DROPS THE SEGMENT, in that order, and
// the placeholder goes back to carrying the name when it does: something on the
// row has to say it.
func TestTheComposersSegmentGivesWayOnANarrowFrame(t *testing.T) {
	a, _, _ := roomApp(t)
	railRun(a)
	clickRailNode(t, a, 1)

	full := plain(a.roomLead(120 - len(inputPad)))
	if !strings.Contains(full, "Ship the port") {
		t.Fatalf("a wide frame did not spell the whole name: %q", full)
	}
	trimmed := plain(a.roomLead(40 - len(inputPad)))
	if trimmed == "" || trimmed == full {
		t.Fatalf("a 40-column frame did not trim the name: %q", trimmed)
	}
	if strings.Contains(trimmed, "Ship the port") {
		t.Fatalf("the trimmed segment still spells the whole name: %q", trimmed)
	}
	if got := a.roomLead(24 - len(inputPad)); got != "" {
		t.Fatalf("a 24-column frame kept a segment there is no room for: %q", plain(got))
	}

	// AND THE BOX STILL SAYS WHERE THE WORDS GO down there, because the
	// placeholder takes the name back.
	a.width = 24
	line := ""
	for _, row := range composerRows(a) {
		line = plain(row)
	}
	if !strings.Contains(line, "Steer Ship the port") {
		t.Fatalf("the narrow box names no room at all:\n%q", line)
	}

	// AND WHERE THE SEGMENT IS UP, THE PLACEHOLDER STOPS REPEATING THE NAME.
	a.width = 120
	said := ""
	for _, row := range composerRows(a) {
		said = plain(row)
	}
	if strings.Count(said, "Ship the port") != 1 {
		t.Fatalf("the box says the room's name twice on one row:\n%q", said)
	}
	if !strings.Contains(said, roomSteerHere) {
		t.Fatalf("the placeholder does not say what the box does:\n%q", said)
	}
}

// A WRAPPED DRAFT LINES UP UNDER THE TEXT, segment included. A continuation that
// began under the segment would be a sentence with a step in its left margin.
func TestAWrappedDraftLinesUpPastTheRoomsSegment(t *testing.T) {
	a, _, _ := roomApp(t)
	railRun(a)
	clickRailNode(t, a, 1)
	a.width = 60
	a.input.setText("fix the flake in the loader and then run the whole suite again please")

	rows := composerRows(a)
	if len(rows) < 2 {
		t.Fatalf("the draft did not wrap at 60 columns: %q", rows)
	}
	lead := len(inputPad) + ansi.StringWidth(plain(a.roomLead(a.width-len(inputPad)))) +
		ansi.StringWidth(prompt)
	for i, row := range rows[1:] {
		text := plain(row)
		if got := len(text) - len(strings.TrimLeft(text, " ")); got != lead {
			t.Fatalf("continuation row %d is indented %d, want %d:\n%q", i+1, got, lead, text)
		}
	}
}

// A RUN'S PAGE HAS NO NODE AND SO NO STATE, and it says so by being dim rather
// than by borrowing a colour that would mean something.
func TestARunsPageGetsADimSegment(t *testing.T) {
	a, _, _ := roomApp(t)
	a.room = &taskRoom{id: 0, title: "port the parser", unfolded: map[int]bool{}, live: -1, think: -1,
		orch: &orchRun{id: "run-1", goal: "port the parser", seen: map[string]bool{}, fresh: map[string]bool{}}}
	lead := a.roomLead(a.width - len(inputPad))
	if !strings.Contains(plain(lead), "port the parser") {
		t.Fatalf("a run's page does not name itself at the box:\n%q", plain(lead))
	}
	if !strings.Contains(lead, sgr256(hueDim)) {
		t.Fatalf("a run's segment claims a state it does not have:\n%q", lead)
	}
}

// ── THE SEGMENT IS DRAWN ONCE, ON THE DRAFT'S OWN ROW ────────────────────────
//
// The tray takes the block's first row whenever there is one (attach.go's
// [app.chipStrip]), so the draft is not always row zero — and the two lanes that
// splice a placeholder onto the box wrote row zero blind. With a picture on the
// tray or the thinking dial up, the placeholder landed ON the chips: the tray was
// gone, and the room's segment was drawn twice, once on the row it had taken and
// once on the draft row still beneath it. The owner saw both prompts stacked.

// roomLeadRows is how many rows of the composer carry the room's segment. It
// counts what a reader sees rather than what any one function returns, because
// the defect was two functions each drawing one.
func roomLeadRows(a *app, title string) int {
	n := 0
	for _, row := range composerRows(a) {
		if strings.Contains(plain(row), title+" "+prompt) {
			n++
		}
	}
	return n
}

func TestTheRoomsSegmentIsDrawnOnceWhateverIsOnTheTray(t *testing.T) {
	a, _, _ := roomApp(t)
	a.width, a.height = 100, 30
	a.openRoom(7, "budget back bed")

	if got := roomLeadRows(a, "budget back bed"); got != 1 {
		t.Fatalf("a bare box draws the segment %d times, want 1:\n%q", got, composerRows(a))
	}

	// A PICTURE ON THE TRAY, which is what puts a row in front of the draft.
	a.chips = []chip{{path: "/tmp/shot.png"}}
	a.touch()
	if strip := plain(a.chipStrip(a.width - len(inputPad))); !strings.Contains(strip, "shot.png") {
		t.Fatalf("the fixture drew no tray: %q", strip)
	}
	if got := roomLeadRows(a, "budget back bed"); got != 1 {
		t.Fatalf("with a tray up the segment is drawn %d times, want 1:\n%q", got, composerRows(a))
	}
	// AND THE TRAY SURVIVES THE PLACEHOLDER. The row was not just duplicated, it
	// was overwritten: the chip a person had just attached vanished off the frame.
	kept := false
	for _, row := range composerRows(a) {
		if strings.Contains(plain(row), "shot.png") {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("the placeholder took the tray's row:\n%q", composerRows(a))
	}

	// AND A TYPED DRAFT IS THE SAME BOX. The placeholder is gone by now, so what
	// is being asked is whether the segment itself moved.
	a.input.setText("look at this")
	a.touch()
	if got := roomLeadRows(a, "budget back bed"); got != 1 {
		t.Fatalf("a typed draft over a tray draws the segment %d times, want 1:\n%q",
			got, composerRows(a))
	}

	// AND A WRAPPED DRAFT DOES NOT REPEAT THE SEGMENT PER ROW: it is a fact about
	// the box, not a gutter down the side of the sentence.
	a.width = 60
	a.input.setText("fix the flake in the loader and then run the whole suite again please")
	a.touch()
	rows := composerRows(a)
	if len(rows) < 3 {
		t.Fatalf("the fixture did not wrap over a tray: %q", rows)
	}
	if got := roomLeadRows(a, "budget back bed"); got != 1 {
		t.Fatalf("a wrapped draft draws the segment %d times, want 1:\n%q", got, rows)
	}
}

// THE PLACEHOLDER SAYS WHO IS LISTENING AND NOTHING ELSE (ISSUE-126). It used to
// end `… (esc: main)`, which is the way out — already named on the legend one row
// above the box, at every width a room can be drawn at, and again on the top bar
// wherever the bar is drawn. And it named the destination `main` where both of
// those call it `back`.
func TestTheSteerPlaceholderDoesNotRepeatTheWayOut(t *testing.T) {
	a, _, _ := roomApp(t)
	a.height = 30
	for _, width := range []int{200, 120, 80, 60, 40} {
		a.width = width
		a.openRoom(7, "budget back bed")
		box := ""
		for _, row := range composerRows(a) {
			box += plain(row) + "\n"
		}
		if strings.Contains(box, "esc") {
			t.Fatalf("at %d columns the placeholder still names the way out:\n%s", width, box)
		}
		// AND THE WAY OUT IS STILL SAID, one row up, where the slot for it is.
		if leg := plain(a.legend(width)); !strings.Contains(leg, roomLegendWord) {
			t.Fatalf("at %d columns nothing names the way out: %q", width, leg)
		}
	}
}
