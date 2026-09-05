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
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
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
		// The other honest shape: `aforge engine` on a pipe, which says nothing
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
	if _, err := client.StandingItems("/srv/app"); err == nil || !strings.Contains(err.Error(), "reconnecting to devbox — try that again in a moment") {
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

// THE HONEST CASE, END TO END. `aforge engine` on a pipe is a legitimate far end
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
