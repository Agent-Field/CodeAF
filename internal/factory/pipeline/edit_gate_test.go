package pipeline

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// AN APPROVAL IS ADDED LIKE ANY STEP: add_step of kind gate puts a person in
// one pipeline, waiting on whom its who names, and the runner reads it as one.
func TestAddStepPutsAnApprovalInOnePipeline(t *testing.T) {
	got := mustApply(t, editBase, EditOp{Op: OpAddStep, Pipeline: "issue", Name: "sec-review", After: s("test"),
		Kind: s("gate"), Prompt: s("read the auth changes"), Who: s("@Rel")})
	back := reread(t, got.Text)
	if g := joined(back.Pipeline[factory.KindIssue]); g != "plan,approve,write,test,sec-review,lint-pass,proof" {
		t.Fatalf("%s", g)
	}
	if st := back.Steps["sec-review"]; st.Kind != "gate" || st.Who != "@rel" || st.Prompt != "read the auth changes" {
		t.Fatalf("%+v", st)
	}
	if len(got.Added) != 1 || got.Added[0] != "sec-review" {
		t.Fatalf("%+v", got.Added)
	}
	var stage factory.Stage
	for _, st := range back.Recipe().For(factory.KindIssue) {
		if st.Name == "secreview" {
			stage = st
		}
	}
	if stage.Kind != factory.StageGate || stage.Who != "@rel" {
		t.Fatalf("the runner does not wait on the person: %+v", stage)
	}
	// The gate is now a person in that pipeline: lint-pass's run-up is unchanged,
	// and the new gate's own run-up is reported.
	if len(got.Gates) == 0 {
		t.Fatalf("no gate reported: %+v", got)
	}
}

// A gate needs no prompt and no who: it then waits on the job's person.
func TestAddStepGateWithNothingWaitsOnTheJobsPerson(t *testing.T) {
	got := mustApply(t, editBase, EditOp{Op: OpAddStep, Pipeline: "ci", Name: "signoff", After: s("proof"), Kind: s("gate")})
	back := reread(t, got.Text)
	if st := back.Steps["signoff"]; st.Kind != "gate" || st.Who != "" || st.Prompt != "" {
		t.Fatalf("%+v", st)
	}
	if g := joined(back.Pipeline[factory.KindCI]); g != "bisect,fix,test,proof,signoff,approve" {
		t.Fatalf("%s", g)
	}
}

func TestAddStepGateTakesOnlyAPersonsWords(t *testing.T) {
	gate := func(op EditOp) EditOp {
		op.Op, op.Pipeline, op.Name, op.After, op.Kind = OpAddStep, "issue", "signoff", s("test"), s("gate")
		return op
	}
	for name, op := range map[string]EditOp{
		"an until":     gate(EditOp{Until: s("they agree")}),
		"rounds":       gate(EditOp{Max: i(2)}),
		"an effort":    gate(EditOp{Effort: s("strong")}),
		"a fanout":     gate(EditOp{Fanout: s("per-file")}),
		"a bad who":    gate(EditOp{Who: s("everyone")}),
		"who on chat":  {Op: OpAddStep, Pipeline: "issue", Name: "lint", After: s("write"), Prompt: s("x"), Who: s("@rel")},
		"who on check": {Op: OpAddStep, Pipeline: "issue", Name: "lint", After: s("write"), Prompt: s("go vet"), Kind: s("check"), Who: s("@rel")},
		"who on move":  {Op: OpMoveStep, Pipeline: "issue", Name: "test", After: s("plan"), Who: s("@rel")},
		"an approve":   {Op: OpAddStep, Pipeline: "issue", Name: "approve2", After: s("test"), Kind: s("gate")},
	} {
		if _, bad := CheckEdit(0, op); bad == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// A gate already defined with other words is not rewritten through this door.
	base := strings.Replace(editBase, "steps:\n", "steps:\n  signoff:\n    kind: gate\n    who: operator\n", 1)
	if e := refusal(t, base, gate(EditOp{Who: s("@rel")})); e.Code != EditBadValue {
		t.Fatalf("%+v", e)
	}
	// The same words list the existing definition.
	got := mustApply(t, base, gate(EditOp{Who: s("operator")}))
	if back := reread(t, got.Text); joined(back.Pipeline[factory.KindIssue]) != "plan,approve,write,test,signoff,lint-pass,proof" {
		t.Fatalf("%s", got.Text)
	}
}
