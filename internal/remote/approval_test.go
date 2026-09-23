package remote

import (
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// postureAgent is an engine with the conversation's posture door on it.
type postureAgent struct {
	*fakeAgent
	stored, standing string
	gate             bool
}

func (a *postureAgent) ApprovalDial() bool { return a.gate }

func (a *postureAgent) ResolvedApprovalPosture() string {
	if a.stored != "" && a.stored != session.PostureAuto {
		return a.stored
	}
	return a.standing
}

// It takes the words the real door takes and refuses the rest with a sentence,
// which is the shape the wire carries (approval.go).
func (a *postureAgent) SetApprovalPosture(posture string) error {
	for _, known := range session.ApprovalPostures {
		if posture == known {
			a.stored = posture
			return nil
		}
	}
	return errors.New(posture + " is not a posture")
}

func TestThePostureDialCrossesTheHostConnection(t *testing.T) {
	far := &postureAgent{fakeAgent: &fakeAgent{}, standing: session.PostureAsk, gate: true}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, SessionFile: "/srv/session.jsonl"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	agent := loop.Client.Agent()
	if !agent.ApprovalDial() {
		t.Fatal("host hid the posture dial")
	}
	// The posture a frame draws arrives with the welcome and is read from memory.
	if got := agent.ResolvedApprovalPosture(); got != session.PostureAsk {
		t.Fatalf("welcome carried %q rather than the engine's posture", got)
	}
	if err := agent.SetApprovalPosture(session.PostureAllow); err != nil {
		t.Fatalf("the engine refused a posture it knows: %v", err)
	}
	if far.stored != session.PostureAllow {
		t.Fatalf("the far conversation is still at %q", far.stored)
	}
	// AND THE SURFACE'S OWN COPY MOVED WITHOUT WAITING FOR THE PUSH.
	if got := agent.ResolvedApprovalPosture(); got != session.PostureAllow {
		t.Fatalf("the surface still draws %q after the keystroke", got)
	}
	// A refusal crosses as the engine's own sentence.
	if err := agent.SetApprovalPosture("sideways"); err == nil {
		t.Fatal("a word that is not a posture was taken")
	}
}

// AN ENGINE WITH NO DOOR HAS NO DIAL, and an engine with the door and no gate
// behind it has none either: the flag is false and nothing reaches the far
// conversation.
func TestAnEngineWithoutThePostureDoorAdvertisesNoneAndTakesNothing(t *testing.T) {
	for name, far := range map[string]WrappedAgent{
		"no door": &fakeAgent{},
		"no gate": &postureAgent{fakeAgent: &fakeAgent{}, standing: session.PostureAsk},
	} {
		t.Run(name, func(t *testing.T) {
			loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
				return &Engine{Agent: far, SessionFile: "/srv/session.jsonl"}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			defer loop.Close()
			agent := loop.Client.Agent()
			if agent.ApprovalDial() {
				t.Fatal("an engine with no dial advertised one")
			}
			if err := agent.SetApprovalPosture(session.PostureAllow); err == nil {
				t.Fatal("a posture was taken by an engine that has no dial")
			}
		})
	}
}

// The actual engine, rather than only a fixture, must expose the door.
var _ approvalDoor = (*session.Agent)(nil)
