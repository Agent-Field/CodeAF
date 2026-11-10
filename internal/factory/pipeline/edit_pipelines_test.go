package pipeline

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── add_pipeline, remove_pipeline and rename_pipeline ───────────────────────

func addPipeline(name string, steps ...string) EditOp {
	return EditOp{Op: OpAddPipeline, Name: name, Steps: steps}
}

func addPipelineFrom(name, from string) EditOp {
	return EditOp{Op: OpAddPipeline, Name: name, From: from}
}

func removePipeline(name string) EditOp { return EditOp{Op: OpRemovePipeline, Name: name} }

func renamePipeline(name, to string) EditOp {
	return EditOp{Op: OpRenamePipeline, Name: name, To: to}
}

// A NEW NAMED PIPELINE IS ONE NEW LIST, and a step it needs that does not
// exist yet is added to it in the same preview. Nothing else in the file moves.
func TestAddPipelineMakesANamedPipelineAndAStepCanBeAddedToIt(t *testing.T) {
	got := mustApply(t, editBase,
		addPipeline("docs", "write", "proof"),
		add("docs", "docs-check", "write", "check the docs build"))
	if !got.Changed || !got.Structural {
		t.Fatalf("%+v", got)
	}
	back := reread(t, got.Text)
	if g := joined(back.Pipeline["docs"]); g != "write,docs-check,proof" {
		t.Fatalf("docs %s", g)
	}
	if g := joined(back.Pipeline[factory.KindIssue]); g != "plan,approve,write,test,lint-pass,proof" {
		t.Fatalf("issue moved: %s", g)
	}
	if back.Steps["docs-check"].Prompt != "check the docs build" {
		t.Fatalf("%+v", back.Steps["docs-check"])
	}
	if len(got.Pipelines) != 1 || got.Pipelines[0].Op != OpAddPipeline || got.Pipelines[0].Pipeline != "docs" || joined(got.Pipelines[0].Steps) != "write,proof" {
		t.Fatalf("pipelines %+v", got.Pipelines)
	}
	var order bool
	for _, o := range got.Order {
		if o.Pipeline == "docs" && len(o.Before) == 0 && joined(o.After) == "write,docs-check,proof" {
			order = true
		}
	}
	if !order || len(got.Added) != 1 || got.Added[0] != "docs-check" {
		t.Fatalf("order %+v added %v", got.Order, got.Added)
	}
	// The file runs it under its name, as a Start pick or a rule would.
	if stages := back.Named("docs"); len(stages) != 3 || stages[1].Name != "docscheck" {
		t.Fatalf("named %+v", stages)
	}
}

// A REPOSITORY WITH NO FILE MAKES ONE: the new file holds the new pipeline and
// nothing the person did not write.
func TestAddPipelineOnARepositoryWithNoFileWritesOnlyThatPipeline(t *testing.T) {
	got := mustApply(t, "", addPipeline("quick", "write", "review", "proof"))
	want := "pipeline:\n    quick:\n        - write\n        - review\n        - proof\n"
	if got.Text != want {
		t.Fatalf("got\n%s\nwant\n%s", got.Text, want)
	}
}

func TestAddPipelineFromCopiesAnotherPipeline(t *testing.T) {
	got := mustApply(t, editBase, addPipelineFrom("careful", "issue"))
	back := reread(t, got.Text)
	if g := joined(back.Pipeline["careful"]); g != "plan,approve,write,test,lint-pass,proof" {
		t.Fatalf("careful %s", g)
	}
}

func TestRemoveAndRenamePipeline(t *testing.T) {
	base := editBase + "adapt:\n  docs: fixed\n"
	base = strings.Replace(base, "  ci: [bisect, fix, test, proof, approve]\n", "  ci: [bisect, fix, test, proof, approve]\n  docs: [write, proof]\n  quick: [write, proof]\n", 1)
	got := mustApply(t, base, renamePipeline("docs", "handbook"), removePipeline("quick"))
	back := reread(t, got.Text)
	if _, ok := back.Pipeline["docs"]; ok {
		t.Fatal("docs is still there")
	}
	if _, ok := back.Pipeline["quick"]; ok {
		t.Fatal("quick is still there")
	}
	if g := joined(back.Pipeline["handbook"]); g != "write,proof" {
		t.Fatalf("handbook %s", g)
	}
	if back.AdaptKinds["handbook"] != factory.AdaptFixed {
		t.Fatalf("the adapt word did not move with the name: %v", back.AdaptKinds)
	}
	// The renamed list did not change, so it is no order change.
	for _, o := range got.Order {
		if o.Pipeline == "handbook" {
			t.Fatalf("a rename alone moved steps: %+v", o)
		}
	}
	if len(got.Pipelines) != 2 || got.Pipelines[0].To != "handbook" || got.Pipelines[1].Op != OpRemovePipeline {
		t.Fatalf("%+v", got.Pipelines)
	}
	// A removed pipeline's steps keep their definitions.
	if back.Steps["write"].Prompt != "carry out the plan" {
		t.Fatalf("%+v", back.Steps["write"])
	}
}

// THE REFUSALS: a reserved or taken name, a step nobody defined, an empty
// list, both or neither of steps and from, a pipeline named twice, and the
// proof law on the new list.
func TestPipelineVerbsRefuse(t *testing.T) {
	for _, tc := range []struct {
		name     string
		ops      []EditOp
		code     string
		op       int
		pipeline bool
		why      string
	}{
		{"pr is reserved", []EditOp{addPipeline("pr", "write")}, EditLocked, 0, true, "PR jobs"},
		{"ci is reserved", []EditOp{addPipeline("ci", "write")}, EditLocked, 0, true, "CI jobs"},
		{"issue is always there", []EditOp{addPipeline("issue", "write")}, EditLocked, 0, true, "always there"},
		{"issue is not removed", []EditOp{removePipeline("issue")}, EditLocked, 0, true, ""},
		{"pr is not removed", []EditOp{removePipeline("pr")}, EditLocked, 0, true, ""},
		{"nothing is renamed into ci", []EditOp{addPipeline("docs", "write"), renamePipeline("docs", "ci")}, EditLocked, 1, true, ""},
		{"a badly spelled name", []EditOp{addPipeline("Docs!", "write")}, EditBadValue, 0, true, ""},
		{"a taken name", []EditOp{addPipeline("docs", "write"), renamePipeline("docs", "docs")}, EditBadValue, 1, true, ""},
		{"a step nobody defined", []EditOp{addPipeline("docs", "write", "nosuchstep")}, EditUnknownStep, 0, false, "add_step it there"},
		{"an empty list", []EditOp{{Op: OpAddPipeline, Name: "docs", Steps: []string{}}}, EditBadValue, 0, false, ""},
		{"both steps and from", []EditOp{{Op: OpAddPipeline, Name: "docs", Steps: []string{"write"}, From: "issue"}}, EditBadOp, 0, false, ""},
		{"neither steps nor from", []EditOp{{Op: OpAddPipeline, Name: "docs"}}, EditBadOp, 0, false, ""},
		{"from a pipeline that is not there", []EditOp{addPipelineFrom("docs", "nope")}, EditUnknownStep, 0, true, ""},
		{"a step listed twice", []EditOp{addPipeline("docs", "write", "write")}, EditBadValue, 0, false, ""},
		{"made twice", []EditOp{addPipeline("docs", "write"), addPipeline("docs", "proof")}, EditBadOp, 1, false, ""},
		{"removing a pipeline that is not there", []EditOp{removePipeline("docs")}, EditUnknownStep, 0, true, ""},
		{"steps on another verb", []EditOp{{Op: OpRemovePipeline, Name: "docs", Steps: []string{"write"}}}, EditBadOp, 0, false, ""},
		{"a chat after proof", []EditOp{addPipeline("docs", "proof", "write")}, EditLocked, 0, false, "proof has to see the work"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := refusal(t, editBase, tc.ops...)
			wantCode(t, e, tc.code, tc.op)
			if e.Pipeline != tc.pipeline {
				t.Fatalf("about a pipeline %v, want %v (%s)", e.Pipeline, tc.pipeline, e.Why)
			}
			if tc.why != "" && !strings.Contains(e.Why, tc.why) {
				t.Fatalf("why %q lacks %q", e.Why, tc.why)
			}
		})
	}
}

// AN ADD_STEP TO A PIPELINE THE FILE DOES NOT HAVE SAYS PIPELINE: it once read
// "That is not a step in this pipeline".
func TestAddStepToAMissingPipelineIsAboutThePipeline(t *testing.T) {
	e := refusal(t, editBase, add("docs", "write", "", "write it"))
	wantCode(t, e, EditUnknownStep, 0)
	if !e.Pipeline || !strings.Contains(e.Why, "docs is not a pipeline in this file") {
		t.Fatalf("%+v", e)
	}
}
