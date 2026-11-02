package main

import (
	"os"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
)

// AN ERRAND REACHES A CONVERSATION THE APP JOINED ON ITS HOST. After the app
// restarts, a job's chat that an engine host still holds is joined there
// (appJoinExisting), not opened again; the brief writer's errand must run on
// that host's agent like on one the app opened itself.
func TestAnErrandReachesAConversationJoinedOnItsHost(t *testing.T) {
	b, workspace := organizeProductionBackend(t)
	agents, err := b.ListAgents("")
	if err != nil || len(agents) == 0 {
		t.Fatalf("agents %v (%v)", agents, err)
	}
	id := agents[0].ID
	file := b.agents[id].transcript
	stopped := make(chan error, 1)
	go func() {
		stopped <- enginehost.Run(workspace, enginehost.Options{Boot: func(hello remote.Hello) (*remote.Engine, error) {
			proc, err := openAppEngineProcess()
			if err != nil {
				return nil, err
			}
			launch, err := openV3Launch(proc, v3Options{Workspace: workspace, Model: "test/model", Session: file, OneModel: true})
			if err != nil {
				return nil, err
			}
			agent, err := session.New(launch.Config)
			if err != nil {
				return nil, err
			}
			proc.track(agent)
			return &remote.Engine{Agent: agent, Workspace: workspace, SessionFile: file}, nil
		}})
	}()
	t.Cleanup(func() {
		_, _ = enginehost.Stop(workspace)
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Error("host did not stop")
		}
	})
	waitEnvironmentHost(t, workspace, func(self remote.HostSelf) bool { return self.PID == os.Getpid() })
	client := environmentWindow(t, workspace, file)
	defer client.Close()
	if _, err := b.OpenAgent(id); err != nil {
		t.Fatalf("join: %v", err)
	}
	var joined bool
	b.locked(func() {
		joined = b.agents[id].live != nil && !b.agents[id].live.conn.Assembled && !b.agents[id].live.conn.Lent
	})
	if !joined {
		t.Fatal("the conversation was not joined on its host")
	}
	ask, err := session.ErrandAsk{Purpose: briefPurpose, Role: string(roles.RoleBrief), System: "write json",
		Messages: []session.ErrandMessage{{Role: "user", Text: "the job"}}, JSON: true, Deadline: 20 * time.Second}.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	got, err := b.Errand(t.Context(), id, ask)
	if err != nil || got.Reason != "" || got.Text == "" {
		t.Fatalf("an errand on a joined conversation: %+v %v", got, err)
	}
}
