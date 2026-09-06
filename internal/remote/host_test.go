package remote

// host_test.go is the version-2 half of the engine's tests: the sequence
// numbers, the gap a reattach asks for, the three different ways a connection
// can end, and the questions that wait in an empty room.
//
// It drives the same scripted agent server_test.go does and the same raw frame
// link, because every question here is a question about the PROTOCOL — did this
// frame carry that number, did this surface get exactly the events it missed —
// and none of them is a question about a model.

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// dialSession is [dial] against a conversation that ALREADY EXISTS, which is
// the whole shape a host serves: several connections, one after another or at
// once, onto one session that outlives all of them.
//
// It repeats dial's few lines rather than sharing them because the difference
// is the entry point — [ServeAttach] instead of [Serve] — and a helper with a
// mode switch would have hidden exactly the thing these tests are about.
func dialSession(t *testing.T, sess *Session) *link {
	t.Helper()
	return dialOpening(t, func(Hello) (*Session, error) { return sess, nil })
}

// dialOpening is the same link with the conversation door in the test's hands,
// for the tests that are about WHETHER that door is reached at all.
func dialOpening(t *testing.T, open func(Hello) (*Session, error)) *link {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	l := &link{t: t, toward: inW, frames: make(chan Frame, 256), served: make(chan error, 1)}

	go func() {
		err := ServeAttach(inR, outW, AttachOptions{Open: open})
		_ = outW.Close()
		l.served <- err
	}()
	go func() {
		scan := bufio.NewScanner(outR)
		scan.Buffer(make([]byte, 0, 64*1024), frameCap)
		for scan.Scan() {
			var frame Frame
			if err := json.Unmarshal(scan.Bytes(), &frame); err != nil {
				l.frames <- Frame{Kind: unparsable, Error: scan.Text()}
				continue
			}
			l.frames <- frame
		}
		close(l.frames)
	}()

	t.Cleanup(func() { _ = inW.Close() })
	return l
}

// heldSession is one persistent conversation on the scripted agent.
func heldSession(agent *fakeAgent) *Session {
	return NewSession(engineOn(agent), true)
}

// A JOINED READER IS BOUND TO THE CONVERSATION IT JOINED, ON EVERY CALL.
//
// THE WELCOME ONLY PROVES WHOSE CONVERSATION IT WAS AT THE INSTANT OF ARRIVING.
// Several surfaces share one [Session], and the window that owns it can call
// [MethodSessionNew] or [MethodSessionOpen] a second later — which replaces the
// agent and the transcript underneath everybody. A reader opened onto one
// conversation's task 7 would then be answered by the REPLACEMENT conversation's
// task 7: a different piece of work, drawn under the name the reader is still
// showing, with no error anywhere. That is the wrong-owner failure the whole task
// lane exists to end, arriving one layer below the check that ends it.
func TestAJoinedReaderIsRefusedAfterTheOwnerOpensAnotherConversation(t *testing.T) {
	first := &fakeAgent{model: "a/b", title: "the one being read", taskJournal: "/w/task-7.jsonl"}
	second := &fakeAgent{model: "a/b", title: "something else", taskJournal: "/w/other-7.jsonl"}

	engine := engineOn(first)
	engine.Fresh = func() (WrappedAgent, string, error) { return second, "/sessions/two.jsonl", nil }
	var read []string
	engine.TaskRecord = func(uri string, _ int) (session.TaskRecord, error) {
		read = append(read, uri)
		return session.TaskRecord{Report: "what it said"}, nil
	}
	sess := NewSession(engine, true)

	owner := dialSession(t, sess)
	owner.hello(Hello{Version: Version, Surface: "macbook"})
	reader := dialSession(t, sess)
	reader.hello(Hello{
		Version: Version, Surface: "reader",
		Session: engine.SessionFile, Join: true, Watch: true,
	})

	// While it is the conversation that was joined, the reader reads it.
	if frame := reader.call(1, MethodTaskRoom, TaskRoomArgs{ID: 7, Tail: 4}); frame.Error != "" {
		t.Fatalf("the reader could not read the conversation it joined: %v", frame.Error)
	}
	if len(read) != 1 || !strings.HasSuffix(read[0], "/w/task-7.jsonl") {
		t.Fatalf("the reading went to %v, want the joined conversation's own journal", read)
	}

	// The window that owns the session opens something else in it.
	owner.ok(2, MethodSessionNew, nil)

	// AND THE READER IS TOLD RATHER THAN QUIETLY MOVED.
	frame := reader.call(3, MethodTaskRoom, TaskRoomArgs{ID: 7, Tail: 4})
	if frame.Error == "" {
		t.Fatal("the reader was handed the replacement conversation's task 7")
	}
	if !strings.Contains(frame.Error, "not open here any more") {
		t.Fatalf("the reader was refused with %q, want the sentence its page acts on", frame.Error)
	}
	if len(read) != 1 {
		t.Fatalf("the replacement conversation's journal was read: %v", read)
	}
	// And the owner's own connection follows its session, as it always has.
	if frame := owner.call(4, MethodTaskRoom, TaskRoomArgs{ID: 7, Tail: 4}); frame.Error != "" {
		t.Fatalf("the owning window was refused its own conversation: %v", frame.Error)
	}
	if len(read) != 2 || !strings.HasSuffix(read[1], "/w/other-7.jsonl") {
		t.Fatalf("the owning window did not follow its session: %v", read)
	}
}

// ── sequence numbers ────────────────────────────────────────────────────────

func TestEveryEventIsNumberedAndTheCloseNamesTheLast(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)
	l := dialSession(t, sess)
	l.hello(Hello{Version: Version})

	ref := decode[StreamRef](t, l.ok(1, MethodSubmit, SubmitArgs{Text: "go"}).Payload)
	stream := agent.stream(0)
	stream <- session.Event{Kind: session.EventTextDelta, Text: "one"}
	stream <- session.Event{Kind: session.EventTextDelta, Text: "two"}
	stream <- session.Event{Kind: session.EventTurnDone}
	agent.finish(stream)

	for want := uint64(1); want <= 3; want++ {
		frame := l.recv()
		if frame.Kind != "event" || frame.ID != ref.Stream {
			t.Fatalf("frame %d was %q on stream %d", want, frame.Kind, frame.ID)
		}
		if frame.Seq != want {
			t.Fatalf("event %d carried seq %d", want, frame.Seq)
		}
	}
	closed := l.recv()
	if closed.Kind != "closed" || closed.Seq != 3 {
		// The close carries the seq of the LAST event it follows, which is how
		// a surface tells a stream that ended from one it lost the tail of.
		t.Fatalf("the stream ended with %q seq %d, want closed seq 3", closed.Kind, closed.Seq)
	}
}

// ── the gap, and only the gap ───────────────────────────────────────────────

func TestAReattachIsSentTheGapAndNotTheConversation(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	first := dialSession(t, sess)
	first.hello(Hello{Version: Version})
	ref := decode[StreamRef](t, first.ok(1, MethodSubmit, SubmitArgs{Text: "refactor everything"}).Payload)
	stream := agent.stream(0)
	stream <- session.Event{Kind: session.EventTextDelta, Text: "one"}
	stream <- session.Event{Kind: session.EventTextDelta, Text: "two"}
	for seq := uint64(1); seq <= 2; seq++ {
		if frame := first.recv(); frame.Seq != seq {
			t.Fatalf("the first surface saw seq %d, want %d", frame.Seq, seq)
		}
	}
	// The link dies without a goodbye, which is a laptop lid and not a decision.
	if err := first.end(); err != nil {
		t.Fatalf("the engine ended the first connection with %v", err)
	}

	// The turn keeps going with nobody watching — that is the whole point — and
	// its events pile up in the ring.
	stream <- session.Event{Kind: session.EventTextDelta, Text: "three"}
	stream <- session.Event{Kind: session.EventTextDelta, Text: "four"}
	waitForEngine(t, func() bool { return sess.liveSeq(ref.Stream) == 4 })

	second := dialSession(t, sess)
	frame := second.hello(Hello{Version: Version, Resume: []StreamCursor{{Stream: ref.Stream, Seq: 2}}})
	welcome := decode[Welcome](t, frame.Payload)
	if welcome.Live != ref.Stream {
		t.Fatalf("the welcome named live stream %d, want %d", welcome.Live, ref.Stream)
	}
	if !welcome.Persistent {
		t.Fatal("a session that outlives its connections said it was not persistent")
	}
	for _, want := range []struct {
		seq  uint64
		text string
	}{{3, "three"}, {4, "four"}} {
		got := second.recv()
		if got.Kind != "event" || got.Seq != want.seq {
			t.Fatalf("the replay gave %q seq %d, want event seq %d", got.Kind, got.Seq, want.seq)
		}
		if text := decode[EventWire](t, got.Payload).Unwire().Text; text != want.text {
			t.Fatalf("the replay gave %q, want %q", text, want.text)
		}
	}

	// And then it is live: the next event arrives as an ordinary frame.
	stream <- session.Event{Kind: session.EventTextDelta, Text: "five"}
	if got := second.recv(); got.Seq != 5 {
		t.Fatalf("the live tail carried seq %d, want 5", got.Seq)
	}
	agent.finish(stream)
	if closed := second.recv(); closed.Kind != "closed" || closed.Seq != 5 {
		t.Fatalf("the stream ended with %q seq %d", closed.Kind, closed.Seq)
	}
}

// A cursor for a turn that is OVER is answered with nothing, and never with an
// error: the transcript is the authority on a finished turn and the surface
// reads it anyway.
func TestACursorForAFinishedTurnIsAnsweredWithNothing(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	first := dialSession(t, sess)
	first.hello(Hello{Version: Version})
	ref := decode[StreamRef](t, first.ok(1, MethodSubmit, SubmitArgs{Text: "go"}).Payload)
	stream := agent.stream(0)
	stream <- session.Event{Kind: session.EventTextDelta, Text: "one"}
	first.recv()
	agent.finish(stream)
	first.recv()
	_ = first.end()

	second := dialSession(t, sess)
	frame := second.hello(Hello{Version: Version, Resume: []StreamCursor{{Stream: ref.Stream, Seq: 0}}})
	welcome := decode[Welcome](t, frame.Payload)
	if welcome.Live != 0 {
		t.Fatalf("a finished turn was named live as %d", welcome.Live)
	}
	// Nothing at all should follow the welcome, so an ordinary call is the next
	// frame on the wire.
	if result := second.ok(1, MethodTitle, nil); result.Kind != "result" {
		t.Fatalf("something was replayed for a stream the engine no longer holds: %q", result.Kind)
	}
}

// ── the three roads out ─────────────────────────────────────────────────────

func TestDetachLeavesTheTurnRunning(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	l := dialSession(t, sess)
	l.hello(Hello{Version: Version})
	ref := decode[StreamRef](t, l.ok(1, MethodSubmit, SubmitArgs{Text: "the long one"}).Payload)
	l.ok(2, MethodDetach, nil)
	if err := l.end(); err != nil {
		t.Fatalf("the engine ended a detach with %v", err)
	}

	if agent.interrupts != 0 {
		t.Fatalf("a detach interrupted the turn %d times", agent.interrupts)
	}
	if agent.closes != 0 {
		t.Fatalf("a detach closed the conversation %d times", agent.closes)
	}

	// The turn is still the engine's to finish, and what it says while nobody
	// is there is waiting when somebody comes back.
	stream := agent.stream(0)
	stream <- session.Event{Kind: session.EventTextDelta, Text: "finished it"}
	waitForEngine(t, func() bool { return sess.liveSeq(ref.Stream) == 1 })

	back := dialSession(t, sess)
	back.hello(Hello{Version: Version, Resume: []StreamCursor{{Stream: ref.Stream}}})
	got := back.recv()
	if text := decode[EventWire](t, got.Payload).Unwire().Text; text != "finished it" {
		t.Fatalf("the returning surface was given %q", text)
	}
}

func TestATornPipeLeavesAPersistentConversationAlone(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	l := dialSession(t, sess)
	l.hello(Hello{Version: Version})
	l.ok(1, MethodSubmit, SubmitArgs{Text: "go"})
	if err := l.end(); err != nil {
		t.Fatalf("the engine ended a torn pipe with %v", err)
	}
	if agent.interrupts != 0 || agent.closes != 0 {
		t.Fatalf("a torn pipe interrupted %d times and closed %d times", agent.interrupts, agent.closes)
	}
	if sess.Ended() {
		t.Fatal("a torn pipe ended a conversation something else was holding")
	}
}

// The other half of the same fork, driven through [Loopback] because the shape
// being tested is the REAL one: a bare `aforge engine` on a pipe, whose life
// the conversation's life is. [Loop.Cut] is a link that died without saying
// goodbye — the laptop lid — and on this shape that has to mean the end.
func TestATornPipeStillEndsAnEngineThatIsThePipe(t *testing.T) {
	agent := &fakeAgent{}
	loop, err := Loopback(Hello{Version: Version}, Options{
		Boot: func(Hello) (*Engine, error) { return engineOn(agent), nil },
	})
	if err != nil {
		t.Fatalf("dial the loopback: %v", err)
	}
	if loop.Client.Welcome().Persistent {
		t.Fatal("an engine on a pipe said it was persistent")
	}
	if _, err := loop.Client.Agent().Submit(t.Context(), "go"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if err := loop.Cut(); err != nil {
		t.Fatalf("cut the link: %v", err)
	}
	select {
	case <-loop.Served:
	case <-time.After(5 * time.Second):
		t.Fatal("the engine did not finish after the link was cut")
	}
	if agent.interrupts == 0 || agent.closes == 0 {
		t.Fatalf("the pipe died and the turn was left running: %d interrupts, %d closes",
			agent.interrupts, agent.closes)
	}
}

func TestACloseEndsTheConversationForEverybody(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	l := dialSession(t, sess)
	l.hello(Hello{Version: Version})
	l.ok(1, MethodClose, nil)
	_ = l.end()

	if !sess.Ended() {
		t.Fatal("a deliberate close left the conversation open")
	}
	if agent.closes == 0 {
		t.Fatal("a deliberate close did not flush the journal")
	}
}

// ── the waiting room ────────────────────────────────────────────────────────

func TestAQuestionRaisedInAnEmptyRoomIsWaitingOnTheNextAttach(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	l := dialSession(t, sess)
	l.hello(Hello{Version: Version})
	l.ok(1, MethodSubmit, SubmitArgs{Text: "tidy the repo"})
	if err := l.end(); err != nil {
		t.Fatalf("the engine ended the connection with %v", err)
	}

	// The turn reaches a tool it has to ask about, with nobody there to ask.
	stream := agent.stream(0)
	stream <- session.Event{
		Kind: session.EventConsentRequest,
		ID:   7,
		Tool: "bash",
		Hint: "rm -rf build",
		Rule: `bash pattern "rm -rf *"`,
	}
	waitForEngine(t, func() bool { return sess.heldCount() == 1 })

	back := dialSession(t, sess)
	frame := back.hello(Hello{Version: Version})
	welcome := decode[Welcome](t, frame.Payload)
	if len(welcome.Held) != 1 {
		t.Fatalf("the welcome carried %d held questions, want 1", len(welcome.Held))
	}
	question := welcome.Held[0]
	if question.Kind != HeldConsent {
		t.Fatalf("the held question was a %q", question.Kind)
	}
	if question.Event.Unwire().Hint != "rm -rf build" {
		t.Fatalf("the held question carried %q", question.Event.Unwire().Hint)
	}
	if question.Since.IsZero() {
		t.Fatal("the held question does not say how long it has been waiting")
	}

	// Answering it takes it out of the room.
	back.ok(1, MethodConsent, ConsentArgs{ID: 7, Allow: true})
	after := decode[[]HeldQuestion](t, back.ok(2, MethodHeldQuestions, nil).Payload)
	if len(after) != 0 {
		t.Fatalf("%d questions were still waiting after one was answered", len(after))
	}
}

// A card raised while somebody IS watching is on their screen, so it is not
// held — until they walk away without answering it, which is the same fact
// arriving later.
func TestAQuestionLeftBehindBecomesOneThatIsWaiting(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	l := dialSession(t, sess)
	l.hello(Hello{Version: Version})
	l.ok(1, MethodSubmit, SubmitArgs{Text: "go"})
	stream := agent.stream(0)
	stream <- session.Event{Kind: session.EventConsentRequest, ID: 3, Tool: "edit"}
	l.recv()

	held := decode[[]HeldQuestion](t, l.ok(2, MethodHeldQuestions, nil).Payload)
	if len(held) != 0 {
		t.Fatalf("a card on somebody's screen was reported as waiting: %d", len(held))
	}
	if err := l.end(); err != nil {
		t.Fatalf("the engine ended the connection with %v", err)
	}

	back := dialSession(t, sess)
	welcome := decode[Welcome](t, back.hello(Hello{Version: Version}).Payload)
	if len(welcome.Held) != 1 {
		t.Fatalf("the card left behind was not waiting: %d held", len(welcome.Held))
	}
}

// AND IT REACHES THE NEXT WINDOW BEFORE THE DEAD ONE HAS LEFT. This is the
// ordering a killed terminal actually produces: it says nothing on its way out,
// so the session only learns it is gone when its socket gets around to
// reporting the end — which can be after the window that replaced it has been
// welcomed. Nothing here detaches, and the card is still handed over, because
// what makes a question waiting is that THIS surface has not been sent it.
func TestTheNextWindowIsHandedTheCardBeforeTheOneItReplacedHasLeft(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	first := dialSession(t, sess)
	first.hello(Hello{Version: Version})
	first.ok(1, MethodSubmit, SubmitArgs{Text: "port the parser"})
	stream := agent.stream(0)
	stream <- session.Event{Kind: session.EventConsentRequest, ID: 5, Tool: "bash", Hint: "git push"}
	// Receiving the frame is the barrier: the card has been recorded and this
	// surface is on record as having drawn it.
	first.recv()

	back := dialSession(t, sess)
	welcome := decode[Welcome](t, back.hello(Hello{Version: Version}).Payload)
	if len(welcome.Held) != 1 || welcome.Held[0].Event.Unwire().ID != 5 {
		t.Fatalf("the returning window was handed %+v, want the unanswered card", welcome.Held)
	}

	// And the window that already drew it is not handed it a second time.
	again := decode[[]HeldQuestion](t, first.ok(2, MethodHeldQuestions, nil).Payload)
	if len(again) != 0 {
		t.Fatalf("a card already on somebody's screen was handed to them again: %d", len(again))
	}
}

// ── more than one surface ───────────────────────────────────────────────────

func TestASecondSurfaceIsToldItIsNotAlone(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	first := dialSession(t, sess)
	welcome := decode[Welcome](t, first.hello(Hello{Version: Version}).Payload)
	if welcome.Attached != 0 {
		t.Fatalf("the first surface was told %d others were attached", welcome.Attached)
	}

	second := dialSession(t, sess)
	welcome = decode[Welcome](t, second.hello(Hello{Version: Version}).Payload)
	if welcome.Attached != 1 {
		t.Fatalf("the second surface was told %d others were attached, want 1", welcome.Attached)
	}

	// And a turn's events reach both of them — started by the SECOND, because
	// the newest window is the one holding the keyboard (driver.go).
	ref := decode[StreamRef](t, second.ok(1, MethodSubmit, SubmitArgs{Text: "go"}).Payload)
	stream := agent.stream(0)
	stream <- session.Event{Kind: session.EventTextDelta, Text: "hello"}
	for _, l := range []*link{first, second} {
		frame := l.await(func(f Frame) bool { return f.Kind == "event" && f.ID == ref.Stream })
		if text := decode[EventWire](t, frame.Payload).Unwire().Text; text != "hello" {
			t.Fatalf("a surface was given %q", text)
		}
	}
}

// ── the ring's own bound ────────────────────────────────────────────────────

func TestTheRingKeepsTheNewestAndSaysSoWithItsNumbers(t *testing.T) {
	r := &ring{first: 1}
	for i := 0; i < ringEvents+10; i++ {
		r.add(json.RawMessage(`{}`))
	}
	if r.last != uint64(ringEvents+10) {
		t.Fatalf("the ring counted %d events", r.last)
	}
	if len(r.events) != ringEvents {
		t.Fatalf("the ring kept %d events, want %d", len(r.events), ringEvents)
	}
	// A cursor that fell off the front is answered with what is still held, and
	// the seq of the first frame back declares the hole rather than hiding it.
	from, kept := r.after(1)
	if from != r.first || len(kept) != ringEvents {
		t.Fatalf("a fallen-off cursor was answered from %d with %d events", from, len(kept))
	}
	if from <= 2 {
		t.Fatalf("the replay pretended nothing was missed: it began at %d", from)
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

// liveSeq is how far a running stream has got, for a test that has to wait for
// a pump it does not own.
func (sess *Session) liveSeq(id uint64) uint64 {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if held := sess.rings[id]; held != nil {
		return held.last
	}
	return 0
}

// heldCount is how many questions are outstanding, read under the session's own
// lock — a test that reached into the set directly would be racing the pump
// that fills it.
func (sess *Session) heldCount() int {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.held.outstanding()
}

func waitForEngine(t *testing.T, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatal("the engine did not get there in five seconds")
		}
		time.Sleep(2 * time.Millisecond)
	}
}
