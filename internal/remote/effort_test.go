package remote

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/effort"
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

// It takes the words the real dial takes, absence among them: [effort.Parse]
// lands `auto`, the legacy `off` and "" on [effort.None], and the surface's way
// back to auto is a set of that empty word (internal/tui3's runEffort).
func (a *rungAgent) SetConversationEffort(rung string) bool {
	parsed, ok := effort.Parse(rung)
	if !ok {
		return false
	}
	a.stored = parsed.String()
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

// AUTO CROSSES THE WIRE LIKE ANY OTHER WORD, and it is the one that could not be
// read off the type system: "" is a real answer here, so a hosted conversation
// put back to auto must still have its dial. The surface draws `⠿ auto` off
// exactly this pair — an empty resolved rung and a welcome that says there is a
// dial — and a connection that lost the second would go back to drawing nothing.
func TestPuttingAHostedConversationBackToAutoKeepsItsDial(t *testing.T) {
	far := &rungAgent{fakeAgent: &fakeAgent{}, stored: "high"}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, SessionFile: "/srv/session.jsonl"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	agent := loop.Client.Agent()
	if rung := agent.ResolvedEffort(); rung != "high" {
		t.Fatalf("the welcome carried %q rather than the engine's rung", rung)
	}
	if !agent.SetConversationEffort("") {
		t.Fatal("the engine refused absence, which is a word its dial takes")
	}
	if far.stored != "" {
		t.Fatalf("the far conversation is still at %q, want absence", far.stored)
	}
	// The surface's own copy moved on the keystroke, as it does for a rung.
	if rung := agent.ResolvedEffort(); rung != "" {
		t.Fatalf("the surface still draws %q after the conversation was cleared", rung)
	}
	// AND THE DIAL IS STILL THERE. The word is empty and the capability is not:
	// only the welcome can say which of the two an empty rung means.
	if !agent.EffortSupported() {
		t.Fatal("clearing the rung took the dial away with it")
	}
	if rung := agent.ConversationEffort(); rung != "" {
		t.Fatalf("the stored rung read back as %q", rung)
	}
}
