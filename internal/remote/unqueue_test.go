package remote

import (
	"github.com/Agent-Field/codeaf/internal/session"
	"testing"
)

func TestFollowReceiptDisappearsWhenStreamCloses(t *testing.T) {
	client, e := newEngine(t)
	e.after = func(e *engine, id uint64) { e.closeStream(id) }
	ch, err := client.Agent().FollowUp("finishes normally")
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	client.followRefs.mu.Lock()
	defer client.followRefs.mu.Unlock()
	if n := len(client.followRefs.byCh); n != 0 {
		t.Fatalf("closed stream retains %d take-back receipts", n)
	}
}

func TestOldEngineUnqueueReturnsFalseAndKeepsStream(t *testing.T) {
	client, e := newEngine(t)
	e.fails[MethodUnqueueFollowUp] = "engine: no such method \"UnqueueFollowUp\""
	ch, err := client.Agent().FollowUp("old engine queue")
	if err != nil {
		t.Fatal(err)
	}
	if client.Agent().UnqueueFollowUp(ch) {
		t.Fatal("unknown method reported success")
	}
	e.event(1, session.Event{Kind: session.EventTextDelta, Text: "still runs"})
	e.closeStream(1)
	var text string
	for event := range ch {
		if event.Kind == session.EventError {
			t.Fatalf("unknown method surfaced an error: %v", event.Err)
		}
		text += event.Text
	}
	if text != "still runs" {
		t.Fatalf("row's stream lost: %q", text)
	}
}

func TestNonDriverCannotUnqueueAnotherConnectionsStream(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)
	old := dialSession(t, sess)
	old.hello(Hello{Version: Version, Surface: "old"})
	newer := dialSession(t, sess)
	newer.hello(Hello{Version: Version, Surface: "new"})
	ref := decode[StreamRef](t, newer.ok(1, MethodFollowUp, SubmitArgs{Text: "new window's follow-up"}).Payload)
	answer := old.call(1, MethodUnqueueFollowUp, UnqueueArgs{Stream: ref.Stream})
	if answer.Error == "" && decode[bool](t, answer.Payload) {
		t.Fatal("non-driver unqueued another connection's message using its stream id")
	}
}

func TestTheDriverCannotTakeBackAnotherConnectionsFollowUp(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)
	owner := dialSession(t, sess)
	owner.hello(Hello{Version: Version, Surface: "owner"})
	ref := decode[StreamRef](t, owner.ok(1, MethodFollowUp, SubmitArgs{Text: "owner's words"}).Payload)
	other := dialSession(t, sess)
	other.hello(Hello{Version: Version, Surface: "other"})
	if decode[bool](t, other.ok(1, MethodUnqueueFollowUp, UnqueueArgs{Stream: ref.Stream}).Payload) {
		t.Fatal("driver took back a foreign receipt")
	}
	owner.ok(2, MethodTake, nil)
	if !decode[bool](t, owner.ok(3, MethodUnqueueFollowUp, UnqueueArgs{Stream: ref.Stream}).Payload) {
		t.Fatal("foreign press consumed the owner's receipt")
	}
}
