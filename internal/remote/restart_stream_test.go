package remote

import (
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// A restarted persistent engine owns fresh stream IDs even when the transcript
// is unchanged. Completed turns from its predecessor must not swallow replies.
func TestPersistentEngineRestartDeliversNewReplyOnReusedStream(t *testing.T) {
	agents := []*fakeAgent{{model: "a/b", title: "before"}, {model: "a/b", title: "after"}}
	sessions := []*Session{NewSession(engineOn(agents[0]), true), NewSession(engineOn(agents[1]), true)}
	var mu sync.Mutex
	var surfaces []io.ReadWriteCloser
	dialer := func() (io.ReadWriteCloser, error) {
		mu.Lock()
		n := len(surfaces)
		if n >= len(sessions) {
			mu.Unlock()
			return nil, errors.New("no more engines")
		}
		surface, engine := Pipe()
		surfaces = append(surfaces, surface)
		mu.Unlock()
		go func() {
			_ = ServeAttach(engine, engine, AttachOptions{Open: func(Hello) (*Session, error) { return sessions[n], nil }})
			_ = engine.Close()
		}()
		return surface, nil
	}
	client, err := Roam("devbox", Hello{Workspace: "app"}, Roaming{Dial: dialer, Window: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		for _, s := range sessions {
			_ = s.Close()
		}
	})
	first, err := client.Agent().Submit(nil, "first")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "first stream", func() bool { return agents[0].stream(0) != nil })
	agents[0].stream(0) <- session.Event{Kind: session.EventTextDelta, Text: "old reply"}
	agents[0].finish(agents[0].stream(0))
	for range first {
	}
	mu.Lock()
	old := surfaces[0]
	mu.Unlock()
	_ = old.Close()
	waitFor(t, "new persistent engine", func() bool { return client.Welcome().Title == "after" && client.LinkNote() == "" })
	second, err := client.Agent().Submit(nil, "second")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "second stream", func() bool { return agents[1].stream(0) != nil })
	agents[1].stream(0) <- session.Event{Kind: session.EventTextDelta, Text: "new reply"}
	agents[1].finish(agents[1].stream(0))
	ev, ok := nextEvent(t, second)
	if !ok || ev.Text != "new reply" {
		t.Fatalf("restarted engine reply lost: open=%v event=%+v", ok, ev)
	}
}

// A cursor from the replaced owner cannot skip a restarted task's live reply.
func TestReplacedOwnerCursorReplaysBeginningOfNewLiveTurn(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)
	t.Cleanup(func() { _ = sess.Close() })
	first := dialSession(t, sess)
	first.hello(Hello{Version: Version})
	ref := decode[StreamRef](t, first.ok(1, MethodSubmit, SubmitArgs{Text: "resume useful work"}).Payload)
	turn := agent.stream(0)
	turn <- session.Event{Kind: session.EventTextDelta, Text: "new beginning"}
	if got := first.recv(); got.Seq != 1 {
		t.Fatalf("first event: %+v", got)
	}
	if err := first.end(); err != nil {
		t.Fatal(err)
	}
	second := dialSession(t, sess)
	second.hello(Hello{Version: Version, SessionInstance: "replaced-owner", Resume: []StreamCursor{{Stream: ref.Stream, Seq: 99}}})
	got := second.recv()
	if got.Kind != "event" || got.Seq != 1 || decode[EventWire](t, got.Payload).Unwire().Text != "new beginning" {
		t.Fatalf("old cursor suppressed new reply: %+v", got)
	}
	agent.finish(turn)
}

// A replacement with a live turn must be followed only after clearing old IDs.
func TestReplacedPersistentOwnerEndsOldTurnAndFollowsNewLiveTurn(t *testing.T) {
	first := make(chan struct{})
	client, links := roam(t,
		script{welcome: Welcome{Version: Version, SessionFile: "/j.jsonl", Persistent: true, SessionInstance: "old"}, play: func(e *scripted, _ Hello) { <-first; e.event(1, 5, "old partial") }},
		script{welcome: Welcome{Version: Version, SessionFile: "/j.jsonl", Persistent: true, SessionInstance: "new", Live: 1}, play: func(e *scripted, h Hello) {
			if h.SessionInstance != "old" {
				t.Errorf("resume did not identify old owner: %q", h.SessionInstance)
			}
			e.event(1, 1, "new resumed reply")
			e.closeStream(1, 1)
		}},
	)
	events, err := client.Agent().Submit(nil, "first")
	if err != nil {
		t.Fatal(err)
	}
	close(first)
	if ev, _ := nextEvent(t, events); ev.Text != "old partial" {
		t.Fatalf("old reply: %+v", ev)
	}
	links.cut(0)
	if ev, ok := nextEvent(t, events); !ok || ev.Kind != session.EventError {
		t.Fatalf("old turn not interrupted: %+v", ev)
	}
	select {
	case follow := <-client.Follow():
		if ev, ok := nextEvent(t, follow.Events); !ok || ev.Text != "new resumed reply" {
			t.Fatalf("new turn lost: %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("new live turn was not followed")
	}
}
