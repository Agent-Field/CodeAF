package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// A JOB'S CONVERSATIONS ARE ITS OWN PAGE'S. Reading them makes the job's agent
// once (the same one on every read after), keeps it on the job, holds it open
// for the app, lists no step that has not run, and starts nothing; another
// factory's chats are not this engine's to answer.
func TestAJobsChatsAreItsAgentMadeOnceAndItsStepsThatRan(t *testing.T) {
	s := newLocalSetup(t)
	// The app opens a conversation through its opener; this one holds a fake.
	s.backend.open = func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	st := s.start()
	floor := s.backend.local.host.floor
	it, err := floor.Create(factory.Item{Repo: "acme/api", Num: 7, Title: "Fix the ledger", Kind: factory.KindIssue, State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	var first appwire.FactoryJobChats
	s.must("factory.job_chats", map[string]any{"factory": st.Factory, "job": it.ID}, &first)
	if first.Agent == "" || len(first.Steps) != 0 {
		t.Fatalf("first read: %+v", first)
	}
	kept, err := floor.Get(it.ID)
	if err != nil || strings.TrimSpace(kept.Talk) == "" || appAgentID(kept.Talk) != first.Agent {
		t.Fatalf("the job does not keep its agent: %+v %v", kept.Talk, err)
	}
	if !s.backend.isHeld(first.Agent) {
		t.Fatal("the job's agent is not held open for the app")
	}
	var again appwire.FactoryJobChats
	s.must("factory.job_chats", map[string]any{"factory": st.Factory, "job": it.ID}, &again)
	if again.Agent != first.Agent {
		t.Fatalf("a second read made another agent: %s then %s", first.Agent, again.Agent)
	}
	if kept.State != factory.StateNew {
		t.Fatalf("reading the chats started the job: %q", kept.State)
	}
	s.mustFail("factory.job_chats", map[string]any{"factory": "f-elsewhere", "job": it.ID}, appwire.CodeNotFound)
}
