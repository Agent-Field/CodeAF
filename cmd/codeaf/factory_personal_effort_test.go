package main

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestPersonalFactoryManagerRecoversEffort(t *testing.T) {
	for _, door := range []string{"factory.job_send", "manager agents.send", "completed stage", "previous stage round", "ordinary"} {
		t.Run(door, func(t *testing.T) {
			s := newLocalSetup(t)
			st := s.start()
			floor := s.backend.local.host.floor
			server, requests := refusingFactoryEffort(t)
			var opened *session.Agent
			s.backend.open = func(ask appOpenAsk) (*appConn, error) {
				a, err := session.New(session.Config{Workspace: ask.Workspace, SessionFile: ask.Transcript, ProfileDir: t.TempDir(), Model: "fixture/manager", APIKey: "fixture", BaseURL: server.URL, Effort: effort.High, OneModel: true, DeterministicNames: true, JobChat: jobChatDoorOf(floor), SupportsParameter: func(string, string) (bool, bool) { return true, true }})
				if err != nil {
					return nil, err
				}
				opened = a
				return &appConn{Conv: a, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() { _ = a.Close() }}, nil
			}
			it, err := floor.Create(factory.Item{Repo: "acme/api", Num: 72, Kind: factory.KindIssue, Origin: factory.OriginForge, State: factory.StateNew, Effort: "high"})
			if err != nil {
				t.Fatal(err)
			}
			if door == "factory.job_send" {
				var sent appwire.FactoryJobSent
				s.must("factory.job_send", map[string]any{"factory": st.Factory, "job": it.ID, "text": "Please check the ledger"}, &sent)
			} else {
				var id string
				switch door {
				case "manager agents.send":
					makeJobManager(t, s, it.ID)
					var chats appwire.FactoryJobChats
					s.must("factory.job_chats", map[string]any{"factory": st.Factory, "job": it.ID}, &chats)
					id = chats.Agent
					if err := s.backend.MoveToClosed([]string{id}); err != nil {
						t.Fatal(err)
					}
				case "completed stage", "previous stage round":
					chat, stageID := jobConversation(t)
					id = stageID
					phase := factory.Phase{Name: "review", Kind: factory.StageChat, State: factory.PhaseDone, Round: 2, Chat: chat}
					if door == "previous stage round" {
						phase.Chat, _ = jobConversation(t)
						phase.Rounds = []factory.RoundLine{{Round: 1, Chat: chat}, {Round: 2, Chat: phase.Chat}}
					}
					if err := floor.Update(it.ID, func(it *factory.Item) error {
						it.State = factory.StateShipped
						it.Stream = &factory.Stream{Phases: []factory.Phase{phase}}
						return nil
					}); err != nil {
						t.Fatal(err)
					}
				case "ordinary":
					_, id = jobConversation(t)
				}
				s.must("agents.send", map[string]any{"id": id, "text": "Please check the ledger"}, nil)
			}
			deadline := time.Now().Add(5 * time.Second)
			for len(requests()) == 0 && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if opened == nil {
				t.Fatal("conversation not opened")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			if err := opened.WaitIdle(ctx); err != nil {
				t.Fatal(err)
			}
			got := requests()
			if door == "ordinary" {
				if len(got) != 1 || !got[0] {
					t.Fatalf("ordinary request knobs=%v; want [true]", got)
				}
				return
			}
			if len(got) != 2 || !got[0] || got[1] {
				t.Fatalf("Factory request knobs=%v; want [true false]", got)
			}
			found := false
			for _, entry := range opened.Transcript() {
				if entry.Text == "done" {
					found = true
				}
			}
			if !found {
				t.Fatal("Factory manager did not answer done")
			}
		})
	}
}
