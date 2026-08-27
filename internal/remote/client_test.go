package remote

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// ── A SCRIPTED ENGINE ───────────────────────────────────────────────────────
//
// Every test in this file drives the client against an engine written here, over
// a net.Pipe. THERE IS NO SSH ANYWHERE, and there must not be: a unit test that
// needed a second machine — or a second machine pretending to be this one — would
// be a test nobody runs. The pipe is the only thing the client has ever known
// about its far end (Dial takes an io.ReadWriteCloser and nothing else), so a
// test that satisfies this one satisfies the real thing.

// engine is a scripted far end: a table of answers, and the frames it sends when
// a call opens a stream.
type engine struct {
	t *testing.T
	// conn is the engine's side of the pipe.
	conn net.Conn
	// welcome is what it answers the hello with.
	welcome Welcome
	// answers maps a method to the value it returns, or to an error.
	answers map[string]any
	fails   map[string]string
	// seen records every call, in order, so a test can say what travelled.
	mu   sync.Mutex
	seen []Frame
	// streams is the id the next stream-opening call gets.
	streams uint64
	// after is run once a stream has been opened, with that stream's id — the
	// hook a test writes its events in.
	after func(e *engine, stream uint64)
	// silent is the set of methods that get no answer at all, so a test can hang
	// a call on purpose and then cut the connection under it.
	silent map[string]bool
}

func newEngine(t *testing.T) (*Client, *engine) {
	return newEngineStating(t, nil)
}

// newEngineStating is [newEngine] with the fact set the engine states at the
// door shaped by the caller. It is a second door rather than a field because
// the welcome is written BEFORE the handshake and read during it: a test that
// set the facts on the engine it was handed back would be setting them after
// the surface had already been told.
func newEngineStating(t *testing.T, shape func(*Welcome)) (*Client, *engine) {
	t.Helper()
	ours, theirs := net.Pipe()
	welcome := Welcome{
		Version: Version, Workspace: "/srv/app", SessionFile: "/srv/j.jsonl", Model: "a/b",
		Facts: &FactsPush{Rev: 1, Facts: session.Facts{Model: "a/b"}},
	}
	if shape != nil {
		shape(&welcome)
	}
	e := &engine{
		t:       t,
		conn:    theirs,
		welcome: welcome,
		answers: map[string]any{},
		fails:   map[string]string{},
		silent:  map[string]bool{},
	}
	ready := make(chan struct{})
	go e.serve(ready)

	type dialed struct {
		client *Client
		err    error
	}
	done := make(chan dialed, 1)
	go func() {
		client, err := Dial(ours, "devbox", Hello{Workspace: "app"})
		done <- dialed{client: client, err: err}
	}()
	<-ready
	answer := <-done
	if answer.err != nil {
		t.Fatalf("dial: %v", answer.err)
	}
	t.Cleanup(func() { _ = answer.client.Close() })
	return answer.client, e
}

// serve reads frames and answers them until the pipe ends.
func (e *engine) serve(ready chan struct{}) {
	lines := bufio.NewScanner(e.conn)
	lines.Buffer(make([]byte, 0, 1<<20), 1<<24)
	first := true
	for lines.Scan() {
		var frame Frame
		if err := json.Unmarshal(lines.Bytes(), &frame); err != nil {
			return
		}
		e.mu.Lock()
		e.seen = append(e.seen, frame)
		e.mu.Unlock()
		switch frame.Kind {
		case "hello":
			e.send(Frame{Kind: "welcome", Payload: mustClientJSON(e.welcome)})
			if first && ready != nil {
				first = false
				close(ready)
			}
		case "call":
			e.answer(frame)
		}
	}
}

func (e *engine) answer(frame Frame) {
	if e.silent[frame.Method] {
		return
	}
	if reason, ok := e.fails[frame.Method]; ok {
		e.send(Frame{Kind: "result", ID: frame.ID, Error: reason})
		return
	}
	switch frame.Method {
	case MethodSubmit, MethodSubmitImage, MethodFollowUp, MethodSteer:
		e.streams++
		stream := e.streams
		e.send(Frame{Kind: "result", ID: frame.ID, Payload: mustClientJSON(StreamRef{Stream: stream})})
		if e.after != nil {
			e.after(e, stream)
		}
	default:
		value, ok := e.answers[frame.Method]
		if !ok {
			e.send(Frame{Kind: "result", ID: frame.ID})
			return
		}
		e.send(Frame{Kind: "result", ID: frame.ID, Payload: mustClientJSON(value)})
	}
}

func (e *engine) send(frame Frame) {
	line, err := json.Marshal(frame)
	if err != nil {
		e.t.Errorf("marshal: %v", err)
		return
	}
	if _, err := e.conn.Write(append(line, '\n')); err != nil {
		return
	}
}

func (e *engine) event(stream uint64, ev session.Event) {
	e.send(Frame{Kind: "event", ID: stream, Payload: mustClientJSON(WireEvent(ev))})
}

func (e *engine) closeStream(stream uint64) { e.send(Frame{Kind: "closed", ID: stream}) }

// state pushes one fact set, unasked — the engine half of "intent up, facts
// down" (wire.go's version 4). It belongs to no stream and answers no call.
func (e *engine) state(rev uint64, facts session.Facts) {
	e.send(Frame{Kind: "facts", Payload: mustClientJSON(FactsPush{Rev: rev, Facts: facts})})
}

// calls is every call the engine saw, by method.
func (e *engine) calls(method string) []Frame {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []Frame
	for _, frame := range e.seen {
		if frame.Kind == "call" && frame.Method == method {
			out = append(out, frame)
		}
	}
	return out
}

func mustClientJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}

// ── the handshake ───────────────────────────────────────────────────────────

func TestDialCarriesTheWorkspaceAndKeepsTheWelcome(t *testing.T) {
	client, e := newEngine(t)

	hellos := 0
	e.mu.Lock()
	for _, frame := range e.seen {
		if frame.Kind == "hello" {
			hellos++
			var hello Hello
			if err := json.Unmarshal(frame.Payload, &hello); err != nil {
				t.Fatalf("hello payload: %v", err)
			}
			if hello.Version != Version {
				t.Fatalf("hello version = %d, want %d", hello.Version, Version)
			}
			if hello.Workspace != "app" {
				t.Fatalf("hello workspace = %q, want the path as typed", hello.Workspace)
			}
			if !supportsEncoding(hello.Encodings, frameEncodingGzip) {
				t.Fatalf("hello encodings = %q, want gzip offered additively", hello.Encodings)
			}
		}
	}
	e.mu.Unlock()
	if hellos != 1 {
		t.Fatalf("the client sent %d hellos, want exactly one", hellos)
	}

	welcome := client.Welcome()
	if welcome.Workspace != "/srv/app" || welcome.SessionFile != "/srv/j.jsonl" {
		t.Fatalf("welcome = %+v, want the engine's own answer", welcome)
	}
	if client.Host() != "devbox" {
		t.Fatalf("host = %q", client.Host())
	}
}

func TestDialRefusesAnotherProtocolVersionAtTheDoor(t *testing.T) {
	ours, theirs := net.Pipe()
	go func() {
		lines := bufio.NewScanner(theirs)
		for lines.Scan() {
			line, _ := json.Marshal(Frame{Kind: "welcome", Payload: mustClientJSON(Welcome{Version: Version + 1})})
			_, _ = theirs.Write(append(line, '\n'))
			return
		}
	}()
	_, err := Dial(ours, "devbox", Hello{})
	if err == nil {
		t.Fatal("a version mismatch opened a session")
	}
	if !strings.Contains(err.Error(), "devbox") || !strings.Contains(err.Error(), "version") {
		t.Fatalf("the refusal does not name the machine and the reason: %v", err)
	}
}

// ── the agent's own methods ─────────────────────────────────────────────────

// THE FACT SET IS THE WELCOME'S, AND ASKING FOR IT COSTS NOTHING. The five
// getters a frame draws read the replica (replica.go), so the engine below is
// given no answer for any of their methods and the readings are still right.
func TestTheFactsAFrameReadsCostNoRoundTrip(t *testing.T) {
	client, _ := newEngineStating(t, func(w *Welcome) {
		w.Facts = &FactsPush{Rev: 1, Facts: session.Facts{
			Model:         "openai/gpt-5",
			Title:         "the roof leaks",
			Spent:         session.Usage{Turns: 3, CostUSD: 0.25},
			ContextTokens: 4212,
			Reasoning:     map[string]string{"openai/gpt-5": "high"},
		}}
	})

	agent := client.Agent()
	before := client.CallsMade()
	if got := agent.Model(); got != "openai/gpt-5" {
		t.Fatalf("Model = %q", got)
	}
	if got := agent.Title(); got != "the roof leaks" {
		t.Fatalf("Title = %q", got)
	}
	if got := agent.ContextTokens(); got != 4212 {
		t.Fatalf("ContextTokens = %d", got)
	}
	if got := agent.Usage(); got.Turns != 3 || got.CostUSD != 0.25 {
		t.Fatalf("Usage = %+v", got)
	}
	// AND THE LEVEL IS FOUND UNDER ANY SPELLING OF THE ID, because the map is
	// keyed the way internal/session keys it ([session.ReasoningKey]).
	if got := agent.ReasoningFor("OpenAI/GPT-5"); got != "high" {
		t.Fatalf("ReasoningFor = %q", got)
	}
	if spent := client.CallsMade() - before; spent != 0 {
		t.Fatalf("the five facts a frame draws cost %d round trips, want 0", spent)
	}
}

func TestEveryGetterIsOneRoundTrip(t *testing.T) {
	client, e := newEngine(t)
	e.answers[MethodTranscript] = []session.DisplayEntry{{Role: "user", Text: "hello"}}
	e.answers[MethodRewindPoints] = []session.RewindPoint{{Index: 2, Turn: true, Said: "hello"}}
	e.answers[MethodRewindAt] = []session.DisplayEntry{{Role: "user", Text: "hello"}}

	agent := client.Agent()
	if got := agent.Transcript(); len(got) != 1 || got[0].Text != "hello" {
		t.Fatalf("Transcript = %+v", got)
	}
	if got := agent.RewindPoints(); len(got) != 1 || !got[0].Turn {
		t.Fatalf("RewindPoints = %+v", got)
	}
	entries, err := agent.RewindAt(0)
	if err != nil || len(entries) != 1 {
		t.Fatalf("RewindAt = %+v, %v", entries, err)
	}

	// The setters and the answers, which return nothing and must still travel.
	agent.SetModel("anthropic/claude")
	agent.SetContextWindow(200000)
	agent.SetReasoningFor("anthropic/claude", "low")
	agent.Interrupt()
	agent.ResolveConsent(7, true)
	agent.ResolveConsentRemember(8, true, session.ConsentToolSession)
	agent.ResolveHarness(9, true, "a/b")
	agent.ResolveConnect("google", false)
	agent.ResolveConnectKey("notion", "secret")
	agent.NoteConnected("google", "me@example.com")
	if err := agent.Compact(context.Background()); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	for _, method := range []string{
		MethodSetModel, MethodSetReasoningFor, MethodInterrupt,
		MethodConsent, MethodConsentRemember, MethodHarness, MethodConnect,
		MethodConnectKey, MethodNoteConnected, MethodCompact, MethodClose,
	} {
		if len(e.calls(method)) != 1 {
			t.Fatalf("%s did not travel exactly once", method)
		}
	}
	if len(e.calls(MethodSetContext)) != 0 {
		t.Fatal("the laptop's context window crossed to the engine")
	}
	// The arguments are the method's own struct, not a guess.
	var consent ConsentArgs
	if err := json.Unmarshal(e.calls(MethodConsentRemember)[0].Payload, &consent); err != nil {
		t.Fatalf("consent payload: %v", err)
	}
	if consent.ID != 8 || !consent.Allow || consent.Scope != session.ConsentToolSession {
		t.Fatalf("consent args = %+v", consent)
	}
}

func TestCloseTheAgentLeavesTheConnectionStanding(t *testing.T) {
	client, e := newEngine(t)
	agent := client.Agent()
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// /new is exactly this: close the agent, then ask for another session on the
	// same connection. A Close that hung up would make the second one impossible.
	e.welcome = Welcome{Version: Version, Workspace: "/srv/app", SessionFile: "/srv/next.jsonl"}
	e.answers[MethodSessionNew] = e.welcome
	next, err := client.NewSession()
	if err != nil {
		t.Fatalf("NewSession after Close: %v", err)
	}
	if next.SessionFile != "/srv/next.jsonl" {
		t.Fatalf("NewSession = %+v", next)
	}
	if client.Welcome().SessionFile != "/srv/next.jsonl" {
		t.Fatal("the client kept the old welcome after a swap")
	}
}

// ── the streams ─────────────────────────────────────────────────────────────

func TestSubmitStreamsEventsInOrderAndClosesOnClosed(t *testing.T) {
	client, e := newEngine(t)
	e.after = func(e *engine, stream uint64) {
		go func() {
			e.event(stream, session.Event{Kind: session.EventTextDelta, Text: "one "})
			e.event(stream, session.Event{Kind: session.EventTextDelta, Text: "two "})
			e.event(stream, session.Event{Kind: session.EventToolBegin, Tool: "read"})
			e.event(stream, session.Event{Kind: session.EventError, Err: errors.New("the tool fell over")})
			e.closeStream(stream)
		}()
	}
	events, err := client.Agent().Submit(context.Background(), "go on then")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	var got []session.Event
	for ev := range events {
		got = append(got, ev)
	}
	if len(got) != 4 {
		t.Fatalf("got %d events, want 4: %+v", len(got), got)
	}
	if got[0].Text != "one " || got[1].Text != "two " || got[2].Tool != "read" {
		t.Fatalf("the events arrived out of order: %+v", got)
	}
	// THE ERROR SURVIVED THE ENCODING, which is the one thing wire.go's EventWire
	// exists for: `error` is an interface and marshals to nothing.
	if got[3].Err == nil || got[3].Err.Error() != "the tool fell over" {
		t.Fatalf("the error did not survive the wire: %+v", got[3])
	}
	var args SubmitArgs
	if err := json.Unmarshal(e.calls(MethodSubmit)[0].Payload, &args); err != nil {
		t.Fatalf("submit payload: %v", err)
	}
	if args.Text != "go on then" {
		t.Fatalf("submit args = %+v", args)
	}
}

func TestSteerOpensTheRunningTurnsTail(t *testing.T) {
	client, e := newEngine(t)
	e.after = func(e *engine, stream uint64) {
		e.event(stream, session.Event{Kind: session.EventSteerAccepted, Steer: &session.SteerNote{ID: 7, Words: "use staging"}})
		e.closeStream(stream)
	}
	events, err := client.Agent().Steer("use staging")
	if err != nil {
		t.Fatalf("Steer: %v", err)
	}
	ev := <-events
	if ev.Kind != session.EventSteerAccepted || ev.Steer == nil || ev.Steer.Words != "use staging" {
		t.Fatalf("steer event = %+v", ev)
	}
	var args SubmitArgs
	if err := json.Unmarshal(e.calls(MethodSteer)[0].Payload, &args); err != nil {
		t.Fatalf("steer payload: %v", err)
	}
	if args.Text != "use staging" {
		t.Fatalf("steer args = %+v", args)
	}
}

func TestEventsThatArriveBeforeTheResultAreNotLost(t *testing.T) {
	// The engine is allowed to write the events immediately behind the result,
	// and on a fast pipe the reader can see them before the caller of Submit has
	// come back to claim the stream. Get-or-create is what makes that safe.
	client, e := newEngine(t)
	e.after = func(e *engine, stream uint64) {
		e.event(stream, session.Event{Kind: session.EventTextDelta, Text: "immediately"})
		e.closeStream(stream)
	}
	events, err := client.Agent().FollowUp("after you")
	if err != nil {
		t.Fatalf("FollowUp: %v", err)
	}
	var texts []string
	for ev := range events {
		texts = append(texts, ev.Text)
	}
	if len(texts) != 1 || texts[0] != "immediately" {
		t.Fatalf("events = %v", texts)
	}
}

func TestAGetterDuringALiveStreamDoesNotDeadlock(t *testing.T) {
	// The surface reads events on the update loop and asks getters on the same
	// loop. If the reader goroutine ever blocked handing an event over, a getter
	// asked between two events would wait for a reader that was waiting for it.
	client, e := newEngine(t)
	e.after = func(e *engine, stream uint64) {
		go func() {
			for i := 0; i < 200; i++ {
				e.event(stream, session.Event{Kind: session.EventTextDelta, Text: "."})
			}
			e.closeStream(stream)
		}()
	}
	agent := client.Agent()
	events, err := agent.Submit(context.Background(), "flood")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	// Ask a getter WITHOUT draining first, which is the deadlock shape. The
	// facts a frame draws no longer take the wire at all, so the shape is gone
	// rather than survived — but the readings must still be right mid-flood, and
	// a fetch that DOES ask (the transcript) must still get through.
	if got := agent.Model(); got != "a/b" {
		t.Fatalf("Model during a live stream = %q", got)
	}
	if got := agent.Transcript(); got != nil {
		t.Fatalf("Transcript during a live stream = %+v", got)
	}
	count := 0
	for range events {
		count++
	}
	if count != 200 {
		t.Fatalf("got %d events, want 200", count)
	}
}

// ── a connection that dies ──────────────────────────────────────────────────

func TestAClosedPipeFailsWhatWasWaitingAndSaysSo(t *testing.T) {
	client, e := newEngine(t)
	e.silent[MethodUsage] = true

	waited := make(chan error, 1)
	go func() {
		_, err := client.call(nil, MethodUsage, nil)
		waited <- err
	}()
	// Give the call time to be written and registered, then cut the pipe.
	time.Sleep(20 * time.Millisecond)
	_ = e.conn.Close()

	select {
	case err := <-waited:
		if err == nil {
			t.Fatal("a call outlived the connection")
		}
		assertGoneSentence(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("a call on a dead pipe hung instead of failing")
	}

	// And everything afterwards says the same thing, in the same words.
	if _, err := client.Agent().Submit(context.Background(), "still there?"); err == nil {
		t.Fatal("Submit succeeded on a dead connection")
	} else {
		assertGoneSentence(t, err)
	}
}

func TestALiveStreamIsToldWhyItStopped(t *testing.T) {
	client, e := newEngine(t)
	e.after = func(e *engine, stream uint64) {
		go func() {
			e.event(stream, session.Event{Kind: session.EventTextDelta, Text: "half a sen"})
			time.Sleep(10 * time.Millisecond)
			_ = e.conn.Close()
		}()
	}
	events, err := client.Agent().Submit(context.Background(), "go")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	var got []session.Event
	for ev := range events {
		got = append(got, ev)
	}
	if len(got) < 2 {
		t.Fatalf("the stream closed without saying why: %+v", got)
	}
	last := got[len(got)-1]
	if last.Kind != session.EventError || last.Err == nil {
		t.Fatalf("the last event is not an error: %+v", last)
	}
	assertGoneSentence(t, last.Err)
}

func TestAFatalFrameCarriesTheEnginesOwnReason(t *testing.T) {
	client, e := newEngine(t)
	e.silent[MethodTitle] = true
	waited := make(chan error, 1)
	go func() {
		_, err := client.call(nil, MethodTitle, nil)
		waited <- err
	}()
	time.Sleep(20 * time.Millisecond)
	e.send(Frame{Kind: "fatal", Error: "the workspace disappeared"})

	select {
	case err := <-waited:
		assertGoneSentence(t, err)
		if !strings.Contains(err.Error(), "the workspace disappeared") {
			t.Fatalf("the engine's own reason was dropped: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a fatal frame did not reach the waiting call")
	}
}

// assertGoneSentence is the honesty check: one sentence, the machine named, and
// the one thing a person can do about it.
func assertGoneSentence(t *testing.T, err error) {
	t.Helper()
	said := err.Error()
	if !strings.Contains(said, "devbox") {
		t.Fatalf("the sentence does not name the machine: %q", said)
	}
	if !strings.Contains(said, "connection") || !strings.Contains(said, "gone") {
		t.Fatalf("the sentence does not say what happened: %q", said)
	}
	if !strings.Contains(said, "run the same command") {
		t.Fatalf("the sentence does not say what to do: %q", said)
	}
}

// ── the session doors ───────────────────────────────────────────────────────

func TestSessionDoors(t *testing.T) {
	client, e := newEngine(t)
	e.answers[MethodSessionsRecent] = []session.Summary{
		{File: "/srv/one.jsonl", Title: "the roof", Opening: "hello", Asked: 3},
		{File: "/srv/two.jsonl", Title: "the floor"},
	}
	rows := client.Recent()
	if len(rows) != 2 || rows[0].Title != "the roof" || rows[1].File != "/srv/two.jsonl" {
		t.Fatalf("Recent = %+v", rows)
	}

	e.answers[MethodSessionOpen] = Welcome{Version: Version, Workspace: "/srv/app", SessionFile: "/srv/one.jsonl", Resumed: true}
	opened, err := client.OpenSession("/srv/one.jsonl")
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if !opened.Resumed || opened.SessionFile != "/srv/one.jsonl" {
		t.Fatalf("OpenSession = %+v", opened)
	}
	var path string
	if err := json.Unmarshal(e.calls(MethodSessionOpen)[0].Payload, &path); err != nil {
		t.Fatalf("open payload: %v", err)
	}
	if path != "/srv/one.jsonl" {
		t.Fatalf("the path did not travel as a bare string: %q", path)
	}
}

func TestRecentIsAnEmptyListWhenTheEngineRefuses(t *testing.T) {
	client, e := newEngine(t)
	e.fails[MethodSessionsRecent] = "no session directory"
	if rows := client.Recent(); rows != nil {
		t.Fatalf("Recent = %+v, want nothing at all", rows)
	}
}

func TestACallThatFailsCarriesTheEnginesWords(t *testing.T) {
	client, e := newEngine(t)
	e.fails[MethodCompact] = "nothing to compact"
	err := client.Agent().Compact(context.Background())
	if err == nil || err.Error() != "nothing to compact" {
		t.Fatalf("Compact = %v, want the engine's own sentence", err)
	}
}

// ── pictures ────────────────────────────────────────────────────────────────

func TestSubmitImageReadsTheBytesOnThisMachine(t *testing.T) {
	client, e := newEngine(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(path, []byte("not really a png but bytes are bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.after = func(e *engine, stream uint64) { e.closeStream(stream) }

	events, err := client.Agent().SubmitImage(context.Background(), "what is this", []session.Image{{Path: path}})
	if err != nil {
		t.Fatalf("SubmitImage: %v", err)
	}
	for range events {
	}
	var args SubmitImageArgs
	if err := json.Unmarshal(e.calls(MethodSubmitImage)[0].Payload, &args); err != nil {
		t.Fatalf("image payload: %v", err)
	}
	if len(args.Images) != 1 {
		t.Fatalf("images = %+v", args.Images)
	}
	if string(args.Images[0].Bytes) != "not really a png but bytes are bytes" {
		t.Fatal("the bytes did not travel")
	}
	if args.Images[0].MIME != "image/png" {
		t.Fatalf("MIME = %q, want the type read off the extension", args.Images[0].MIME)
	}
	if args.Text != "what is this" {
		t.Fatalf("text = %q", args.Text)
	}
}

func TestSubmitImageRefusesWhatTheLocalLaneRefuses(t *testing.T) {
	client, _ := newEngine(t)
	agent := client.Agent()
	dir := t.TempDir()

	odd := filepath.Join(dir, "drawing.bmp")
	if err := os.WriteFile(odd, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.SubmitImage(context.Background(), "", []session.Image{{Path: odd}}); err == nil {
		t.Fatal("a bmp was accepted")
	} else if !strings.Contains(err.Error(), "png, jpeg, webp and gif") {
		t.Fatalf("the refusal does not name what works: %v", err)
	}

	if _, err := agent.SubmitImage(context.Background(), "", []session.Image{{Path: filepath.Join(dir, "gone.png")}}); err == nil {
		t.Fatal("a missing file was accepted")
	}

	huge := make([]byte, maxImageBytes+1)
	if _, err := agent.SubmitImage(context.Background(), "", []session.Image{{Path: "big.png", Bytes: huge}}); err == nil {
		t.Fatal("an oversize picture was accepted")
	} else if !strings.Contains(err.Error(), "image limit") {
		t.Fatalf("the refusal does not say why: %v", err)
	}

	half := make([]byte, maxImageBytes)
	both := []session.Image{{Path: "a.png", Bytes: half}, {Path: "b.png", Bytes: half}, {Path: "c.png", Bytes: half}}
	if _, err := agent.SubmitImage(context.Background(), "", both); err == nil {
		t.Fatal("three big pictures in one message were accepted")
	} else if !strings.Contains(err.Error(), "a single message") {
		t.Fatalf("the refusal does not say why: %v", err)
	}
}

// ── the optional pair ───────────────────────────────────────────────────────

// rewindPair is internal/tui3's own optional interface, restated here rather
// than imported: this package must not depend on the surface, and the point of
// the test is that the SHAPE matches. If rewind.go's pair changes, this fails and
// somebody looks.
type rewindPair interface {
	RewindPoints() []session.RewindPoint
	RewindAt(index int) ([]session.DisplayEntry, error)
}

func TestTheAgentSatisfiesTheRewindPair(t *testing.T) {
	var agent any = &Agent{}
	if _, ok := agent.(rewindPair); !ok {
		t.Fatal("the remote agent does not satisfy the rewind pair, so esc-esc would do nothing over --host")
	}
}

// ── the ambient side ────────────────────────────────────────────────────────

// standingAgent is internal/tui3's own optional interface, restated here for
// [rewindPair]'s reason: this package must not depend on the surface, and the
// point of the test is that the SHAPE matches. The surface asserts it on
// whatever agent it holds and draws no chips at all for one that fails the
// assertion, so this is the difference between a card a person can answer over
// --host and a card they can only look at.
type standingAgent interface {
	ResolveStanding(id uint64, answer session.StandingAnswer)
}

func TestTheAgentSatisfiesTheStandingContract(t *testing.T) {
	var agent any = &Agent{}
	if _, ok := agent.(standingAgent); !ok {
		t.Fatal("the remote agent cannot answer a standing card, so a proposal over --host could only be looked at")
	}
}

// THE CARD ARRIVES WHOLE AT THE SURFACE'S OWN END, which is the half of the trip
// [TestAStandingCardCrossesTheWireAndIsAnsweredBack] does not watch: that one
// reads frames off the pipe, and this one reads the session.Event the client
// hands the surface after its own decode.
func TestAStandingProposalReachesTheSurfaceWithItsItemIntact(t *testing.T) {
	client, e := newEngine(t)
	card := session.StandingNotice{
		ID: 4,
		Item: standing.Item{
			Schema: standing.Schema, ID: "01HQ", Words: "remind me to stand up",
			Workspace: "/srv/app",
			When:      standing.When{Kind: standing.WhenEvery, Words: "every hour", Every: "1h"},
			Does:      standing.Action{Kind: standing.ActionSay, Say: "stand up"},
			Rails:     standing.Rails{PerRunUSD: 0.01, MaxPerDay: 8},
			Status:    standing.StatusActive,
		},
		WhenWords: "every hour", CostWords: "about a cent a run",
	}
	card.Options = session.StandingOptions(card.Item)
	e.after = func(e *engine, stream uint64) {
		e.event(stream, session.Event{Kind: session.EventStandingProposal, Tool: "stand", Standing: &card})
		e.closeStream(stream)
	}
	events, err := client.Agent().Submit(context.Background(), "remind me hourly")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	var arrived []session.Event
	for event := range events {
		arrived = append(arrived, event)
	}
	if len(arrived) != 1 || arrived[0].Standing == nil {
		t.Fatalf("the surface was handed %+v", arrived)
	}
	if !reflect.DeepEqual(*arrived[0].Standing, card) {
		t.Fatalf("the card changed on the way in:\n got %+v\nwant %+v", *arrived[0].Standing, card)
	}
	// The chips especially: the engine says which answers a card offers, and a
	// card whose Options were lost would be drawn with the whole row instead —
	// including `once, not standing` on an item where it means nothing.
	if len(arrived[0].Standing.Options) != len(card.Options) || len(card.Options) == 0 {
		t.Fatalf("the chips did not survive: %+v", arrived[0].Standing.Options)
	}
}

func TestTheStandingDoorsTravelAsTheirOwnPayloads(t *testing.T) {
	client, e := newEngine(t)
	agent := client.Agent()

	agent.ResolveStanding(9, session.StandingAnswer{Change: "make it 8pm"})
	calls := e.calls(MethodStandingResolve)
	if len(calls) != 1 {
		t.Fatalf("ResolveStanding travelled %d times", len(calls))
	}
	var answered StandingArgs
	if err := json.Unmarshal(calls[0].Payload, &answered); err != nil {
		t.Fatalf("standing payload: %v", err)
	}
	if answered.ID != 9 || answered.Answer.Change != "make it 8pm" {
		t.Fatalf("standing args = %+v", answered)
	}

	e.answers[MethodStandingItems] = []standing.Item{{ID: "01HQ", Words: "watch CI", Workspace: "/srv/app"}}
	items, err := client.StandingItems("/srv/app")
	if err != nil || len(items) != 1 || items[0].Words != "watch CI" {
		t.Fatalf("StandingItems = %+v, %v", items, err)
	}
	var asked string
	if err := json.Unmarshal(e.calls(MethodStandingItems)[0].Payload, &asked); err != nil {
		t.Fatalf("items payload: %v", err)
	}
	if asked != "/srv/app" {
		t.Fatalf("the workspace did not travel as a bare string: %q", asked)
	}

	if err := client.SaveStanding(items[0]); err != nil {
		t.Fatalf("SaveStanding: %v", err)
	}
	var written standing.Item
	if err := json.Unmarshal(e.calls(MethodStandingSave)[0].Payload, &written); err != nil {
		t.Fatalf("save payload: %v", err)
	}
	if written.ID != "01HQ" || written.Workspace != "/srv/app" {
		t.Fatalf("the item did not travel whole: %+v", written)
	}
}

// A FAULT IS NOT AN EMPTY BAND. StandingItems answers an error rather than
// swallowing it, so the caller that keeps the last good list can tell a round
// trip that failed from a workspace with nothing set up in it — and a refused
// write comes back in the engine's own words for home to print.
func TestAStandingCallThatFailsSaysSoRatherThanAnsweringNothing(t *testing.T) {
	client, e := newEngine(t)
	e.fails[MethodStandingItems] = "engine: this engine keeps an eye on nothing"
	items, err := client.StandingItems("/srv/app")
	if err == nil {
		t.Fatalf("a failed call answered %+v as though the workspace were empty", items)
	}
	e.fails[MethodStandingSave] = "an item needs a per-run budget"
	if err := client.SaveStanding(standing.Item{ID: "01HQ"}); err == nil || err.Error() != "an item needs a per-run budget" {
		t.Fatalf("SaveStanding = %v, want the store's own refusal", err)
	}
}
