package run

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestStopHoldsTheJobUntilExecutorCleanupFinishes(t *testing.T) {
	started, cleanup := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(ctx context.Context, j Job) (factory.StageResult, error) {
		if calls.Add(1) > 1 {
			return done("finished"), nil
		}
		close(started)
		<-ctx.Done()
		<-cleanup
		return factory.StageResult{}, ctx.Err()
	})}, nil)
	id := g.add("cleanup fence", chat("test"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("executor did not start")
	}
	if err := g.r.Stop(id); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Launch(id); err == nil {
		t.Fatal("replacement admitted before old executor cleanup")
	}
	if calls.Load() != 1 {
		t.Fatal("overlapping execution")
	}
	close(cleanup)
	deadline := time.After(2 * time.Second)
	for g.r.loop().ctl(id) != nil {
		select {
		case <-deadline:
			t.Fatal("cleanup did not release control")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.waitState(id, factory.StateLanded)
}

func TestRestartKeepsTheGrantedRoundLimitAndHistory(t *testing.T) {
	stage := chat("test")
	stage.Max = 3
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(ctx context.Context, j Job) (factory.StageResult, error) {
		return factory.StageResult{Done: false, Exit: 1, Chat: fmt.Sprintf("chat-%d", j.Round), Output: "assertion still fails"}, nil
	})}, nil)
	id := g.add("granted rounds", stage)
	if err := g.st.Update(id, func(it *factory.Item) error {
		it.State = factory.StateRunning
		it.Stream = &factory.Stream{Paused: true, Phases: []factory.Phase{{Name: "test", Kind: factory.StageChat, State: factory.PhaseRunning, Round: 4, RoundLimit: 6, Rounds: []factory.RoundLine{{Round: 1, Chat: "first", At: time.Now()}}}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateNeedsYou)
	ph := it.Stream.Phases[0]
	if ph.Round != 6 || ph.RoundLimit != 6 {
		t.Fatalf("grant lost: round=%d limit=%d", ph.Round, ph.RoundLimit)
	}
	if len(ph.Rounds) != 4 || ph.Rounds[0].Chat != "first" {
		t.Fatalf("history lost: %d tries", len(ph.Rounds))
	}
}

func TestWorkerRoundHistoryMergesByConversation(t *testing.T) {
	now := time.Now()
	it := factory.Item{State: factory.StateRunning, Stages: []factory.Stage{{Name: "test", On: true, Kind: factory.StageChat, Max: 3}}, Stream: &factory.Stream{Phases: []factory.Phase{{Name: "test", Kind: factory.StageChat, State: factory.PhaseRunning, Round: 1, RoundLimit: 6, Rounds: []factory.RoundLine{{Round: 1, Chat: "old", At: now}}}}}}
	r := factory.RunReport{Stages: []factory.StageReport{{Name: "test", Round: 1, RoundLimit: 6, Rounds: []factory.RoundLine{{Round: 1, Chat: "new", At: now.Add(time.Second)}}}}}
	for range 2 {
		if err := ApplyReport(&it, r, now); err != nil {
			t.Fatal(err)
		}
	}
	if got := it.Stream.Phases[0]; len(got.Rounds) != 2 || got.Rounds[0].Chat != "old" || got.RoundLimit != 6 {
		t.Fatal("suffix report erased history or duplicate grew it")
	}
}

func TestAuthoritativeAnswerPersistsTheGrantBeforeWorkerDelivery(t *testing.T) {
	it := factory.Item{State: factory.StateNeedsYou, QKind: factory.QKindOutOfAttempts, Question: "try again?", Stream: &factory.Stream{Phases: []factory.Phase{{Name: "test", State: factory.PhaseWaiting, Round: 4, RoundLimit: 4}}}}
	if err := AnswerHeld(&it, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if it.Stream.Phases[0].RoundLimit != 5 {
		t.Fatal("answer acknowledged without persisting the grant")
	}
	// Reporting incompleteness is not a request for a new content-repair round.
	it.State, it.QKind, it.Question = factory.StateNeedsYou, factory.QKindOutOfAttempts, "retry reporting?"
	it.Stream.Phases[0].Verification = &factory.StepVerification{State: "incomplete"}
	it.Stream.Phases[0].State = factory.PhaseWaiting
	if err := AnswerHeld(&it, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if it.Stream.Phases[0].RoundLimit != 5 {
		t.Fatal("mechanical retry granted extra content rounds")
	}
}

func TestWordBasedAndColdRetryGrantsSurviveWorkerRetirement(t *testing.T) {
	makeItem := func() factory.Item {
		return factory.Item{State: factory.StateNeedsYou, QKind: factory.QKindOutOfAttempts, Question: "try again?", Stages: []factory.Stage{{Name: "test", On: true, Kind: factory.StageChat, Max: 3}}, Stream: &factory.Stream{Phases: []factory.Phase{{Name: "test", Kind: factory.StageChat, State: factory.PhaseWaiting, Round: 4, RoundLimit: 4}}}}
	}
	it := makeItem()
	if err := AnswerHeld(&it, false, time.Now(), factory.ChoiceTryOnceMore); err != nil {
		t.Fatal(err)
	}
	if it.Stream.Phases[0].RoundLimit != 5 {
		t.Fatal("word-based grant was lost")
	}
	it = makeItem()
	if err := AnswerUnheld(&it, false, factory.ChoiceTryOnceMore, time.Now()); err != nil {
		t.Fatal(err)
	}
	if it.State != factory.StateQueued || it.Question != "" || it.Stream.Phases[0].Round != 5 || it.Stream.Phases[0].RoundLimit != 5 {
		t.Fatal("retired worker's decision cannot recover")
	}
}

func TestPendingSendBackCarriesItsAbsoluteGrantAcrossWorkerReports(t *testing.T) {
	now := time.Now()
	it := factory.Item{State: factory.StateRunning, Stages: []factory.Stage{{Name: "test", On: true, Kind: factory.StageChat, Max: 2}, {Name: "approve", On: true, Kind: factory.StageGate}}, Stream: &factory.Stream{Phases: []factory.Phase{{Name: "test", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1, RoundLimit: 2}, {Name: "approve", Kind: factory.StageGate, State: factory.PhaseWaiting}}}}
	remote := it
	cp := *it.Stream
	cp.Phases = append([]factory.Phase(nil), it.Stream.Phases...)
	remote.Stream = &cp
	rewindToStep(&it, 1, 0, "fix", now)
	if it.Stream.Phases[0].RoundLimit != 3 {
		t.Fatal("sendback did not save full new loop")
	}
	report, ok := Diff(remote, it)
	if !ok {
		t.Fatal("no rewind report")
	}
	if err := ApplyReport(&remote, report, now); err != nil {
		t.Fatal(err)
	}
	if remote.Stream.Phases[0].Round != 2 || remote.Stream.Phases[0].RoundLimit != 3 {
		t.Fatal("pending grant lost at worker boundary")
	}
}

func TestMechanicalIncompleteWaitDoesNotUseAnotherRepairRound(t *testing.T) {
	var calls atomic.Int32
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(ctx context.Context, j Job) (factory.StageResult, error) {
		if calls.Add(1) == 1 {
			return factory.StageResult{ExecutionBlocked: "report_incomplete", Output: "Check result retained; terminal report missing", Chat: "first"}, nil
		}
		if j.Round != 1 {
			t.Errorf("mechanical retry spent round %d", j.Round)
		}
		return factory.StageResult{Done: true, Chat: "second", Output: "checked"}, nil
	})}, nil)
	stage := chat("test")
	stage.Max = 3
	id := g.add("incomplete report", stage)
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateNeedsYou)
	if it.Stream.Phases[0].Round != 1 || it.Stream.Phases[0].Rounds[0].Rejected || !strings.Contains(it.Question, "verification is incomplete") {
		t.Fatal("mechanical wait presented as failed assertion")
	}
	if err := g.r.Answer(id, true, ""); err != nil {
		t.Fatal(err)
	}
	final := g.waitState(id, factory.StateLanded)
	if final.Stream.Phases[0].Round != 1 || len(final.Stream.Phases[0].Rounds) != 2 {
		t.Fatal("same-round recovery lost its attempts")
	}
}
