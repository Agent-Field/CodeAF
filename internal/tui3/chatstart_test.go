package tui3

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// ── THE NEW-CHAT START PAGE ─────────────────────────────────────────────────
//
// `+` is a control on a strip a person reaches for by position, so the thing it
// must never do is act on the conversation they are standing in. These hold the
// three promises the design makes about it — the page costs nothing to open, the
// conversation behind it keeps its draft, and the conversation is made by the
// first message and not a keystroke earlier — and the two ways every one of them
// could quietly stop being true: a create that fires on the way in, and a
// composer that leaks in either direction.

// startLab is a window in one conversation, with a door onto new ones that
// counts how many times it was asked.
type startLab struct {
	a *app
	// agent is the conversation on screen when the page opens.
	agent *fakeAgent
	// made counts creates. It is the number every test in this file is really
	// about: `+` may not raise it, `esc` may not raise it, and the first message
	// may raise it exactly once.
	made int
	// next is the agent the door hands back.
	next *fakeAgent
	// refuse is what the door answers instead, when a test wants a failure.
	refuse error
}

func newStartLab(t *testing.T) *startLab {
	t.Helper()
	lab := &startLab{agent: &fakeAgent{model: "m"}, next: &fakeAgent{model: "m"}}
	a := newTestApp(lab.agent)
	a.file, a.workspace, a.title = "/tmp/lab/one.jsonl", "/tmp/lab", "Shipping the parser"
	emptyMachine(a)
	a.start = func(string) (Conversation, error) {
		lab.made++
		if lab.refuse != nil {
			return Conversation{}, lab.refuse
		}
		return Conversation{
			Agent: lab.next, SessionFile: "/tmp/lab/two.jsonl", Workspace: "/tmp/lab",
		}, nil
	}
	a.recentSessions = func() []Session {
		return []Session{
			{Title: "openrouter price scrape", File: "/tmp/lab/scrape.jsonl", At: a.now().Add(-2 * time.Hour)},
			{Title: "rail scope", File: "/tmp/lab/rail.jsonl", At: a.now().Add(-3 * time.Hour)},
		}
	}
	// A CONVERSATION THAT HAS RUN SOMETHING. `+` over an empty session is the
	// easy case; the one worth holding is `+` over work, where a create on the
	// way in would put a running turn into the keeper — or over a shared handle,
	// end it.
	a.turn = 1
	lab.a = a
	return lab
}

// openStart presses `+` and settles whatever it asked for — the recent list is
// read off the loop, so a test that did not drive its message would be asserting
// against a page half built.
func openStart(t *testing.T, a *app) {
	t.Helper()
	drive(t, a, runCmd(a.openChatStart())...)
}

// ── OPENING COSTS NOTHING ───────────────────────────────────────────────────

// THE PAGE IS A PAGE. Nothing is asked of the door, nothing is closed, nothing
// is interrupted, and the conversation on screen is still the conversation on
// screen — which is the difference between this control and /new, and the whole
// reason it is not wired to it.
func TestPlusOpensAPageAndMakesNothing(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	before := a.agent
	openStart(t, a)

	if !a.startingChat() {
		t.Fatal("+ did not open the start page")
	}
	if lab.made != 0 {
		t.Fatalf("+ asked the door for %d conversations, and it may ask for none", lab.made)
	}
	if lab.agent.closes != 0 || lab.agent.stops != 0 {
		t.Fatalf("+ closed %d and interrupted %d — it may do neither", lab.agent.closes, lab.agent.stops)
	}
	if a.agent != before {
		t.Fatal("+ swapped the agent under the person")
	}
	if a.file != "/tmp/lab/one.jsonl" {
		t.Fatalf("+ moved the surface to %q", a.file)
	}
	if len(a.behind) != 0 {
		t.Fatalf("+ put %d conversations into the keeper", len(a.behind))
	}
	// AND THE STRIP IS STILL THERE. The page is drawn inside the frame's own
	// chrome, so the tabs a person came in with are still above it.
	_ = a.tabsRow(a.width)
	if !tabsHold(a.chatTabs, a.frontTabKey()) {
		t.Fatal("start page lost the original chat tab")
	}
}

// app is the lab's window.
func (l *startLab) app() *app { return l.a }

// THREE PRESSES AND THREE ESCAPES ARE STILL NOTHING. A person leaning on a
// control they have just discovered must not find three sessions behind them.
func TestThreeOpensAndThreeCancelsMakeNothing(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	for i := 0; i < 3; i++ {
		openStart(t, a)
		// AND PRESSING IT AGAIN WHILE IT IS UP REUSES THE PAGE.
		openStart(t, a)
		drive(t, a, key("esc"))
		if a.startingChat() {
			t.Fatalf("esc did not close the start page on round %d", i+1)
		}
	}
	if lab.made != 0 || lab.agent.closes != 0 || lab.agent.stops != 0 {
		t.Fatalf("six presses made %d conversations, closed %d and interrupted %d — all three must be zero",
			lab.made, lab.agent.closes, lab.agent.stops)
	}
}

// AND A WINDOW WITH NO DOOR ONTO A NEW CONVERSATION REFUSES RATHER THAN DRAWING
// A PAGE THAT COULD COLLECT A SENTENCE AND THEN HAVE NOWHERE TO SEND IT. It is
// /new's own refusal in /new's own words.
func TestAWindowWithNoDoorRefusesTheStartPage(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	a.start, a.fresh = nil, nil
	openStart(t, a)
	if a.startingChat() {
		t.Fatal("the start page opened on a window that cannot start a conversation")
	}
	if !strings.Contains(strings.Join(noteTexts(a), "\n"), newUnavailableWord) {
		t.Fatalf("the refusal did not say %q:\n%v", newUnavailableWord, noteTexts(a))
	}
}

// ── THE TWO BOXES ───────────────────────────────────────────────────────────

// THE PAGE'S BOX IS BLANK AND THE OLD ONE KEEPS EVERYTHING. The sentence, the
// caret inside it, the pictures on its tray and the documents behind its compact
// tokens all belong to the conversation they were typed at, and none of them is
// on the page a person is about to write a different message in.
func TestTheStartPageGetsItsOwnBoxAndTheOldOneKeepsEverything(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	a.input.setText("half a sentence about the parser")
	a.input.cursor = 4
	a.chips = []chip{{path: "/tmp/lab/shot.png"}}
	a.pastes = []pasteChip{{n: 1, text: "forty two lines"}}
	openStart(t, a)

	if got := a.input.String(); got != "" {
		t.Fatalf("the start page opened holding %q, and it must open blank", got)
	}
	if len(a.chips) != 0 {
		t.Fatalf("the start page opened with %d attachments on its tray", len(a.chips))
	}
	if len(a.pastes) != 0 {
		t.Fatalf("the start page opened with %d pasted documents", len(a.pastes))
	}
	main := a.mainComposer()
	if main.box.String() != "half a sentence about the parser" {
		t.Fatalf("the conversation lost its sentence: %q", main.box.String())
	}
	if main.box.cursor != 4 {
		t.Fatalf("the conversation's caret moved to %d", main.box.cursor)
	}
	if len(main.chips) != 1 || len(main.pastes) != 1 {
		t.Fatalf("the conversation lost its tray: %d chips, %d pastes", len(main.chips), len(main.pastes))
	}
}

// AND ESCAPE GIVES ALL OF IT BACK, exactly — the caret included, because a
// person who comes back to their sentence with the caret at the end of it has
// been given something other than what they left.
func TestEscapeGivesBackTheSentenceTheCaretAndTheTray(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	a.input.setText("half a sentence")
	a.input.cursor = 4
	a.chips = []chip{{path: "/tmp/lab/shot.png"}}
	a.pastes = []pasteChip{{n: 1, text: "forty two lines"}}
	a.offset, a.stick = 7, false

	openStart(t, a)
	drive(t, a, key("n"), key("e"), key("w"))
	drive(t, a, key("esc"))

	if a.startingChat() {
		t.Fatal("esc did not close the start page")
	}
	if got := a.input.String(); got != "half a sentence" {
		t.Fatalf("esc gave back %q", got)
	}
	if a.input.cursor != 4 {
		t.Fatalf("esc gave back the caret at %d, want 4", a.input.cursor)
	}
	if len(a.chips) != 1 || len(a.pastes) != 1 {
		t.Fatalf("esc gave back %d chips and %d pastes, want one of each", len(a.chips), len(a.pastes))
	}
	if a.offset != 7 || a.stick {
		t.Fatalf("esc left them reading at %d (stick %v), want 7 and false", a.offset, a.stick)
	}
	// AND THE PAGE'S OWN WORDS ARE PARKED RATHER THAN THROWN AWAY: pressing `+`
	// again is coming back to something half written, not starting over.
	openStart(t, a)
	if got := a.input.String(); got != "new" {
		t.Fatalf("the start page came back holding %q, want the parked \"new\"", got)
	}
}

// ── THE FIRST MESSAGE ───────────────────────────────────────────────────────

// ONE MESSAGE, ONE CONVERSATION, AND IT GOES TO THE NEW ONE. The old agent is
// never sent to, and the sentence it was holding is still with it in the keeper.
func TestTheFirstMessageMakesExactlyOneConversationAndSendsItThere(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	a.input.setText("the old unsent line")
	openStart(t, a)
	drive(t, a, key("g"), key("o"))
	drive(t, a, key("enter"))

	if lab.made != 1 {
		t.Fatalf("the first message asked the door %d times, want exactly one", lab.made)
	}
	if len(lab.agent.sent) != 0 {
		t.Fatalf("the sentence went into the OLD conversation: %v", lab.agent.sent)
	}
	if len(lab.next.sent) != 1 || lab.next.sent[0] != "go" {
		t.Fatalf("the new conversation was sent %v, want [go]", lab.next.sent)
	}
	if a.startingChat() {
		t.Fatal("the start page is still up after the message went")
	}
	// AND THE CONVERSATION THAT WAS LEFT STILL HAS ITS OWN SENTENCE, one press of
	// its tab away.
	held := 0
	for _, kept := range a.behind {
		held++
		if kept.side == nil || kept.side.draft != "the old unsent line" {
			t.Fatalf("the kept conversation is holding %+v, want its own unsent line", kept.side)
		}
	}
	if held != 1 {
		t.Fatalf("the keeper is holding %d conversations, want one", held)
	}
	// AND THE NEW BOX IS EMPTY. Nothing the old conversation was holding rode out
	// on the create.
	if got := a.input.String(); got != "" {
		t.Fatalf("the new conversation opened holding %q", got)
	}
	if len(a.chips) != 0 {
		t.Fatalf("the new conversation opened with %d attachments", len(a.chips))
	}
}

// A PICTURE WITH NO WORDS IS A MESSAGE, which is this surface's law about enter
// said at the one door that had to learn it again.
func TestAPictureWithNoWordsStartsTheConversation(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	openStart(t, a)
	a.chips = []chip{{path: "/tmp/lab/shot.png"}}
	drive(t, a, key("enter"))

	if lab.made != 1 {
		t.Fatalf("a picture alone asked the door %d times, want one", lab.made)
	}
	if a.startingChat() {
		t.Fatal("the start page stayed up after a picture was sent")
	}
}

// AND A COMPACT PASTE'S DOCUMENT GOES WITH THE FIRST MESSAGE. The token in the
// box means nothing without what is behind it, and the start page's pastes are
// its own — the ones from the chat behind it stay there.
func TestALargePasteOnTheStartPageGoesWithTheFirstMessage(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	a.pastes = []pasteChip{{n: 1, text: "the chat behind's document"}}
	openStart(t, a)
	if len(a.pastes) != 0 {
		t.Fatalf("the start page opened holding %d of the old chat's documents", len(a.pastes))
	}
	a.pastes = []pasteChip{{n: 1, text: "forty two lines of log"}}
	a.input.setText("look at " + pasteToken(1, pasteLineCount("forty two lines of log")) + " please analyze")
	drive(t, a, key("enter"))

	if len(lab.next.sent) != 1 {
		t.Fatalf("the new conversation was sent %v; box=%q notes=%v msg=%q", lab.next.sent, a.input.String(), noteTexts(a), a.welcome.msg)
	}
	if !strings.Contains(lab.next.sent[0], "forty two lines of log") {
		t.Fatalf("the document behind the token did not go with the message: %q", lab.next.sent[0])
	}
	if strings.Contains(lab.next.sent[0], "the chat behind's document") {
		t.Fatalf("the old chat's document rode out on the first message: %q", lab.next.sent[0])
	}
	// AND THE OLD CHAT STILL HAS ITS OWN, in the keeper with the sentence it
	// belonged to.
	for _, kept := range a.behind {
		if len(kept.side.pastes) != 1 || kept.side.pastes[0].text != "the chat behind's document" {
			t.Fatalf("the chat that was left lost its document: %+v", kept.side.pastes)
		}
	}
}

// AND AN EMPTY BOX WITH AN EMPTY TRAY MAKES NOTHING. A conversation created for
// a stray enter is a session file nobody typed in.
func TestEnterAtABlankStartPageMakesNothing(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	openStart(t, a)
	drive(t, a, key("enter"))
	if lab.made != 0 {
		t.Fatalf("enter at a blank page asked the door %d times", lab.made)
	}
	if !a.startingChat() {
		t.Fatal("enter at a blank page closed it")
	}
}

// A DOOR THAT REFUSES COSTS NOTHING. The words stay on the page, the refusal is
// said ON the page — the transcript is not on the frame to say it into — and the
// conversation behind is exactly where it was.
func TestADoorThatRefusesKeepsTheWordsOnThePage(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	a.input.setText("the old unsent line")
	lab.refuse = errors.New("no room on the disk")
	openStart(t, a)
	drive(t, a, key("g"), key("o"))
	drive(t, a, key("enter"))

	if !a.startingChat() {
		t.Fatal("a refused create took the page down")
	}
	if got := a.input.String(); got != "go" {
		t.Fatalf("a refused create left %q in the box, want the words still there", got)
	}
	if a.welcome.msg == "" || !strings.Contains(a.welcome.msg, "no room on the disk") {
		t.Fatalf("the refusal was not said on the page: %q", a.welcome.msg)
	}
	if len(lab.agent.sent) != 0 {
		t.Fatalf("a refused create sent %v into the old conversation", lab.agent.sent)
	}
	if len(a.behind) != 0 || lab.agent.closes != 0 {
		t.Fatalf("a refused create put the old conversation down: %d kept, %d closed", len(a.behind), lab.agent.closes)
	}
	// AND THE OLD SENTENCE IS STILL RETRIEVABLE, which is what esc is for.
	drive(t, a, key("esc"))
	if got := a.input.String(); got != "the old unsent line" {
		t.Fatalf("after a refusal esc gave back %q", got)
	}
}

// AND A FRESH CONVERSATION'S UNSENT LINE STAYS WITH THE FRESH CONVERSATION. It
// used to be handed forward into the new conversation's box — the create closed
// its owner, so there was nowhere else for it — and that was a draft standing in
// front of a model it was never addressed to. The conversation is KEPT instead,
// on the strength of the sentence in it, and the words come back by pressing its
// tab like everybody else's ([app.startKeepsLeaving]; the two cases are held
// whole in chatstartowner_test.go).
func TestAFreshConversationsUnsentLineStaysInItsOwnConversation(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	a.turn = 0 // fresh and empty but for the words in the box
	a.input.setText("something I never sent")
	openStart(t, a)
	drive(t, a, key("g"), key("o"))
	drive(t, a, key("enter"))

	if lab.made != 1 {
		t.Fatalf("the door was asked %d times", lab.made)
	}
	if len(lab.next.sent) != 1 || lab.next.sent[0] != "go" {
		t.Fatalf("the new conversation was sent %v", lab.next.sent)
	}
	if got := a.input.String(); got != "" {
		t.Fatalf("the old conversation's line rode out on the first message: %q", got)
	}
	if strings.Contains(strings.Join(noteTexts(a), "\n"), "kept your unsent sentence") {
		t.Fatalf("the line was handed forward instead of kept with its own conversation:\n%v", noteTexts(a))
	}
	held := a.behind[a.convKey("/tmp/lab/one.jsonl")]
	if held == nil || held.side == nil || held.side.draft != "something I never sent" {
		t.Fatalf("the sentence is not where its own conversation keeps it (%d held)", len(a.behind))
	}
}

// ── THE RECENT LIST ─────────────────────────────────────────────────────────

// CHOOSING A RECENT CONVERSATION OPENS IT AND SENDS NOTHING. What was typed on
// the page is navigation's bystander: it stays parked, and it is not delivered
// into the conversation that was chosen.
func TestChoosingARecentOpensItAndSendsNothing(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	opened := ""
	a.open = func(_, transcript string) (Conversation, error) {
		opened = transcript
		return Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: transcript, Workspace: "/tmp/lab"}, nil
	}
	openStart(t, a)
	drive(t, a, key("h"), key("i"))
	// The walk only moves over an EMPTY box, so the sentence is taken out first —
	// which is exactly what a person choosing a row instead of sending does.
	drive(t, a, key("backspace"), key("backspace"))
	drive(t, a, key("down"), key("enter"))

	if opened != "/tmp/lab/scrape.jsonl" {
		t.Fatalf("the row opened %q, want the first recent conversation", opened)
	}
	if lab.made != 0 {
		t.Fatalf("choosing a recent conversation asked the new-conversation door %d times", lab.made)
	}
	for _, agent := range []*fakeAgent{lab.agent, lab.next} {
		if len(agent.sent) != 0 {
			t.Fatalf("choosing a recent conversation sent %v", agent.sent)
		}
	}
	if a.startingChat() {
		t.Fatal("the start page is still up over the conversation it opened")
	}
}

// A DIRECTORY WITH NOTHING BEHIND IT DRAWS NO HEADING AND NO BLANK SLOTS, which
// is the emptiness law's plainest case and the greeting's own behaviour.
func TestNoRecentConversationsDrawsNoHeading(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	a.recentSessions = func() []Session { return nil }
	openStart(t, a)
	if got := plain(strings.Join(a.welcomeRows(a.width), "\n")); strings.Contains(got, "recent sessions") {
		t.Fatalf("the page drew a heading over nothing:\n%s", got)
	}
}

// AND A LIST THAT ARRIVES LATE DOES NOT TOUCH THE BOX. It is read off the loop,
// so it lands while somebody is typing their first sentence into the page.
func TestARecentListArrivingLateDoesNotTouchTheBox(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	openStart(t, a)
	drive(t, a, key("h"), key("i"))
	a.welcome.sel = 1
	drive(t, a, startRecentsMsg{gen: a.startGen, list: []Session{{Title: "one", File: "/tmp/lab/one.jsonl"}}})

	if got := a.input.String(); got != "hi" {
		t.Fatalf("the list arriving rewrote the box to %q", got)
	}
	if len(a.welcome.recent) != 1 {
		t.Fatalf("the list did not land: %d rows", len(a.welcome.recent))
	}
	// AND A LIST FROM AN EARLIER OPENING IS DROPPED rather than drawn over the
	// one this page asked for.
	drive(t, a, startRecentsMsg{gen: a.startGen - 1, list: nil})
	if len(a.welcome.recent) != 1 {
		t.Fatal("a stale list was folded in")
	}
}

// ── THE FRAME AROUND IT ─────────────────────────────────────────────────────

// THE TRANSCRIPT UNDERNEATH IS NOT DRAWN AND NOT PRESSABLE. The unit is centred
// in the body's slack; over a full transcript there is none, and the two would be
// drawn through each other.
func TestTheStartPageHidesTheConversationUnderIt(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	a.note("a line from the conversation behind")
	openStart(t, a)
	frame := plain(mustFrame(a))
	if strings.Contains(frame, "a line from the conversation behind") {
		t.Fatalf("the conversation is drawn under the start page:\n%s", frame)
	}
	if _, ok := a.rowAt(a.bodyTop()); ok {
		t.Fatal("a row of the hidden conversation still answers the pointer")
	}
	drive(t, a, key("esc"))
	if got := plain(mustFrame(a)); !strings.Contains(got, "a line from the conversation behind") {
		t.Fatalf("esc did not bring the conversation back:\n%s", got)
	}
}

// AND A WINDOW TOO SMALL FOR THE UNIT STILL STARTS A CHAT. The box falls back to
// the foot of the frame where it lives on every other screen, and one row says
// which page it belongs to — rather than a blank screen with a working caret.
func TestAWindowTooSmallForTheUnitStillStartsAChat(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	a.width, a.height = 30, 10 // under welcomeMinCols and welcomeMinRows both
	openStart(t, a)
	if !a.startingChat() {
		t.Fatal("+ refused a small window outright")
	}
	if got := plain(mustFrame(a)); !strings.Contains(got, "new chat") {
		t.Fatalf("a small window drew no sign of the page:\n%s", got)
	}
	drive(t, a, key("g"), key("o"))
	drive(t, a, key("enter"))
	if lab.made != 1 || len(lab.next.sent) != 1 {
		t.Fatalf("a small window could not send: %d creates, %v sent", lab.made, lab.next.sent)
	}
}

// AND THE TASK PAGE A PERSON PRESSED `+` FROM IS THE PAGE ESCAPE PUTS THEM BACK
// ON, with the line they were steering that node with still in its box. The
// start page needs the body region, so the room is put down through its own door
// — which is what stashes that node's composer under its own reader — and opened
// again through the door a rail click uses.
func TestTheTaskPageAndItsOwnLineComeBackWhenTheStartPageIsCancelled(t *testing.T) {
	a, fake, _ := roomApp(t)
	fake.journal = roomJournal(t, `{"type":"message","role":"user","content":"go"}`)
	made := 0
	a.start = func(string) (Conversation, error) {
		made++
		return Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: "/tmp/lab/two.jsonl"}, nil
	}
	a.openRoom(7, "Fix the nil-map crash")
	if !a.roomOpen() {
		t.Fatal("the fixture did not open the node's page")
	}
	drive(t, a, key("u"), key("s"), key("e"), key(" "), key("v"), key("2"))

	openStart(t, a)
	if a.roomOpen() {
		t.Fatal("the task page is still open under the start page")
	}
	if got := a.input.String(); got != "" {
		t.Fatalf("the start page opened holding the node's line %q", got)
	}
	drive(t, a, key("esc"))

	if !a.roomOpen() || a.room.id != 7 {
		t.Fatalf("esc did not put them back on the task page (open %v)", a.roomOpen())
	}
	if got := a.input.String(); got != "use v2" {
		t.Fatalf("the node's own line came back as %q, want \"use v2\"", got)
	}
	if made != 0 {
		t.Fatalf("opening and cancelling the page over a task made %d conversations", made)
	}
}
