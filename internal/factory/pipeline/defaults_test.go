package pipeline_test

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// ONE DEFAULT RECIPE. A repository with no pipeline file, and an empty one,
// run exactly [factory.DefaultRecipe], stage for stage and field for field, for
// every kind: a job a person starts is stamped with the default recipe on
// arrival, and one a start rule starts is re-stamped with the file's recipe
// (internal/factory/intake), so any difference here is a job that runs other
// steps, asks or rounds depending on who started it. Until 2026-10-20 the two
// were separate tables that had drifted on every one of those.
func TestTheDefaultFileIsTheDefaultRecipe(t *testing.T) {
	empty, err := pipeline.Parse("")
	if err != nil {
		t.Fatal(err)
	}
	want := factory.DefaultRecipe()
	for name, f := range map[string]pipeline.File{"defaults": pipeline.Defaults(), "an empty file": empty} {
		got := f.Recipe()
		for _, kind := range []factory.Kind{factory.KindIssue, factory.KindPR, factory.KindCI, factory.KindChore} {
			if !reflect.DeepEqual(got.For(kind), want.For(kind)) {
				t.Errorf("%s: %s runs\n%s\nwant the default recipe's\n%s", name, kind,
					stageLines(got.For(kind)), stageLines(want.For(kind)))
			}
		}
		if got.DefaultBudget != want.DefaultBudget {
			t.Errorf("%s: budget %v, want %v", name, got.DefaultBudget, want.DefaultBudget)
		}
	}
}

// EVERY SHIPPED STAGE IS A BUILT-IN STEP under its own name, with its own
// prompt, until and rounds, so a file that names one without saying more runs
// the shipped stage, and `codeaf factory steps` prints what a job runs.
func TestEveryShippedStageIsABuiltInStep(t *testing.T) {
	builtins := pipeline.Builtins()
	shipped := factory.ShippedStages()
	if len(builtins) != len(shipped) {
		t.Fatalf("%d built-in steps for %d shipped stages", len(builtins), len(shipped))
	}
	for _, st := range shipped {
		step, ok := builtins[st.Name]
		if !ok {
			t.Errorf("%s is shipped but not a built-in step", st.Name)
			continue
		}
		if factory.IsApprove(st) {
			continue
		}
		if step.Prompt != st.Ask || step.Until != st.Until || step.Max != st.Max || step.Fanout != st.Fanout {
			t.Errorf("%s: step %+v, shipped %+v", st.Name, step, st)
		}
	}
	// A file that names security, which no default list does, runs the
	// shipped one.
	f, err := pipeline.Parse("pipeline:\n  issue: [plan, approve, write, test, review, security, proof]\n")
	if err != nil {
		t.Fatal(err)
	}
	sec, _ := factory.ShippedStage("security")
	if got := f.Recipe().Stages[factory.StageIndex(f.Recipe().Stages, "security")]; !reflect.DeepEqual(got, sec) {
		t.Fatalf("security = %+v, want the shipped %+v", got, sec)
	}
}

// A FILE STILL SETS ITS OWN ROUNDS: `max:` on a shipped step replaces the
// shipped figure, lower or higher, and the rest of the step is kept.
func TestAFileSetsItsOwnRounds(t *testing.T) {
	f, err := pipeline.Parse("steps:\n  review:\n    max: 1\n  test:\n    max: 5\n")
	if err != nil {
		t.Fatal(err)
	}
	r := f.Recipe()
	review := r.Stages[factory.StageIndex(r.Stages, "review")]
	test := r.Stages[factory.StageIndex(r.Stages, "test")]
	shipped, _ := factory.ShippedStage("review")
	if review.Max != 1 || test.Max != 5 || review.Ask != shipped.Ask || review.Until != factory.UntilClean {
		t.Fatalf("review %+v, test %+v", review, test)
	}
}

func stageLines(stages []factory.Stage) string {
	out := ""
	for _, line := range factory.StageLines(stages) {
		out += "  " + line + "\n"
	}
	return out
}
