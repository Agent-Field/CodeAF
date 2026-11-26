package remote

// Everything here drives the REAL client through the REAL redial loop. What is
// faked is only the far end and only the pipe under it — the same bargain
// client_test.go's scripted engine strikes, and the same one loopback.go makes
// for the packages outside this one: a dialer that hands back an in-memory pipe
// is exactly what [Dialer] is for, and cutting that pipe without saying goodbye
// is [Loop.Cut]'s event — a laptop that slept, a network that went away.

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── a far end the redial loop can reach twice ───────────────────────────────

// script is one engine a dial will find: what it answers the hello with, and
// what it plays afterwards. A roaming test needs a list of these because the
// whole point is that the second dial reaches a DIFFERENT far end — a fresh
// process on the engine machine, or the session host that was there all along.
type script struct {
	welcome Welcome
	// play runs on the engine's own goroutine once the welcome is out, with the
	// hello that arrived — which is where a resume test reads the cursors.
	play func(e *scripted, hello Hello)
}

// scripted is one live instance of a script.
type scripted struct {
	t    *testing.T
	conn net.Conn
	// said is what this far end answers a hello with.
	said Welcome

	mu    sync.Mutex
	hello Hello
	calls []Frame
}

func (e *scripted) serve(play func(*scripted, Hello)) {
	lines := bufio.NewScanner(e.conn)
	lines.Buffer(make([]byte, 0, 1<<16), 1<<24)
	streams := uint64(0)
	for lines.Scan() {
		var frame Frame
		if err := json.Unmarshal(lines.Bytes(), &frame); err != nil {
			return
		}
		switch frame.Kind {
		case "hello":
			var hello Hello
			_ = json.Unmarshal(frame.Payload, &hello)
			e.mu.Lock()
			e.hello = hello
			e.mu.Unlock()
			e.send(Frame{Kind: "welcome", Payload: mustClientJSON(e.welcomeOf())})
			if play != nil {
				go play(e, hello)
			}
		case "call":
			e.mu.Lock()
			e.calls = append(e.calls, frame)
			e.mu.Unlock()
			switch frame.Method {
			case MethodSubmit, MethodSubmitImage, MethodFollowUp:
				streams++
				e.send(Frame{Kind: "result", ID: frame.ID, Payload: mustClientJSON(StreamRef{Stream: streams})})
			default:
				e.send(Frame{Kind: "result", ID: frame.ID})
			}
		}
	}
}

func (e *scripted) welcomeOf() Welcome {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.said
}

func (e *scripted) send(frame Frame) {
	line, err := json.Marshal(frame)
	if err != nil {
		e.t.Errorf("marshal: %v", err)
		return
	}
	_, _ = e.conn.Write(append(line, '\n'))
}

// event is one numbered event of one stream, which is what a resume is about.
func (e *scripted) event(stream, seq uint64, text string) {
	e.send(Frame{
		Kind:    "event",
		ID:      stream,
		Seq:     seq,
		Payload: mustClientJSON(WireEvent(session.Event{Kind: session.EventTextDelta, Text: text})),
	})
}

func (e *scripted) closeStream(stream, seq uint64) {
	e.send(Frame{Kind: "closed", ID: stream, Seq: seq})
}

func (e *scripted) sawHello() Hello {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.hello
}

// roamer hands out one far end per dial, in order, and keeps the surface half of
// every pipe so a test can cut one.
type roamer struct {
	t       *testing.T
	scripts []script

	mu       sync.Mutex
	dials    int
	engines  []*scripted
	surfaces []net.Conn
}

func (r *roamer) dial() (io.ReadWriteCloser, error) {
	r.mu.Lock()
	if r.dials >= len(r.scripts) {
		r.dials++
		r.mu.Unlock()
		// A machine that is not answering: the ordinary shape of a link that
		// stays down, and what the give-up test is made of.
		return nil, errors.New("no route to host")
	}
	next := r.scripts[r.dials]
	r.dials++
	ours, theirs := net.Pipe()
	engine := &scripted{t: r.t, conn: theirs, said: next.welcome}
	r.engines = append(r.engines, engine)
	r.surfaces = append(r.surfaces, ours)
	r.mu.Unlock()
	go engine.serve(next.play)
	return ours, nil
}

// cut kills the nth link the way a dropped connection kills one: no goodbye, no
// [MethodClose], nothing the client can read as a door being shut.
func (r *roamer) cut(n int) {
	r.mu.Lock()
	conn := r.surfaces[n]
	r.mu.Unlock()
	_ = conn.Close()
}

func (r *roamer) engine(n int) *scripted {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n >= len(r.engines) {
		return nil
	}
	return r.engines[n]
}

func (r *roamer) dialled() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dials
}

// roam dials a real client through the real loop, on a short window so a test
// that watches it give up finishes in a second rather than in five minutes.
func roam(t *testing.T, scripts ...script) (*Client, *roamer) {
	t.Helper()
	r := &roamer{t: t, scripts: scripts}
	client, err := Roam("devbox", Hello{Workspace: "app"}, Roaming{Dial: r.dial, Window: time.Second})
	if err != nil {
		t.Fatalf("roam: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, r
}

// nextEvent reads one event off a turn, or fails the test rather than hanging
// for the package's whole timeout.
func nextEvent(t *testing.T, events <-chan session.Event) (session.Event, bool) {
	t.Helper()
	select {
	case ev, ok := <-events:
		return ev, ok
	case <-time.After(10 * time.Second):
		t.Fatal("nothing arrived on the turn")
		return session.Event{}, false
	}
}

// waitFor spins until something is true, which is how a test watches a loop
// that runs on the client's own reader goroutine.
func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// ── the replay law ──────────────────────────────────────────────────────────

// A turn's text drawn twice is the one failure a person would read as the
// program having lost its mind, so an event whose seq has already been
// delivered is dropped — on the same connection or after a redial, it makes no
// difference which.
func TestAnEventAlreadyDeliveredIsNotDeliveredTwice(t *testing.T) {
	drew := make(chan struct{})
	client, r := roam(t, script{
		welcome: Welcome{Version: Version, SessionFile: "/j.jsonl", Persistent: true},
		play: func(e *scripted, _ Hello) {
			<-drew
			e.event(1, 1, "one")
			e.event(1, 2, "two")
			e.event(1, 2, "two")
			e.event(1, 1, "one")
			e.event(1, 3, "three")
			e.closeStream(1, 3)
		},
	})
	events, err := client.Agent().Submit(nil, "go")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	close(drew)

	var text []string
	for ev := range events {
		if ev.Kind == session.EventTextDelta {
			text = append(text, ev.Text)
		}
	}
	if strings.Join(text, " ") != "one two three" {
		t.Fatalf("the surface saw %q, and every event belongs on the screen exactly once", strings.Join(text, " "))
	}
	if got := r.dialled(); got != 1 {
		t.Fatalf("nothing was lost, so nothing should have been redialled: %d dials", got)
	}
}

// ── roaming ─────────────────────────────────────────────────────────────────

func TestARedialStillDeliversAQuestionMissedByTheExistingWindow(t *testing.T) {
	first := make(chan struct{})
	question := session.Event{Kind: session.EventConsentRequest, ID: 7, Tool: "bash"}
	client, links := roam(t,
		script{
			welcome: Welcome{Version: Version, SessionFile: "/j.jsonl", Persistent: true},
			play: func(engine *scripted, _ Hello) {
				<-first
				engine.event(1, 1, "started")
			},
		},
		script{
			welcome: Welcome{Version: Version, SessionFile: "/j.jsonl", Persistent: true, Live: 1,
				Held: []HeldQuestion{{Kind: HeldConsent, Event: WireEvent(question), Stream: 1}}},
			play: func(engine *scripted, _ Hello) {
				engine.send(Frame{Kind: "event", ID: 1, Seq: 2, Payload: mustClientJSON(WireEvent(question))})
				engine.closeStream(1, 2)
			},
		},
	)
	events, err := client.Agent().Submit(nil, "run the checks")
	if err != nil {
		t.Fatal(err)
	}
	close(first)
	if event, _ := nextEvent(t, events); event.Text != "started" {
		t.Fatalf("initial event = %+v", event)
	}
	links.cut(0)
	questions := 0
	for event := range events {
		if event.Kind == session.EventError {
			t.Fatal(event.Err)
		}
		if event.Kind == session.EventConsentRequest && event.ID == 7 {
			questions++
		}
	}
	if questions != 1 {
		t.Fatalf("the existing window received %d copies of its missed question", questions)
	}
}

// The whole story in one test: a turn is running, the link dies without a
// goodbye, the client redials by itself, says how far it got, and the turn
// carries on where it was — with the overlap the engine replays drawn nowhere.
func TestARedialResumesTheTurnItWasInTheMiddleOf(t *testing.T) {
	first := make(chan struct{})
	client, r := roam(t,
		script{
			welcome: Welcome{Version: Version, SessionFile: "/j.jsonl", Persistent: true},
			play: func(e *scripted, _ Hello) {
				<-first
				e.event(1, 1, "the ")
				e.event(1, 2, "answer ")
			},
		},
		script{
			welcome: Welcome{Version: Version, SessionFile: "/j.jsonl", Persistent: true, Live: 1},
			play: func(e *scripted, hello Hello) {
				// The gap, and one event the engine is not sure we had.
				e.event(1, 2, "answer ")
				e.event(1, 3, "is ")
				e.event(1, 4, "42")
				e.closeStream(1, 4)
			},
		},
	)

	events, err := client.Agent().Submit(nil, "what is it")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	close(first)
	for _, want := range []string{"the ", "answer "} {
		ev, ok := nextEvent(t, events)
		if !ok || ev.Text != want {
			t.Fatalf("wanted %q, got %q (open %v)", want, ev.Text, ok)
		}
	}

	r.cut(0)

	var rest []string
	for ev := range events {
		if ev.Kind == session.EventError {
			t.Fatalf("the turn was told it failed, and it did not: %v", ev.Err)
		}
		rest = append(rest, ev.Text)
	}
	if strings.Join(rest, "") != "is 42" {
		t.Fatalf("after the redial the turn drew %q, and the replayed overlap must not be drawn again", strings.Join(rest, ""))
	}

	// And the second hello said exactly where the surface had got to, which is
	// the only thing that lets an engine send the gap and not the conversation.
	hello := r.engine(1).sawHello()
	if len(hello.Resume) != 1 || hello.Resume[0].Stream != 1 || hello.Resume[0].Seq != 2 {
		t.Fatalf("the redial asked to resume %+v, and the surface had seen stream 1 through event 2", hello.Resume)
	}
	if hello.Session != "/j.jsonl" {
		t.Fatalf("the redial asked for session %q, and this window was in /j.jsonl", hello.Session)
	}
}

// AGAINST AN ENGINE THAT KEEPS NOTHING, A REDIAL IS A FRESH ENGINE. The
// conversation is still on that machine's disk; the turn that was in flight is
// not, and the surface says which of the two it lost rather than leaving a
// person to guess at a reply that stopped mid-sentence.
func TestARedialOntoAnEngineThatKeepsNothingSaysTheTurnIsGone(t *testing.T) {
	first := make(chan struct{})
	client, r := roam(t,
		script{
			welcome: Welcome{Version: Version, SessionFile: "/j.jsonl", Persistent: true},
			play: func(e *scripted, _ Hello) {
				<-first
				e.event(1, 1, "half an ")
			},
		},
		// The other honest shape: `codeaf engine` on a pipe, which says nothing
		// about persistence because it has none to claim.
		script{welcome: Welcome{Version: Version, SessionFile: "/j.jsonl"}},
	)

	events, err := client.Agent().Submit(nil, "go")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	close(first)
	if ev, ok := nextEvent(t, events); !ok || ev.Text != "half an " {
		t.Fatalf("wanted the first half of the answer, got %q", ev.Text)
	}

	r.cut(0)

	failed, ok := nextEvent(t, events)
	if !ok || failed.Kind != session.EventError || failed.Err == nil {
		t.Fatalf("the turn should have been told why it stopped, got %+v", failed)
	}
	said := failed.Err.Error()
	for _, want := range []string{"devbox does not keep a turn running while nothing is attached", "asking again"} {
		if !strings.Contains(said, want) {
			t.Fatalf("the turn was told %q, which does not say %q", said, want)
		}
	}
	if _, open := <-events; open {
		t.Fatal("the turn should have ended: nothing is running it")
	}
	waitFor(t, "the notice about the lost turn", func() bool {
		return strings.Contains(client.TakeNotice(), "did not survive the drop")
	})
	if notice := client.TakeNotice(); notice != "" {
		t.Fatalf("a notice is news and is shown once, and this one came back a second time: %q", notice)
	}
	if client.Err() != nil {
		t.Fatalf("the connection itself is fine: %v", client.Err())
	}
	if r.dialled() != 2 {
		t.Fatalf("one drop is one redial, and there were %d dials", r.dialled())
	}
}

// A conversation must not be swapped under a person in silence.
func TestAReattachOntoAnotherConversationIsSaidOutLoud(t *testing.T) {
	first := make(chan struct{})
	client, r := roam(t,
		script{
			welcome: Welcome{Version: Version, SessionFile: "/j.jsonl", Persistent: true},
			play: func(e *scripted, _ Hello) {
				<-first
				e.event(1, 1, "working")
			},
		},
		script{welcome: Welcome{Version: Version, SessionFile: "/somewhere-else.jsonl", Persistent: true}},
	)

	events, err := client.Agent().Submit(nil, "go")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	close(first)
	nextEvent(t, events)
	r.cut(0)

	failed, ok := nextEvent(t, events)
	if !ok || failed.Kind != session.EventError || failed.Err == nil {
		t.Fatalf("the turn should have been ended with a reason, got %+v", failed)
	}
	if !strings.Contains(failed.Err.Error(), "opened a different conversation") {
		t.Fatalf("the turn was told %q", failed.Err.Error())
	}
	waitFor(t, "the notice about the swap", func() bool {
		return strings.Contains(client.TakeNotice(), "different conversation open than the one this window left")
	})
}

// THE QUIET SENTENCE IS THE ONE A PERSON READS WHILE IT IS STILL TRYING, and a
// call made in that gap is refused with the same fact rather than written onto a
// pipe that is not there.
func TestWhileItIsReconnectingItSaysSoQuietly(t *testing.T) {
	client, r := roam(t,
		script{welcome: Welcome{Version: Version, SessionFile: "/j.jsonl", Persistent: true}},
		script{welcome: Welcome{Version: Version, SessionFile: "/j.jsonl", Persistent: true}},
	)
	if note := client.LinkNote(); note != "" {
		t.Fatalf("a healthy link says nothing at all, and this one said %q", note)
	}

	r.cut(0)
	waitFor(t, "the reconnecting note", func() bool { return client.LinkNote() != "" })
	if note := client.LinkNote(); note != "reconnecting to devbox — trying for up to 1 second" {
		t.Fatalf("the note reads %q", note)
	}
	if _, err := client.HeldQuestions(); err == nil || !strings.Contains(err.Error(), "reconnecting to devbox — try that again in a moment") {
		t.Fatalf("a call in the gap answered %v", err)
	}

	waitFor(t, "the link to come back", func() bool { return r.dialled() == 2 && client.LinkNote() == "" })
	if client.Err() != nil {
		t.Fatalf("the link came back, so the connection is not gone: %v", client.Err())
	}
}

// AND THE ROAMING IS BOUNDED. When the window runs out the sentence is the one
// this package has always said about a lost connection — there is no second
// vocabulary for a link that ended.
func TestRoamingGivesUpAndSaysTheOneSentence(t *testing.T) {
	client, r := roam(t, script{welcome: Welcome{Version: Version, SessionFile: "/j.jsonl", Persistent: true}})
	r.cut(0)

	waitFor(t, "the connection to be given up on", func() bool { return client.Err() != nil })
	assertGoneSentence(t, client.Err())
	if r.dialled() < 2 {
		t.Fatalf("it gave up without trying: %d dials", r.dialled())
	}
	if note := client.LinkNote(); note != "" {
		t.Fatalf("nothing is reconnecting any more, so the note is empty, not %q", note)
	}
}

// A close is a person leaving, and it must take the redialling with it: an ssh
// child spawned after the terminal was handed back is a real thing to get wrong.
func TestClosingWhileItRoamsStopsTheRedialling(t *testing.T) {
	client, r := roam(t,
		script{welcome: Welcome{Version: Version, SessionFile: "/j.jsonl", Persistent: true}},
		script{welcome: Welcome{Version: Version, SessionFile: "/j.jsonl", Persistent: true}},
	)
	r.cut(0)
	waitFor(t, "the reconnecting note", func() bool { return client.LinkNote() != "" })
	_ = client.Close()
	waitFor(t, "the client to settle", func() bool { return client.Err() != nil })
	time.Sleep(2 * firstBackoff)
	if r.dialled() != 1 {
		t.Fatalf("it kept dialling after the person left: %d dials", r.dialled())
	}
	if err := client.Err(); err == nil || !strings.Contains(err.Error(), "this connection is closed") {
		t.Fatalf("a connection the person closed reads %v", err)
	}
}

// A TURN THAT FINISHED WHILE NOBODY WAS WATCHING MUST NOT LEAVE THE SURFACE
// SPINNING. The engine still holds its events, so the tail is replayed and then
// there is silence — no "closed" frame, because the engine has moved on to a
// newer turn. The stream takes the tail, waits out the quiet, and ends without
// claiming anything failed.
func TestAResumedTurnThatTheEngineHasMovedOnFromEndsQuietly(t *testing.T) {
	first := make(chan struct{})
	client, r := roam(t,
		script{
			welcome: Welcome{Version: Version, SessionFile: "/j.jsonl", Persistent: true},
			play: func(e *scripted, _ Hello) {
				<-first
				e.event(1, 1, "the ")
			},
		},
		script{
			// Live names a newer turn, so the one this window was on is over.
			welcome: Welcome{Version: Version, SessionFile: "/j.jsonl", Persistent: true, Live: 2},
			play: func(e *scripted, _ Hello) {
				e.event(1, 2, "rest of it")
			},
		},
	)

	events, err := client.Agent().Submit(nil, "go")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	close(first)
	nextEvent(t, events)
	r.cut(0)

	if ev, ok := nextEvent(t, events); !ok || ev.Text != "rest of it" {
		t.Fatalf("the replayed tail should have been drawn, got %q (open %v)", ev.Text, ok)
	}
	select {
	case ev, open := <-events:
		if open {
			t.Fatalf("the turn was over, and it said %+v", ev)
		}
	case <-time.After(4 * resumeTail):
		t.Fatal("the turn never ended, so the surface is waiting on work nobody is doing")
	}
}

// ── against the real engine half ────────────────────────────────────────────

// realFarEnd is [Loopback]'s own body with the dialling split out of it, which
// is the one change roaming needs: [Roam] opens the pipe itself, so a test
// cannot hand it one. The server is the real [Serve], the frames are the real
// frames, and the returned cut kills the link the way [Loop.Cut] does — without
// a goodbye.
func realFarEnd(boot func(Hello) (*Engine, error)) (io.ReadWriteCloser, func()) {
	surface, engine := Pipe()
	go func() {
		_ = Serve(engine, engine, Options{Boot: boot})
		_ = engine.Close()
	}()
	return surface, func() { _ = surface.Close() }
}

// THE HONEST CASE, END TO END. `codeaf engine` on a pipe is a legitimate far end
// and it keeps nothing: the second dial reaches a second engine, which is a
// second conversation opened on the same file, and the turn that was in flight
// when the link died is over. Nothing here pretends otherwise, and the
// connection itself carries on working.
func TestARealEngineThatKeepsNothingIsRedialledHonestly(t *testing.T) {
	agents := []*fakeAgent{{model: "a/b", title: "one"}, {model: "a/b", title: "one"}}
	var mu sync.Mutex
	dials := 0
	var cut func()
	dialer := func() (io.ReadWriteCloser, error) {
		mu.Lock()
		n := dials
		dials++
		mu.Unlock()
		if n >= len(agents) {
			return nil, errors.New("no route to host")
		}
		conn, kill := realFarEnd(func(Hello) (*Engine, error) { return engineOn(agents[n]), nil })
		if n == 0 {
			mu.Lock()
			cut = kill
			mu.Unlock()
		}
		return conn, nil
	}

	client, err := Roam("devbox", Hello{Workspace: "app"}, Roaming{Dial: dialer, Window: time.Second})
	if err != nil {
		t.Fatalf("roam: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	events, err := client.Agent().Submit(nil, "go")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	waitFor(t, "the turn to open on the engine", func() bool { return agents[0].stream(0) != nil })
	agents[0].stream(0) <- session.Event{Kind: session.EventTextDelta, Text: "half an "}
	if ev, ok := nextEvent(t, events); !ok || ev.Text != "half an " {
		t.Fatalf("wanted the first half of the answer, got %q", ev.Text)
	}

	mu.Lock()
	kill := cut
	mu.Unlock()
	kill()

	failed, ok := nextEvent(t, events)
	if !ok || failed.Kind != session.EventError || failed.Err == nil {
		t.Fatalf("the turn should have been told why it stopped, got %+v", failed)
	}
	if !strings.Contains(failed.Err.Error(), "does not keep a turn running while nothing is attached") {
		t.Fatalf("the turn was told %q", failed.Err.Error())
	}
	// And the conversation itself is reachable on the new engine, which is the
	// half of the sentence that says the work is not lost.
	if model := client.Agent().Model(); model != "a/b" {
		t.Fatalf("the reattached engine answers %q", model)
	}
}

// THE WHOLE STORY, WITH NOTHING SCRIPTED ON EITHER SIDE. A persistent
// conversation on the engine machine, a real surface roaming onto it, and a link
// cut in the middle of a turn: the engine keeps working while nobody is
// attached, the surface redials by itself, and what it draws is what it missed
// and only what it missed.
func TestARoamingSurfaceRejoinsALiveTurnOnAPersistentSession(t *testing.T) {
	agent := &fakeAgent{model: "a/b"}
	sess := NewSession(engineOn(agent), true)

	var mu sync.Mutex
	var surfaces []io.ReadWriteCloser
	dialer := func() (io.ReadWriteCloser, error) {
		surface, engine := Pipe()
		go func() {
			_ = ServeAttach(engine, engine, AttachOptions{
				Open: func(Hello) (*Session, error) { return sess, nil },
			})
			_ = engine.Close()
		}()
		mu.Lock()
		surfaces = append(surfaces, surface)
		mu.Unlock()
		return surface, nil
	}

	client, err := Roam("devbox", Hello{Workspace: "app"}, Roaming{Dial: dialer, Window: time.Second})
	if err != nil {
		t.Fatalf("roam: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if !client.Welcome().Persistent {
		t.Fatal("a session host keeps the conversation, and the welcome must say so")
	}

	events, err := client.Agent().Submit(nil, "go")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	waitFor(t, "the turn to open", func() bool { return agent.stream(0) != nil })
	turn := agent.stream(0)
	turn <- session.Event{Kind: session.EventTextDelta, Text: "before "}
	if ev, _ := nextEvent(t, events); ev.Text != "before " {
		t.Fatalf("wanted the first half, got %q", ev.Text)
	}

	// The link dies with nobody saying goodbye, and the turn keeps going on the
	// far machine — which is the whole of what the persistent shape promises.
	mu.Lock()
	first := surfaces[0]
	mu.Unlock()
	_ = first.Close()
	turn <- session.Event{Kind: session.EventTextDelta, Text: "during "}

	// Waiting for the ATTACH and not merely for the dial: the engine drops a
	// turn's events once it has ended, so a test that finished the turn while
	// the handshake was still in flight would be staging a different story than
	// the one it means to tell.
	waitFor(t, "the redial", func() bool {
		mu.Lock()
		dialled := len(surfaces) > 1
		mu.Unlock()
		return dialled && client.LinkNote() == ""
	})
	turn <- session.Event{Kind: session.EventTextDelta, Text: "after"}
	agent.finish(turn)

	var drawn []string
	for ev := range events {
		if ev.Kind == session.EventError {
			t.Fatalf("the turn was told it failed, and it did not: %v", ev.Err)
		}
		if ev.Kind == session.EventTextDelta {
			drawn = append(drawn, ev.Text)
		}
	}
	if strings.Join(drawn, "") != "during after" {
		t.Fatalf("after rejoining, the turn drew %q — the gap and only the gap", strings.Join(drawn, ""))
	}
	if notice := client.TakeNotice(); notice != "" {
		t.Fatalf("nothing was lost, so there is nothing to announce: %q", notice)
	}
}

// ── the bound, in words ─────────────────────────────────────────────────────

func TestTheWindowIsSpelledTheWayASentenceWantsIt(t *testing.T) {
	for _, row := range []struct {
		span time.Duration
		want string
	}{
		{RoamWindow, "5 minutes"},
		{time.Minute, "1 minute"},
		{90 * time.Second, "1 minute 30 seconds"},
		{2 * time.Second, "2 seconds"},
		{time.Millisecond, "a moment"},
	} {
		if got := roamSpan(row.span); got != row.want {
			t.Errorf("%s reads %q, wanted %q", row.span, got, row.want)
		}
	}
}

// TestTheFirstRetryAfterADropWaitsAJitteredSecond verifies V22 on a road that
// is dialled through a relay ([Roaming.SpreadReturns]): the first retry after a
// dropped link waits 1 s + U[0, 5 s) and each later doubled wait up to the 15 s
// cap is scaled by U[0.75, 1.25), while a drop followed by an immediate success
// still redials once. A road that is not dialled through a relay keeps its old
// schedule, which TestAnSshRoamingClientKeepsItsOldFirstRetryAndSchedule pins.
func TestTheFirstRetryAfterADropWaitsAJitteredSecond(t *testing.T) {
	wasRand := retryRand
	wasSleep := redialSleep
	t.Cleanup(func() {
		retryRand = wasRand
		redialSleep = wasSleep
	})

	recordSchedule := func(t *testing.T, failuresBeforeSuccess int) ([]time.Duration, int) {
		t.Helper()
		var mu sync.Mutex
		var waits []time.Duration
		redialSleep = func(stop <-chan struct{}, d time.Duration) bool {
			select {
			case <-stop:
				return false
			default:
				mu.Lock()
				waits = append(waits, d)
				mu.Unlock()
				return true
			}
		}

		dials := 0
		var firstSurface io.ReadWriteCloser
		dialer := func() (io.ReadWriteCloser, error) {
			mu.Lock()
			attempt := dials
			dials++
			mu.Unlock()
			if attempt > 0 && attempt <= failuresBeforeSuccess {
				return nil, errors.New("no route to host")
			}
			ours, theirs := net.Pipe()
			engine := &scripted{
				t:    t,
				conn: theirs,
				said: Welcome{Version: Version, SessionFile: "/j.jsonl", Persistent: true},
			}
			guard.Go("remote/test-redial-jitter", func() { engine.serve(nil) })
			if attempt == 0 {
				firstSurface = ours
			}
			return ours, nil
		}

		client, err := Roam("devbox", Hello{Workspace: "app"}, Roaming{Dial: dialer, Window: RoamWindow, SpreadReturns: true})
		if err != nil {
			t.Fatalf("roam: %v", err)
		}
		defer func() { _ = client.Close() }()

		_ = firstSurface.Close()
		wantDials := 2 + failuresBeforeSuccess
		waitFor(t, "the redial schedule to finish", func() bool {
			mu.Lock()
			got := dials
			mu.Unlock()
			return got == wantDials && client.LinkNote() == ""
		})
		if err := client.Err(); err != nil {
			t.Fatalf("client failed after redial: %v", err)
		}
		mu.Lock()
		defer mu.Unlock()
		return append([]time.Duration(nil), waits...), dials
	}

	// Low extreme: U = 0 gives exactly 1 s on the first retry, then 0.75 of
	// each doubled wait (2 s, 4 s, 8 s, and 15 s at the cap).
	retryRand = func() float64 { return 0 }
	lowWaits, _ := recordSchedule(t, 5)
	wantLow := []time.Duration{
		time.Second,
		1500 * time.Millisecond,
		3 * time.Second,
		6 * time.Second,
		11250 * time.Millisecond,
		11250 * time.Millisecond,
	}
	if len(lowWaits) != len(wantLow) {
		t.Fatalf("low-extreme schedule length = %d, want %d (%v)", len(lowWaits), len(wantLow), lowWaits)
	}
	for i, want := range wantLow {
		if lowWaits[i] != want {
			t.Fatalf("low-extreme wait %d = %v, want %v", i+1, lowWaits[i], want)
		}
	}

	// High extreme: U just under 1 gives just under 6 s on the first retry,
	// then just under 1.25 of each doubled wait.
	retryRand = func() float64 { return 0.9999999 }
	highWaits, _ := recordSchedule(t, 5)
	highCeilings := []time.Duration{
		6 * time.Second,
		2500 * time.Millisecond,
		5 * time.Second,
		10 * time.Second,
		18750 * time.Millisecond,
		18750 * time.Millisecond,
	}
	if len(highWaits) != len(highCeilings) {
		t.Fatalf("high-extreme schedule length = %d, want %d (%v)", len(highWaits), len(highCeilings), highWaits)
	}
	for i, ceiling := range highCeilings {
		if highWaits[i] >= ceiling || highWaits[i] < ceiling-time.Millisecond {
			t.Fatalf("high-extreme wait %d = %v, want just under %v", i+1, highWaits[i], ceiling)
		}
	}

	// Default source: ten samples all fall in [1 s, 6 s) on the first retry and
	// within [0.75, 1.25) of each doubled wait thereafter, and are not all equal.
	retryRand = nil
	bands := []struct {
		low, high time.Duration
	}{
		{time.Second, 6 * time.Second},
		{1500 * time.Millisecond, 2500 * time.Millisecond},
		{3 * time.Second, 5 * time.Second},
		{6 * time.Second, 10 * time.Second},
		{11250 * time.Millisecond, 18750 * time.Millisecond},
		{11250 * time.Millisecond, 18750 * time.Millisecond},
	}
	firstSeen := map[time.Duration]bool{}
	secondSeen := map[time.Duration]bool{}
	for sample := 0; sample < 10; sample++ {
		waits, _ := recordSchedule(t, 5)
		if len(waits) != len(bands) {
			t.Fatalf("sample %d recorded %d waits, want %d", sample, len(waits), len(bands))
		}
		for i, band := range bands {
			if waits[i] < band.low || waits[i] >= band.high {
				t.Fatalf("sample %d wait %d = %v, outside [%v, %v)", sample, i+1, waits[i], band.low, band.high)
			}
		}
		firstSeen[waits[0]] = true
		secondSeen[waits[1]] = true
	}
	if len(firstSeen) < 2 {
		t.Fatalf("ten default-source first waits were all identical: %v", firstSeen)
	}
	if len(secondSeen) < 2 {
		t.Fatalf("ten default-source second waits were all identical: %v", secondSeen)
	}

	// A drop followed by an immediate success still redials once.
	waits, dials := recordSchedule(t, 0)
	if dials != 2 || len(waits) != 1 {
		t.Fatalf("immediate recovery made %d dials with %d waits (%v), want 2 dials and 1 wait", dials, len(waits), waits)
	}
	if waits[0] < time.Second || waits[0] >= 6*time.Second {
		t.Fatalf("immediate recovery wait = %v, outside [1s, 6s)", waits[0])
	}
}

// ── planned redials and the roads that spread their returns (phase 2, lane C) ─

// endedOnPurpose is the error livedoor's channel returns when the viewer closed
// it for a road swap. It lives here because this package cannot import livedoor
// and the contract between them is only the PlannedRedial method.
type endedOnPurpose struct{}

func (endedOnPurpose) Error() string       { return "road-change" }
func (endedOnPurpose) PlannedRedial() bool { return true }

// swappable is a pipe that can be ended two ways: dropped, as a network does,
// or closed on purpose by the viewer for a road swap.
type swappable struct {
	io.ReadWriteCloser
	planned atomic.Bool
	// said is what the far end ended the link with, when it ended it by saying
	// something: the error [swappable.endWith] stored.
	said atomic.Pointer[error]
}

func (s *swappable) Read(p []byte) (int, error) {
	n, err := s.ReadWriteCloser.Read(p)
	if err != nil {
		if said := s.said.Load(); said != nil {
			return n, *said
		}
		if s.planned.Load() {
			return n, endedOnPurpose{}
		}
	}
	return n, err
}

// endWith closes the link the way a far end that ANSWERED does: the reader gets
// the error the far end's own words are carried by, not a bare end of file.
func (s *swappable) endWith(err error) {
	s.said.Store(&err)
	_ = s.ReadWriteCloser.Close()
}

func (s *swappable) endOnPurpose() {
	s.planned.Store(true)
	_ = s.ReadWriteCloser.Close()
}

// timeline records, in order, every pause the redial loop asked for and every
// dial it made, so a test reads the schedule instead of sleeping through it.
type timeline struct {
	mu     sync.Mutex
	events []string
	pauses []time.Duration
}

func (tl *timeline) add(kind string, d time.Duration) {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	tl.events = append(tl.events, kind)
	if kind == "pause" {
		tl.pauses = append(tl.pauses, d)
	}
}

func (tl *timeline) snapshot() ([]string, []time.Duration) {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	return append([]string(nil), tl.events...), append([]time.Duration(nil), tl.pauses...)
}

// redialRig is a roaming client whose first link can be ended on purpose or
// dropped, whose next `failures` dials fail, and whose pauses are recorded and
// not slept.
func redialRig(t *testing.T, roaming Roaming, failures int) (*Client, *swappable, *timeline) {
	t.Helper()
	was := redialSleep
	t.Cleanup(func() { redialSleep = was })
	tl := &timeline{}
	redialSleep = func(stop <-chan struct{}, d time.Duration) bool {
		select {
		case <-stop:
			return false
		default:
		}
		tl.add("pause", d)
		return true
	}
	var mu sync.Mutex
	dials := 0
	first := &swappable{}
	roaming.Dial = func() (io.ReadWriteCloser, error) {
		mu.Lock()
		attempt := dials
		dials++
		mu.Unlock()
		if attempt > 0 {
			tl.add("dial", 0)
		}
		if attempt > 0 && attempt <= failures {
			return nil, errors.New("no route to host")
		}
		ours, theirs := net.Pipe()
		engine := &scripted{t: t, conn: theirs, said: Welcome{Version: Version, SessionFile: "/j.jsonl", Persistent: true}}
		guard.Go("remote/test-planned-redial", func() { engine.serve(nil) })
		if attempt == 0 {
			first.ReadWriteCloser = ours
			return first, nil
		}
		return ours, nil
	}
	roaming.Window = RoamWindow
	client, err := Roam("devbox", Hello{Workspace: "app"}, roaming)
	if err != nil {
		t.Fatalf("roam: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	waitFor(t, "the first link", func() bool { return first.ReadWriteCloser != nil })
	return client, first, tl
}

func settled(t *testing.T, client *Client, tl *timeline, dials int) {
	t.Helper()
	waitFor(t, "the redial to finish", func() bool {
		events, _ := tl.snapshot()
		n := 0
		for _, e := range events {
			if e == "dial" {
				n++
			}
		}
		return n == dials && client.LinkNote() == "" && !client.isReconnecting()
	})
	if err := client.Err(); err != nil {
		t.Fatalf("the client failed after the redial: %v", err)
	}
}

func (c *Client) isReconnecting() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reconnecting
}

// TestAPlannedRedialSkipsTheFirstJitterAndLaterOnesKeepIt verifies V31 (and G1 of
// SPEC-P2): a link closed on purpose for a road swap is redialled at once, with no
// first pause on any road, and the attempts after it keep the schedule the road
// has: spread on a relay-dialled road, the plain doubling on one that is not.
func TestAPlannedRedialSkipsTheFirstJitterAndLaterOnesKeepIt(t *testing.T) {
	wasRand := retryRand
	t.Cleanup(func() { retryRand = wasRand })
	retryRand = func() float64 { return 0 }

	t.Run("relay road, planned", func(t *testing.T) {
		client, first, tl := redialRig(t, Roaming{SpreadReturns: true}, 2)
		first.endOnPurpose()
		settled(t, client, tl, 3)
		events, pauses := tl.snapshot()
		if len(events) == 0 || events[0] != "dial" {
			t.Fatalf("a planned redial paused before its first attempt: %v", events)
		}
		// U = 0 scales each later wait by exactly 0.75: 2 s and 4 s doubled from the base.
		want := []time.Duration{1500 * time.Millisecond, 3 * time.Second}
		if len(pauses) != len(want) || pauses[0] != want[0] || pauses[1] != want[1] {
			t.Fatalf("later attempts keep the spread schedule: pauses %v, want %v", pauses, want)
		}
	})
	t.Run("plain road, planned", func(t *testing.T) {
		client, first, tl := redialRig(t, Roaming{}, 2)
		first.endOnPurpose()
		settled(t, client, tl, 3)
		events, pauses := tl.snapshot()
		if len(events) == 0 || events[0] != "dial" {
			t.Fatalf("a planned redial paused before its first attempt: %v", events)
		}
		want := []time.Duration{2 * time.Second, 4 * time.Second}
		if len(pauses) != len(want) || pauses[0] != want[0] || pauses[1] != want[1] {
			t.Fatalf("later attempts keep the plain schedule: pauses %v, want %v", pauses, want)
		}
	})
	t.Run("a dropped link is not planned", func(t *testing.T) {
		client, first, tl := redialRig(t, Roaming{SpreadReturns: true}, 0)
		_ = first.ReadWriteCloser.Close()
		settled(t, client, tl, 1)
		events, pauses := tl.snapshot()
		if len(events) < 2 || events[0] != "pause" || len(pauses) != 1 || pauses[0] != time.Second {
			t.Fatalf("an unplanned drop waits before its first attempt: events %v pauses %v", events, pauses)
		}
	})
}

// TestAnSshRoamingClientKeepsItsOldFirstRetryAndSchedule pins the default (fixes
// keep defaults): a road that does not set SpreadReturns, which is every ssh
// `--host` client, local socket and engine pipe, keeps the first pause and the
// doubling it had before the relay's returns were spread, whatever the random
// source says.
func TestAnSshRoamingClientKeepsItsOldFirstRetryAndSchedule(t *testing.T) {
	wasRand := retryRand
	t.Cleanup(func() { retryRand = wasRand })
	retryRand = func() float64 { return 0.5 } // would be 3.5 s and 1.0 x if it were used at all

	client, first, tl := redialRig(t, Roaming{}, 5)
	_ = first.ReadWriteCloser.Close()
	settled(t, client, tl, 6)
	_, pauses := tl.snapshot()
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second, 15 * time.Second}
	if len(pauses) != len(want) {
		t.Fatalf("pauses %v, want %v", pauses, want)
	}
	for i := range want {
		if pauses[i] != want[i] {
			t.Fatalf("pause %d = %v, want %v (all: %v)", i+1, pauses[i], want[i], pauses)
		}
	}
}

// heldRedial is a roaming client whose first link has just been closed on
// purpose and whose next dial blocks until the returned release is called.
func heldRedial(t *testing.T, noteAfter time.Duration) (*Client, func()) {
	t.Helper()
	was := plannedNoteAfter
	plannedNoteAfter = noteAfter
	t.Cleanup(func() { plannedNoteAfter = was })

	hold := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(hold) }) }
	t.Cleanup(release)

	var mu sync.Mutex
	dials := 0
	first := &swappable{}
	dial := func() (io.ReadWriteCloser, error) {
		mu.Lock()
		attempt := dials
		dials++
		mu.Unlock()
		if attempt > 0 {
			<-hold
		}
		ours, theirs := net.Pipe()
		engine := &scripted{t: t, conn: theirs, said: Welcome{Version: Version, SessionFile: "/j.jsonl", Persistent: true}}
		guard.Go("remote/test-planned-note", func() { engine.serve(nil) })
		if attempt == 0 {
			first.ReadWriteCloser = ours
			return first, nil
		}
		return ours, nil
	}
	client, err := Roam("devbox", Hello{Workspace: "app"}, Roaming{Dial: dial, Window: time.Minute})
	if err != nil {
		t.Fatalf("roam: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	first.endOnPurpose()
	waitFor(t, "the redial to be under way", func() bool { return client.isReconnecting() })
	return client, release
}

// TestAPlannedRedialSaysNothingUntilItTakesLongerThanAMoment verifies the
// delayed `reconnecting` note of G1: a swap that has not yet taken a moment
// shows the person nothing, though a call made in the gap is still refused
// (nothing may be written between the new pipe and its welcome), and one that
// takes longer says it in the words an unplanned drop uses.
func TestAPlannedRedialSaysNothingUntilItTakesLongerThanAMoment(t *testing.T) {
	t.Run("within a moment", func(t *testing.T) {
		client, release := heldRedial(t, time.Minute)
		if note := client.LinkNote(); note != "" {
			t.Fatalf("a swap said %q before it had taken a moment", note)
		}
		if _, err := client.HeldQuestions(); err == nil {
			t.Fatalf("a call made in the gap was not refused")
		}
		release()
		waitFor(t, "the link to come back", func() bool { return !client.isReconnecting() })
		if note := client.LinkNote(); note != "" {
			t.Fatalf("a link that came back says %q", note)
		}
	})
	t.Run("longer than a moment", func(t *testing.T) {
		client, release := heldRedial(t, 20*time.Millisecond)
		waitFor(t, "the note once the swap outlasts a moment", func() bool { return client.LinkNote() != "" })
		if note := client.LinkNote(); !strings.HasPrefix(note, "reconnecting to devbox") {
			t.Fatalf("the note reads %q", note)
		}
		release()
		waitFor(t, "the link to come back", func() bool { return !client.isReconnecting() && client.LinkNote() == "" })
	})
}

// farEndSaid is the error livedoor's channel returns when the host ended a view
// on purpose and said why (`remote-control-off`). It lives here for the reason
// [endedOnPurpose] does: the contract between the two packages is only the
// EndsView and Reason methods.
type farEndSaid struct{ reason string }

func (e farEndSaid) Error() string  { return "livedoor: channel reset: " + e.reason }
func (e farEndSaid) EndsView() bool { return true }
func (e farEndSaid) Reason() string { return e.reason }

// TestAViewTheFarEndEndedOnPurposeIsNotRedialled is the viewer half of the
// contract for turning remote control off on the computer being watched: a link
// that ended because the far end said so ends the client at once, with the far
// end's one sentence, and neither pauses nor dials nor says `reconnecting`. A
// link that merely DROPPED still redials (the tests above), which is the default
// this change keeps.
func TestAViewTheFarEndEndedOnPurposeIsNotRedialled(t *testing.T) {
	client, first, tl := redialRig(t, Roaming{SpreadReturns: true}, 0)
	first.endWith(farEndSaid{reason: "remote-control-off"})

	waitFor(t, "the client to end", func() bool { return client.Err() != nil })
	if got, want := client.Err().Error(), "devbox turned remote control off."; got != want {
		t.Fatalf("the ended view says %q, want exactly %q", got, want)
	}
	if note := client.LinkNote(); note != "" {
		t.Fatalf("nothing is reconnecting, so the note is empty, not %q", note)
	}
	time.Sleep(100 * time.Millisecond)
	if events, _ := tl.snapshot(); len(events) != 0 {
		t.Fatalf("the client paused or dialled after the far end ended the view: %v", events)
	}
}

// A far end that ended the view with a reason that is NOT the final one is still
// redialled, and a reason the viewer has no sentence for does not end it either:
// the set that ends a view for good is closed.
func TestAViewEndedWithAnyOtherReasonIsStillRedialled(t *testing.T) {
	for _, reason := range []string{"permit-changed", "lagging", "host-closing", "a-reason-from-a-newer-build"} {
		t.Run(reason, func(t *testing.T) {
			client, first, tl := redialRig(t, Roaming{SpreadReturns: true}, 0)
			first.endWith(notFinal{reason: reason})
			settled(t, client, tl, 1)
			if events, _ := tl.snapshot(); len(events) == 0 {
				t.Fatalf("a %q end was not redialled", reason)
			}
		})
	}
}

// notFinal is a reason-bearing channel error that does not end the view for good.
type notFinal struct{ reason string }

func (e notFinal) Error() string  { return "livedoor: channel reset: " + e.reason }
func (e notFinal) EndsView() bool { return false }
func (e notFinal) Reason() string { return e.reason }
