package remote

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// EditFollowUp and MoveFollowUp are the far halves of the queue's edit and
// reorder: they act on the fake's own queue the way [session.Agent] does and
// answer false for a stream that is not held any more. An edit is recorded in
// follows as "edited:<text>" so the test reads what the far agent was told.
func (f *fakeAgent) EditFollowUp(ch <-chan session.Event, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, held := range f.streams {
		if (<-chan session.Event)(held) == ch {
			f.follows = append(f.follows, "edited:"+text)
			return nil
		}
	}
	return session.ErrFollowUpGone
}

func (f *fakeAgent) MoveFollowUp(ch, before <-chan session.Event) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	from, to := -1, len(f.streams)-1
	for i, held := range f.streams {
		if (<-chan session.Event)(held) == ch {
			from = i
		}
	}
	if from < 0 {
		return false
	}
	item := f.streams[from]
	f.streams = append(f.streams[:from], f.streams[from+1:]...)
	to = len(f.streams)
	for i, held := range f.streams {
		if before != nil && (<-chan session.Event)(held) == before {
			to = i
		}
	}
	f.streams = append(f.streams[:to], append([]chan session.Event{item}, f.streams[to:]...)...)
	return true
}

// THE EDIT AND THE MOVE CROSS THE WIRE BY THE STREAM THE SURFACE WAS HANDED,
// leave the receipt where it was (a later take-back still works), and say false
// once the far queue no longer holds the message.
func TestEditAndMoveFollowUpCrossTheWire(t *testing.T) {
	client, e := newEngine(t)
	e.answers[MethodEditFollowUp] = true
	e.answers[MethodMoveFollowUp] = true
	e.answers[MethodUnqueueFollowUp] = true
	one, err := client.Agent().FollowUp("one")
	if err != nil {
		t.Fatal(err)
	}
	two, err := client.Agent().FollowUp("two")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Agent().EditFollowUp(one, "uno"); err != nil {
		t.Fatalf("EditFollowUp: %v", err)
	}
	if !client.Agent().MoveFollowUp(two, one) {
		t.Fatal("MoveFollowUp answered false for a queued stream")
	}
	if err := client.Agent().EditFollowUp(make(chan session.Event), "x"); err != session.ErrFollowUpGone {
		t.Fatalf("an unknown stream edit = %v, want ErrFollowUpGone", err)
	}
	if client.Agent().MoveFollowUp(make(chan session.Event), nil) {
		t.Fatal("an unknown stream was moved")
	}
	if !client.Agent().UnqueueFollowUp(one) {
		t.Fatal("an edit spent the take-back receipt")
	}
}

func TestEditAndMoveFollowUpOverTheServer(t *testing.T) {
	agent := &fakeAgent{model: "a/b", title: "the queue"}
	l := dialAgent(t, engineOn(agent))
	if frame := l.hello(Hello{Version: Version}); frame.Kind != "welcome" {
		t.Fatalf("handshake: %s", frame.Error)
	}
	one := decode[StreamRef](t, l.ok(1, MethodFollowUp, SubmitArgs{Text: "one"}).Payload)
	two := decode[StreamRef](t, l.ok(2, MethodFollowUp, SubmitArgs{Text: "two"}).Payload)
	if !decode[bool](t, l.ok(3, MethodEditFollowUp, EditQueueArgs{Stream: one.Stream, Text: "uno"}).Payload) {
		t.Fatal("the edit answered false for a queued stream")
	}
	if !decode[bool](t, l.ok(4, MethodMoveFollowUp, MoveQueueArgs{Stream: two.Stream, Before: one.Stream}).Payload) {
		t.Fatal("the move answered false for a queued stream")
	}
	if decode[bool](t, l.ok(5, MethodEditFollowUp, EditQueueArgs{Stream: 777, Text: "x"}).Payload) {
		t.Fatal("an unknown stream was edited")
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if got := strings.Join(agent.follows, ","); got != "one,two,edited:uno" {
		t.Fatalf("far agent was told %q", got)
	}
}
