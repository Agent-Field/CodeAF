package remote

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// rungAgent is an engine with the conversation's thinking dial on it. resolved,
// when set, is the rung it insists the next turn will ask for whatever anybody
// stores — which is the shape of the one case that matters: a level dialled onto
// the model itself outranks the conversation's.
type rungAgent struct {
	*fakeAgent
	stored, resolved string
}

func (a *rungAgent) ConversationEffort() string { return a.stored }

func (a *rungAgent) ResolvedEffort() string {
	if a.resolved != "" {
		return a.resolved
	}
	return a.stored
}

func (a *rungAgent) SetConversationEffort(rung string) bool {
	if rung != "low" && rung != "medium" && rung != "high" {
		return false
	}
	a.stored = rung
	return true
}

func TestTheThinkingDialCrossesTheHostConnection(t *testing.T) {
	far := &rungAgent{fakeAgent: &fakeAgent{}, stored: "medium"}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, SessionFile: "/srv/session.jsonl"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	agent := loop.Client.Agent()
	if !agent.EffortSupported() {
		t.Fatal("host hid the thinking dial")
	}
	// The rung a frame draws arrives with the welcome and is read from memory.
	if rung := agent.ResolvedEffort(); rung != "medium" {
		t.Fatalf("welcome carried %q rather than the engine's rung", rung)
	}
	if rung := agent.ConversationEffort(); rung != "medium" {
		t.Fatalf("the stored rung read back as %q", rung)
	}
	if !agent.SetConversationEffort("high") {
		t.Fatal("the engine refused a rung it knows")
	}
	if far.stored != "high" {
		t.Fatalf("the far conversation is still at %q", far.stored)
	}
	// AND THE SURFACE'S OWN COPY MOVED WITHOUT WAITING FOR THE PUSH, because the
	// caller asks what it resolved to in the very next statement.
	if rung := agent.ResolvedEffort(); rung != "high" {
		t.Fatalf("the surface still draws %q after the keystroke", rung)
	}
	if agent.SetConversationEffort("sideways") {
		t.Fatal("a word that is not a rung was taken")
	}
}

// A rung the conversation cannot win is the case the read-back exists for: the
// set takes, the resolver keeps its own answer, and the surface has to be able
// to say so rather than draw the word the person pressed.
func TestASetRungReadsBackTheRungThatActuallyWins(t *testing.T) {
	far := &rungAgent{fakeAgent: &fakeAgent{}, stored: "low", resolved: "max"}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, SessionFile: "/srv/session.jsonl"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	agent := loop.Client.Agent()
	if !agent.SetConversationEffort("high") {
		t.Fatal("the engine refused a rung it knows")
	}
	if rung := agent.ResolvedEffort(); rung != "max" {
		t.Fatalf("the surface drew %q rather than the rung that wins", rung)
	}
}

// AN ENGINE WITH NO DIAL HAS NO DIAL, and that is a thing the surface must be
// able to read before it draws anything: the flag is false, every door answers
// its own emptiness, and nothing reaches the far conversation.
func TestAnEngineWithoutTheDialAdvertisesNoneAndTakesNothing(t *testing.T) {
	far := &fakeAgent{}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, SessionFile: "/srv/session.jsonl"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	agent := loop.Client.Agent()
	if agent.EffortSupported() {
		t.Fatal("an engine with no dial advertised one")
	}
	if rung := agent.ResolvedEffort(); rung != "" {
		t.Fatalf("a conversation with no dial reported %q", rung)
	}
	if agent.SetConversationEffort("high") {
		t.Fatal("a rung was taken by an engine that has no dial")
	}
}

// The actual engine, rather than only a fixture, must expose the dial.
var _ effortDoor = (*session.Agent)(nil)
