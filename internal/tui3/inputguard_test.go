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

// ── the questions above the router, and home over all of them ───────────────

// A QUESTION NOBODY CAN SEE IS A QUESTION NOBODY CAN ANSWER.
//
// Three rungs of the router sit above home: the connect offer and the harness
// offer swallow every key while they are up, and the proposal takes answer and
// model keys whenever the CONVERSATION's box is empty — which says nothing about
// home's box, the one a person on that screen is actually typing into. So a key
// meant for a search on home could answer a question that was off screen, and
// every other letter did nothing at all.
//
// The rule is one rule and it is stated in both directions: a question that
// ARRIVES takes home down, exactly as the approval question always has
// (app.go's EventConsentRequest), and a question found behind a home somebody
// opened over it hands the letters back. Neither half is enough alone — the
// first would leave a home opened afterwards deaf, the second would leave a
// question standing where nobody could reach it.
func TestHomeKeepsItsLettersOverEveryQuestionAboveIt(t *testing.T) {
	for _, tc := range []struct {
		name string
		// answer is the key that answers this question in its OWN grammar, which
		// is no longer one letter for all three: the harness offer is on the
		// block now and the block answers by the NUMBER beside an answer
		// (questionkeys.go's one table).
		answer string
		// start builds a surface with this question's session under it, the
		// event that raises the question, and a way to ask whether it has been
		// answered.
		start func(t *testing.T) (*app, session.Event, func() bool)
	}{
		{"the connect offer", "y", func(t *testing.T) (*app, session.Event, func() bool) {
			agent, a, _ := connectApp(t)
			return a, askConnectEvent("c1", "notion", "Notion"),
				func() bool { return len(agent.resolved) > 0 }
		}},
		{"the harness offer", session.HarnessRunKey, func(t *testing.T) (*app, session.Event, func() bool) {
			agent := &harnessAgent{fakeAgent: &fakeAgent{model: "m"}}
			a := newTestApp(agent)
			return a, session.Event{
					Kind: session.EventHarnessOffer, ID: 3, Text: "research",
					Hint: "Research a question across sources and write a report",
				},
				func() bool { return len(agent.answers) > 0 }
		}},
		{"the task proposal", "y", func(t *testing.T) (*app, session.Event, func() bool) {
			a, agent, _ := taskApp(t)
			return a, proposal(a, 7, 0), func() bool { return len(agent.answered) > 0 }
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The question arriving takes home down, so it is asked where it can
			// be read and answered.
			a, ev, _ := tc.start(t)
			a.openHome()
			drive(t, a, streamOf(a, ev))
			if a.at(pageHome) {
				t.Fatal("home stayed up over a question the session is waiting on")
			}

			// And home opened OVER a live question keeps every letter.
			a, ev, answered := tc.start(t)
			drive(t, a, streamOf(a, ev))
			a.openHome()
			drive(t, a, key("y"))
			if answered() {
				t.Fatal("a letter typed on home answered the question behind it")
			}
			if got := a.home.box.String(); got != "y" {
				t.Fatalf("the letter reached home's box as %q, want %q", got, "y")
			}

			// With home closed it answers in that question's own grammar. A task
			// proposal keeps the y in its box until enter sees the complete answer;
			// the two modal offers still answer on their single key.
			a, ev, answered = tc.start(t)
			drive(t, a, streamOf(a, ev))
			// THE BLOCK TAKES NO KEY FROM A QUESTION IT HAS NEVER DRAWN
			// (question.go's [app.questionKey]), and the harness draws no
			// frames — so the question is put on screen here, which is what a
			// terminal does before a hand reaches the keyboard.
			a.chrome(a.width)
			settleAsk(a)
			drive(t, a, key(tc.answer))
			if tc.name == "the task proposal" {
				if answered() {
					t.Fatal("y answered the task proposal before enter")
				}
				drive(t, a, key("enter"))
			}
			if !answered() {
				t.Fatal("the visible question did not take its answer with home closed")
			}
		})
	}
}

// AND THE STANDING CARD IS THE FOURTH QUESTION UNDER THAT RULE.
//
// It is not a row of the table above because home answers a standing card by
// DIGIT rather than by letter — the card's own answers are enter, esc and its
// numbered chips (standing.go), and home answers it in place on the asking
// window's row (homeband_answer.go, which is where that half is held). What is
// the same is the half this holds: the card arriving takes home down, so a
// decision the session is blocked on is asked on the screen the person is
// looking at rather than behind it.
func TestAStandingCardArrivingTakesHomeDownLikeTheQuestionsAboveIt(t *testing.T) {
	a, _, _ := standApp(t)
	a.openHome()
	drive(t, a, streamEventMsg{gen: a.gen, ev: standProposal(a, session.StandingNotice{
		WhenWords: "Mondays at 9am", CostWords: "about $0.02 a run",
	})})
	if a.at(pageHome) {
		t.Fatal("home stayed up over a standing card the session is waiting on")
	}
}

// ── the deletion keys ───────────────────────────────────────────────────────

// EVERY NAME A TERMINAL SENDS THEM BY. A word kill that only answered to one
// spelling is a word kill most people never find: alt+backspace is what a Mac
// keyboard does, ctrl+backspace is what Windows does, and cmd+delete is the line
// kill.
//
// ctrl+w IS NOT ON THIS LIST ANY MORE and it is not an omission: it is the chord
// that shuts the tab in front now (tabclosekey.go), read far above the box, and
// tabclosekey_test.go holds both halves of that trade. It still edits the filter
// of every modal overlay, which the test below this one walks.
func TestTheWordAndLineKillsAnswerToEveryNameTheySendUnder(t *testing.T) {
	for _, name := range []string{"alt+backspace", "ctrl+backspace"} {
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

// AND SO ARE THE CARET JUMPS, for the reason above and one that already cost a
// gesture: cmd+←/→ were bound as `super+left`/`super+right` and were DEAD ON
// EVERY TERMINAL. A modified arrow and a modified letter travel by different
// roads through ultraviolet — `CSI 127;9u` is read by the kitty reader, which
// spells modifier 9 `super`, while `CSI 1;9D` is read against the static xterm
// table, where the ninth column is `meta` — so cmd+delete worked, cmd+← arrived
// as a name nothing bound, and no test in this package could tell: every table
// above is written in the spelling the switch matches on, and the switch matched
// itself perfectly.
//
// So every caret jump is driven from the WIRE here: the bytes a terminal sends,
// through the real decoder, into the real router, and the caret is asked where
// it went.
func TestTheCaretJumpsDecodeToTheNamesWeBindThemUnder(t *testing.T) {
	const draft = "read the config file" // 20 runes; "file" starts at 16
	for _, tc := range []struct {
		seq, name  string
		from, want int
	}{
		// cmd+←/→ — the line's ends, and the whole reason this test exists.
		{"\x1b[1;9D", "meta+left", len(draft), 0},
		{"\x1b[1;9C", "meta+right", 0, len(draft)},
		// option+←/→ on a Mac terminal that keeps option a modifier.
		{"\x1b[1;3D", "alt+left", len(draft), 16},
		{"\x1b[1;3C", "alt+right", 0, 4},
		// The esc-b / esc-f a profile sends instead, which is readline's own.
		{"\x1bb", "alt+b", len(draft), 16},
		{"\x1bf", "alt+f", 0, 4},
		// And ctrl+←/→, which is what Windows and Linux send.
		{"\x1b[1;5D", "ctrl+left", len(draft), 16},
		{"\x1b[1;5C", "ctrl+right", 0, 4},
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
		if got := uv.Key(press).String(); got != tc.name {
			t.Fatalf("%q arrives as %q, but this surface binds %q", tc.seq, got, tc.name)
		}
		_, a := wired(nil)
		a.input.setText(draft)
		a.input.cursor = tc.from
		drive(t, a, tea.KeyPressMsg(press))
		if a.input.cursor != tc.want {
			t.Fatalf("%q (%s) left the caret at %d, want %d", tc.seq, tc.name, a.input.cursor, tc.want)
		}
		if a.input.String() != draft {
			t.Fatalf("%q (%s) changed the draft: %q", tc.seq, tc.name, a.input.String())
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
	// THE RUNNING TURN'S WORK IS OPENED FIRST, because the conversation now
	// stands a running turn's machinery — the reasoning block with it — behind
	// three compact lines until somebody asks for it (livesteps.go). The block
	// below is what a reader sees once they have asked.
	showLiveWork(t, a)

	// Closed, it is the reading window: a header and three lines.
	if got := len(thoughtBlockRows(t, a)); got != 1+thoughtLive {
		t.Fatalf("the closed block draws %d rows, want %d", got, 1+thoughtLive)
	}

	// The block's own door opens it, mid-stream. It is called here rather than
	// pressed as `ctrl+e`, because over a running turn that key now belongs to the
	// whole work the block sits inside (workfold.go's [app.toggleLatestWorkfold]);
	// a click on the block is the gesture that reaches this one (thinking.go).
	if !a.toggleLatestThought() {
		t.Fatal("the block's own door did not open the streaming block")
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

	// And the same door closes it again.
	if !a.toggleLatestThought() {
		t.Fatal("the block's own door stopped answering")
	}
	if a.toggledThoughtOpen() {
		t.Fatal("the door did not close what it opened")
	}
}

// AN UNTOUCHED BLOCK STILL COLLAPSES. The latch is about the person's choice,
// not about disabling the automatic collapse for everybody.
func TestAThinkNobodyTouchedCollapsesOnItsOwn(t *testing.T) {
	a := reasoningLines(t, "one", "two")
	showLiveWork(t, a)
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
