package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
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
	listed, err := s.backend.ListAgents("")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range listed {
		if a.ID == first.Agent {
			t.Fatal("job manager leaked into Agents")
		}
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

// THE FACTORY ON THIS MAC IS NAMED FOR WHOSE IT IS, AND A PERSON MAY RENAME IT.
// Once it knows whose GitHub it is the list says `<login>'s factory`; a name
// the person gives it wins and is kept across a relaunch; an empty name goes
// back to the default; an unknown factory is not found.
func TestThisMacsFactoryIsNamedForItsPersonAndCanBeRenamed(t *testing.T) {
	s := newLocalSetup(t)
	st := s.start()
	name := func(w *factoryWire) string {
		var list appwire.FactoryList
		w.must("factory.list", struct{}{}, &list)
		for _, f := range list.Factories {
			if f.ID == st.Factory {
				return f.Name
			}
		}
		return ""
	}
	if got := name(s.factoryWire); got != "owner's factory" {
		t.Fatalf("default name = %q", got)
	}
	s.must("factory.rename", map[string]any{"factory": st.Factory, "name": "  Ada's   desk "}, &struct{}{})
	if got := name(s.factoryWire); got != "Ada's desk" {
		t.Fatalf("renamed = %q", got)
	}
	s.backend.Close()
	w, _ := reopened(t, s.backend.factoryHub())
	if got := name(w); got != "Ada's desk" {
		t.Fatalf("after a relaunch = %q", got)
	}
	w.must("factory.rename", map[string]any{"factory": st.Factory, "name": ""}, &struct{}{})
	if got := name(w); got == "Ada's desk" || got == "" {
		t.Fatalf("back to the default = %q", got)
	}
	w.mustFail("factory.rename", map[string]any{"factory": "f-nowhere", "name": "x"}, appwire.CodeNotFound)
}

// A STEP THAT RAN MORE THAN ONE ROUND NAMES EACH FINISHED ROUND'S CHAT, as an
// agent the app opens like the step's own, held open for it; a round with no
// chat (a check's) or one whose chat cannot be opened is left out, as a step's
// would be. The job's read never names them (serve's withholdChats).
func TestAJobsChatsNameEachFinishedRoundsChat(t *testing.T) {
	s := newLocalSetup(t)
	s.backend.open = func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	st := s.start()
	first, firstID := jobConversation(t)
	latest, latestID := jobConversation(t)
	t0 := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	floor := s.backend.local.host.floor
	it, err := floor.Create(factory.Item{Repo: "acme/api", Num: 8, Title: "Review it", Kind: factory.KindIssue, State: factory.StateRunning,
		Stream: &factory.Stream{Phases: []factory.Phase{
			{Name: "review", Kind: factory.StageChat, State: factory.PhaseDone, Round: 3, Chat: latest, Started: t0.Add(9 * time.Minute), At: t0.Add(12 * time.Minute),
				Rounds: []factory.RoundLine{
					{Round: 1, Started: t0, At: t0.Add(4 * time.Minute), Chat: first},
					{Round: 2, Started: t0.Add(5 * time.Minute), At: t0.Add(8 * time.Minute), Chat: "/nowhere/gone-round/transcript.jsonl"},
					{Round: 3, Started: t0.Add(9 * time.Minute), At: t0.Add(12 * time.Minute), Chat: latest},
				}},
			{Name: "test", Kind: factory.StageCheck, State: factory.PhaseRunning, Round: 1, Started: t0.Add(13 * time.Minute),
				Rounds: []factory.RoundLine{{Round: 1, Started: t0.Add(13 * time.Minute), At: t0.Add(14 * time.Minute)}}},
		}}})
	if err != nil {
		t.Fatal(err)
	}
	var chats appwire.FactoryJobChats
	s.must("factory.job_chats", map[string]any{"factory": st.Factory, "job": it.ID}, &chats)
	if len(chats.Steps) != 1 || chats.Steps[0].Step != "review" || chats.Steps[0].Agent != latestID {
		t.Fatalf("the job's steps read %+v", chats.Steps)
	}
	want := []appwire.FactoryJobRoundChat{{Round: 1, Agent: firstID}, {Round: 3, Agent: latestID}}
	if got := chats.Steps[0].Rounds; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("the review's rounds read %+v, want %+v", got, want)
	}
	if !s.backend.isHeld(firstID) {
		t.Fatal("an earlier round's chat is not held open for the app")
	}
}

func TestFinishedStageChatKeepsItsExecutionModel(t *testing.T) {
	s := newLocalSetup(t)
	s.backend.open = func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{model: "profile/default"}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	st := s.start()
	transcript, id := jobConversation(t)
	it, err := s.backend.local.host.floor.Create(factory.Item{Repo: "acme/api", Num: 9, Title: "Fix it", Kind: factory.KindIssue, State: factory.StateNeedsYou,
		Stream: &factory.Stream{Phases: []factory.Phase{{Name: "plan", Kind: factory.StageChat, State: factory.PhaseDone, Chat: transcript, Result: &factory.StageResult{Model: "vendor/strong"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	var chats appwire.FactoryJobChats
	s.must("factory.job_chats", map[string]any{"factory": st.Factory, "job": it.ID}, &chats)
	a, err := s.backend.find(id)
	if err != nil {
		t.Fatal(err)
	}
	view, _, err := s.backend.ensure(a)
	if err != nil || view.Model != "vendor/strong" {
		t.Fatalf("model=%s err=%v", view.Model, err)
	}
}

// A read is never permission to reinstate an old job default or execution model.
func TestFactoryChatRefreshKeepsThePersonsModelPair(t *testing.T) {
	s := newLocalSetup(t)
	s.backend.open = func(ask appOpenAsk) (*appConn, error) {
		c := &appPairConversation{fakeConversation: &fakeConversation{model: "prior/main"}, pair: session.ConversationModels{Main: "prior/main", Fallback: "prior/backup"}}
		return &appConn{Conv: c, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	st := s.start()
	floor := s.backend.local.host.floor
	step, _ := jobConversation(t)
	it, err := floor.Create(factory.Item{Repo: "acme/api", Kind: factory.KindIssue, Num: 9, State: factory.StateRunning, Model: "prior/main", Stream: &factory.Stream{Phases: []factory.Phase{{Name: "write", Kind: factory.StageChat, Chat: step, State: factory.PhaseDone, Result: &factory.StageResult{Model: "prior/execution"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	var chats appwire.FactoryJobChats
	read := func() { s.must("factory.job_chats", map[string]any{"factory": st.Factory, "job": it.ID}, &chats) }
	read()
	ids := []string{chats.Agent, chats.Steps[0].Agent}
	for _, id := range ids {
		if _, err := s.backend.SetConversationModels(id, session.ConversationModels{Main: "chosen/main", Fallback: "chosen/backup"}); err != nil {
			t.Fatal(err)
		}
	}
	read()
	read()
	for _, id := range ids {
		a, err := s.backend.find(id)
		if err != nil {
			t.Fatal(err)
		}
		if a.view.ModelIdentity == nil || a.view.ModelIdentity.Models == nil || a.view.ModelIdentity.Models.Main != "chosen/main" || a.view.ModelIdentity.Models.Fallback != "chosen/backup" {
			t.Fatalf("chat refresh overwrote an explicit model pair: %+v", a.view.ModelIdentity)
		}
	}
}
