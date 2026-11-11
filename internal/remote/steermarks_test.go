package remote

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// steeringAgent is the scripted agent with marked doors for Steer and
// FollowUp, and not for Submit: each door is its own fact.
type steeringAgent struct {
	*fakeAgent
	steers, follows []session.Submission
}

func (m *steeringAgent) SteerMessage(message session.Submission) (<-chan session.Event, error) {
	m.mu.Lock()
	m.steers = append(m.steers, message)
	m.mu.Unlock()
	return m.fakeAgent.Steer(message.Text)
}

func (m *steeringAgent) FollowUpMessage(message session.Submission) (<-chan session.Event, error) {
	m.mu.Lock()
	m.follows = append(m.follows, message)
	m.mu.Unlock()
	return m.fakeAgent.FollowUp(message.Text)
}

// markedReads is one read the way a door observed it.
func markedReads() []session.MessageRead {
	return []session.MessageRead{{
		Source: session.MessageReference{Kind: "collection", ID: "c1", Name: "Specs"},
		Count:  200, Missing: 1, Unavailable: 2, More: true,
		Warnings: []string{"two could not be asked"},
		Items:    []session.MessageReference{{Kind: "file", ID: "r1", Path: "docs/a.md"}},
	}}
}

// A MARKED CORRECTION AND A MARKED FOLLOW-UP CROSS THE WIRE WHOLE, on the same
// Steer and FollowUp calls a plain one makes: the far agent's own doors get the
// words, the person's words and every count; plain calls go by the plain doors;
// and a marked follow-up's stream is the take-back's receipt exactly as a plain
// one's is. The welcome says the two doors are there and the Submit door is not.
func TestAMarkedSteerAndFollowUpReachTheHostedAgentsOwnDoors(t *testing.T) {
	agent := &steeringAgent{fakeAgent: &fakeAgent{model: "m"}}
	l := dialAgent(t, &Engine{Agent: agent})
	frame := l.hello(Hello{Version: Version})
	if frame.Kind != "welcome" {
		t.Fatalf("handshake: %s", frame.Error)
	}
	welcome := decode[Welcome](t, frame.Payload)
	if !welcome.SteerMarks || !welcome.FollowUpMarks || welcome.MessageMarks {
		t.Fatalf("the welcome says %+v", welcome)
	}
	said := "use these"
	steer := SubmitArgs{Text: "use these\n\n- docs/a.md", Said: &said, Reads: markedReads()}
	l.ok(1, MethodSteer, steer)
	l.ok(2, MethodSteer, SubmitArgs{Text: "plain correction"})
	follow := SubmitArgs{Text: "next\n\n- docs/a.md", Reads: markedReads()}
	ref := decode[StreamRef](t, l.ok(3, MethodFollowUp, follow).Payload)
	queued := agent.stream(2)

	agent.mu.Lock()
	steers, follows := agent.steers, agent.follows
	steered, plainFollows := agent.fakeAgent.steered, agent.fakeAgent.follows
	agent.mu.Unlock()
	if len(steers) != 1 || !reflect.DeepEqual(steers[0], steer.submission()) {
		t.Fatalf("the marked steer door was handed %+v", steers)
	}
	if len(follows) != 1 || follows[0].Said != nil || !reflect.DeepEqual(follows[0].Reads, markedReads()) {
		t.Fatalf("the marked follow-up door was handed %+v", follows)
	}
	if !reflect.DeepEqual(steered, []string{steer.Text, "plain correction"}) || !reflect.DeepEqual(plainFollows, []string{follow.Text}) {
		t.Fatalf("the agent heard steers %q and follow-ups %q", steered, plainFollows)
	}
	if answered := decode[bool](t, l.ok(4, MethodUnqueueFollowUp, UnqueueArgs{Stream: ref.Stream}).Payload); !answered {
		t.Fatal("a marked follow-up's stream could not be taken back")
	}
	select {
	case _, ok := <-queued:
		if ok {
			t.Fatal("the taken-back follow-up streamed an event")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the taken-back follow-up's stream never closed")
	}
}

// AN ENGINE WITHOUT THE DOOR REFUSES A MARKED CORRECTION OR FOLLOW-UP BEFORE IT
// REACHES THE TURN OR THE QUEUE, and still takes plain words; a door for one
// says nothing about the other, and neither about Submit.
func TestAnEngineWithoutTheDoorRefusesAMarkedSteerOrFollowUpBeforeAnything(t *testing.T) {
	plain := &fakeAgent{model: "m"}
	l := dialAgent(t, engineOn(plain))
	welcome := decode[Welcome](t, l.hello(Hello{Version: Version}).Payload)
	if welcome.SteerMarks || welcome.FollowUpMarks || welcome.MessageMarks {
		t.Fatalf("a plain engine's welcome says %+v", welcome)
	}
	said := ""
	for id, call := range []struct {
		method string
		args   SubmitArgs
	}{
		{MethodSteer, SubmitArgs{Text: "x", Said: &said}},
		{MethodSteer, SubmitArgs{Text: "x", Reads: markedReads()}},
		{MethodFollowUp, SubmitArgs{Text: "x", Attachments: []session.MessageAttachment{{Kind: "file", Path: "a.go"}}}},
		{MethodFollowUp, SubmitArgs{Text: "x", Reads: markedReads()}},
	} {
		if frame := l.call(uint64(id+1), call.method, call.args); frame.Error == "" {
			t.Fatalf("%s %+v was not refused", call.method, call.args)
		}
	}
	l.ok(10, MethodSteer, SubmitArgs{Text: "plain correction"})
	l.ok(11, MethodFollowUp, SubmitArgs{Text: "plain follow-up"})
	plain.mu.Lock()
	defer plain.mu.Unlock()
	if !reflect.DeepEqual(plain.steered, []string{"plain correction"}) || !reflect.DeepEqual(plain.follows, []string{"plain follow-up"}) {
		t.Fatalf("the plain agent heard steers %q and follow-ups %q", plain.steered, plain.follows)
	}

	submitOnly := &markedAgent{fakeAgent: &fakeAgent{}}
	if !messageMarksKnown(submitOnly) || steerMarksKnown(submitOnly) || followUpMarksKnown(submitOnly) {
		t.Fatal("a Submit door was taken for a Steer or FollowUp door")
	}
}

// AN OLDER HOST IS NEVER SENT A MARKED CORRECTION OR FOLLOW-UP: the client
// refuses it with no call made, whatever else the welcome said, and each flag
// opens only its own door.
func TestAnOlderHostIsRefusedAMarkedSteerOrFollowUpWithNoCallMade(t *testing.T) {
	said := "mine"
	marked := session.Submission{Text: "x\n\n- a.md", Said: &said, Reads: markedReads()}
	for _, welcome := range []Welcome{
		{Version: Version},
		{Version: Version, MessageMarks: true},
		{Version: Version, FollowUpMarks: true},
	} {
		client := &Client{welcome: welcome}
		agent := &Agent{c: client}
		if _, err := agent.SteerMessage(marked); err == nil {
			t.Fatalf("a host saying %+v was sent a marked steer", welcome)
		}
		if !welcome.FollowUpMarks {
			if _, err := agent.FollowUpMessage(marked); err == nil {
				t.Fatalf("a host saying %+v was sent a marked follow-up", welcome)
			}
		}
		if client.made.Load() != 0 {
			t.Fatalf("a refused message crossed the wire to a host saying %+v", welcome)
		}
	}
	steerOnly := &Agent{c: &Client{welcome: Welcome{Version: Version, SteerMarks: true}}}
	if !steerOnly.SteerMarksKnown() || steerOnly.FollowUpMarksKnown() || steerOnly.MessageMarksKnown() {
		t.Fatal("a steer flag answered for another door")
	}
	if _, err := steerOnly.FollowUpMessage(marked); err == nil || steerOnly.c.made.Load() != 0 {
		t.Fatal("a steer-only host was sent a marked follow-up")
	}
}

// markedAgent's Submit door says nothing about Steer and FollowUp.
var _ interface {
	SubmitMessage(context.Context, session.Submission) (<-chan session.Event, error)
} = (*markedAgent)(nil)
