package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// A queued team question is not a person's Start, including when opening the
// manager in a new engine delivers it. The model deliberately tries the same
// factory_start call that restarted job 64 after its person's Stop.
func TestFactoryStopIsStopQueuedManagerQuestion(t *testing.T) {
	for _, restart := range []bool{false, true} {
		name := "same_engine"
		if restart {
			name = "restart"
		}
		t.Run(name, func(t *testing.T) {
			s, status := newPauseSetup(t)
			floor := s.backend.local.host.floor
			workspace := t.TempDir()
			gitCheckout(t, workspace, "acme/api")
			if err := floor.SetCheckout("acme/api", workspace); err != nil {
				t.Fatal(err)
			}
			item, err := floor.Create(factory.Item{
				Repo: "acme/api", Num: 128, Title: "Stop stays stopped", Kind: factory.KindIssue,
				State: factory.StateNeedsYou, Starter: "owner", Autonomy: factory.AutonomyAuto, Cap: 10,
				Question: "Which test command should I run?", QKind: "step",
				Asking: &factory.Asked{Stage: "test", With: factory.AskedYou, Question: "Which test command should I run?", At: time.Now()},
				Stages: []factory.Stage{{Name: "test", Kind: factory.StageChat, On: true, Until: factory.UntilDone}},
				Stream: &factory.Stream{Phases: []factory.Phase{{Name: "test", Kind: factory.StageChat, State: factory.PhaseWaiting, Round: 1}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.backend.local.host.seam.Talk(context.Background(), item.ID); err != nil {
				t.Fatal(err)
			}
			item, err = floor.Get(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			profile := s.backend.localProfile()
			room, err := factoryItemTeam(profile, item)
			if err != nil {
				t.Fatal(err)
			}
			member := filepath.Join(t.TempDir(), "transcript.jsonl")
			if err := os.WriteFile(member, nil, 0600); err != nil {
				t.Fatal(err)
			}
			if err := teams.Update(profile, func(f *teams.File) error {
				return f.AddMember(room, teams.Member{Key: member, File: member, Handle: "test", Word: "test"})
			}); err != nil {
				t.Fatal(err)
			}
			read := func(w *factoryWire) api.Job {
				var job api.Job
				w.must("factory.job", map[string]any{"factory": status.Factory, "job": item.ID}, &job)
				return job
			}
			before := read(s.factoryWire)
			s.must("factory.job_stop", map[string]any{"factory": status.Factory, "job": item.ID, "expect": map[string]any{"state": before.State, "version": before.Version}}, &appwire.FactoryJobActed{})
			stopped := read(s.factoryWire)
			if stopped.Failure == nil || stopped.Failure.Code != factory.CodeStopped {
				t.Fatalf("Stop did not record Stopped by you: %+v", stopped)
			}
			history, err := record.Open(floor, record.Options{})
			if err != nil {
				t.Fatal(err)
			}
			events, err := history.Since(context.Background(), time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			stopRecorded := false
			for _, e := range events {
				stopRecorded = stopRecorded || e.Job == item.ID && e.Kind == record.Stopped && e.Handle == "owner"
			}
			if !stopRecorded {
				t.Fatal("person's Stop has no durable receipt")
			}
			wire := s.factoryWire
			if restart {
				s.backend.Close()
				wire, _ = reopened(t, s.backend.factoryHub())
			}
			var calls atomic.Int32
			t.Cleanup(func() {
				if got := calls.Load(); got != 0 {
					t.Errorf("Stop allowed %d automatic model calls; want zero", got)
				}
			})
			toolResult := make(chan string, 1)
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var req recordedRequest
				if json.NewDecoder(r.Body).Decode(&req) != nil {
					http.Error(w, "invalid fake model request", http.StatusBadRequest)
					return
				}
				startBelt, answered := false, false
				for _, tool := range req.Tools {
					startBelt = startBelt || tool.Function.Name == "factory_start"
				}
				for _, msg := range req.Messages {
					if msg.Role == "tool" {
						answered = true
						var line string
						_ = json.Unmarshal(msg.Content, &line)
						select {
						case toolResult <- line:
						default:
						}
					}
				}
				delta, finish := map[string]any{"content": "done"}, "stop"
				if startBelt && !answered {
					delta = map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "stop-start", "type": "function", "function": map[string]string{"name": "factory_start", "arguments": "{}"}}}}
					finish = "tool_calls"
				} else if !startBelt && len(req.Tools) > 0 {
					select {
					case toolResult <- "start is unavailable on this wake":
					default:
					}
				}
				chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta}}})
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: %s\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\ndata: [DONE]\n\n", chunk, finish)
			}))
			t.Cleanup(model.Close)
			wire.backend.open = func(ask appOpenAsk) (*appConn, error) {
				host := wire.backend.local.host
				agent, err := session.New(session.Config{Workspace: ask.Workspace, SessionFile: ask.Transcript, ProfileDir: profile, TeamListeningSince: ask.TeamListeningSince, Model: "test/model", APIKey: "test-key", BaseURL: model.URL, OneModel: true, DeterministicNames: true, FactoryRun: runDoor(host.floor, ask.Workspace, profile)})
				if err != nil {
					return nil, err
				}
				loop, err := remote.Loopback(remote.Hello{Version: remote.Version}, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) { return &remote.Engine{Agent: agent}, nil }})
				if err != nil {
					agent.Close()
					return nil, err
				}
				return &appConn{Conv: loop.Client.Agent(), Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() { loop.Close(); agent.Close() }}, nil
			}
			// Reads restore the local factory and open its manager; no person sends
			// a message or presses Start after Stop.
			if got := read(wire); got.Failure == nil || got.Failure.Code != factory.CodeStopped {
				t.Fatalf("restart alone erased Stop: %+v", got)
			}
			wire.must("factory.job_chats", map[string]any{"factory": status.Factory, "job": item.ID}, &appwire.FactoryJobChats{})
			// Deliver the pending question traffic after Stop, as a delayed team
			// notification can arrive after its runner has been cancelled.
			if err := teams.AppendTraffic(profile, room, teams.Entry{Kind: teams.KindQuestion, From: "test", To: teams.ToManager, Text: item.Question}); err != nil {
				t.Fatal(err)
			}
			if err := teams.AppendTraffic(profile, room, teams.Entry{Kind: teams.KindEvent, From: "test", To: teams.ToManager, State: teams.StateAsking, Text: "asks: " + item.Question}); err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-toolResult:
				job := read(wire)
				t.Logf("automatic manager factory_start result=%q; state=%s", result, job.State)
				if job.State == api.StateQueued || job.State == api.StateRunning || strings.Contains(result, " started") {
					t.Fatalf("run started after person's Stop without a person's Start: state=%s, factory_start=%q", job.State, result)
				}
			case <-time.After(12 * time.Second):
				job := read(wire)
				if job.State == api.StateQueued || job.State == api.StateRunning {
					t.Fatalf("run started after person's Stop: state=%s", job.State)
				}
				traffic, err := teams.ReadTraffic(profile, room, "", 0)
				if err != nil {
					t.Fatal(err)
				}
				for _, e := range traffic {
					t.Logf("team traffic: kind=%s state=%s text=%q", e.Kind, e.State, e.Text)
					if strings.Contains(e.Text, "could not wake") && strings.Contains(e.Text, "stopped") {
						return
					}
				}
				t.Fatal("reproduction inconclusive: queued question neither completed a manager turn nor acknowledged Stop")
			}
		})
	}
}
