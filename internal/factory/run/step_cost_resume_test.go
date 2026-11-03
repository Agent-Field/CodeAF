package run

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestStepCostFallbackResumeChargesOnlyNewJobSpend(t *testing.T) {
	calls := 0
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(_ context.Context, job Job) (factory.StageResult, error) {
		calls++
		if calls == 1 {
			return factory.StageResult{Spent: 1, Chat: "fallback", JobBudgetStopped: true, Output: (SpendStop{Job: true, Said: "budget"}).Error()}, nil
		}
		if job.Resume != "fallback" || job.Round != 1 {
			t.Errorf("resume %q round %d", job.Resume, job.Round)
		}
		return factory.StageResult{Done: true, Spent: 1.25, Chat: "fallback"}, nil
	})}, nil)
	id := g.add("fallback resume", chat("write"))
	if err := g.st.Update(id, func(it *factory.Item) error { it.Cap = 1; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.asked(id, "budget of $1 reached · $1 job total spent · raise it to $5, or stop?")
	if err := g.r.Answer(id, true, ""); err != nil {
		t.Fatal(err)
	}
	got := g.waitState(id, factory.StateLanded)
	if math.Abs(factory.JobSpent(got)-1.25) > 1e-9 || math.Abs(got.Stream.Phases[0].StepCost()-1.25) > 1e-9 {
		t.Fatalf("job spend %.4f step cost %.4f, want 1.2500", factory.JobSpent(got), got.Stream.Phases[0].StepCost())
	}
}

type segmentCostMaker struct{ FakeMaker }

type segmentCostConversation struct{ Conversation }

func (segmentCostConversation) Spent() float64 { return .25 }

func (m *segmentCostMaker) Open(ctx context.Context, spec ConversationSpec) (Conversation, error) {
	c, err := m.FakeMaker.Open(ctx, spec)
	return segmentCostConversation{c}, err
}

func TestStepCostChatSegmentAfterRestart(t *testing.T) {
	for _, worker := range []bool{false, true} {
		t.Run(map[bool]string{false: "saved job", true: "worker admission"}[worker], func(t *testing.T) {
			maker := &segmentCostMaker{}
			maker.Script = func(ctx context.Context, c *FakeConversation) {
				if c.Spec.Resume != "paid-chat" {
					t.Errorf("resume = %q, want paid-chat", c.Spec.Resume)
				}
				if err := c.Spec.Stage.Report(ctx, factory.StageResult{Done: true}); err != nil {
					c.Fail(err)
				}
			}
			g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: NewChatExecutor(maker)}, nil)
			id := g.add("resume paid round", chat("write"))
			if err := g.st.Update(id, func(it *factory.Item) error {
				it.State = factory.StateRunning
				it.Stream = &factory.Stream{Paused: true, Spent: 5.75, Phases: []factory.Phase{{
					Name: "write", Kind: factory.StageChat, State: factory.PhaseRunning, Round: 2,
					Chat: "paid-chat", RoundCosts: map[int]float64{1: .5, 2: 5.25},
				}}}
				if worker {
					*it = api.ToItem(api.WorkerJob(*it))
				} else {
					raw, err := json.Marshal(it)
					if err != nil {
						return err
					}
					return json.Unmarshal(raw, it)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := g.r.Launch(id); err != nil {
				t.Fatal(err)
			}
			got := g.waitState(id, factory.StateLanded)
			ph := got.Stream.Phases[0]
			if factory.JobSpent(got) != 6 || ph.StepCost() != 6 || ph.RoundCosts[1] != .5 || ph.RoundCosts[2] != 5.5 {
				t.Fatalf("job spend %g step cost %g rounds %v, want 6 and rounds [1:0.5 2:5.5]", factory.JobSpent(got), ph.StepCost(), ph.RoundCosts)
			}
		})
	}
}
