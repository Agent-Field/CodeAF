package run

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A PLAN THAT NAMES A FILE A RULE GUARDS MAKES THE APPROVAL AFTER IT A
// PERSON'S: the job records the rule and the file, Auto says nothing routine,
// and once the person says yes the hold is gone and the run goes on.
func TestAPlanTouchingAGuardedPathHoldsItsApproval(t *testing.T) {
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			if job.Stage.Name == "plan" {
				return done("files:\n- internal/auth/login.go\n"), nil
			}
			return done(""), nil
		}),
	}, func(o *Options) { o.Benches = 0 })
	id := g.add("auth", chat("plan"), factory.Stage{Name: factory.ApproveName, Kind: factory.StageGate}, chat("write"))
	if err := g.st.Update(id, func(it *factory.Item) error {
		it.Autonomy, it.Assignee, it.Cap = factory.AutonomyAuto, "sam", 5
		it.StartRules = &factory.StartRules{AskFirst: []string{"touch auth/"}, Never: []string{}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.asked(id, "plan is ready · continue, or send it back?")
	if it.RuleHold != "ask first · touch auth/ · internal/auth/login.go" {
		t.Fatalf("hold %q", it.RuleHold)
	}
	if why := factory.AutonomyBlockReason(it); !strings.Contains(why, "touch auth/") {
		t.Fatalf("Auto would pass it: %q", why)
	}
	if logHas(it, "Auto requested routine approval to continue after plan.") {
		t.Fatalf("a held approval was called routine: %+v", it.Stream.Log)
	}
	if err := g.r.Answer(id, true, ""); err != nil {
		t.Fatal(err)
	}
	it = g.waitState(id, factory.StateLanded)
	if it.RuleHold != "" {
		t.Fatalf("the hold outlived the answer: %q", it.RuleHold)
	}
}
