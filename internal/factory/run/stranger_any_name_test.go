package run

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/triage"
)

// recordingChat records which steps ran.
func recordingChat() (map[factory.StageKind]Executor, func() []string) {
	var mu sync.Mutex
	var ran []string
	exec := map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(_ context.Context, job Job) (factory.StageResult, error) {
			mu.Lock()
			defer mu.Unlock()
			ran = append(ran, job.Stage.Name)
			return factory.StageResult{Done: true}, nil
		}),
	}
	return exec, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), ran...)
	}
}

// strangerJob adds an item whose author is a stranger, with these stages.
func strangerJob(t *testing.T, g *rig, kind factory.Kind, stages []factory.Stage) int {
	t.Helper()
	id, err := g.st.Add(context.Background(), factory.Item{Title: "theirs", Repo: "acme/api", Kind: kind, Tier: factory.TierStranger, Stages: stages})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// Stranger jobs refuse conversation steps under every name.
func TestAJobFromSomeoneWithoutWriteAccessIsRefusedForAnyStepName(t *testing.T) {
	for _, name := range []string{"write", "draft", "implement", "change-it", "fix"} {
		t.Run(name, func(t *testing.T) {
			f, err := pipeline.Parse("pipeline:\n  issue: [" + name + "]\nsteps:\n  " + name + ": make the change in the checkout\n")
			if err != nil || len(f.Problems) != 0 {
				t.Fatalf("parse: %v %+v", err, f.Problems)
			}
			exec, ran := recordingChat()
			g := newRig(t, exec, nil)
			id := strangerJob(t, g, factory.KindIssue, f.Recipe().For(factory.KindIssue))
			if err := g.r.Launch(id); err != nil {
				return // refused: a stranger's words are not written from
			}
			g.wait(id, "any step run", func(factory.Item) bool { return len(ran()) > 0 })
			t.Fatalf("the job ran %v", ran())
		})
	}
}

// Switching one step off leaves the others that can write refused.
func TestRefusalHoldsWithOneStepSwitchedOff(t *testing.T) {
	stages := factory.DefaultRecipe().For(factory.KindIssue)
	for i := range stages {
		if stages[i].Name == "write" || factory.IsApprove(stages[i]) {
			stages[i].On = false
		}
	}
	exec, ran := recordingChat()
	g := newRig(t, exec, nil)
	id := strangerJob(t, g, factory.KindIssue, stages)
	if err := g.r.Launch(id); err != nil {
		return
	}
	g.wait(id, "test run", func(factory.Item) bool { return strings.Contains(strings.Join(ran(), ","), "test") })
	t.Fatalf("write was off and the job ran %v", ran())
}

// Plan edits remain subject to the same step capability rule.
func TestAStepAddedAfterStartIsRefused(t *testing.T) {
	var mu sync.Mutex
	var ran []string
	exec := map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(_ context.Context, job Job) (factory.StageResult, error) {
			mu.Lock()
			defer mu.Unlock()
			ran = append(ran, job.Stage.Name)
			res := done("")
			if job.Stage.Name == "plan" && job.Round == 1 {
				res.Edit = &factory.PlanEdit{Add: []factory.Stage{{Ask: "after plan, write: make the change in the checkout"}}, Why: "the issue asks for it"}
			}
			return res, nil
		}),
	}
	g := newRig(t, exec, nil)
	id := strangerJob(t, g, factory.KindIssue, []factory.Stage{{Name: "plan", Kind: factory.StageChat, Ask: "read it", Until: factory.UntilDone, Max: 1, On: true}, {Name: "proof", Kind: factory.StageChat, Ask: "show it", Until: factory.UntilProven, Max: 1, On: true}})
	if err := g.r.Launch(id); err != nil {
		return
	}
	g.wait(id, "two steps run", func(factory.Item) bool {
		mu.Lock()
		defer mu.Unlock()
		return len(ran) >= 2
	})
	mu.Lock()
	defer mu.Unlock()
	if strings.Contains(strings.Join(ran, ","), "write") {
		t.Fatalf("an added step ran: %v", ran)
	}
}

func TestStrangerLaunchAndQueueUseCapabilities(t *testing.T) {
	for _, name := range []string{"draft", "implement", "change-it", "fix", "review", "test", "plan", "x"} {
		for _, kind := range []factory.StageKind{factory.StageChat, factory.StageCheck, factory.StagePost, "", "unknown"} {
			t.Run(name+"/"+string(kind), func(t *testing.T) {
				g := newRig(t, nil, nil)
				id := strangerJob(t, g, factory.KindIssue, []factory.Stage{{Name: name, Kind: kind, On: true}})
				if err := g.r.Launch(id); err == nil || !strings.Contains(err.Error(), "without write access") {
					t.Errorf("launch refusal: %v", err)
				}
				it, _ := g.st.Get(id)
				if err := QueueItem(&it, g.r.now()); err == nil || !strings.Contains(err.Error(), "without write access") {
					t.Errorf("queue refusal: %v", err)
				}
			})
		}
	}
}

func TestStrangerQueueKeepsRunConditions(t *testing.T) {
	for _, tc := range []struct {
		name, when string
		on, refuse bool
	}{
		{"off", "", false, false},
		{"nonmatching", "large", true, false},
		{"deferred", "touches auth", true, true},
		{"unknown files", "has ui", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := factory.Item{Tier: factory.TierStranger, Triage: factory.Triage{Area: "docs"}, Stages: []factory.Stage{{Name: "review", Kind: factory.StageChat, On: tc.on, When: tc.when}}}
			err := QueueItem(&it, time.Now())
			if (err != nil) != tc.refuse {
				t.Fatalf("queue error = %v, refuse = %v", err, tc.refuse)
			}
		})
	}
}

func expectStrangerStopped(t *testing.T, g *rig, id int, calls *atomic.Int32, spent float64) factory.Item {
	t.Helper()
	it := g.wait(id, "refused without work", func(it factory.Item) bool {
		return it.Stream != nil && !it.Stream.Ended.IsZero()
	})
	if calls.Load() != 0 || it.Failure == nil || !strings.Contains(it.Failure.Detail, "without write access") || !strings.Contains(it.Failure.Detail, "new job") || it.State != factory.StateNew || it.Stream.Spent != spent || it.Stream.Paused {
		t.Fatalf("calls=%d state=%s failure=%+v stream=%+v", calls.Load(), it.State, it.Failure, it.Stream)
	}
	wire := api.FromItem(it)
	if wire.Failure == nil || wire.Failure.Detail != it.Failure.Detail {
		t.Fatalf("refusal missing from desk wire: %+v", wire.Failure)
	}
	return it
}

func TestStrangerRestartRefusesNextRound(t *testing.T) {
	for _, paused := range []bool{false, true} {
		t.Run(fmt.Sprint("paused=", paused), func(t *testing.T) {
			var calls atomic.Int32
			g := newRig(t, nil, nil)
			id := strangerJob(t, g, factory.KindIssue, []factory.Stage{{Name: "review", Kind: factory.StageChat, On: true}})
			if err := g.st.Update(id, func(it *factory.Item) error {
				it.State = factory.StateRunning
				it.Stream = &factory.Stream{Paused: paused, Spent: .25, Phases: []factory.Phase{{Name: "review", Kind: factory.StageChat, State: factory.PhaseRunning, Round: 2, Chat: "old-chat"}}}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			r2 := New(Options{Store: g.st, Exec: map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) {
				calls.Add(1)
				return done(""), nil
			})}})
			// A worker restores an in-flight job as paused before resuming it.
			if !paused {
				it, _ := g.st.Get(id)
				if err := PauseItem(&it, g.r.now()); err != nil {
					t.Fatal(err)
				}
				if err := g.st.Update(id, func(cur *factory.Item) error {
					*cur = it
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			if err := r2.Launch(id); err != nil {
				t.Fatal(err)
			}
			expectStrangerStopped(t, g, id, &calls, .25)
			if err := r2.Launch(id); err == nil {
				t.Fatal("refused job launched again")
			}
		})
	}
}

func TestStrangerSendBackAndReverifyRefuseRounds(t *testing.T) {
	for _, scenario := range []string{"send back", "reverify", "restored check"} {
		t.Run(scenario, func(t *testing.T) {
			reverify := scenario != "send back"
			kind := factory.StageCheck
			if scenario == "restored check" {
				kind = factory.StageGate
			}
			var calls atomic.Int32
			exec := ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) {
				calls.Add(1)
				return done(""), nil
			})
			g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: exec, factory.StageCheck: exec}, nil)
			id := strangerJob(t, g, factory.KindIssue, []factory.Stage{{Name: "test", Kind: kind, On: true}})
			if err := g.st.Update(id, func(it *factory.Item) error {
				it.State = factory.StateLanded
				it.Stream = &factory.Stream{Spent: .25, Phases: []factory.Phase{{Name: "test", Kind: factory.StageCheck, State: factory.PhaseDone, Round: 1}}}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			var err error
			if reverify {
				err = g.r.Reverify(id)
			} else {
				err = g.r.SendBack(id, "show the result")
			}
			if err != nil {
				t.Fatal(err)
			}
			expectStrangerStopped(t, g, id, &calls, .25)
		})
	}
}

func TestStrangerTierChangeRefusesRetryAndPlanAddedSteps(t *testing.T) {
	for _, plan := range []bool{false, true} {
		t.Run(fmt.Sprint("plan=", plan), func(t *testing.T) {
			var calls atomic.Int32
			var g *rig
			g = newRig(t, map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(_ context.Context, job Job) (factory.StageResult, error) {
				if calls.Add(1) > 1 {
					return done(""), nil
				}
				if err := g.st.Update(job.Item.ID, func(it *factory.Item) error {
					it.Tier = factory.TierStranger
					return nil
				}); err != nil {
					t.Error(err)
				}
				if plan {
					return factory.StageResult{Done: true, Edit: &factory.PlanEdit{Add: []factory.Stage{{Ask: "after plan, write: make the change"}}, Why: "needed"}}, nil
				}
				return factory.StageResult{Done: false}, nil
			})}, nil)
			st := chat("plan")
			st.Max, st.On = 2, true
			id := g.add("mine", st)
			if err := g.r.Launch(id); err != nil {
				t.Fatal(err)
			}
			it := g.wait(id, "stopped", func(it factory.Item) bool { return it.Stream != nil && !it.Stream.Ended.IsZero() })
			want := "plan"
			if plan {
				want = "write"
				if factory.StageIndex(it.Stages, want) < 0 {
					t.Fatal("plan did not add the step")
				}
			}
			wantAttempts := 1
			if plan {
				wantAttempts = 0
			}
			if calls.Load() != 1 || it.Failure == nil || it.Failure.Step != want || !strings.Contains(it.Failure.Detail, "without write access") || it.Failure.Attempts != wantAttempts {
				t.Fatalf("calls=%d failure=%+v phases=%+v", calls.Load(), it.Failure, it.Stream.Phases)
			}
		})
	}
}

func TestStrangerAnswerCannotContinueOrSendBackWork(t *testing.T) {
	for _, yes := range []bool{false, true} {
		t.Run(fmt.Sprint("yes=", yes), func(t *testing.T) {
			var calls atomic.Int32
			g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) {
				calls.Add(1)
				return done(""), nil
			})}, nil)
			id := g.add("mine", chat("plan"), approveStep(), chat("review"))
			if err := g.r.Launch(id); err != nil {
				t.Fatal(err)
			}
			g.waitState(id, factory.StateNeedsYou)
			if err := g.st.Update(id, func(it *factory.Item) error {
				it.Tier = factory.TierStranger
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			calls.Store(0)
			if err := g.r.Answer(id, yes, "continue"); err != nil {
				t.Fatal(err)
			}
			it := expectStrangerStopped(t, g, id, &calls, 0)
			wantAttempts := 0
			if !yes {
				wantAttempts = 1
			}
			if it.Failure.Attempts != wantAttempts {
				t.Fatalf("refusal counted a scheduled round: %+v", it.Failure)
			}
			if err := g.r.Launch(id); err == nil {
				t.Fatal("refused job launched again")
			}
			it, _ = g.st.Get(id)
			if it.Failure.Step != "plan" || it.Failure.Attempts != 1 {
				t.Fatalf("repeat refusal counted an untried round: %+v", it.Failure)
			}
		})
	}
}

func TestOnlyStrangerTierIsRefused(t *testing.T) {
	for _, tier := range []factory.Tier{"", factory.TierOwner, factory.TierCollab, "unrecognized"} {
		t.Run(string(tier), func(t *testing.T) {
			exec, ran := recordingChat()
			g := newRig(t, exec, nil)
			id := g.add("mine", chat("review"))
			if err := g.st.Update(id, func(it *factory.Item) error {
				it.Tier = tier
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := g.r.Launch(id); err != nil {
				t.Fatal(err)
			}
			g.waitState(id, factory.StateLanded)
			if len(ran()) != 1 {
				t.Fatalf("calls=%v", ran())
			}
		})
	}
}

func TestStrangerApprovalOnlyJobStillRuns(t *testing.T) {
	g := newRig(t, nil, nil)
	id := strangerJob(t, g, factory.KindIssue, []factory.Stage{{Name: "write", Kind: factory.StageGate, On: true}})
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.waitState(id, factory.StateLanded)
}

func TestStageCanWriteIgnoresName(t *testing.T) {
	for _, name := range []string{"write", "draft", "implement", "change-it", "fix", "review", "test", "plan", "x"} {
		for _, tc := range []struct {
			kind factory.StageKind
			want bool
		}{
			{factory.StageChat, true},
			{factory.StageCheck, true},
			{factory.StagePost, true},
			{factory.StageGate, false},
			{"", true},
			{"unknown", true},
		} {
			st := factory.Stage{Name: name, Kind: tc.kind}
			if st.CanWrite() != tc.want {
				t.Errorf("%s/%s: can write=%v, want %v", name, tc.kind, st.CanWrite(), tc.want)
			}
		}
	}
}

func TestStrangerApprovalAnswerAfterRestartRefusesWork(t *testing.T) {
	for _, yes := range []bool{false, true} {
		t.Run(fmt.Sprint("yes=", yes), func(t *testing.T) {
			var calls atomic.Int32
			g := newRig(t, nil, nil)
			id := strangerJob(t, g, factory.KindIssue, []factory.Stage{
				{Name: "plan", Kind: factory.StageChat, On: true},
				{Name: "approve", Kind: factory.StageGate, On: true},
				{Name: "review", Kind: factory.StageChat, On: true},
			})
			if err := g.st.Update(id, func(it *factory.Item) error {
				it.State, it.QKind, it.Question = factory.StateNeedsYou, factory.QKindApprove, "continue?"
				it.Stream = &factory.Stream{Cur: 1, Phases: []factory.Phase{
					{Name: "plan", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1},
					{Name: "approve", Kind: factory.StageGate, State: factory.PhaseWaiting},
					{Name: "review", Kind: factory.StageChat, State: factory.PhasePending},
				}}
				return AnswerUnheld(it, yes, "check this", g.r.now())
			}); err != nil {
				t.Fatal(err)
			}
			// A pulled job with an existing run resumes through Hold.
			if err := g.st.Update(id, func(it *factory.Item) error {
				it.State, it.Stream.Paused = factory.StateRunning, true
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			r2 := New(Options{Store: g.st, Exec: map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) {
				calls.Add(1)
				return done(""), nil
			})}})
			if err := r2.Hold(id, false); err != nil {
				t.Fatal(err)
			}
			expectStrangerStopped(t, g, id, &calls, 0)
		})
	}
}

func TestStrangerPausedJobRefusesWarmResume(t *testing.T) {
	p := newPauseExec()
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: p}, nil)
	id := g.add("mine", chat("review"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, p)
	if err := g.r.Hold(id, true); err != nil {
		t.Fatal(err)
	}
	g.wait(id, "paused drive released", func(it factory.Item) bool { return it.Stream != nil && it.Stream.Paused && !g.r.Running(id) })
	if err := g.st.Update(id, func(it *factory.Item) error {
		it.Tier = factory.TierStranger
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	g.r.opts.Exec[factory.StageChat] = ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) {
		calls.Add(1)
		return done(""), nil
	})
	if err := g.r.Hold(id, false); err != nil {
		t.Fatal(err)
	}
	expectStrangerStopped(t, g, id, &calls, 0)
}

func TestStrangerTriageReadStillWorks(t *testing.T) {
	g := newRig(t, nil, nil)
	id := strangerJob(t, g, factory.KindIssue, []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true}})
	calls := 0
	got := triage.ReadThese(context.Background(), g.st, func(context.Context, string) (string, error) {
		calls++
		return `{"read":"Explain the change", "type":"bug"}`, nil
	}, []int{id}, triage.Options{})
	it, err := g.st.Get(id)
	if err != nil || calls != 1 || len(got.Read) != 1 || it.Triage.Read != "Explain the change" || it.Triage.TriagedAt.IsZero() {
		t.Fatalf("triage: calls=%d result=%+v read=%+v error=%v", calls, got, it.Triage, err)
	}
}

func TestStrangerApprovalJobKeepsManager(t *testing.T) {
	talk := newFakeTalk()
	g, made := managed(t, nil, talk, time.Hour)
	id := strangerJob(t, g, factory.KindIssue, []factory.Stage{{Name: "approve", Kind: factory.StageGate, On: true}})
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateLanded)
	if *made != 1 || it.Talk == "" {
		t.Fatalf("manager: made=%d talk=%q", *made, it.Talk)
	}
}

func TestStrangerRuleTextStillParses(t *testing.T) {
	if unknown := UnknownPolicy([]string{"a stranger's PR never runs write"}); len(unknown) != 0 {
		t.Fatalf("rule became unknown: %v", unknown)
	}
}

func TestStrangerStartRefusalKeepsNewerJob(t *testing.T) {
	for _, granted := range []bool{false, true} {
		t.Run(fmt.Sprint("granted=", granted), func(t *testing.T) {
			var id int
			g := newRig(t, nil, func(o *Options) {
				o.SpentToday = func() float64 { return 0 }
				o.Rail = func() float64 {
					if err := o.Store.Update(id, func(it *factory.Item) error {
						if granted {
							it.Tier = factory.TierCollab
						} else {
							it.State = factory.StateRunning
						}
						return nil
					}); err != nil {
						t.Fatal(err)
					}
					return 0
				}
			})
			id = strangerJob(t, g, factory.KindIssue, []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true}})
			var refused WriteAccessRefusal
			if err := g.r.Launch(id); !errors.As(err, &refused) {
				t.Fatalf("refusal = %v", err)
			}
			it, err := g.st.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			if it.Failure != nil {
				t.Fatalf("stale refusal changed the newer job: %+v", it.Failure)
			}
		})
	}
}

func TestStrangerRefusalDuringPreparationDoesNotCountAnAttempt(t *testing.T) {
	var calls atomic.Int32
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) {
		calls.Add(1)
		return done(""), nil
	})}, func(o *Options) {
		o.Workdir = func(it factory.Item) (string, error) {
			err := o.Store.Update(it.ID, func(cur *factory.Item) error {
				cur.Tier = factory.TierStranger
				return nil
			})
			return "", err
		}
	})
	id := g.add("mine", chat("plan"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := expectStrangerStopped(t, g, id, &calls, 0)
	if it.Failure.Attempts != 0 || api.FromItem(it).Failure.Rounds != 0 {
		t.Fatalf("preparation counted as an attempt: %+v", it.Failure)
	}
}
