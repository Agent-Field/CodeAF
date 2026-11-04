package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/worker"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestAppFactoryManagerGateSendBack(t *testing.T) {
	t.Run("every_waiting_gate_has_an_exact_identity", func(t *testing.T) {
		for _, kind := range []string{"step", factory.QKindApprove, factory.QKindAskFirst, "cap", "gate", "plan", "scope", factory.QKindOutOfAttempts} {
			it := planJob("owner")
			it.ID, it.QKind = 7, kind
			it.Asking = &factory.Asked{Stage: "approve", With: factory.AskedYou, At: time.Unix(100, 0)}
			status := factory.StatusForManager(it, false, time.Now())
			if status.QuestionID == "" || !strings.Contains(status.Text, status.QuestionID) {
				t.Errorf("%s gate has no exact identity", kind)
			}
			it.Stream.Spent += 0.1
			if factory.PendingQuestionID(it) != status.QuestionID {
				t.Errorf("%s polling changed the identity", kind)
			}
			if kind != "step" {
				it.Stream.Phases[0].Round++
				if factory.PendingQuestionID(it) == status.QuestionID {
					t.Errorf("%s re-used the old plan decision", kind)
				}
			}
		}
	})
	for _, scenario := range []string{"send_back", "wrong_id", "wrong_gate"} {
		t.Run(scenario, func(t *testing.T) {
			wrong := scenario == "wrong_id"
			s := newLocalSetup(t)
			local := s.start()
			floor := s.backend.local.host.floor
			if err := floor.SetRepos([]string{"acme/api"}); err != nil {
				t.Fatal(err)
			}
			const words = "do not change README, change a new file NOTES.md instead"
			var mu sync.Mutex
			var status, result string
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req recordedRequest
				if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&req) != nil {
					http.NotFound(w, r)
					return
				}
				var replies []string
				for _, msg := range req.Messages {
					if msg.Role == "tool" {
						var text string
						_ = json.Unmarshal(msg.Content, &text)
						replies = append(replies, text)
					}
				}
				name, args, finish := "factory_status", `{}`, "tool_calls"
				delta := map[string]any{}
				switch len(replies) {
				case 0:
				case 1:
					mu.Lock()
					status = replies[0]
					mu.Unlock()
					_, id, _ := strings.Cut(replies[0], "Pending question ID: ")
					id, _, _ = strings.Cut(id, ".")
					if wrong {
						id = "outdated-question"
					}
					name = "factory_answer"
					body, _ := json.Marshal(map[string]string{"question_id": id, "answer": words, "action": "reject_plan"})
					args = string(body)
				default:
					mu.Lock()
					result = replies[len(replies)-1]
					mu.Unlock()
					delta["content"], finish = "The action result has been checked.", "stop"
				}
				if finish == "tool_calls" {
					delta["tool_calls"] = []any{map[string]any{"index": 0, "id": fmt.Sprintf("gate-%d", len(replies)), "type": "function", "function": map[string]string{"name": name, "arguments": args}}}
				}
				w.Header().Set("Content-Type", "text/event-stream")
				chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta}}})
				fmt.Fprintf(w, "data: %s\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\ndata: [DONE]\n\n", chunk, finish)
			}))
			t.Cleanup(model.Close)
			s.backend.open = func(ask appOpenAsk) (*appConn, error) {
				engine, err := session.New(session.Config{Workspace: ask.Workspace, SessionFile: ask.Transcript, ProfileDir: s.backend.localProfile(), Model: "test/model", APIKey: "test-key", BaseURL: model.URL, OneModel: true, DeterministicNames: true, FactoryRun: runDoor(floor, ask.Workspace, s.backend.localProfile())})
				if err != nil {
					return nil, err
				}
				loop, err := remote.Loopback(remote.Hello{Version: remote.Version}, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) { return &remote.Engine{Agent: engine}, nil }})
				if err != nil {
					engine.Close()
					return nil, err
				}
				return &appConn{Conv: loop.Client.Agent(), Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() { loop.Close(); engine.Close() }}, nil
			}
			item := planJob("owner")
			item.WorktreeMachine = "test-worker"
			if scenario == "wrong_gate" {
				item.QKind = "cap"
			}
			item.Repo, item.Num, item.Kind, item.Title = "acme/api", 7, factory.KindIssue, "Revise the plan"
			item.Asking = &factory.Asked{Stage: "approve", With: factory.AskedYou, Question: item.Question, At: time.Now()}
			item.Stages = []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true, Until: factory.UntilDone}, {Name: "approve", Kind: factory.StageGate, On: true}, {Name: "build", Kind: factory.StageChat, On: true, Until: factory.UntilDone}}
			job, err := floor.Create(item)
			if err != nil {
				t.Fatal(err)
			}
			makeJobManager(t, s, job.ID)
			var chats appwire.FactoryJobChats
			s.must("factory.job_chats", map[string]any{"factory": local.Factory, "job": job.ID}, &chats)
			read := func() api.Job {
				var j api.Job
				s.must("factory.job", map[string]any{"factory": local.Factory, "job": job.ID}, &j)
				return j
			}
			before, _ := json.Marshal(read())
			s.must("agents.send", map[string]any{"id": chats.Agent, "text": words}, nil)
			lendWaitFor(t, "manager tool results", func() bool { mu.Lock(); defer mu.Unlock(); return result != "" })
			mu.Lock()
			gotStatus, gotResult := status, result
			mu.Unlock()
			if scenario != "send_back" {
				if !strings.Contains(gotResult, "factory_status") || !strings.Contains(gotResult, "Nothing changed") || !strings.Contains(gotResult, "requested action did not happen") || strings.Contains(gotResult, "Action not confirmed") {
					t.Fatalf("refusal lacks recovery and honest outcome: %s", gotResult)
				}
				after, _ := json.Marshal(read())
				if string(after) != string(before) {
					t.Fatalf("refused action changed the job: %s", after)
				}
				return
			}
			if !strings.Contains(gotStatus, "Pending question ID: ") {
				t.Fatalf("factory_status omitted the approve gate question id: %s", gotStatus)
			}
			revised := read()
			if revised.Steps[0].Round != 2 || revised.Steps[0].State != "pending" || revised.State != api.StateQueued {
				t.Fatalf("plan did not go back: %s; result: %s", mustJSON(t, revised), gotResult)
			}
			// A real worker pulls the revised job and reports the next gate to the app.
			mirror, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			brief := make(chan factoryrun.Job, 1)
			runner := factoryrun.New(factoryrun.Options{Store: mirror, Exec: map[factory.StageKind]factoryrun.Executor{factory.StageChat: factoryrun.ExecutorFunc(func(_ context.Context, j factoryrun.Job) (factory.StageResult, error) {
				brief <- j
				return factory.StageResult{Done: true, Output: "The plan uses NOTES.md."}, nil
			})}})
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				defer close(done)
				credential := ""
				worker.Run(ctx, worker.Options{
					Dial: func(int) *client.WorkerClient {
						return client.Open(local.URL, s.backend.local.host.token).Worker(credential)
					},
					Keep:   func(_ int, w *client.WorkerClient) { credential = w.Credential() },
					Attach: api.WorkerAttach{Machine: "test-worker", Name: "test worker", Handle: "owner"},
					Runner: runner, Mirror: mirror, Slots: 1, ReportEvery: 20 * time.Millisecond, BeatEvery: 20 * time.Millisecond,
				})
			}()
			t.Cleanup(func() { cancel(); <-done })
			select {
			case j := <-brief:
				if j.Round != 2 || !slices.Contains(j.Notes, words) {
					t.Fatalf("round 2 brief lost the words: round %d notes %q", j.Round, j.Notes)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("plan did not re-run")
			}
			lendWaitFor(t, "fresh approval after plan round 2", func() bool {
				j := read()
				return j.State == api.StateNeedsYou && j.Steps[0].State == "done" && j.Steps[0].Round == 2 && j.Steps[1].State == "waiting"
			})
		})
	}
}
