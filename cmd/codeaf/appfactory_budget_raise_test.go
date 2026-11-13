package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	factoryworker "github.com/Agent-Field/codeaf/internal/factory/worker"
)

type budgetModel struct {
	maker factoryrun.FakeMaker
	spent float64
}

type budgetConversation struct {
	factoryrun.Conversation
	spent float64
}

func (c budgetConversation) Spent() float64 { return c.spent }

func (m *budgetModel) Open(ctx context.Context, spec factoryrun.ConversationSpec) (factoryrun.Conversation, error) {
	c, err := m.maker.Open(ctx, spec)
	spent := m.spent
	if spec.Resume != "" {
		spent = .25
	}
	return budgetConversation{Conversation: c, spent: spent}, err
}

// A budget edit answers the spending question through the same app door on
// both runner roads. The scripted model stops mid-step, keeping its chat.
func TestAppFactoryBudgetRaiseResumes(t *testing.T) {
	for _, leased := range []bool{false, true} {
		for _, mode := range []string{factory.AutonomyAuto, factory.AutonomyManual} {
			for _, budget := range []float64{10, 5, 20, 0} {
				t.Run(fmt.Sprintf("leased=%v/%s/budget=%g", leased, mode, budget), func(t *testing.T) {
					s := newLocalSetup(t)
					t.Setenv("OPENROUTER_API_KEY", "test-key")
					t.Setenv("CODEAF_BASE_URL", newStageModel(t).server.URL)
					s.backend.open = func(ask appOpenAsk) (*appConn, error) {
						return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
					}
					st, err := store.Open(v3FactoryRoot())
					if err != nil {
						t.Fatal(err)
					}
					if err := st.SetCheckout("acme/api", t.TempDir()); err != nil {
						t.Fatal(err)
					}
					release, ok := st.TryWorker()
					if !ok {
						t.Fatal("fixture could not own the runner")
					}
					defer release()
					mirror := st
					if leased {
						mirror, err = store.Open(filepath.Join(t.TempDir(), "mirror"))
						if err != nil {
							t.Fatal(err)
						}
					}
					transcript, _ := jobConversation(t)
					var calls atomic.Int32
					resumed := make(chan factoryrun.Job, 2)
					finish := make(chan struct{})
					spent := 5.25
					if budget == 20 {
						spent = 19.5
					}
					model := &budgetModel{spent: spent}
					model.maker.Script = func(ctx context.Context, c *factoryrun.FakeConversation) {
						if c.Spec.Resume == "" {
							c.Fail(factoryrun.SpendStop{Job: true, Said: "budget"})
							return
						}
						select {
						case <-finish:
						case <-ctx.Done():
							return
						}
						if err := c.Spec.Stage.Report(ctx, factory.StageResult{Done: true}); err != nil {
							c.Fail(err)
						}
					}
					chat := factoryrun.NewChatExecutor(model)
					runner := factoryrun.New(factoryrun.Options{Store: mirror, Benches: api.DefaultRunnerSlots, Talk: sessionTalk{},
						Exec: map[factory.StageKind]factoryrun.Executor{factory.StageChat: factoryrun.ExecutorFunc(func(ctx context.Context, j factoryrun.Job) (factory.StageResult, error) {
							if calls.Add(1) > 1 {
								resumed <- j
							}
							return chat.Run(ctx, j)
						})}})
					hook := s.backend.local.seamHook
					s.backend.local.seamHook = func(seam factory.Seam) factory.Seam {
						seam = hook(seam)
						if !leased {
							seam.Launch, seam.Answer = runner.Launch, runner.Answer
						}
						return seam
					}
					cap := 1.0
					if budget == 0 {
						cap = 5
					}
					status := s.start()
					it, err := st.Create(factory.Item{Tier: factory.TierOwner, Title: "Resume capped write", Repo: "acme/api", Num: 99, Kind: factory.KindIssue,
						State: factory.StateNew, Cap: cap, SpentBefore: .5, Autonomy: mode, Talk: transcript,
						Stages: []factory.Stage{{Name: "write", Kind: factory.StageChat, Ask: "write it", Until: "done", Max: 1, On: true}}})
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = runner.Stop(it.ID) })
					if leased {
						ctx, cancel := context.WithCancel(context.Background())
						done := make(chan struct{})
						var mu sync.Mutex
						credentials := map[int]string{}
						go func() {
							defer close(done)
							factoryworker.Run(ctx, factoryworker.Options{
								Dial: func(slot int) *client.WorkerClient {
									mu.Lock()
									defer mu.Unlock()
									return client.Open(status.URL, s.backend.local.host.token).Worker(credentials[slot])
								},
								Keep: func(slot int, w *client.WorkerClient) {
									mu.Lock()
									defer mu.Unlock()
									credentials[slot] = w.Credential()
								},
								Attach: api.WorkerAttach{Machine: "budget-worker", Name: "Budget worker", Handle: "owner", TakesUnassigned: map[string]bool{lease.EveryRepo: true}},
								Runner: runner, Mirror: mirror, Slots: api.DefaultRunnerSlots, ReportEvery: 10 * time.Millisecond, BeatEvery: 10 * time.Millisecond,
							})
						}()
						t.Cleanup(func() { cancel(); <-done })
					}
					read := func() api.Job {
						var j api.Job
						s.must("factory.job", map[string]any{"factory": status.Factory, "job": it.ID}, &j)
						return j
					}
					act := func(method string, extra map[string]any) appwire.FactoryJobActed {
						j := read()
						params := map[string]any{"factory": status.Factory, "job": it.ID, "expect": map[string]any{"version": j.Version}}
						for k, v := range extra {
							params[k] = v
						}
						var out appwire.FactoryJobActed
						s.must(method, params, &out)
						return out
					}
					wait := func(what string, ok func(api.Job) bool) api.Job {
						deadline := time.Now().Add(5 * time.Second)
						for {
							j := read()
							if ok(j) {
								return j
							}
							if time.Now().After(deadline) {
								t.Fatalf("%s: state=%s budget=%g questions=%+v steps=%+v calls=%d", what, j.State, j.Budget, j.Questions, j.Steps, calls.Load())
							}
							time.Sleep(10 * time.Millisecond)
						}
					}
					act("factory.job_start", nil)
					wait("cap ask", func(j api.Job) bool { return j.SpendingLimit != nil })
					if budget == 0 {
						if err := client.Open(status.URL, s.backend.local.host.token).Seam().Answer(it.ID, true, factory.ChoiceContinue); err != nil {
							t.Fatal(err)
						}
					} else {
						act("factory.job_set_budget", map[string]any{"budget": budget})
					}
					if budget > 0 && budget <= spent+.5 {
						held := read()
						if held.Budget != budget || held.SpendingLimit == nil || calls.Load() != 1 {
							t.Fatalf("exhausted edit released work: %+v", held)
						}
						if len(held.Questions) == 0 || !strings.Contains(held.Questions[0].Text, fmt.Sprintf("$%g", budget)) || strings.Contains(held.Questions[0].Text, fmt.Sprintf("budget of $%g reached", cap)) {
							t.Fatalf("stale budget question: %+v", held.Questions)
						}
						return
					}
					select {
					case j := <-resumed:
						if j.Resume != "fake-1" || j.Round != 1 {
							t.Fatalf("resumed chat=%q round=%d", j.Resume, j.Round)
						}
					case <-time.After(5 * time.Second):
						j := read()
						t.Fatalf("budget raised but write did not resume: state=%s budget=%g questions=%+v calls=%d", j.State, j.Budget, j.Questions, calls.Load())
					}
					wait("cap ask cleared", func(j api.Job) bool {
						if j.SpendingLimit != nil {
							return false
						}
						for _, q := range j.Questions {
							if q.State == "open" {
								return false
							}
						}
						return true
					})
					close(finish)
					finished := wait("write finished", func(j api.Job) bool { return len(j.Steps) == 1 && j.Steps[0].State == "done" })
					entries, _, err := s.backend.Entries(appAgentID(transcript), "", 100)
					if err != nil {
						t.Fatal(err)
					}
					raised := 0
					for _, entry := range entries {
						if entry.Text == "budget raised to $10 · carrying on" {
							raised++
						}
					}
					if raised != 1 {
						t.Fatalf("budget continuation chat lines=%d, want 1", raised)
					}
					if finished.Cost != spent+.25 || finished.LifetimeCost != spent+.75 || calls.Load() != 2 {
						t.Fatalf("spend=%g calls=%d", finished.Cost, calls.Load())
					}
					logged := 0
					if finished.Desk != nil && finished.Desk.Run != nil {
						for _, line := range finished.Desk.Run.Log {
							if line.Text == "budget raised to $10 · carrying on" {
								logged++
							}
						}
					}
					if (!leased || budget != 0) && logged != 1 {
						t.Fatalf("budget continuation activity lines=%d, want 1", logged)
					}
				})
			}
		}
	}
}
