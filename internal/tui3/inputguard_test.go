package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// The input ecosystem's acceptance tests: the paste bracket, the deletion keys,
// the arrows over an empty box, and the reasoning block's expand latch.
//
// Each asserts the FACT the behaviour exists for. What a person pasted must
// arrive as one draft; the keys that delete a word must delete a word under
// every name a terminal sends them by; the arrows must move between the
// conversation and the work; and a think somebody opened must stay open.

// ── the paste bracket ───────────────────────────────────────────────────────

// THE DEFECT: a paste whose keys leaked out of the parser's coalescing arrived
// as keystrokes, and its newlines were "enter" — which SUBMITS. Ten lines of a
// stack trace became ten turns.
func TestAPasteWhoseKeysLeakArrivesAsOneDraftAndSubmitsNothing(t *testing.T) {
	agent, a := wired(nil)

	drive(t, a, tea.PasteStartMsg{})
	for _, r := range "panic: nil map" {
		drive(t, a, key(string(r)))
	}
	drive(t, a, key("enter"))
	for _, r := range "\tat main.go:12" {
		if r == '\t' {
			drive(t, a, key("tab"))
			continue
		}
		drive(t, a, key(string(r)))
	}
	// Nothing has been sent, and nothing is in the box yet either: the bracket
	// spends what it holds in ONE edit, at the close.
	if len(agent.sent) != 0 {
		t.Fatalf("a key inside the bracket submitted: %+v", agent.sent)
	}
	drive(t, a, tea.PasteEndMsg{})

	want := "panic: nil map\n\tat main.go:12"
	if a.input.String() != want {
		t.Fatalf("the pasted draft is %q, want %q", a.input.String(), want)
	}
	if len(agent.sent) != 0 {
		t.Fatalf("the paste sent something: %+v", agent.sent)
	}
	// And the enter AFTER the bracket is a submit again.
	drive(t, a, key("enter"))
	if len(agent.sent) != 1 || agent.sent[0] != want {
		t.Fatalf("the draft did not submit whole: %+v", agent.sent)
	}
}

// A KEY INSIDE THE BRACKET CANNOT DO ANYTHING ELSE EITHER. esc would interrupt
// the turn and "/" would open the command list, and neither is a keystroke: they
// are characters in a document somebody copied.
func TestKeysInsideThePasteBracketAreTextAndNothingElse(t *testing.T) {
	agent, a := wired([]session.Event{text(session.EventTextDelta, "working")})
	typeLine(t, a, "go")

	drive(t, a, tea.PasteStartMsg{})
	drive(t, a, key("/"), key("h"), key("esc"))
	if a.state == stateInterrupted || agent.stops != 0 {
		t.Fatal("an esc inside a paste interrupted the turn")
	}
	if a.menu.open {
		t.Fatal("a pasted slash opened the command list mid-paste")
	}
	drive(t, a, tea.PasteEndMsg{})
	if a.input.String() != "/h" {
		t.Fatalf("the bracket kept %q", a.input.String())
	}
}

// THE COALESCED FORM STILL WORKS, and inside an open bracket it JOINS what the
// bracket has collected rather than landing on its own — the two forms can
// arrive in the same paste.
func TestACoalescedPasteInsideAnOpenBracketJoinsIt(t *testing.T) {
	_, a := wired(nil)
	drive(t, a,
		tea.PasteStartMsg{},
		key("a"),
		tea.PasteMsg{Content: "bc"},
		key("d"),
		tea.PasteEndMsg{},
	)
	if a.input.String() != "abcd" {
		t.Fatalf("the bracket assembled %q, want %q", a.input.String(), "abcd")
	}
}

// AN ABANDONED BRACKET IS NOT A BRACKET. A terminal that sends the open and
// never the close would otherwise leave this surface reading every key as text
// forever, which is worse than the defect being fixed.
func TestAnUnclosedPasteBracketGivesTheKeyboardBack(t *testing.T) {
	at := time.Now()
	agent, a := wired(nil)
	a.clock = func() time.Time { return at }

	drive(t, a, tea.PasteStartMsg{})
	drive(t, a, key("h"), key("i"))
	// Long past any gap between two keys of one paste.
	at = at.Add(pasteGrace + time.Second)
	drive(t, a, key("enter"))

	if a.pasting {
		t.Fatal("the bracket is still open")
	}
	// What the bracket held was spent, and the key that ended it was a keystroke
	// again — so the draft went to the model.
	if len(agent.sent) != 1 || agent.sent[0] != "hi" {
		t.Fatalf("the abandoned bracket lost its text or its key: %+v", agent.sent)
	}
}

// A PASTE IS SOMEBODY STARTING WORK, so it puts the welcome box away on the
// same terms every other input does.
func TestAPasteDismissesTheWelcomeBox(t *testing.T) {
	_, a := wired(nil)
	a.welcome = welcome{open: true}
	drive(t, a, tea.PasteMsg{Content: "fix this"})
	if a.welcome.open {
		t.Fatal("the welcome box survived a paste")
	}
	if a.input.String() != "fix this" {
		t.Fatalf("the paste did not reach the draft: %q", a.input.String())
	}
}

// ── the deletion keys ───────────────────────────────────────────────────────

// EVERY NAME A TERMINAL SENDS THEM BY. A word kill that only answered to ctrl+w
// is a word kill most people never find: alt+backspace is what a Mac keyboard
// does, ctrl+backspace is what Windows does, and cmd+delete is the line kill.
func TestTheWordAndLineKillsAnswerToEveryNameTheySendUnder(t *testing.T) {
	for _, name := range []string{"ctrl+w", "alt+backspace", "ctrl+backspace"} {
		_, a := wired(nil)
		a.input.setText("read the config file")
		drive(t, a, key(name))
		if got := a.input.String(); got != "read the config " {
			t.Fatalf("%s deleted %q, want the last word", name, got)
		}
	}
	for _, name := range []string{"ctrl+u", "super+backspace"} {
		_, a := wired(nil)
		a.input.setText("read the config file")
		drive(t, a, key(name))
		if got := a.input.String(); got != "" {
			t.Fatalf("%s left %q, want the line killed", name, got)
		}
	}
	// The line kill takes THIS line, not the draft: on a pasted block only one
	// of the two is a gesture anybody wants.
	_, a := wired(nil)
	a.input.setText("first line\nsecond line")
	drive(t, a, key("ctrl+u"))
	if got := a.input.String(); got != "first line\n" {
		t.Fatalf("ctrl+u killed across the newline: %q", got)
	}
}

// AND THE NAMES ARE THE ONES A TERMINAL ACTUALLY SENDS UNDER. Every table above
// is written in the spelling this surface's key switches match on, and that
// spelling is a fact about a library rather than about this repo: the names come
// out of ultraviolet's decoder, through bubbletea's [tea.KeyPressMsg.String].
// A rename there would leave every binding in this file syntactically perfect
// and permanently unreachable, with no test failing — which is exactly the state
// `cmd+delete` would arrive in.
//
// So the wire is checked directly. These are the escape codes a terminal
// speaking the kitty keyboard protocol sends for the three modified backspaces,
// and the third of them is also the sequence the manual tells an iTerm2 user to
// map `⌘⌫` to, which is a promise this surface has to be able to keep.
func TestTheModifiedBackspacesDecodeToTheNamesWeBindThemUnder(t *testing.T) {
	for _, tc := range []struct{ seq, want string }{
		{"\x1b[127;3u", "alt+backspace"},
		{"\x1b[127;5u", "ctrl+backspace"},
		{"\x1b[127;9u", "super+backspace"},
	} {
		var decoder uv.EventDecoder
		n, event := decoder.Decode([]byte(tc.seq))
		press, ok := event.(uv.KeyPressEvent)
		if !ok {
			t.Fatalf("%q decoded to %#v, want a key press", tc.seq, event)
		}
		if n != len(tc.seq) {
			t.Fatalf("%q was read %d bytes deep, want %d", tc.seq, n, len(tc.seq))
		}
		if got := uv.Key(press).String(); got != tc.want {
			t.Fatalf("%q arrives as %q, but this surface binds %q", tc.seq, got, tc.want)
		}
	}
}

// AND THEY ANSWER TO THE SAME NAMES IN EVERY FILTERABLE BOX. Until this wave
// they did not: `super+backspace` was the composer's alone, so cmd+delete
// cleared the message box and did nothing whatever in the model picker, the
// sessions roster, the connect panels, the memory panel or the settings filter —
// all seven of which walk through the one [listNavigate] this checks. A gesture
// that works in one box and dies in the next is a gesture people stop reaching
// for anywhere.
func TestTheOverlayFilterAnswersToTheSameKillsAsTheMessageBox(t *testing.T) {
	for _, tc := range []struct {
		key, want string
	}{
		{"ctrl+w", "read the config "},
		{"alt+backspace", "read the config "},
		{"ctrl+backspace", "read the config "},
		{"ctrl+u", ""},
		{"super+backspace", ""},
	} {
		var box editor
		box.setText("read the config file")
		listNavigate(key(tc.key), &box, func(int) {}, func() {}, 5)
		if got := box.String(); got != tc.want {
			t.Fatalf("%s left %q, want %q", tc.key, got, tc.want)
		}
	}
}

// ── the arrows over an empty box ────────────────────────────────────────────

// → GOES INTO THE WORK and ← comes back out. Until this wave the keyboard could
// leave a room and could only enter one through a proposal row that had long
// scrolled away.
func TestTheArrowsMoveBetweenTheConversationAndTheWork(t *testing.T) {
	a, _, _ := roomApp(t)

	// → over an empty box opens the first running node's room.
	drive(t, a, key("right"))
	if !a.roomOpen() {
		t.Fatal("→ over an empty box did not open a room")
	}
	// A second → has nowhere else to go, and a door that shut on the second
	// press of a forward key would be answering the gesture with its opposite.
	drive(t, a, key("right"))
	if !a.roomOpen() {
		t.Fatal("→ closed the room it had just opened")
	}
	// ← steps back out.
	drive(t, a, key("left"))
	if a.roomOpen() {
		t.Fatal("← did not leave the room")
	}

	// With a sentence in the box the arrows are the caret's again, whatever else
	// is on screen — which is the whole guard against a nav key eating an edit.
	a.input.setText("abc")
	a.input.cursor = 3
	drive(t, a, key("left"))
	if a.roomOpen() || a.input.cursor != 2 {
		t.Fatalf("← over a sentence navigated instead of moving the caret (cursor %d)", a.input.cursor)
	}
	drive(t, a, key("right"))
	if a.roomOpen() || a.input.cursor != 3 {
		t.Fatalf("→ over a sentence navigated instead of moving the caret (cursor %d)", a.input.cursor)
	}
}

// ←← IS HOME: out of everything, and back at the live edge — which is the one
// thing stepping out of a room deliberately does not do.
func TestATwoTapLeftGoesHome(t *testing.T) {
	at := time.Now()
	a, _, _ := roomApp(t)
	a.clock = func() time.Time { return at }

	drive(t, a, key("right"))
	if !a.roomOpen() {
		t.Fatal("→ did not open a room")
	}
	// A selection and a scrolled-up transcript are the two things home undoes
	// beyond the room, so both are put back after the door reset them.
	a.sel, a.stick = 2, false
	drive(t, a, key("left"))
	at = at.Add(navDoubleTap / 2)
	drive(t, a, key("left"))

	if a.roomOpen() || a.sel != -1 || !a.stick {
		t.Fatalf("←← left room=%v sel=%d stick=%v", a.roomOpen(), a.sel, a.stick)
	}

	// Two taps far enough apart are two steps back, not a home.
	a.sel, a.stick = 2, false
	drive(t, a, key("left"))
	at = at.Add(navDoubleTap * 2)
	drive(t, a, key("left"))
	if a.stick {
		t.Fatal("two slow taps were read as a double-tap")
	}
}

// ── the reasoning block's expand latch ──────────────────────────────────────

// THE DEFECT: the toggle refused while the block was streaming, so the one
// moment a person most wants the model's working — while it is still going, on
// the file it is about to edit — was the one moment they could not have it. And
// the settle that followed collapsed the block over whatever they had chosen.
func TestAThinkOpenedWhileItStreamsShowsTheWholeBufferAndStaysOpen(t *testing.T) {
	a := reasoningLines(t, "one", "two", "three", "four", "five")

	// Closed, it is the reading window: a header and three lines.
	if got := len(thoughtBlockRows(t, a)); got != 1+thoughtLive {
		t.Fatalf("the closed block draws %d rows, want %d", got, 1+thoughtLive)
	}

	// ctrl+e over an empty box opens it, mid-stream.
	drive(t, a, key("ctrl+e"))
	if !a.toggledThoughtOpen() {
		t.Fatal("ctrl+e did not open the streaming block")
	}
	body := strings.Join(plainRows(a), "\n")
	for _, want := range []string{"one", "two", "three", "four", "five"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the opened block resolved from the window and not the buffer, %q is missing:\n%s",
				want, body)
		}
	}

	// The turn says something that is not reasoning, which settles the block —
	// and the person's choice survives it.
	drive(t, a, streamEventMsg{gen: a.gen, ev: text(session.EventTextDelta, "so:")})
	if !a.toggledThoughtOpen() {
		t.Fatal("the settle collapsed a block the person had opened")
	}
	if !strings.Contains(strings.Join(plainRows(a), "\n"), "one") {
		t.Fatalf("the settled block lost the words it was showing:\n%s",
			strings.Join(plainRows(a), "\n"))
	}

	// And the same key closes it again.
	drive(t, a, key("ctrl+e"))
	if a.toggledThoughtOpen() {
		t.Fatal("ctrl+e did not close what it opened")
	}
}

// AN UNTOUCHED BLOCK STILL COLLAPSES. The latch is about the person's choice,
// not about disabling the automatic collapse for everybody.
func TestAThinkNobodyTouchedCollapsesOnItsOwn(t *testing.T) {
	a := reasoningLines(t, "one", "two")
	drive(t, a, streamEventMsg{gen: a.gen, ev: text(session.EventTextDelta, "so:")})
	if a.toggledThoughtOpen() {
		t.Fatal("a block nobody opened came back expanded")
	}
	if !strings.Contains(plain(frame(a)), "thought for ") {
		t.Fatalf("the block did not collapse to its one row:\n%s", plain(frame(a)))
	}
}

// toggledThoughtOpen reports whether the newest reasoning block is expanded.
func (a *app) toggledThoughtOpen() bool {
	for i := len(a.entries) - 1; i >= 0; i-- {
		if a.entries[i].kind == entryThinking {
			return a.entries[i].open
		}
	}
	return false
}
