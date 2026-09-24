package remote

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
)

// shelfAgent is an engine whose conversation carries skills put in front of it
// by hand, over a shelf of its own — the five doors *session.Agent has.
type shelfAgent struct {
	*fakeAgent
	held  []string
	shelf []store.Fact
}

func (a *shelfAgent) AttachSkills(names ...string) []string {
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			a.held = append(a.held, name)
		}
	}
	return append([]string(nil), a.held...)
}

func (a *shelfAgent) DetachSkill(name string) bool {
	for index, held := range a.held {
		if held == name {
			a.held = append(a.held[:index], a.held[index+1:]...)
			return true
		}
	}
	return false
}

func (a *shelfAgent) AttachedSkills() []string { return append([]string(nil), a.held...) }

func (a *shelfAgent) ClearAttachedSkills() int {
	count := len(a.held)
	a.held = nil
	return count
}

func (a *shelfAgent) SkillFacts(string, int) ([]store.Fact, error) { return a.shelf, nil }

// THE ATTACHMENT IS THE FAR SESSION'S, AND /skill REACHES IT. The ordinary
// launch attaches to this workspace's session host, and a picker whose doors
// stopped at this end of the socket answered every choice with "this
// conversation cannot carry attached skills".
func TestAttachedSkillsCrossTheHostConnection(t *testing.T) {
	far := &shelfAgent{fakeAgent: &fakeAgent{}, shelf: []store.Fact{{
		Kind: store.FactSkill, Status: store.FactActive, Artifact: "/srv/skills/release-notes",
		Body: "drafts release notes", Trust: "imported-provisional", Digest: "d1",
	}}}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, SessionFile: "/srv/session.jsonl"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	agent := loop.Client.Agent()
	if !agent.SkillsSupported() {
		t.Fatal("the host hid the skill doors of a conversation that has them")
	}

	if held := agent.AttachSkills("release-notes", "pdf"); strings.Join(held, ",") != "release-notes,pdf" {
		t.Fatalf("AttachSkills answered %v", held)
	}
	if strings.Join(far.held, ",") != "release-notes,pdf" {
		t.Fatalf("the far conversation holds %v", far.held)
	}
	if held := agent.AttachedSkills(); strings.Join(held, ",") != "release-notes,pdf" {
		t.Fatalf("AttachedSkills read back %v", held)
	}
	if !agent.DetachSkill("pdf") || strings.Join(far.held, ",") != "release-notes" {
		t.Fatalf("DetachSkill did not reach the far conversation: %v", far.held)
	}
	if count := agent.ClearAttachedSkills(); count != 1 || len(far.held) != 0 {
		t.Fatalf("ClearAttachedSkills answered %d and left %v", count, far.held)
	}

	facts, err := agent.SkillFacts(store.FactActive, 50)
	if err != nil {
		t.Fatalf("the shelf did not cross: %v", err)
	}
	if len(facts) != 1 || facts[0].SkillName() != "release-notes" || facts[0].Body != "drafts release notes" {
		t.Fatalf("the shelf crossed as %+v", facts)
	}
	if facts[0].Digest != "" || facts[0].Trust != "" {
		t.Fatalf("the shelf carried the store's bookkeeping across the wire: %+v", facts[0])
	}
}

// AN ENGINE WITHOUT THE DOORS HAS NONE: the flag is false, the doors answer a
// conversation with nothing attached, and the shelf answers an error — the
// picker's own word for a conversation that cannot attach anything.
func TestAnEngineWithoutSkillDoorsAdvertisesNone(t *testing.T) {
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: &fakeAgent{}, SessionFile: "/srv/session.jsonl"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	agent := loop.Client.Agent()
	if agent.SkillsSupported() {
		t.Fatal("an engine with no skill doors advertised them")
	}
	if held := agent.AttachSkills("pdf"); len(held) != 0 {
		t.Fatalf("an engine with no doors took %v", held)
	}
	if _, err := agent.SkillFacts(store.FactActive, 50); err == nil {
		t.Fatal("an engine with no shelf answered one")
	}
}

// The actual engine, rather than only a fixture, must expose every door.
var _ skillDoor = (*session.Agent)(nil)
