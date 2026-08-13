package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/golden"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// §7's hug, as the two rows a reader actually meets: the row you type into and
// the row that says where you are, on a two-tone ground with no line between
// them.
//
// Everything here is frame-shaped. The hug's whole claim is that a reader can
// tell the two planes apart and tell what the surface is doing without reading a
// word, and neither claim can be made about a struct field.

// hugRows is the last two rows of a real frame: the composer's own last row and
// the bar under it.
func hugRows(t *testing.T, app *App, width, height int) (input, bar string) {
	t.Helper()
	rows := strings.Split(app.Frame(width, height), "\n")
	if len(rows) != height {
		t.Fatalf("frame is %d rows, want %d", len(rows), height)
	}
	return rows[height-2], rows[height-1]
}

// TestTheHugIsTwoTonedWhereThereIsAGroundToPaint is the depth cue, measured.
//
// The input row stands one rung LIGHTER than the bar row, and the step between
// them is what says which one you type into — there is no hairline, because §16
// admits exactly two ruled lines in the product and neither of them is here.
func TestTheHugIsTwoTonedWhereThereIsAGroundToPaint(t *testing.T) {
	const width, height = 100, 20
	for _, profile := range []tokens.Profile{tokens.ANSI256, tokens.TrueColor} {
		backend := &fakeBackend{}
		dressedThread(backend)
		app := goldenApp(backend, profile, golden.Theme{Mode: golden.Dark})
		drivePoll(app)
		// The padding row is the composer's last row when the draft is empty,
		// and it is the row whose FLOOR this test is about.
		rows := strings.Split(app.Frame(width, height), "\n")
		bar := rows[height-1]
		var input string
		for i := height - 2; i >= 0; i-- {
			if strings.Contains(ansi.Strip(rows[i]), tokens.GlyphHugEdge) {
				input = rows[i]
				break
			}
		}
		if input == "" {
			t.Fatalf("%v: no composer row in the frame", profile)
		}

		inputGround := seamParams(tokens.HugGroundInput.Bg(profile, tokens.FocusNormal))
		barGround := seamParams(tokens.HugGroundBar.Bg(profile, tokens.FocusNormal))
		if !strings.Contains(input, inputGround) {
			t.Errorf("%v: the input row does not stand on its own rung: %q", profile, input)
		}
		if !strings.Contains(bar, barGround) {
			t.Errorf("%v: the bar row does not stand on its own rung: %q", profile, bar)
		}
		// At 256 the two rungs resolve to different greyscale entries; at
		// truecolor they are different literals. Either way the two rows must
		// not be painted with the same sequence, or there is no depth at all.
		if profile == tokens.TrueColor && inputGround == barGround {
			t.Errorf("%v: the two rows share one ground", profile)
		}
		// And the lighter of the two is the one you type into.
		if tokens.HugGroundInput.Color(tokens.FocusNormal).Luminance() <=
			tokens.HugGroundBar.Color(tokens.FocusNormal).Luminance() {
			t.Errorf("%v: the input row is not the nearer plane", profile)
		}
	}
}

// TestTheHugPaintsNoGroundAndNoRuleWhereItCannot is the honest degradation: at
// 16 colours and at none the two rows are identical plain text, with no
// fallback hairline invented to stand in for the plane — which is precisely the
// mark §16 forbids, on the profiles least able to afford another idiom.
func TestTheHugPaintsNoGroundAndNoRuleWhereItCannot(t *testing.T) {
	const width, height = 100, 20
	for _, profile := range []tokens.Profile{tokens.NoColor, tokens.ANSI16} {
		backend := &fakeBackend{}
		dressedThread(backend)
		app := goldenApp(backend, profile, golden.Theme{Mode: golden.Dark})
		drivePoll(app)
		input, bar := hugRows(t, app, width, height)
		for _, row := range []string{input, bar} {
			if strings.Contains(row, "\x1b[4") {
				t.Errorf("%v: a row painted a ground it cannot trust: %q", profile, row)
			}
			for _, rule := range []string{tokens.GlyphTreeDash, "─", "_"} {
				if strings.Contains(ansi.Strip(row), rule) {
					t.Errorf("%v: the hug fell back to a rule line (%q): %q", profile, rule, row)
				}
			}
		}
		// The edge glyph IS the whole seam here, and it is still drawn.
		frame := ansi.Strip(app.Frame(width, height))
		if !strings.Contains(frame, tokens.GlyphHugEdge) {
			t.Errorf("%v: the state edge is missing, so the hug has no boundary at all", profile)
		}
	}
}

// TestTheHugGivesTheWriterRoomToWrite is the reader-reported amendment: a
// one-row writing area reads as a text field, and the composer is a place to
// describe work in sentences. The floor is two rows of input plus a blank one
// under them, and the blank one is what keeps the words off the bar.
func TestTheHugGivesTheWriterRoomToWrite(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	const width, height = 100, 20
	rows := strings.Split(ansi.Strip(app.Frame(width, height)), "\n")

	edge := -1
	for i, row := range rows {
		if strings.Contains(row, tokens.GlyphHugEdge) {
			edge = i
		}
	}
	if edge < 0 {
		t.Fatalf("no composer row in the frame:\n%s", strings.Join(rows, "\n"))
	}
	// The bar is the last row, and there is a BLANK row between it and the
	// words: the region is prompt, room, breathing space, bar.
	bar := rows[height-1]
	if !strings.Contains(bar, "chat") {
		t.Fatalf("the last row is not the bar: %q", bar)
	}
	// draftFloor rows of writing area (the second one blank while the draft is
	// one line) and draftPad blank rows under them.
	want := height - 1 - draftFloor - draftPad
	if edge != want {
		t.Fatalf("the prompt is on row %d, want %d:\n%s", edge, want,
			strings.Join(rows[edge:], "\n"))
	}
	for i := edge + 1; i < height-1; i++ {
		if strings.TrimSpace(rows[i]) != "" {
			t.Fatalf("row %d should be breathing room, not %q", i, rows[i])
		}
	}
	// And the words really do have somewhere to go: a two-line draft fills the
	// floor without the region having to grow.
	app2 := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	for _, key := range []string{"h", "i", "alt+enter", "t", "h", "e", "r", "e"} {
		press(app2, key)
	}
	rows2 := strings.Split(ansi.Strip(app2.Frame(width, height)), "\n")
	if !strings.Contains(rows2[want], "hi") || !strings.Contains(rows2[want+1], "there") {
		t.Fatalf("a two-line draft did not fit the floor:\n%s", strings.Join(rows2[want:], "\n"))
	}
}

// hugHead is the composer's head as it is drawn: the field's own edge, §19's
// one cell of inner padding inside it, and then the marker.
//
// It exists so no test in this package spells the edge and a glyph adjacent.
// They used to be, and that WAS the defect a reader reported as "does not seem
// to be proper padding for input text": the pad between them is the fix, and a
// test written against the old spelling would pin the bug back on.
func hugHead(glyph string) string { return tokens.GlyphHugEdge + " " + glyph }

// TestTheHugSpeaksEveryStateFromTheFrame walks the states the approved spec
// names and reads the answer off the frame's own gutter — the two cells a
// reader sees without looking for them.
func TestTheHugSpeaksEveryStateFromTheFrame(t *testing.T) {
	const width, height = 100, 20
	gut := func(app *App) string {
		rows := strings.Split(ansi.Strip(app.Frame(width, height)), "\n")
		for i := len(rows) - 1; i >= 0; i-- {
			if at := strings.Index(rows[i], tokens.GlyphHugEdge); at >= 0 {
				return rows[i][at:]
			}
		}
		return ""
	}

	t.Run("idle", func(t *testing.T) {
		app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
		if got := gut(app); !strings.HasPrefix(got, hugHead(tokens.GlyphPromptChat)) {
			t.Fatalf("an idle room's gutter is %q", got)
		}
	})

	t.Run("typing", func(t *testing.T) {
		app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
		for _, key := range []string{"h", "i"} {
			press(app, key)
		}
		got := gut(app)
		if !strings.HasPrefix(got, hugHead(tokens.GlyphPromptChat)+" hi") {
			t.Fatalf("a typed draft's row is %q", got)
		}
	})

	t.Run("streaming", func(t *testing.T) {
		app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
		stream(app, StreamEvent{Kind: StreamStarted, Session: testSession})
		got := gut(app)
		if strings.HasPrefix(got, hugHead(tokens.GlyphPromptChat)) {
			t.Fatalf("a streaming room still draws the resting prompt: %q", got)
		}
		spinning := false
		for _, frame := range tokens.SpinnerFrames {
			if strings.HasPrefix(got, hugHead(frame)) {
				spinning = true
			}
		}
		if !spinning {
			t.Fatalf("a streaming room's gutter is %q, want the spinner at the prompt", got)
		}
	})

	t.Run("question open", func(t *testing.T) {
		backend := &fakeBackend{}
		backend.add(store.Message{
			SessionID: testSession, Role: store.RoleAgent, Body: "which one?",
			Parts: []store.MessagePart{store.QuestionRef(7)},
		})
		app := newTestApp(backend, &fakeCommander{}, nil)
		poll(t, app)
		if got := gut(app); !strings.HasPrefix(got, hugHead(tokens.GlyphNeedsHuman)) {
			t.Fatalf("an open question's gutter is %q, want the amber ask mark", got)
		}
	})

	t.Run("send failed", func(t *testing.T) {
		app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
		app.failSend("the words that did not go", errFailedSend)
		app.refresh()
		if got := gut(app); !strings.HasPrefix(got, hugHead(tokens.GlyphFailed)) {
			t.Fatalf("a failed send's gutter is %q, want the coral cross", got)
		}
		// The words come back so enter can try again, and the row says why.
		if got := app.composer.Draft(); got != "the words that did not go" {
			t.Fatalf("the failed line was not put back: %q", got)
		}
		frame := ansi.Strip(app.Frame(width, height))
		if !strings.Contains(frame, errFailedSend.Error()) {
			t.Fatalf("the failure says nothing on the bar:\n%s", frame)
		}
		// And the next attempt clears it.
		press(app, "enter")
		if app.sendErr != "" {
			t.Fatalf("a new attempt kept the old failure: %q", app.sendErr)
		}
	})

	t.Run("entered", func(t *testing.T) {
		app, _ := boardApp(t)
		press(app, "ctrl+o")
		press(app, "5")
		press(app, "enter")
		frame := ansi.Strip(app.Frame(120, 30))
		// THE TABS SURVIVE. A reader inside a task can still reach `work`.
		for _, tab := range []string{"chat", "work", "notebook"} {
			if !strings.Contains(frame, tab) {
				t.Fatalf("entering a room took the %q tab off the row:\n%s", tab, frame)
			}
		}
		if !strings.Contains(frame, tokens.GlyphScopeUp) {
			t.Fatalf("entering a room drew no trail:\n%s", frame)
		}
	})
}

// TestTheBarNeverSaysUntitled: a thread nobody has named yet is NEW, not
// defective, and the one row a lost reader looks at should not read as a filing
// error.
//
// The chats wave settled it more strongly than the original rule did. The trail
// used to lead with the thread's name and invent a word for it when there was
// none; the title chip owns that fact now and draws NOTHING while the thread is
// unnamed (threads.go's threadName, footer's FocusContext.Thread). So the trail
// says only how deep inside the reader is, and neither surface has a
// placeholder left to say.
func TestTheBarNeverSaysUntitled(t *testing.T) {
	pane := &statusPane{thread: "", breadcrumb: "some step"}
	tail := pane.scopeTail()
	if strings.Contains(strings.ToLower(tail), "untitled") {
		t.Fatalf("the trail calls an unnamed thread untitled: %q", tail)
	}
	if !strings.Contains(tail, "some step") {
		t.Fatalf("the trail dropped the scope the reader is standing in: %q", tail)
	}
	// And the chip is silent rather than inventive: an unnamed thread has no
	// name, and 13.3.4 forbids the id standing in for one.
	if got := pane.focusContext(120).Thread; got != "" {
		t.Fatalf("the title chip named an unnamed thread %q", got)
	}
}

// errFailedSend is a store refusing one write. It is a value rather than a
// fixture because what the test needs is a non-nil error whose words can be
// found on the row.
var errFailedSend = errFailed("the store refused this write")

type errFailed string

func (e errFailed) Error() string { return string(e) }
