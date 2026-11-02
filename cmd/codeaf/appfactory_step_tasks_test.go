package main

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// A STEP TRY'S TASKS ARE LISTED THROUGH THAT TRY'S OWN AGENT. Each try of a
// step is its own conversation, which factory.job_chats names as an agent
// (steps[].agent for the latest, steps[].rounds[].agent for each finished
// one), and tasks.list on that agent answers that conversation's task list:
// the job, the step and the try are the agent, so no task carries a second
// grouping for them. A try whose agent has no tasks answers an empty list,
// which the job page shows as "No task list for this step".
func TestAStepTrysTasksAreListedThroughItsOwnAgent(t *testing.T) {
	s := newLocalSetup(t)
	s.backend.open = func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	st := s.start()
	first, firstID := jobConversation(t)
	latest, latestID := jobConversation(t)
	t0 := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	it, err := s.backend.local.host.floor.Create(factory.Item{Repo: "acme/api", Num: 8, Title: "Review it", Kind: factory.KindIssue, State: factory.StateRunning,
		Stream: &factory.Stream{Phases: []factory.Phase{
			{Name: "review", Kind: factory.StageChat, State: factory.PhaseDone, Round: 2, Chat: latest, Started: t0.Add(5 * time.Minute), At: t0.Add(8 * time.Minute),
				Rounds: []factory.RoundLine{
					{Round: 1, Started: t0, At: t0.Add(4 * time.Minute), Chat: first},
					{Round: 2, Started: t0.Add(5 * time.Minute), At: t0.Add(8 * time.Minute), Chat: latest},
				}},
		}}})
	if err != nil {
		t.Fatal(err)
	}
	var chats appwire.FactoryJobChats
	s.must("factory.job_chats", map[string]any{"factory": st.Factory, "job": it.ID}, &chats)
	if len(chats.Steps) != 1 || len(chats.Steps[0].Rounds) != 2 {
		t.Fatalf("the job's steps read %+v", chats.Steps)
	}
	agents := []string{chats.Steps[0].Agent}
	for _, r := range chats.Steps[0].Rounds {
		agents = append(agents, r.Agent)
	}
	if agents[0] != latestID || agents[1] != firstID || agents[2] != latestID {
		t.Fatalf("the tries' agents read %v", agents)
	}
	for _, agent := range agents {
		var got appwire.TasksResult
		s.must("tasks.list", map[string]any{"agent": agent}, &got)
		if got.Tasks == nil || len(got.Tasks) != 0 {
			t.Fatalf("tasks.list on try agent %s = %+v", agent, got.Tasks)
		}
	}
}
