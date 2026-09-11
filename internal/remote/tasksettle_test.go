package remote

import (
	"errors"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// settleFar is an engine that can decide a landing, recording which act reached
// it and answering with whatever the test wants back.
type settleFar struct {
	*fakeAgent
	id     uint64
	spent  string
	why    string
	refuse error
}

func (a *settleFar) ResolveUnverified(id uint64, resolution session.TaskResolution, why string) error {
	a.id, a.spent, a.why = id, string(resolution), why
	return a.refuse
}

func (a *settleFar) HandUnverifiedToModel(id uint64) error {
	a.id, a.spent = id, "hand"
	return a.refuse
}

func (a *settleFar) TakeBackDecision(id uint64) error {
	a.id, a.spent = id, "back"
	return a.refuse
}

func (a *settleFar) ResolveConflict(id uint64) error {
	a.id, a.spent = id, "merge"
	return a.refuse
}

// TestDecidingALandingCrossesTheHostConnection is the whole of #706 measured on
// the wire: a window on an engine host must be able to spend every answer its
// card draws, because the surface draws no answers at all where it cannot.
func TestDecidingALandingCrossesTheHostConnection(t *testing.T) {
	far := &settleFar{fakeAgent: &fakeAgent{}}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, SessionFile: "/srv/task-session.jsonl"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	agent := loop.Client.Agent()
	if !agent.SettleSupported() {
		t.Fatal("a host holding an engine that decides landings did not say so")
	}
	for _, one := range []struct {
		said  string
		spend func() error
		spent string
	}{
		{"accept", func() error { return agent.ResolveUnverified(7, session.TaskAccept, "looked at it") }, string(session.TaskAccept)},
		{"not right", func() error { return agent.ResolveUnverified(7, session.TaskRefute, "looked at it") }, string(session.TaskRefute)},
		{"resolve it", func() error { return agent.ResolveConflict(7) }, "merge"},
		{"hand it over", func() error { return agent.HandUnverifiedToModel(7) }, "hand"},
		{"take it back", func() error { return agent.TakeBackDecision(7) }, "back"},
	} {
		far.id, far.spent = 0, ""
		if err := one.spend(); err != nil {
			t.Fatalf("%s did not cross: %v", one.said, err)
		}
		if far.id != 7 || far.spent != one.spent {
			t.Fatalf("%s reached the engine as task %d %q", one.said, far.id, far.spent)
		}
	}
	if far.why != "looked at it" {
		t.Fatalf("the sentence on the record crossed as %q", far.why)
	}
}

// TestALandingSomebodyElseAnsweredComesBackAsOne holds the one refusal the card
// tells apart from every other: the question is gone rather than unspendable, so
// it must arrive wearing the sentinel and not as prose.
func TestALandingSomebodyElseAnsweredComesBackAsOne(t *testing.T) {
	far := &settleFar{fakeAgent: &fakeAgent{}, refuse: session.ErrTaskDecided}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, SessionFile: "/srv/task-session.jsonl"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	if err := loop.Client.Agent().ResolveUnverified(7, session.TaskAccept, ""); !errors.Is(err, session.ErrTaskDecided) {
		t.Fatalf("an already-answered landing came back as %v", err)
	}
	far.refuse = errors.New("the working copy is gone")
	err = loop.Client.Agent().ResolveUnverified(7, session.TaskAccept, "")
	if err == nil || errors.Is(err, session.ErrTaskDecided) {
		t.Fatalf("a refusal that is not a decision came back as %v", err)
	}
}

// TestAnEngineThatCannotDecideALandingSaysSo is the absence law on the wire: a
// window told no draws the card with its reason and no chips, which is what it
// does against any engine with no such door.
func TestAnEngineThatCannotDecideALandingSaysSo(t *testing.T) {
	far := &fakeAgent{}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, SessionFile: "/srv/task-session.jsonl"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	agent := loop.Client.Agent()
	if agent.SettleSupported() {
		t.Fatal("an engine with no landing doors advertised them")
	}
	if err := agent.ResolveUnverified(7, session.TaskAccept, ""); err == nil {
		t.Fatal("a door nobody advertised took an answer")
	}
}

// TestAWatcherMayNotDecideALanding keeps the reading surface reading: the
// allow-list is what refuses it, and a method added here must not have quietly
// become a way to change somebody's conversation.
func TestAWatcherMayNotDecideALanding(t *testing.T) {
	far := &settleFar{fakeAgent: &fakeAgent{}}
	loop, err := Loopback(Hello{Version: Version, Watch: true}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, SessionFile: "/srv/task-session.jsonl"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	if err := loop.Client.Agent().ResolveUnverified(7, session.TaskAccept, ""); err == nil {
		t.Fatal("a reading window answered a landing")
	}
	if far.spent != "" {
		t.Fatalf("a reading window reached the engine with %q", far.spent)
	}
}

// The actual engine, and not only a fixture, has to carry the seam.
var _ taskSettleDoor = (*session.Agent)(nil)
var _ interface{ ResolveConflict(uint64) error } = (*session.Agent)(nil)
