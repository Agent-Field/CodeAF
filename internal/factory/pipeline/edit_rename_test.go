package pipeline

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func rename(name, to string) EditOp { return EditOp{Op: OpRenameStep, Name: name, To: to} }

// A RENAME IS ONE STEP UNDER A NEW NAME EVERYWHERE: every pipeline that lists
// it, the required list, and its own words, who included.
func TestRenameStepRenamesItEverywhereItIsListed(t *testing.T) {
	base := `pipeline:
  issue: [plan, write, review, signoff, proof]
  pr: [read, review, proof]
steps:
  plan:
    prompt: plan it
  write:
    prompt: write it
  review:
    prompt: review the diff
    until: clean
    max: 2
  signoff:
    kind: gate
    who: operator
  read:
    prompt: read it
  proof:
    prompt: show the evidence
fixed: [proof, review, signoff]
`
	got := mustApply(t, base, rename("review", "deep-review"), rename("signoff", "sign"))
	back := reread(t, got.Text)
	if g := joined(back.Pipeline[factory.KindIssue]); g != "plan,write,deep-review,sign,proof" {
		t.Fatalf("issue %s", g)
	}
	if g := joined(back.Pipeline[factory.KindPR]); g != "read,deep-review,proof" {
		t.Fatalf("pr %s", g)
	}
	if st := back.Steps["deep-review"]; st.Prompt != "review the diff" || st.Until != "clean" || st.Max != 2 {
		t.Fatalf("%+v", st)
	}
	if st := back.Steps["sign"]; st.Kind != "gate" || st.Who != "operator" {
		t.Fatalf("%+v", st)
	}
	if _, kept := back.Steps["signoff"]; kept {
		t.Fatal("the old name is still defined")
	}
	if !listed(back.Fixed, "deep-review") || !listed(back.Fixed, "sign") || listed(back.Fixed, "signoff") {
		t.Fatalf("fixed %v", back.Fixed)
	}
	if len(got.Renamed) != 2 || got.Renamed[0] != (Rename{From: "review", To: "deep-review"}) || got.Renamed[1] != (Rename{From: "signoff", To: "sign"}) {
		t.Fatalf("%+v", got.Renamed)
	}
	if !got.Structural || len(got.Order) != 2 || len(got.Steps) != 0 || len(got.Gates) != 0 {
		t.Fatalf("a rename is an order change, no word change and no person's run-up: %+v", got)
	}
}

func TestRenameStepRefusals(t *testing.T) {
	for name, op := range map[string]EditOp{
		"no to":            {Op: OpRenameStep, Name: "write"},
		"a bad to":         rename("write", "Write"),
		"too long":         rename("write", "write-it-very-carefully"),
		"to itself":        rename("write", "write"),
		"to a person":      rename("write", "approve2"),
		"a person":         rename("approve", "okay"),
		"to on set_step":   {Op: OpSetStep, Name: "write", Prompt: s("x"), To: "w"},
		"with a pipeline":  {Op: OpRenameStep, Pipeline: "issue", Name: "write", To: "build"},
		"with step fields": {Op: OpRenameStep, Name: "write", To: "build", Prompt: s("x")},
	} {
		if _, bad := CheckEdit(0, op); bad == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for name, c := range map[string]struct {
		ops  []EditOp
		code string
	}{
		"not listed":        {[]EditOp{rename("nope", "yes")}, EditUnknownStep},
		"to a defined step": {[]EditOp{rename("write", "plan")}, EditBadValue},
		"to a shipped step": {[]EditOp{rename("write", "security")}, EditBadValue},
		"to the same stage": {[]EditOp{rename("write", "lintpass")}, EditBadValue},
		"proof":             {[]EditOp{rename("proof", "evidence")}, EditLocked},
		"into proof":        {[]EditOp{rename("write", "prove-it")}, EditLocked},
		"twice":             {[]EditOp{rename("write", "build"), rename("write", "make")}, EditBadOp},
		"and edited":        {[]EditOp{rename("write", "build"), {Op: OpSetStep, Name: "write", Max: i(2)}}, EditBadOp},
		"and the new name":  {[]EditOp{rename("write", "build"), {Op: OpSetFixed, Name: "build", Fixed: new(bool)}}, EditBadOp},
	} {
		if e := refusal(t, editBase, c.ops...); e.Code != c.code {
			t.Errorf("%s: %s (%s), want %s", name, e.Code, e.Why, c.code)
		}
	}
}

// A shipped step renamed takes its shipped words under the new name.
func TestRenameStepOfAShippedStepWritesItsWords(t *testing.T) {
	got := mustApply(t, editBase, rename("bisect", "find"))
	back := reread(t, got.Text)
	if g := joined(back.Pipeline[factory.KindCI]); g != "find,fix,test,proof,approve" {
		t.Fatalf("ci %s", g)
	}
	if want := Builtins()["bisect"].Prompt; want == "" || back.Steps["find"].Prompt != want {
		t.Fatalf("%q", back.Steps["find"].Prompt)
	}
	// A shipped name the file had reworded keeps none of the file's words.
	got = mustApply(t, editBase, rename("write", "build"))
	back = reread(t, got.Text)
	if back.Steps["build"].Prompt != "carry out the plan" || back.Steps["write"].Prompt != Builtins()["write"].Prompt {
		t.Fatalf("%+v %+v", back.Steps["build"], back.Steps["write"])
	}
}
