package worker_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

func TestBudgetRaisePreservesStopSpendingOnTheLeasedRunner(t *testing.T) {
	entered, finish := make(chan struct{}), make(chan struct{})
	g := newRigWith(t, nil, func(o *run.Options) {
		o.Exec = map[factory.StageKind]run.Executor{factory.StageCheck: run.ExecutorFunc(func(ctx context.Context, j run.Job) (factory.StageResult, error) {
			if j.Stage.Name == "after" {
				close(entered)
				select {
				case <-finish:
				case <-ctx.Done():
					return factory.StageResult{}, ctx.Err()
				}
				return factory.StageResult{Done: true, Spent: 5}, nil
			}
			return factory.StageResult{Done: true, Spent: 2}, nil
		})}
	}, "me")
	id := g.add(t, factory.Stage{Name: "charge", Kind: factory.StageCheck, On: true}, factory.Stage{Name: "after", Kind: factory.StageCheck, On: true})
	if err := g.st.Update(id, func(it *factory.Item) error {
		it.Cap = 2
		it.Autonomy = factory.AutonomyAuto
		it.Assignee = "me"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.waitFor(t, id, factory.StateNeedsYou)
	g.raise(t, id, 10)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("raised cap did not reach the next step")
	}
	post := func(verb, body string) {
		t.Helper()
		resp, err := http.Post(fmt.Sprintf("%s/v1/jobs/%d/%s", g.base, id, verb), "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			t.Fatalf("%s: %d %s", verb, resp.StatusCode, data)
		}
	}
	post("stop-after-step", `{"on":true}`)
	g.raise(t, id, 20)
	close(finish)
	stopped := g.waitFor(t, id, factory.StateNeedsYou)
	if stopped.QKind != factory.QKindSpendStopped || !stopped.StopAfterStep || stopped.Cap != 20 || stopped.Stream.Spent != 7 {
		t.Fatalf("raise lost boundary stop: kind=%q armed=%t cap=%g spent=%g", stopped.QKind, stopped.StopAfterStep, stopped.Cap, stopped.Stream.Spent)
	}
	g.raise(t, id, 30)
	kept, err := g.st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if kept.QKind != factory.QKindSpendStopped || kept.Question != stopped.Question || !kept.StopAfterStep || kept.Cap != 30 {
		t.Fatalf("raise answered the spending choice: kind=%q armed=%t cap=%g", kept.QKind, kept.StopAfterStep, kept.Cap)
	}
	post("answer", `{"yes":true,"words":"continue"}`)
	landed := g.waitFor(t, id, factory.StateLanded)
	if landed.StopAfterStep || landed.Cap != 30 || landed.Stream.Spent != 7 {
		t.Fatalf("continue lost cap or reran work: armed=%t cap=%g spent=%g", landed.StopAfterStep, landed.Cap, landed.Stream.Spent)
	}
}

func TestBudgetAdmissionCountsUnreportedRoundSpend(t *testing.T) {
	var calls atomic.Int32
	var g rig
	g = newRigWithReportEvery(t, nil, func(o *run.Options) {
		o.Exec = map[factory.StageKind]run.Executor{factory.StageCheck: run.ExecutorFunc(func(ctx context.Context, j run.Job) (factory.StageResult, error) {
			if calls.Add(1) == 1 {
				if err := g.st.Update(j.Item.ID, func(it *factory.Item) error { it.SpentBefore = .6; return nil }); err != nil {
					return factory.StageResult{}, err
				}
			}
			return factory.StageResult{Done: true, Spent: .5}, nil
		})}
	}, time.Hour)
	id := g.add(t, check("first"), factory.Stage{Name: "second", Kind: factory.StageCheck, On: true})
	if err := g.st.Update(id, func(it *factory.Item) error { it.Cap = 1; it.Autonomy = factory.AutonomyAuto; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.waitFor(t, id, factory.StateNeedsYou)
	if got := calls.Load(); got != 1 {
		t.Fatalf("paid rounds=%d, want 1: admission ignored unreported $0.50 beside server $0.60", got)
	}
}

func TestBudgetAdmissionDrainsOlderPendingSpend(t *testing.T) {
	var calls atomic.Int32
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	failed := make(chan struct{})
	var rejected atomic.Bool
	var g rig
	g = newRigWithDelivery(t, nil, func(o *run.Options) {
		o.Exec = map[factory.StageKind]run.Executor{factory.StageCheck: run.ExecutorFunc(func(ctx context.Context, j run.Job) (factory.StageResult, error) {
			if calls.Add(1) == 1 {
				if err := g.mirror.Update(j.Item.ID, func(it *factory.Item) error { it.Stream.Spent = .1; return nil }); err != nil {
					return factory.StageResult{}, err
				}
				select {
				case <-failed:
				case <-ctx.Done():
					return factory.StageResult{}, ctx.Err()
				}
				clock.Add(int64(3 * time.Second))
				if err := g.st.Update(j.Item.ID, func(it *factory.Item) error { it.SpentBefore = .6; return nil }); err != nil {
					return factory.StageResult{}, err
				}
			}
			return factory.StageResult{Done: true, Spent: .5}, nil
		})}
	}, time.Second, func(w http.ResponseWriter, r *http.Request) bool {
		if isReport(r) && strings.Contains(peek(r), `"cost":0.1`) && rejected.CompareAndSwap(false, true) {
			w.WriteHeader(http.StatusServiceUnavailable)
			return true
		}
		return false
	}, func() time.Time {
		now := time.Unix(0, clock.Load())
		if rejected.Load() {
			select {
			case <-failed:
			default:
				close(failed)
			}
		}
		return now
	}, 1)
	id := g.add(t, check("first"), factory.Stage{Name: "second", Kind: factory.StageCheck, On: true})
	if err := g.st.Update(id, func(it *factory.Item) error { it.Cap = 1; it.Autonomy = factory.AutonomyAuto; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.waitFor(t, id, factory.StateNeedsYou)
	if got := calls.Load(); got != 1 {
		t.Fatalf("paid rounds=%d, want 1: older pending report hid newer spending", got)
	}
}
