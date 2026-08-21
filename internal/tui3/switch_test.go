package tui3

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// SWITCHING IS NOT CLOSING, and these are the two halves of that sentence: what
// a detached conversation keeps, and what a person gets back when they arrive.

// switchAgent is a fakeAgent that can be LEFT and COME BACK TO: it answers the
// in-flight turn door ([session.Agent.Attach]) and the approval-question door
// ([session.Agent.PendingConsent]), which are the two things attach reads that
// no other test on this surface needs.
type switchAgent struct {
	*fakeAgent
	// backlog is what a turn already in flight has said. Attach hands it over in
	// order and then stays open, which is what the real hub does under one hold
	// of its lock.
	backlog []session.Event
	running bool
	// pending is the approval questions the engine is still holding.
	pending []uint64
	stops   int
	closed  bool
}

func (s *switchAgent) Attach() (<-chan session.Event, bool, func()) {
	if !s.running {
		return nil, false, func() {}
	}
	out := make(chan session.Event, len(s.backlog)+1)
	for _, ev := range s.backlog {
		out <- ev
	}
	return out, true, func() { s.stops++ }
}

func (s *switchAgent) PendingConsent() []uint64 { return s.pending }

func (s *switchAgent) Close() error { s.closed = true; return s.fakeAgent.Close() }

// TestASwitchKeepsTheTranscriptTheDraftTheScrollAndTheQuestion is the round
// trip, asserted field by field.
//
// It is one test rather than four because the failure it guards against is a
// detach that clears a field the sidecar was supposed to be holding — and that
// mistake is made once, in one function, for all of them at the same time.
func TestASwitchKeepsTheTranscriptTheDraftTheScrollAndTheQuestion(t *testing.T) {
	agent := &switchAgent{fakeAgent: &fakeAgent{model: "m", past: []session.DisplayEntry{
		{Role: "user", Text: "what does the gate do"},
		{Role: "assistant", Text: "it stops a call and asks"},
	}}}
	a := newTestApp(agent)
	a.askWait = 10 * time.Second

	// A sentence half typed, a picture on it, and a reader part way up.
	typeChars(t, a, "and then what")
	a.chips = []chip{{path: "/tmp/lab/shot.png"}}
	a.offset, a.stick = 4, false

	// A question the engine is holding, with most of its clock still to run.
	a.askConsent(consentEvent(7, "read", "read consent.go", `tool "read"`))
	agent.pending = []uint64{7}
	agent.running, agent.backlog = true, []session.Event{
		consentEvent(7, "read", "read consent.go", `tool "read"`),
	}
	a.askAt = a.now().Add(-6 * time.Second)
	before, _ := a.askLeft()

	conv, side := a.front(), a.detachConversation()

	// NOTHING OF THE CONVERSATION IS ON SCREEN, and its agent is untouched.
	if len(a.entries) != 0 || a.asking() || !a.input.empty() {
		t.Fatalf("the detached conversation is still drawn: %d entries, asking=%v, box=%q",
			len(a.entries), a.asking(), a.input.String())
	}
	if agent.closed || agent.stops > 0 {
		t.Fatal("detaching closed the agent — a switch is not a close")
	}
	if side.draft != "and then what" {
		t.Fatalf("the box went into the sidecar as %q", side.draft)
	}
	if side.offset != 4 || side.stick {
		t.Fatalf("the reading position went in as %d/%v", side.offset, side.stick)
	}
	if side.askLeft <= 0 || side.askLeft > before {
		t.Fatalf("the countdown went in as %s, and %s was left", side.askLeft, before)
	}

	drain(t, a, a.attachConversation(conv, side))

	if len(a.entries) < 2 {
		t.Fatalf("the transcript was not redrawn: %d entries", len(a.entries))
	}
	if a.input.String() != "and then what" {
		t.Fatalf("the box came back as %q", a.input.String())
	}
	if len(a.chips) != 1 || a.chips[0].path != "/tmp/lab/shot.png" {
		t.Fatalf("the picture came back as %v", a.chips)
	}
	if a.offset != 4 || a.stick {
		t.Fatalf("the reading position came back as %d/%v", a.offset, a.stick)
	}
	if !a.asking() {
		t.Fatal("the question the engine is still holding did not come back")
	}
	// THE SAME READING TIME, NOT A FRESH CLOCK. The countdown measures how long
	// somebody has had to read the question, and nobody read it while the
	// conversation was in another project (consent.go's [app.tickAsk]).
	after, _ := a.askLeft()
	if after > side.askLeft+time.Second || after < side.askLeft-time.Second {
		t.Fatalf("the countdown came back with %s left, and %s went in", after, side.askLeft)
	}
}

// A question the engine has since resolved is NOT restored, and that is the one
// direction the sidecar can be stale in: the turn was cancelled, the agent
// answered something else, the call went through. A card put back for a
// question nobody is asking is the worst outcome available, because a person
// would answer it.
func TestASwitchDoesNotBringBackAQuestionTheEngineHasResolved(t *testing.T) {
	agent := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	a := newTestApp(agent)
	a.askWait = 10 * time.Second
	a.askConsent(consentEvent(7, "read", "read consent.go", `tool "read"`))

	conv, side := a.front(), a.detachConversation()
	if side.askLeft <= 0 {
		t.Fatalf("the countdown did not go into the sidecar")
	}
	// The engine holds nothing now, and no turn is in flight to replay it.
	agent.pending, agent.running = nil, false
	drain(t, a, a.attachConversation(conv, side))

	if a.asking() {
		t.Fatal("a resolved question came back on the card")
	}
	if a.askResume != 0 {
		t.Fatalf("the countdown is still armed for the next question: %s", a.askResume)
	}
}

// The parked messages come back IN THE BOX rather than being dropped with a
// note, because the turn they were queued behind is still running.
func TestASwitchPutsParkedMessagesBackInTheBox(t *testing.T) {
	agent := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	a := newTestApp(agent)
	a.parks = []parked{{text: "and check the tests"}, {text: "then push"}}
	typeChars(t, a, "one more thing")

	conv, side := a.front(), a.detachConversation()
	drain(t, a, a.attachConversation(conv, side))

	want := "one more thing\nand check the tests\nthen push"
	if a.input.String() != want {
		t.Fatalf("the box came back as %q, want %q", a.input.String(), want)
	}
	if len(a.parks) != 0 {
		t.Fatalf("%d messages are still parked against a conversation nobody is drawing", len(a.parks))
	}
}

// An in-flight turn is redrawn from its first token rather than joined halfway.
func TestAttachingJoinsATurnThatIsStillRunning(t *testing.T) {
	agent := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	a := newTestApp(agent)
	agent.running = true
	agent.backlog = []session.Event{{Kind: session.EventTextDelta, Text: "half an answer"}}

	conv, side := a.front(), a.detachConversation()
	drain(t, a, a.attachConversation(conv, side))

	if a.state != stateWorking {
		t.Fatalf("the surface came back %v, and the turn is still running", a.state)
	}
	if !containsEntry(a, "half an answer") {
		t.Fatalf("the turn's first token was not redrawn:\n%s", frame(a))
	}
}

// containsEntry reports whether any block on screen carries this text.
func containsEntry(a *app, text string) bool {
	for _, e := range a.entries {
		if e.text == text {
			return true
		}
	}
	return false
}

// typeChars puts a sentence in the box WITHOUT pressing enter, which is what
// [typeLine] does and what a draft is the absence of.
func typeChars(t *testing.T, a *app, line string) {
	t.Helper()
	for _, r := range line {
		drive(t, a, key(string(r)))
	}
}

// drain runs whatever a door handed back and folds the result in, which is the
// program loop's own job.
func drain(t *testing.T, a *app, cmd tea.Cmd) {
	t.Helper()
	drive(t, a, runCmd(cmd)...)
}
