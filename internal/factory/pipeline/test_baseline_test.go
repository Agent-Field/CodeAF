package pipeline_test

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

func TestTestBaselinePolicyCanBeTightened(t *testing.T) {
	const baseline = factory.UntilNoNewFailures
	testUntil := func(f pipeline.File) string {
		stages := f.Recipe().For(factory.KindIssue)
		return stages[factory.StageIndex(stages, "test")].Until
	}
	if got := testUntil(pipeline.Defaults()); got != baseline {
		t.Errorf("default Test until = %q, want %q", got, baseline)
	}
	green := factory.UntilGreen
	strict, err := pipeline.ApplyEdits("", []pipeline.EditOp{{Op: pipeline.OpSetStep, Name: "test", Until: &green}})
	if err != nil || !strict.Changed {
		t.Fatalf("strict Test edit = %+v, %v", strict, err)
	}
	strictFile, err := pipeline.Parse(strict.Text)
	if err != nil || testUntil(strictFile) != green {
		t.Fatalf("strict Test file did not retain until: green: %v\n%s", err, strict.Text)
	}
	option := baseline
	restored, err := pipeline.ApplyEdits(strict.Text, []pipeline.EditOp{{Op: pipeline.OpSetStep, Name: "test", Until: &option}})
	if err != nil || !strings.Contains(restored.Text, "until: no new failures") {
		t.Fatalf("baseline Test edit = %+v, %v", restored, err)
	}
	personal, err := pipeline.ParsePersonal("steps:\n  test:\n    until: green\n")
	if err != nil {
		t.Fatal(err)
	}
	tightened, problems := pipeline.Merge(pipeline.Defaults(), personal)
	if len(problems) != 0 || testUntil(tightened) != green {
		t.Fatalf("personal strict Test until = %q, problems = %v", testUntil(tightened), problems)
	}
	personal, err = pipeline.ParsePersonal("steps:\n  test:\n    until: no new failures\n")
	if err != nil {
		t.Fatal(err)
	}
	loosened, problems := pipeline.Merge(strictFile, personal)
	if len(problems) == 0 || testUntil(loosened) != green {
		t.Fatalf("personal file weakened strict Test: until = %q, problems = %v", testUntil(loosened), problems)
	}
}

func TestBaselinePolicyIsLimitedToTestConversation(t *testing.T) {
	const baseline = factory.UntilNoNewFailures
	for _, tc := range []struct{ name, kind string }{{"proof", "chat"}, {"test", "check"}} {
		f, err := pipeline.Parse("pipeline:\n  issue: [" + tc.name + "]\nsteps:\n  " + tc.name + ":\n    kind: " + tc.kind + "\n    prompt: go test ./...\n    until: " + baseline + "\n")
		if err != nil {
			t.Fatal(err)
		}
		stages := f.Recipe().For(factory.KindIssue)
		if len(f.Problems) == 0 || len(stages) != 1 || stages[0].Kind != factory.StageGate {
			t.Errorf("unsupported %s %s did not hold: problems = %v, stages = %+v", tc.name, tc.kind, f.Problems, stages)
		}
	}
	option := baseline
	if _, err := pipeline.ApplyEdits("", []pipeline.EditOp{{Op: pipeline.OpSetStep, Name: "review", Until: &option}}); err == nil {
		t.Fatal("review accepted the Test-only policy")
	}
	renamed, err := pipeline.ApplyEdits("", []pipeline.EditOp{{Op: pipeline.OpRenameStep, Name: "test", To: "verify"}})
	if err != nil {
		t.Fatal(err)
	}
	f, err := pipeline.Parse(renamed.Text)
	if err != nil || f.Steps["verify"].Until != factory.UntilGreen {
		t.Fatalf("renamed Test did not keep strict verification: %v\n%s", err, renamed.Text)
	}
}
