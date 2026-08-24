package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/history"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The wave-2 surface: what a resumed session shows, what ctrl+c means, how a
// draft is composed, recalled, completed and kept.

// keyed spells the keys this file presses that the shared helper does not know.
func keyed(s string) tea.KeyPressMsg {
	switch s {
	case "alt+enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt}
	case "ctrl+j":
		return tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	return key(s)
}

// ── 1. replay on resume ─────────────────────────────────────────────────────

func TestAResumedSessionReplaysItsTail(t *testing.T) {
	agent := &fakeAgent{model: "m", past: []session.DisplayEntry{
		{Role: "user", Text: "what does bar.go do?"},
		{Role: "assistant", Text: ""},
		{Role: "tool", Tool: "read", Hint: "read foo/bar.go"},
		{Role: "tool", Text: "a hundred lines of file"}, // the RESULT message
		{Role: "assistant", Text: "# it parses\n\nnothing else."},
		{Role: "note", Text: "compacted 12 messages"},
	}}
	a := newApp(t.Context(), Options{Agent: agent, Workspace: "/tmp/lab"})
	a.width, a.height = 60, 24
	a.pal = newPalette(tokens.ANSI256, false)
	a.touch()

	got := plain(frame(a))
	for _, want := range []string{
		"› what does bar.go do?", // the person's own words, with the glyph
		"▸ worked",               // completed machinery is folded on replay
		"it parses",              // through renderMarkdown
		"nothing else.",
		"⚭ compacted 12 messages", // a note is a divider
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the replayed frame is missing %q:\n%s", want, got)
		}
	}
	// A tool RESULT is not a call and is not drawn as one.
	if strings.Contains(got, "a hundred lines of file") {
		t.Fatalf("replay drew a tool result:\n%s", got)
	}
	// Nothing replayed is alive: no spinner, no running mark.
	for i := range a.entries {
		if a.entries[i].kind == entryTool && a.entries[i].status == toolRunning {
			t.Fatal("a replayed call is still spinning")
		}
	}
	// The person's message opened a turn, so the next live one is turn 2.
	if a.turn != 1 {
		t.Fatalf("replay left the turn counter at %d, want 1", a.turn)
	}
}

func TestReplayDrawsOnlyTheTail(t *testing.T) {
	agent := &fakeAgent{model: "m"}
	for i := 0; i < replayTail*2; i++ {
		agent.past = append(agent.past, session.DisplayEntry{Role: "user", Text: "line " + itoa(i)})
	}
	a := newApp(t.Context(), Options{Agent: agent, Workspace: "/tmp/lab"})

	users := 0
	for i := range a.entries {
		if a.entries[i].kind == entryUser {
			users++
		}
	}
	if users != replayTail {
		t.Fatalf("replay drew %d entries, want the last %d", users, replayTail)
	}
	if a.entries[0].text != "line "+itoa(replayTail) {
		t.Fatalf("replay started at %q, want the tail", a.entries[0].text)
	}
}

// ── 2. ctrl+c ───────────────────────────────────────────────────────────────
//
// THE RULE THESE TESTS PIN: mid-turn ctrl+c is the interrupt and nothing else;
// at rest it takes TWO presses inside [quitArmWindow] to leave, and the first
// one says so in the hint slot (quitarm.go).

// quitting is a surface with a pinned clock, so the arm window can be walked
// rather than waited out.
func quitting(t *testing.T, agent Agent) (*app, func(time.Duration)) {
	t.Helper()
	a := newTestApp(agent)
	// Wide enough for the legend to carry its hint slot at all: under hudTight
	// the cells go to the conversation's name (render.go's [app.legendRight]).
	a.width = 100
	now := time.Now()
	a.clock = func() time.Time { return now }
	return a, func(d time.Duration) {
		now = now.Add(d)
		// The frame clock is what runs the window down — no goroutine of its own.
		drive(t, a, frameMsg{})
	}
}

// quitCmd presses ctrl+c and hands back whatever command came of it, without
// [drive]'s message pump: what is being asserted is the command itself.
func quitCmd(a *app) tea.Cmd {
	_, cmd := a.Update(keyed("ctrl+c"))
	return cmd
}

// isQuit reports whether a command is the door.
func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, quit := cmd().(tea.QuitMsg)
	return quit
}

func TestCtrlCInterruptsAWorkingTurnAndTakesTwoPressesAtRest(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		text(session.EventTextDelta, "thinking about it"),
	}}}
	a, _ := quitting(t, agent)
	typeLine(t, a, "long one")
	if a.state != stateWorking {
		t.Fatalf("state is %v, want working", a.state)
	}

	// Mid-turn: ctrl+c is esc. It stops the model and stays in the room.
	if cmd := quitCmd(a); cmd != nil {
		t.Fatal("ctrl+c mid-turn must not return a command — it interrupts, it does not leave")
	}
	if agent.stops != 1 {
		t.Fatalf("ctrl+c did not interrupt (%d)", agent.stops)
	}
	if agent.closes != 0 {
		t.Fatal("ctrl+c mid-turn closed the session")
	}
	// AND IT DID NOT ARM. The press was aimed at the model and it hit the model.
	if a.quitArmed() {
		t.Fatal("an interrupting ctrl+c armed the door")
	}
	// AND THE STATUS LINE SAYS THE STOP LANDED. It is asked of the line rather
	// than of the whole frame, which is what this assertion always meant to say:
	// the stop's own note is in the transcript on the same frame, so a search
	// over the frame passed on the note whatever the status segment said.
	if !strings.Contains(plain(a.status(a.width)), stoppingWord) {
		t.Fatalf("the status line has to say %q:\n%s", stoppingWord, plain(frame(a)))
	}

	// At rest the first press arms and says so, and nothing closes. It returns
	// the frame clock rather than nothing — the window has an end to reach — so
	// what is asserted is that it is not the DOOR.
	if cmd := quitCmd(a); isQuit(cmd) {
		t.Fatal("the first ctrl+c at rest quit")
	}
	if !a.quitArmed() {
		t.Fatal("the first ctrl+c at rest did not arm the door")
	}
	if agent.closes != 0 {
		t.Fatalf("the first ctrl+c closed the agent (%d)", agent.closes)
	}
	if got := a.hintWord(); got != quitArmWord {
		t.Fatalf("hint slot = %q, want %q", got, quitArmWord)
	}
	if got := plain(frame(a)); !strings.Contains(got, quitArmWord) {
		t.Fatalf("the armed frame does not say so:\n%s", got)
	}

	// The second press inside the window is the door.
	if cmd := quitCmd(a); !isQuit(cmd) {
		t.Fatal("the second ctrl+c inside the window has to quit")
	}
	if agent.closes != 1 {
		t.Fatalf("ctrl+c closed the agent %d times", agent.closes)
	}
}

// THE TWO-TAP MID-TURN IS THE DEFECT THIS WAVE EXISTS FOR: press it again,
// harder, because the first one did not seem to land. It stops the model and
// then arms — and it must not leave.
func TestADoubleTapMidTurnInterruptsAndArmsWithoutQuitting(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		text(session.EventTextDelta, "thinking about it"),
	}}}
	a, _ := quitting(t, agent)
	typeLine(t, a, "long one")

	if cmd := quitCmd(a); cmd != nil {
		t.Fatal("the first ctrl+c mid-turn returned a command")
	}
	if cmd := quitCmd(a); isQuit(cmd) {
		t.Fatal("a double-tap mid-turn quit — the second press has to arm, not leave")
	}
	if !a.quitArmed() {
		t.Fatal("the second press of the double-tap did not arm the door")
	}
	if agent.closes != 0 {
		t.Fatalf("a double-tap mid-turn closed the agent (%d)", agent.closes)
	}
	if agent.stops != 1 {
		t.Fatalf("the second press interrupted a turn that was already stopped (%d)", agent.stops)
	}
}

// A WINDOW THAT LAPSES TAKES THE MEANING WITH IT: the next press arms again
// rather than leaving, and the hint slot has already let go of the sentence.
func TestTheQuitArmLapsesAndTheNextPressArmsAgain(t *testing.T) {
	agent := &fakeAgent{model: "m"}
	a, advance := quitting(t, agent)

	if cmd := quitCmd(a); isQuit(cmd) {
		t.Fatal("the first ctrl+c quit")
	}
	advance(quitArmWindow + time.Millisecond)
	if a.quitArmed() || !a.quitArm.IsZero() {
		t.Fatal("the arm survived its window")
	}
	if got := a.hintWord(); got == quitArmWord {
		t.Fatal("the hint slot still offers the door after the window lapsed")
	}

	if cmd := quitCmd(a); isQuit(cmd) {
		t.Fatal("a press after the window quit instead of arming again")
	}
	if !a.quitArmed() {
		t.Fatal("the press after the window did not re-arm")
	}
	if agent.closes != 0 {
		t.Fatalf("a lapsed window still closed the agent (%d)", agent.closes)
	}
}

// ANY OTHER KEY ANSWERS THE QUESTION. A person who armed the door and then went
// on typing has said no.
func TestAnotherKeyDisarmsTheDoor(t *testing.T) {
	agent := &fakeAgent{model: "m"}
	a, _ := quitting(t, agent)

	quitCmd(a)
	if !a.quitArmed() {
		t.Fatal("the first ctrl+c did not arm the door")
	}
	drive(t, a, key("a"))
	if a.quitArmed() {
		t.Fatal("a keystroke left the door armed underneath it")
	}
	if cmd := quitCmd(a); isQuit(cmd) {
		t.Fatal("ctrl+c after another key quit on the first press")
	}
	if agent.closes != 0 {
		t.Fatalf("the agent was closed (%d)", agent.closes)
	}
}

// THE MODAL STAYS UP. ctrl+c is read above every overlay — leaving is never
// modal — so the first press arms without closing anything, and the hint slot
// says the door's word rather than the overlay's keys.
func TestCtrlCWithTheModelPickerUpArmsWithoutClosingIt(t *testing.T) {
	agent := &fakeAgent{model: "openai/gpt-4.1-mini"}
	a := pickerApp(t, agent, pickerCatalog)
	now := time.Now()
	a.clock = func() time.Time { return now }

	typeLine(t, a, "/model")
	if !a.pick.open {
		t.Fatal("/model did not open the picker")
	}

	if cmd := quitCmd(a); isQuit(cmd) {
		t.Fatal("ctrl+c over the picker quit on the first press")
	}
	if !a.pick.open {
		t.Fatal("ctrl+c closed the picker instead of arming the door")
	}
	if !a.quitArmed() {
		t.Fatal("ctrl+c over the picker did not arm the door")
	}
	if got := a.hintWord(); got != quitArmWord {
		t.Fatalf("hint slot = %q, want the door's word %q", got, quitArmWord)
	}

	if cmd := quitCmd(a); !isQuit(cmd) {
		t.Fatal("the second ctrl+c over the picker has to quit")
	}
}

// A TURN THAT FINISHED BETWEEN THE TWO PRESSES DOES NOT SHORTEN THE ROAD OUT.
// session.EventTurnDone flips the state to idle one Update cycle before the
// stream close drains what is parked, and a press landing in that gap used to
// leave and take the parked message with it.
func TestATurnFinishingBetweenPressesStillNeedsTheSecond(t *testing.T) {
	a, agent := streaming(t, "reading the tree. ")
	now := time.Now()
	a.clock = func() time.Time { return now }
	typeLine(t, a, "do much more of a deep research please")
	if len(a.parks) != 1 {
		t.Fatalf("the second message was not parked: %+v", a.parks)
	}

	// The turn reports done; the stream has not closed yet, so nothing has
	// drained. This is exactly the gap.
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventTurnDone}})
	if a.state == stateWorking {
		t.Fatal("the turn did not settle — this test is not standing in the gap it is about")
	}

	if cmd := quitCmd(a); isQuit(cmd) {
		t.Fatal("a press in the gap at the end of a turn quit on the first press")
	}
	if len(a.parks) != 1 {
		t.Fatalf("the parked message was lost: %+v", a.parks)
	}
	if agent.closes != 0 {
		t.Fatalf("the agent was closed (%d)", agent.closes)
	}
	if cmd := quitCmd(a); !isQuit(cmd) {
		t.Fatal("the second press has to quit")
	}
}

// WHAT IS PARKED COMES BACK NEXT LAUNCH. The words were typed and entered; a
// session closed before the answer landed folds them into the draft file rather
// than dropping them.
func TestQuitFoldsParkedMessagesIntoTheDraft(t *testing.T) {
	a, _ := streaming(t, "reading the tree. ")
	path := filepath.Join(t.TempDir(), "drafts", "one")
	a.draftFile = path
	typeLine(t, a, "first correction")
	typeLine(t, a, "second correction")
	typeInto(t, a, "and this is still in the box")

	if cmd := a.quit(); !isQuit(cmd) {
		t.Fatal("quit did not return the door")
	}
	kept := readDraft(path)
	want := "and this is still in the box\nfirst correction\nsecond correction"
	if kept != want {
		t.Fatalf("the draft file holds %q, want %q", kept, want)
	}
}

// WHAT THE SECOND PRESS WOULD STOP IS NAMED BEFORE IT HAPPENS — and named
// NOTHING when nothing is running, which is the emptiness law.
func TestTheArmedHintNamesRunningWorkAndOnlyWhenThereIsSome(t *testing.T) {
	agent := &fakeAgent{model: "m"}
	a, _ := quitting(t, agent)

	if got := a.quitWorkWord(); got != "" {
		t.Fatalf("an idle surface named %q as running work", got)
	}
	if got := a.quitHint(); got != quitArmWord {
		t.Fatalf("the armed hint is %q with nothing running, want %q", got, quitArmWord)
	}

	a.tasks = map[uint64]*taskNode{
		1: {id: 1, state: session.TaskRunning},
		2: {id: 2, state: session.TaskRunning},
	}
	a.taskOrder = []uint64{1, 2}
	if got := a.quitWorkWord(); got != "2 tasks" {
		t.Fatalf("two running tasks are %q", got)
	}

	a.taskOrder = []uint64{1}
	if got := a.quitWorkWord(); got != "a task" {
		t.Fatalf("one running task is %q, want the article rather than the digit", got)
	}
	if got := a.quitHint(); got != quitArmWord+" · a task will stop" {
		t.Fatalf("the armed hint is %q", got)
	}

	// And a background job counts on the same terms, from the number the frame
	// already draws (render.go's ambient segment).
	a.entries = append(a.entries, entry{
		kind: entryTool, tool: "bash", status: toolOK,
		detail: toolDetail{Args: `{"command":"serve","background":"true"}`, Output: "job 1 started"},
	})
	a.hudStale = true
	if got := a.quitWorkWord(); got != "a task and a job" {
		t.Fatalf("a task and a job are %q", got)
	}
}

func TestTheOpeningHintNamesBothDoors(t *testing.T) {
	a := newApp(t.Context(), Options{Agent: &fakeAgent{model: "m"}, Workspace: "/tmp/lab"})
	// IT HAS TO BE TRUE ON THE FIRST FRAME, where nothing is running: esc is the
	// interrupt when there is a turn, and ctrl+c takes two presses always.
	if !strings.Contains(plain(frame(a)), "esc interrupts · ctrl+c twice quits") {
		t.Fatalf("the hint has to name both doors truthfully:\n%s", plain(frame(a)))
	}
	if !strings.Contains(helpText(""), "alt+enter") {
		t.Fatalf("help has to name the newline key:\n%s", helpText(""))
	}
}

// ── 3. multi-line input and paste ───────────────────────────────────────────

func TestAltEnterAndCtrlJOpenALineAndEnterStillSubmits(t *testing.T) {
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)

	typeInto(t, a, "one")
	drive(t, a, keyed("alt+enter"))
	typeInto(t, a, "two")
	drive(t, a, keyed("ctrl+j"))
	typeInto(t, a, "three")

	if got := a.input.String(); got != "one\ntwo\nthree" {
		t.Fatalf("the draft is %q", got)
	}
	// Three logical lines are three rows in the box, and the caret is on the
	// last of them.
	rows, _, caretRow := a.inputBlock(a.width)
	if len(rows) != 3 || caretRow != 2 {
		t.Fatalf("the box drew %d rows with the caret on %d, want 3 and 2", len(rows), caretRow)
	}
	if !strings.HasPrefix(plain(rows[0]), "› one") || !strings.HasPrefix(plain(rows[2]), "  three") {
		t.Fatalf("the block is laid out wrong:\n%q", rows)
	}

	drive(t, a, key("enter"))
	if len(agent.sent) != 1 || agent.sent[0] != "one\ntwo\nthree" {
		t.Fatalf("submit did not keep the newlines: %q", agent.sent)
	}
	if a.input.String() != "" {
		t.Fatalf("the box kept %q after submit", a.input.String())
	}
}

func TestAPastedBlockArrivesWholeAndSubmitsAsTyped(t *testing.T) {
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)

	// Bracketed paste: charm.land/bubbletea/v2 delivers the whole clipboard in
	// one tea.PasteMsg (paste.go), bracketed mode being on by default.
	drive(t, a, tea.PasteMsg{Content: "fix this:\n\tpanic: nil map\n\tat main.go:12"})
	if !strings.Contains(a.input.String(), "\n\tpanic") {
		t.Fatalf("the paste did not arrive whole: %q", a.input.String())
	}
	drive(t, a, key("enter"))
	if len(agent.sent) != 1 || !strings.Contains(agent.sent[0], "\n") {
		t.Fatalf("the paste was flattened: %q", agent.sent)
	}
}

func TestALongDraftScrollsInsideTheBoxAndLeavesTheChromeAlone(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	tall := a.viewHeight()

	lines := make([]string, 0, 12)
	for i := 0; i < 12; i++ {
		lines = append(lines, "line "+itoa(i))
	}
	a.input.setText(strings.Join(lines, "\n"))
	a.touch()

	rows, _, caretRow := a.inputBlock(a.width)
	if len(rows) != draftRows {
		t.Fatalf("a twelve-line draft drew %d rows, want the %d-row ceiling", len(rows), draftRows)
	}
	if caretRow != draftRows-1 {
		t.Fatalf("the caret is on row %d, want the last visible one", caretRow)
	}
	if !strings.Contains(plain(rows[len(rows)-1]), "line 11") {
		t.Fatalf("the box did not scroll to the caret:\n%q", rows)
	}
	// The conversation gave up the rows, and the status line and box did not.
	if got := a.viewHeight(); got != tall-(draftRows-1) {
		t.Fatalf("the conversation is %d rows, want %d", got, tall-(draftRows-1))
	}
	// The status line is the LAST row of the frame, and a six-line paste does
	// not push it anywhere (view.go).
	painted := strings.Split(plain(frame(a)), "\n")
	if len(painted) != a.height || !strings.Contains(painted[len(painted)-1], a.model) {
		t.Fatalf("the frame lost its status line:\n%s", strings.Join(painted, "\n"))
	}
}

func TestALineWiderThanTheBoxWrapsAtAWordAndKeepsTheCaret(t *testing.T) {
	pal := newPalette(tokens.ANSI256, false)
	e := &editor{}
	e.setText("abcdefghij klmnopqrstuvwx") // 25 runes, a box 18 wide

	rows, caretX, caretRow := draftBlock(e, pal, 20, draftRows, "", "")
	if len(rows) != 2 {
		t.Fatalf("the line drew %d rows:\n%q", len(rows), rows)
	}
	if got := plain(rows[0]); got != "› abcdefghij " {
		t.Fatalf("the first row is %q — it has to break at the space", got)
	}
	if got := plain(rows[1]); got != "  klmnopqrstuvwx" {
		t.Fatalf("the second row is %q", got)
	}
	if caretRow != 1 || caretX != 16 {
		t.Fatalf("the caret is at %d,%d, want 16,1", caretX, caretRow)
	}

	// The caret walks back onto the first row with the text.
	e.cursor = 3
	_, caretX, caretRow = draftBlock(e, pal, 20, draftRows, "", "")
	if caretRow != 0 || caretX != 5 {
		t.Fatalf("the caret is at %d,%d, want 5,0", caretX, caretRow)
	}

	// An empty draft is one row with the caret against the prompt.
	rows, caretX, caretRow = draftBlock(&editor{}, pal, 20, draftRows, "", "")
	if len(rows) != 1 || caretX != 2 || caretRow != 0 {
		t.Fatalf("an empty box drew %d rows with the caret at %d,%d", len(rows), caretX, caretRow)
	}
}

// ── 4. history recall ───────────────────────────────────────────────────────

func recallApp(t *testing.T, entries ...history.Entry) (*app, *history.Store) {
	t.Helper()
	store := history.New(filepath.Join(t.TempDir(), "history.jsonl"))
	t.Cleanup(func() { _ = store.Close() })
	for _, entry := range entries {
		store.Append(entry.Text, entry.Cwd)
	}
	a := newApp(t.Context(), Options{
		Agent: &fakeAgent{model: "m"}, Workspace: "/tmp/lab", History: store,
	})
	a.width, a.height = 60, 20
	a.pal = newPalette(tokens.ANSI256, false)
	a.entries = nil
	a.touch()
	return a, store
}

func TestRecallWalksThisDirectoryFirstAndKeepsTheDraft(t *testing.T) {
	a, _ := recallApp(t,
		history.Entry{Text: "somewhere else", Cwd: "/tmp/other"},
		history.Entry{Text: "older here", Cwd: "/tmp/lab"},
		history.Entry{Text: "newer here", Cwd: "/tmp/lab"},
	)
	typeInto(t, a, "half a sentence")

	// Newest first, this directory before the rest.
	for _, want := range []string{"newer here", "older here", "somewhere else"} {
		drive(t, a, key("up"))
		if a.input.String() != want {
			t.Fatalf("recall shows %q, want %q", a.input.String(), want)
		}
	}
	// Past the oldest, the walk stops rather than emptying the box.
	drive(t, a, key("up"))
	if a.input.String() != "somewhere else" {
		t.Fatalf("the walk fell off the end: %q", a.input.String())
	}

	// Forward, and the person's own draft is still there at the end of it.
	drive(t, a, keyed("down"))
	drive(t, a, keyed("down"))
	if a.input.String() != "newer here" {
		t.Fatalf("walking forward shows %q", a.input.String())
	}
	drive(t, a, keyed("down"))
	if a.input.String() != "half a sentence" {
		t.Fatalf("the draft was clobbered: %q", a.input.String())
	}
	if a.recalling() {
		t.Fatal("the walk did not end when it reached the draft")
	}
}

func TestEscDuringRecallPutsTheDraftBack(t *testing.T) {
	a, _ := recallApp(t, history.Entry{Text: "an old prompt", Cwd: "/tmp/lab"})
	typeInto(t, a, "mine")

	drive(t, a, key("up"))
	if a.input.String() != "an old prompt" {
		t.Fatalf("recall shows %q", a.input.String())
	}
	drive(t, a, key("esc"))
	if a.input.String() != "mine" || a.recalling() {
		t.Fatalf("esc left %q (recalling=%v)", a.input.String(), a.recalling())
	}
}

func TestEverySubmittedLineIsRemembered(t *testing.T) {
	a, store := recallApp(t)
	typeLine(t, a, "the first thing")
	typeLine(t, a, "/compact") // a command is a line nobody wants to retype either

	got := store.RecentFor("/tmp/lab", 10)
	if len(got) != 2 || got[0].Text != "/compact" || got[1].Text != "the first thing" {
		t.Fatalf("history holds %+v", got)
	}
	// And the walk reaches them in that order, newest first.
	drive(t, a, key("up"))
	if a.input.String() != "/compact" {
		t.Fatalf("↑ recalled %q", a.input.String())
	}
}

func TestUpInsideAMultilineDraftMovesTheCaretRatherThanRecalling(t *testing.T) {
	a, _ := recallApp(t, history.Entry{Text: "an old prompt", Cwd: "/tmp/lab"})
	typeInto(t, a, "one")
	drive(t, a, keyed("alt+enter"))
	typeInto(t, a, "two")

	drive(t, a, key("up")) // caret to the first line, draft untouched
	if a.input.String() != "one\ntwo" || a.recalling() {
		t.Fatalf("↑ inside a draft recalled: %q", a.input.String())
	}
	drive(t, a, key("up")) // now at the top: history
	if a.input.String() != "an old prompt" {
		t.Fatalf("↑ at the top of the draft did not recall: %q", a.input.String())
	}
}

// ── 5. @ file completion ────────────────────────────────────────────────────

func completionApp(t *testing.T, files ...string) *app {
	t.Helper()
	root := t.TempDir()
	for _, name := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a := newApp(t.Context(), Options{Agent: &fakeAgent{model: "m"}, Workspace: root})
	a.width, a.height = 60, 20
	a.pal = newPalette(tokens.ANSI256, false)
	a.entries = nil
	a.touch()
	return a
}

func TestAtOpensTheFileListImmediatelyAndInsertsThePath(t *testing.T) {
	a := completionApp(t,
		"internal/tui3/app.go",
		"internal/tui3/appendix/notes.md",
		"cmd/aforge/main.go",
		".git/config",
		"vendor/foo/app.go",
	)

	typeInto(t, a, "look at @")
	if !a.comp.open {
		t.Fatal("the bare @ opens the list, the way / opens commands")
	}
	typeInto(t, a, "app")
	if !a.comp.open {
		t.Fatal("a query narrows the open list")
	}
	if !a.comp.loaded {
		t.Fatal("the walk did not land")
	}

	// Ranking: the base-name prefix wins, and the skipped directories are not
	// on offer at all.
	first, ok := a.comp.choice()
	if !ok || first != "internal/tui3/app.go" {
		t.Fatalf("the first hit is %q", first)
	}
	for _, path := range a.comp.all {
		if strings.HasPrefix(path, ".git/") || strings.HasPrefix(path, "vendor/") {
			t.Fatalf("the walk entered %s", path)
		}
	}
	if !strings.Contains(plain(strings.Join(a.overlayRows(a.width, a.overlayHeight()), "\n")), "app.go") {
		t.Fatal("the overlay is not drawing the hits")
	}

	drive(t, a, key("enter"))
	if got := a.input.String(); got != "look at @internal/tui3/app.go" {
		t.Fatalf("the completion inserted %q", got)
	}
	if a.comp.open {
		t.Fatal("enter left the list open")
	}

	// The submitted text keeps the @path AS TYPED — the surface does not read
	// the file, the agent's read tool does.
	drive(t, a, key("enter"))
	agent := a.agent.(*fakeAgent)
	if len(agent.sent) != 1 || agent.sent[0] != "look at @internal/tui3/app.go" {
		t.Fatalf("the message sent was %q", agent.sent)
	}
}

func TestEscClosesTheFileListAndLeavesTheDraft(t *testing.T) {
	a := completionApp(t, "internal/tui3/app.go")
	typeInto(t, a, "@app")
	if !a.comp.open {
		t.Fatal("the list did not open")
	}
	drive(t, a, key("esc"))
	if a.comp.open || a.input.String() != "@app" {
		t.Fatalf("esc left open=%v draft=%q", a.comp.open, a.input.String())
	}
}

func TestPathRankingIsPrefixThenSubstringThenSubsequence(t *testing.T) {
	// One query, four tiers, in the order a person means them.
	ranked := []string{
		"agent/roles.go",             // the path itself starts with it
		"internal/session/agent.go",  // the base name starts with it
		"internal/agentless/road.go", // it is in there somewhere
		"docs/A-GENeral-ENTry.md",    // the letters are in there, in order
	}
	scores := make([]int, len(ranked))
	for i, path := range ranked {
		score, ok := pathScore(path, "agent")
		if !ok {
			t.Fatalf("%s did not match at all", path)
		}
		scores[i] = score
	}
	for i := 1; i < len(scores); i++ {
		if scores[i-1] >= scores[i] {
			t.Fatalf("%s (%d) has to rank above %s (%d)",
				ranked[i-1], scores[i-1], ranked[i], scores[i])
		}
	}
	// Within a tier, the earlier match and then the shorter path win.
	near, _ := pathScore("a/agent.go", "agent")
	far, _ := pathScore("a/b/c/d/agentry/x.go", "agent")
	if near >= far {
		t.Fatalf("the shorter, earlier match has to lead (%d vs %d)", near, far)
	}
	if _, ok := pathScore("cmd/aforge/main.go", "zzz"); ok {
		t.Fatal("nothing must match zzz")
	}
}

// ── 6. the command list ─────────────────────────────────────────────────────

func TestSlashOpensTheCommandListFiltersItAndRunsIt(t *testing.T) {
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)

	typeInto(t, a, "/")
	if !a.menu.open || len(a.menu.hits) != len(commands) {
		t.Fatalf("a bare slash has to offer everything (%d hits)", len(a.menu.hits))
	}
	drawn := plain(strings.Join(a.overlayRows(a.width, a.overlayHeight()), "\n"))
	if !strings.Contains(drawn, "/compact") || !strings.Contains(drawn, "summarize the conversation") {
		t.Fatalf("the list draws a name and a line about it:\n%s", drawn)
	}

	typeInto(t, a, "comp")
	if len(a.menu.hits) != 1 {
		t.Fatalf("the filter left %d hits", len(a.menu.hits))
	}
	drive(t, a, key("enter"))
	if agent.packs != 1 {
		t.Fatalf("enter did not run the command (%d)", agent.packs)
	}
	if a.menu.open || a.input.String() != "" {
		t.Fatalf("running left open=%v draft=%q", a.menu.open, a.input.String())
	}

	// A command that TAKES something is written into the box, not run.
	typeInto(t, a, "/mod")
	drive(t, a, keyed("down")) // the second /model row is the one with <slug>
	drive(t, a, key("enter"))
	if a.input.String() != "/model " {
		t.Fatalf("a command with an argument put %q in the box", a.input.String())
	}

	// A space is an argument being typed, and the list gets out of the way.
	typeInto(t, a, "x")
	if a.menu.open {
		t.Fatal("the list stayed up over an argument")
	}
}

// ── 6b. the words a command also answers to ─────────────────────────────────
//
// The table is the one place a command is written down, and the aliases are on
// its rows. These tests hold that law from both ends: what the list SHOWS when
// an alias is typed (the canonical row, with the other words beside it) and what
// the surface RUNS (the canonical command, whichever door the word came in by).

// aliasApp is a surface that can actually answer /new, so an alias for it can be
// asserted on the agent it swapped in rather than on a note about being unable.
func aliasApp(t *testing.T, first Agent, next Agent) *app {
	t.Helper()
	a := newApp(t.Context(), Options{
		Agent:     first,
		Workspace: "/tmp/lab",
		Fresh:     func() (Agent, string, error) { return next, "/tmp/next.jsonl", nil },
	})
	a.width, a.height = 100, 24
	a.pal = newPalette(tokens.ANSI256, false)
	a.entries = nil
	a.welcome = welcome{spent: true}
	a.touch()
	return a
}

func TestAnAliasSurfacesTheCanonicalRowAndRunsIt(t *testing.T) {
	first, second := &fakeAgent{model: "m"}, &fakeAgent{model: "m2"}
	a := aliasApp(t, first, second)

	// "clea" is nobody's command name; it is /clear and /clean, and both of them
	// are /new.
	typeInto(t, a, "/clea")
	if len(a.menu.hits) != 1 {
		t.Fatalf("an alias has to narrow to the one row it belongs to (%d hits)", len(a.menu.hits))
	}
	if chosen, _ := a.menu.choice(); chosen.name != "new" {
		t.Fatalf("the row under the cursor is %q, want the canonical /new", chosen.name)
	}

	// The row is the canonical one, and it says what else reaches it.
	drawn := plain(strings.Join(a.overlayRows(a.width, a.overlayHeight()), "\n"))
	if !strings.Contains(drawn, "/new") || strings.Contains(drawn, "/clea ") {
		t.Fatalf("the list has to draw the canonical name:\n%s", drawn)
	}
	if !strings.Contains(drawn, "also /clear /clean /reset") {
		t.Fatalf("the row has to name the other words for it:\n%s", drawn)
	}

	// And enter on it runs /new.
	drive(t, a, key("enter"))
	if a.agent != Agent(second) {
		t.Fatal("enter on an alias row did not run the canonical command")
	}
	if a.input.String() != "" || a.menu.open {
		t.Fatalf("running left draft=%q open=%v", a.input.String(), a.menu.open)
	}
}

func TestAnAliasTypedInFullRunsWithoutTheList(t *testing.T) {
	first, second := &fakeAgent{model: "m"}, &fakeAgent{model: "m2"}
	a := aliasApp(t, first, second)

	// Typed out and entered, the list never decides anything: enter submits the
	// line, and the dispatch resolves the word.
	typeLine(t, a, "/reset")
	if a.agent != Agent(second) {
		t.Fatal("/reset typed in full did not reach /new")
	}
	if got := plain(frame(a)); strings.Contains(got, "unknown command") {
		t.Fatalf("an alias was answered as unknown:\n%s", got)
	}

	// /? is the alias with no letters in it, and it reaches /help.
	typeLine(t, a, "/?")
	if got := plain(frame(a)); !strings.Contains(got, "settings") {
		t.Fatalf("/? did not reach the help table:\n%s", got)
	}

	// /exit and /q close the surface exactly as /quit does.
	for _, word := range []string{"/exit", "/q"} {
		closer := &fakeAgent{model: "m"}
		b := aliasApp(t, closer, &fakeAgent{model: "m2"})
		typeLine(t, b, word)
		if closer.closes != 1 {
			t.Fatalf("%s closed the agent %d times", word, closer.closes)
		}
	}
}

func TestTheCanonicalNamesStillWorkEverywhere(t *testing.T) {
	first, second := &fakeAgent{model: "m"}, &fakeAgent{model: "m2"}
	a := aliasApp(t, first, second)

	// A name found by its OWN name outranks a name found by a word it merely
	// also answers to: "res" is /resume first and /new (via /reset) second.
	typeInto(t, a, "/res")
	if len(a.menu.hits) != 2 {
		t.Fatalf("res has to find /resume and /new-by-reset (%d hits)", len(a.menu.hits))
	}
	if chosen, _ := a.menu.choice(); chosen.name != "resume" {
		t.Fatalf("the alias match outranked the name match: %q leads", chosen.name)
	}
	drive(t, a, key("esc"))
	drive(t, a, key("ctrl+u"))

	typeLine(t, a, "/new")
	if a.agent != Agent(second) {
		t.Fatal("the canonical /new stopped working")
	}
	if canonicalCommand("new") != "new" || canonicalCommand("nonsense") != "nonsense" {
		t.Fatal("a canonical name and an unknown word both resolve to themselves")
	}
}

func TestRewindIsOnTheTableAndDispatches(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})

	typeInto(t, a, "/rew")
	if chosen, _ := a.menu.choice(); chosen.name != "rewind" {
		t.Fatalf("/rew has to find the rewind row, found %q", chosen.name)
	}
	drawn := plain(strings.Join(a.overlayRows(a.width, a.overlayHeight()), "\n"))
	if !strings.Contains(drawn, "/rewind") {
		t.Fatalf("the rewind row has to be drawable:\n%s", drawn)
	}
	drive(t, a, key("esc"))
	drive(t, a, key("ctrl+u"))

	// The word, and both of the words people arrive with, all reach the command
	// — which is the stub until the rewind branch lands, so what is asserted is
	// that the dispatch CLAIMED them rather than answering "unknown".
	for _, word := range []string{"rewind", "undo", "back"} {
		if got := canonicalCommand(word); got != "rewind" {
			t.Fatalf("/%s resolves to %q", word, got)
		}
		b := newTestApp(&fakeAgent{model: "m"})
		typeLine(t, b, "/"+word)
		if got := plain(frame(b)); strings.Contains(got, "unknown command") {
			t.Fatalf("/%s was not dispatched:\n%s", word, got)
		}
	}
}

func TestHelpPrintsTheAliasesFromTheSameTable(t *testing.T) {
	text := helpText("")
	for _, want := range []string{
		"/new",
		"also /clear /clean /reset",
		"/quit",
		"also /exit /q",
		"also /?",
		"/rewind",
		"go back to an earlier point · esc esc takes back the last",
		"also /undo /back",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("help is missing %q:\n%s", want, text)
		}
	}
	// One source, two renderings: every row's tail is the same string the list
	// draws from ([command.note]).
	for _, c := range commands {
		if !strings.Contains(text, c.note()) {
			t.Fatalf("help lost the tail of /%s:\n%s", c.name, text)
		}
	}
}

func TestTheTableRefusesAWordThatMeansTwoThings(t *testing.T) {
	if err := checkCommands(commands); err != nil {
		t.Fatalf("the shipped table does not pass its own check: %v", err)
	}
	// The two collisions the law names, plus the empty word, each on a table of
	// their own so the shipped one is never the thing under test.
	for name, table := range map[string][]command{
		"an alias shadowing a command": {
			{name: "new", alias: []string{"clear"}},
			{name: "clear", desc: "a command in its own right"},
		},
		"one word for two commands": {
			{name: "new", alias: []string{"reset"}},
			{name: "settings", alias: []string{"reset"}},
		},
		"an empty alias": {
			{name: "new", alias: []string{""}},
		},
	} {
		if err := checkCommands(table); err == nil {
			t.Fatalf("%s has to fail the table check", name)
		}
	}
	// A canonical name repeated is NOT a collision: /model and /model <slug> are
	// two forms of one command.
	if err := checkCommands([]command{
		{name: "model"},
		{name: "model", args: "<slug>"},
	}); err != nil {
		t.Fatalf("two forms of one command are legal: %v", err)
	}
}

func TestAnUnknownSlashStillReachesTheOldAnswer(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	typeLine(t, a, "/nonsense")
	if !strings.Contains(plain(frame(a)), "unknown command: /nonsense") {
		t.Fatalf("an unknown slash has to answer:\n%s", plain(frame(a)))
	}
	if a.menu.open {
		t.Fatal("the list survived the submit")
	}
}

// ── 7. the draft file ───────────────────────────────────────────────────────

func TestTheDraftIsWrittenRestoredAndClearedOnSubmit(t *testing.T) {
	dir := t.TempDir()
	path := DraftFile(dir, "/tmp/lab")
	// ONE DIRECTORY IS ONE PREFIX, whichever way it is spelled — the trailing
	// slash is cleaned away before the hash is taken. The names themselves
	// differ, because each call takes the next ordinal for that workspace: one
	// terminal can hold two conversations in one project, and two boxes cannot
	// share one file (draft.go).
	if draftPrefix("/tmp/lab") != draftPrefix("/tmp/lab/") {
		t.Fatal("one directory has to name one family of draft files")
	}
	if second := DraftFile(dir, "/tmp/lab/"); second == path {
		t.Fatalf("two conversations were given one draft file: %s", path)
	}

	agent := &fakeAgent{model: "m"}
	a := newApp(t.Context(), Options{Agent: agent, Workspace: "/tmp/lab", DraftFile: path})
	a.width, a.height = 60, 20
	typeInto(t, a, "half a thought")

	// The debounce is armed by the edit and writes when it fires.
	if !a.draftPending {
		t.Fatal("typing did not arm the debounce")
	}
	if cmd := a.saveDraft(""); cmd != nil {
		cmd()
	}
	if got := readDraft(path); got != "half a thought" {
		t.Fatalf("the draft file holds %q", got)
	}

	// A NEW surface on the same directory opens with it in the box — even
	// though the session it belongs to has moved on. The draft is the person's.
	next := newApp(t.Context(), Options{
		Agent:     &fakeAgent{model: "m", past: []session.DisplayEntry{{Role: "user", Text: "later"}}},
		Workspace: "/tmp/lab", DraftFile: path,
	})
	if next.input.String() != "half a thought" {
		t.Fatalf("the draft was not restored: %q", next.input.String())
	}

	drive(t, next, key("enter"))
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("submit left the draft file behind (%v)", err)
	}
	if len(agent.sent) != 0 && agent.sent[0] == "" {
		t.Fatal("an empty message was sent")
	}
}

func TestNoDraftFileIsWrittenWhenTheSurfaceWasGivenNone(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	typeInto(t, a, "nothing to see")
	if a.draftPending {
		t.Fatal("a surface with no draft file armed the debounce")
	}
	if cmd := a.saveDraft(""); cmd != nil {
		t.Fatal("a surface with no draft file returned a write")
	}
}

// ── 8. the context meter ────────────────────────────────────────────────────

// THE METER IS THE AGENT'S FIGURE, NOT THE SURFACE'S GUESS. It used to be the
// display transcript's bytes over four, which could only see words somebody
// said: the system prompt, the tool schemas and every tool result were invisible
// to it, and a working session read a few percent all afternoon.
func TestTheContextMeterIsTheAgentsTokensOverTheWindow(t *testing.T) {
	agent := &fakeAgent{model: "m", weight: 1000, past: []session.DisplayEntry{
		// A short transcript with a heavy context: the words a person can see
		// are a fraction of what the model is carrying, which is the whole
		// reason the surface stopped counting them.
		{Role: "user", Text: "hi"},
	}}
	a := newApp(t.Context(), Options{Agent: agent, Workspace: "/tmp/lab", ContextWindow: 10000})
	a.width, a.height = 60, 20
	a.pal = newPalette(tokens.ANSI256, false)

	// 1000 tokens of a 10k window, whatever the transcript weighs.
	pct, ok := a.ctxPercent()
	if !ok || pct != 10 {
		t.Fatalf("the meter says %d%% (ok=%v), want 10%%", pct, ok)
	}
	if got := plain(frame(a)); !strings.Contains(got, "1k/10k · 10%") {
		t.Fatalf("the status line is missing the meter:\n%s", got)
	}

	// A window nobody knows draws no meter at all.
	bare := newApp(t.Context(), Options{Agent: &fakeAgent{model: "nobody/knows", weight: 1000}, Workspace: "/tmp/lab"})
	bare.width, bare.height = 60, 20
	if _, ok := bare.ctxPercent(); ok {
		t.Fatal("a percentage of an unknown window is a number that means nothing")
	}
	if strings.Contains(plain(frame(bare)), "1k/") {
		t.Fatalf("the meter was drawn without a window:\n%s", plain(frame(bare)))
	}
}

func TestSwitchingModelsMovesTheMeterWithTheWindow(t *testing.T) {
	agent := &fakeAgent{model: "small/model", weight: 10000}
	a := newApp(t.Context(), Options{Agent: agent, Workspace: "/tmp/lab", ContextWindow: 100000})
	a.width, a.height = 60, 20

	before, _ := a.ctxPercent()
	a.switchModel("big/model", 1000000)
	after, _ := a.ctxPercent()
	if before != 10 || after != 1 {
		t.Fatalf("the meter read %d%% then %d%%, want 10%% then 1%%", before, after)
	}
	if agent.window != 1000000 {
		t.Fatalf("the session was told %d", agent.window)
	}
}
