package serve_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/route/judge/judgetest"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// planWaiting is an Auto job whose plan approval waits, its approve step
// saying pass.
func planWaiting(pass string) factory.Item {
	return factory.Item{Autonomy: "auto", State: factory.StateNeedsYou, Assignee: "sam", Cap: 10, QKind: factory.QKindApprove, Question: "plan is ready",
		Stages: []factory.Stage{{Name: "plan", On: true}, {Name: "approve", Kind: factory.StageGate, On: true, Pass: pass}, {Name: "write", On: true}},
		Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{
			{Name: "plan", State: factory.PhaseDone, Round: 1, Result: &factory.StageResult{Done: true, Output: "change src/greet.py only"}},
			{Name: "approve", Kind: factory.StageGate, State: factory.PhaseWaiting},
			{Name: "write", State: factory.PhasePending}}}}
}

// ONLY A PERSON PASSES A PERSON'S APPROVE STEP, in Auto too: it waits in
// Needs you with the reason said, and Auto records no decision.
func TestAutoLeavesAPersonOnlyApprovalToAPerson(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, planWaiting(factory.PassPerson))
	j := f.read(t, it)
	if j.State != api.StateNeedsYou {
		t.Fatalf("Auto passed an approval only a person may pass: %s", j.State)
	}
	saved, _ := f.st.Get(it.ID)
	if saved.Approval != nil && saved.Approval.Automatic {
		t.Fatalf("Auto recorded a decision: %+v", saved.Approval)
	}
	if why := factory.AutonomyBlockReason(saved); !strings.Contains(why, "Only a person can pass this approval") {
		t.Fatalf("the reason: %q", why)
	}
}

// THE MANAGER PASSES A MANAGER'S APPROVE STEP in Auto, and the step keeps who
// passed it.
func TestAutoPassesAManagersApprovalAndTheStepSaysTheManagerPassedIt(t *testing.T) {
	for _, pass := range []string{"", factory.PassManager} {
		t.Run(pass, func(t *testing.T) {
			f := newV2Jobs(t)
			it := f.create(t, planWaiting(pass))
			j := f.read(t, it)
			if j.State == api.StateNeedsYou {
				t.Fatal("the manager did not pass a routine approval")
			}
			saved, _ := f.st.Get(it.ID)
			ph := saved.Stream.Phases[1]
			if ph.State != factory.PhaseDone || ph.Passed == nil || ph.Passed.By != factory.PassedByManager || saved.Passing != nil {
				t.Fatalf("the approve step does not say the manager passed it: %+v passing %+v", ph, saved.Passing)
			}
			j = f.read(t, it)
			if s := j.Steps[1]; s.Passed == nil || s.Passed.By != "manager" || s.At == nil {
				t.Fatalf("the wire does not say who passed it: %+v", s)
			}
		})
	}
}

// THE MANAGER JUDGES A `manager when` CONDITION: a yes passes the approval
// with its reason kept; a no leaves it to a person with the reason said.
func TestAManagerWhenApprovalIsPassedOnlyWhenTheManagerJudgesItHolds(t *testing.T) {
	const cond = "the plan touches 3 files or fewer"
	for _, yes := range []bool{true, false} {
		t.Run(map[bool]string{true: "holds", false: "does not hold"}[yes], func(t *testing.T) {
			f := newV2Jobs(t)
			fake := &judgetest.Judge{Answer: func(ask string, it factory.Item) (bool, string, error) {
				if ask != cond || !strings.Contains(it.Body, "change src/greet.py only") {
					t.Errorf("judged %q on %q", ask, it.Body)
				}
				if yes {
					return true, "The plan names one file.", nil
				}
				return false, "The plan touches seven files.", nil
			}}
			f.h.Close()
			f.cfg.PassJudge = fake
			f.h = serve.New(f.seam, f.cfg)
			t.Cleanup(func() { f.h.Close() })
			it := f.create(t, planWaiting(factory.PassManagerWhen+" "+cond))
			f.read(t, it)
			var saved factory.Item
			deadline := time.Now().Add(5 * time.Second)
			for {
				saved, _ = f.st.Get(it.ID)
				if saved.PassJudge != nil && (!yes || saved.Stream.Phases[1].State == factory.PhaseDone) {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("never judged: %+v", saved.PassJudge)
				}
				time.Sleep(10 * time.Millisecond)
			}
			ph := saved.Stream.Phases[1]
			if yes {
				if ph.Passed == nil || ph.Passed.By != factory.PassedByManager || ph.Passed.Why != "The plan names one file." {
					t.Fatalf("passed without the manager's reason: %+v", ph.Passed)
				}
				return
			}
			if ph.State != factory.PhaseWaiting {
				t.Fatalf("a condition judged false passed the approval: %+v", ph)
			}
			if why := factory.AutonomyBlockReason(saved); !strings.Contains(why, "The plan touches seven files") || !strings.Contains(why, cond) {
				t.Fatalf("the reason: %q", why)
			}
			if j := f.read(t, it); j.State != api.StateNeedsYou {
				t.Fatalf("not left to a person: %s", j.State)
			}
		})
	}
}
