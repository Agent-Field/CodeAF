package run

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// EACH LINE THAT REPORTS ONE STEP'S ROUND CARRIES THAT STEP BESIDE ITS WORDS,
// and the lines about the job carry none: a surface folds the first kind into
// the round's frame and shows the second as words. The words are the floor's
// own, unchanged (TestTheManagerHearsEveryStageInOrderInTheFloorsWords).
func TestEachStepLineCarriesItsStepAndRoundBesideItsWords(t *testing.T) {
	talk := newFakeTalk()
	g, _ := managed(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) {
			return factory.StageResult{Done: true, Notes: []string{"ok"}}, nil
		}),
		factory.StageCheck: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) {
			return factory.StageResult{Done: true, Claims: []factory.Claim{{Text: "a", OK: true, Evidence: "x"}, {Text: "b"}}}, nil
		}),
	}, talk, time.Hour)
	test := factory.Stage{Name: "test", Kind: factory.StageCheck, Ask: "go test", Until: factory.UntilProven, Max: 1}
	id := g.add("fix the ledger", chat("plan"), test)
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	q := "test is not proven: 1 of 2 claims shown · try again, send it back, or stop the job?"
	g.asked(id, q)
	m := "manager-" + strconv.Itoa(id)
	talk.waitSaid(t, m, "asking you: "+q)
	if err := g.r.Answer(id, false, ""); err != nil {
		t.Fatal(err)
	}
	g.waitState(id, factory.StateNew)
	talk.waitSaid(t, m, sayStopped)

	for _, want := range []struct {
		line string
		step *factory.StepProgress
	}{
		{"plan started", &factory.StepProgress{Step: "plan", Round: 1, Event: factory.ProgressStarted}},
		{"plan done", &factory.StepProgress{Step: "plan", Round: 1, Event: factory.ProgressDone}},
		{"test started", &factory.StepProgress{Step: "test", Round: 1, Event: factory.ProgressStarted}},
		{"asking you: " + q, &factory.StepProgress{Step: "test", Round: 1, Event: factory.ProgressWaiting}},
		{"answered: no", nil},
		{sayStopped, &factory.StepProgress{Step: "test", Round: 1, Event: factory.ProgressStopped}},
	} {
		got, said := talk.stepOf(m, want.line)
		if !said {
			t.Fatalf("the manager never said %q; it said %q", want.line, talk.lines(m))
		}
		if (got == nil) != (want.step == nil) || (got != nil && *got != *want.step) {
			t.Fatalf("%q carries %+v, want %+v", want.line, got, want.step)
		}
	}
}

// A STEP THAT ERRORED ENDS THE RUN UNDER IT, and the manager's `stopped` says
// which step failed rather than that a person stopped it.
func TestAStepThatErroredSaysItsStopAsThatStepFailing(t *testing.T) {
	talk := newFakeTalk()
	g, _ := managed(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) {
			return factory.StageResult{}, errors.New("the model refused")
		}),
	}, talk, time.Hour)
	id := g.add("fix it", chat("plan"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.waitState(id, factory.StateNew)
	m := "manager-" + strconv.Itoa(id)
	talk.waitSaid(t, m, sayStopped)
	got, _ := talk.stepOf(m, sayStopped)
	if want := (factory.StepProgress{Step: "plan", Round: 1, Event: factory.ProgressFailed}); got == nil || *got != want {
		t.Fatalf("stopped carries %+v, want %+v", got, want)
	}
}

// A STOP THAT CUT NO STEP IS ABOUT THE JOB: a job stopped while it waited in
// the queue says `stopped` with no step beside it.
func TestAStopThatCutNoStepCarriesNoStep(t *testing.T) {
	talk := newFakeTalk()
	g, _ := managed(t, nil, talk, time.Hour)
	id := g.add("fix it", chat("plan"))
	if err := g.st.Update(id, func(it *factory.Item) error {
		it.State = factory.StateQueued
		it.Talk = "manager-" + strconv.Itoa(id)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Stop(id); err != nil {
		t.Fatal(err)
	}
	m := "manager-" + strconv.Itoa(id)
	talk.waitSaid(t, m, sayStopped)
	if got, _ := talk.stepOf(m, sayStopped); got != nil {
		t.Fatalf("a stop that cut no step carries %+v", got)
	}
}

// EACH ROUND KEEPS WHEN IT BEGAN AND THE CHAT IT RAN IN, and the step keeps
// when its latest round began, so the read can place a frame for every round.
func TestEachRoundKeepsWhenItBeganAndItsChat(t *testing.T) {
	t0 := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	var tick atomic.Int64
	var round atomic.Int64
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) {
			n := round.Add(1)
			findings := 0
			if n == 1 {
				findings = 2
			}
			return factory.StageResult{Done: true, Findings: findings, Chat: "review-" + strconv.FormatInt(n, 10)}, nil
		}),
	}, func(o *Options) {
		o.Clock = func() time.Time { return t0.Add(time.Duration(tick.Add(1)) * time.Second) }
	})
	review := factory.Stage{Name: "review", Kind: factory.StageChat, Ask: "review it", Until: factory.UntilClean, Max: 2}
	id := g.add("review it", review)
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateLanded)
	ph := it.Stream.Phases[0]
	if len(ph.Rounds) != 2 {
		t.Fatalf("the step kept rounds %+v", ph.Rounds)
	}
	first, second := ph.Rounds[0], ph.Rounds[1]
	if first.Chat != "review-1" || second.Chat != "review-2" {
		t.Fatalf("the rounds ran in %q and %q", first.Chat, second.Chat)
	}
	if first.Started.IsZero() || !first.Started.Before(first.At) || first.At.After(second.Started) || !second.Started.Before(second.At) {
		t.Fatalf("the rounds' times do not follow one another: %+v", ph.Rounds)
	}
	if !ph.Started.Equal(second.Started) || !ph.At.After(ph.Started) {
		t.Fatalf("the step began %v and ended %v; its latest round began %v", ph.Started, ph.At, second.Started)
	}
}

// A STEP SENT BACK TO PENDING HAS NOT BEGUN AGAIN until its next round does,
// and a worker's report carries when its round began, on the worker's clock,
// onto the factory's copy along with each round's start and chat.
func TestAReportCarriesWhenTheRoundBeganAndAPendingStepForgetsIt(t *testing.T) {
	t0 := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	prev := diffJob()
	cur := diffJob()
	cur.Stream.Phases[0].Started = t0
	cur.Stream.Phases[0].Round = 2
	cur.Stream.Phases[0].Rounds = []factory.RoundLine{{Round: 1, Summary: "2 findings", Started: t0.Add(-time.Minute), At: t0, Chat: "plan-1"}}
	r, ok := Diff(prev, cur)
	if !ok || len(r.Stages) != 1 || !r.Stages[0].Started.Equal(t0) {
		t.Fatalf("the report carries %+v", r.Stages)
	}
	server := diffJob()
	if err := ApplyReport(&server, r, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	ph := server.Stream.Phases[0]
	if !ph.Started.Equal(t0) || len(ph.Rounds) != 1 || ph.Rounds[0].Chat != "plan-1" || !ph.Rounds[0].Started.Equal(t0.Add(-time.Minute)) {
		t.Fatalf("the server's step: %+v", ph)
	}
	setPhaseState(&server.Stream.Phases[0], factory.PhasePending, t0.Add(2*time.Hour))
	if !server.Stream.Phases[0].Started.IsZero() {
		t.Fatalf("a pending step still says it began at %v", server.Stream.Phases[0].Started)
	}
}
