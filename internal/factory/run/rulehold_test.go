package run

import (
	"context"
	"strings"
	"testing"
	"time"

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
	served, err := g.st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.asked(id, "plan is ready · continue, or send it back?")
	// THE SERVING FACTORY HOLDS IT TOO: a worker runs on a mirror and only its
	// report reaches the server, which works the hold out from what it holds.
	served.State = factory.StateRunning
	report, ok := Diff(served, it)
	if !ok {
		t.Fatal("the plan's question made no report")
	}
	if err := ApplyReport(&served, report, time.Now()); err != nil {
		t.Fatal(err)
	}
	if served.RuleHold != "protected · auth/ · internal/auth/login.go" {
		t.Fatalf("the server's copy holds %q", served.RuleHold)
	}
	if why := factory.AutonomyBlockReason(served); !strings.Contains(why, "A protected path: auth/") {
		t.Fatalf("the server's Auto would pass it: %q", why)
	}
	if it.RuleHold != "protected · auth/ · internal/auth/login.go" {
		t.Fatalf("hold %q", it.RuleHold)
	}
	if why := factory.AutonomyBlockReason(it); !strings.Contains(why, "A protected path: auth/") {
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

// planRig runs plan, then the given steps, under an ask-first auth/ rule; the
// plan declares files.
func planRig(t *testing.T, files string, steps ...factory.Stage) (*rig, int) {
	t.Helper()
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			if job.Stage.Name == "plan" {
				return done("files:\n" + files), nil
			}
			return done(""), nil
		}),
	}, func(o *Options) { o.Benches = 0 })
	id := g.add("auth", append([]factory.Stage{chat("plan")}, steps...)...)
	if err := g.st.Update(id, func(it *factory.Item) error {
		it.StartRules = &factory.StartRules{AskFirst: []string{"touch auth/"}, Never: []string{}}
		for i := range it.Stages {
			if factory.IsApprove(it.Stages[i]) && it.Stages[i].Ask == "off" {
				it.Stages[i].On, it.Stages[i].Ask = false, ""
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	return g, id
}

// TURNING THE PLAN'S APPROVAL OFF DOES NOT TURN A RULE OFF: under a path rule
// the step still runs, holds a plan that touches what the rule guards, and
// passes on its own when the plan does not.
func TestAnApprovalTurnedOffStillHoldsAPlanARuleGuards(t *testing.T) {
	off := factory.Stage{Name: factory.ApproveName, Kind: factory.StageGate, Ask: "off"}
	t.Run("touches auth", func(t *testing.T) {
		g, id := planRig(t, "- internal/auth/login.go\n", off, chat("write"))
		it := g.asked(id, "plan is ready · continue, or send it back?")
		if it.RuleHold != "protected · auth/ · internal/auth/login.go" {
			t.Fatalf("hold %q", it.RuleHold)
		}
	})
	// A plan whose list is empty cannot clear the rule, and its hold names
	// the path its files went unchecked against.
	t.Run("names no files", func(t *testing.T) {
		g, id := planRig(t, "", off, chat("write"))
		it := g.asked(id, "plan is ready · continue, or send it back?")
		if it.RuleHold != "protected · auth/ · "+factory.PlanNoFiles || PlanRuleHold(it) != it.RuleHold {
			t.Fatalf("hold %q, the server reads %q", it.RuleHold, PlanRuleHold(it))
		}
	})
	t.Run("touches docs", func(t *testing.T) {
		g, id := planRig(t, "- docs/readme.md\n", off, chat("write"))
		it := g.waitState(id, factory.StateLanded)
		if it.RuleHold != "" || logHas(it, "asking you: plan is ready · continue, or send it back?") {
			t.Fatalf("an approval turned off asked about an unguarded plan: %q %+v", it.RuleHold, it.Stream.Log)
		}
	})
}

// THE PLAN'S APPROVAL IS FOUND BY WHERE IT STANDS: a plan another step looks
// over before its approval is still held by its files.
func TestAPlanReviewedBeforeItsApprovalIsStillHeldByItsFiles(t *testing.T) {
	g, id := planRig(t, "- internal/auth/login.go\n", chat("check-plan"), approveStep(), chat("write"))
	it := g.wait(id, "a held approval", func(it factory.Item) bool {
		return it.State == factory.StateNeedsYou && it.QKind == factory.QKindApprove
	})
	if it.RuleHold != "protected · auth/ · internal/auth/login.go" {
		t.Fatalf("hold %q", it.RuleHold)
	}
	if PlanRuleHold(it) != it.RuleHold {
		t.Fatalf("the server's reading %q differs from the runner's %q", PlanRuleHold(it), it.RuleHold)
	}
}

// THE CHANGE IS CHECKED, NOT ONLY THE PLAN: a job whose diff touches a path a
// rule guards waits for the person before anything is published, though its
// plan said nothing of it, its proof is green and no approve step is left.
func TestAChangeTouchingAGuardedPathWaitsBeforeItIsPublished(t *testing.T) {
	landed := func(rules *factory.StartRules, files ...string) factory.Item {
		it := factory.Item{Autonomy: "", Assignee: "sam", Cap: 5, StartRules: rules, Stages: []factory.Stage{chat("write")},
			Stream: &factory.Stream{Phases: []factory.Phase{{Name: "write", Kind: factory.StageChat, State: factory.PhaseDone}}},
			Proof:  []factory.Claim{{Text: "it works", OK: true}}, Change: &factory.Change{}}
		for _, f := range files {
			it.Change.Files = append(it.Change.Files, factory.ChangedFile{Path: f, Status: "modified"})
		}
		landItem(&it, time.Now())
		return it
	}
	rules := &factory.StartRules{AskFirst: []string{"touch auth/"}, Never: []string{"infra/"}}
	if it := landed(rules, "docs/readme.md", "internal/auth/login.go"); it.State != factory.StateLanded || it.RuleHold != "protected · auth/ · internal/auth/login.go" {
		t.Fatalf("a change into auth/: %s %q", it.State, it.RuleHold)
	}
	if it := landed(rules, "infra/main.tf"); it.State != factory.StateLanded || it.RuleHold != "protected · infra/ · infra/main.tf" {
		t.Fatalf("a change into infra/: %s %q", it.State, it.RuleHold)
	}
	if it := landed(rules, "docs/readme.md"); it.State != factory.StateShipped || it.RuleHold != "" {
		t.Fatalf("an unguarded change did not ship as a habit: %s %q", it.State, it.RuleHold)
	}
	// AUTO STOPS THERE TOO, naming the rule and the file.
	it := landed(rules, "internal/auth/login.go")
	it.Autonomy = factory.AutonomyAuto
	if why := factory.AutonomyBlockReason(it); why != "A protected path: auth/ · internal/auth/login.go. Review the changes, then press Approve." {
		t.Fatalf("Auto would publish it: %q", why)
	}
}
