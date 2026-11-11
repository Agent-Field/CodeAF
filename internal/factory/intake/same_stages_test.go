package intake_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// A JOB A RULE STARTS RUNS WHAT A JOB A PERSON STARTS RUNS. An issue arrives
// through the poll stamped with the default recipe, which is what a person's
// start runs; a start rule's start re-stamps it with the repository's pipeline
// recipe, which on a repository whose file names no steps is the defaults.
// Until 2026-10-20 that re-stamp swapped in a second default with other asks
// and no rounds, so the same issue ran differently depending on who started it.
func TestARuleStartedJobRunsTheStagesAPersonStartedOneWould(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"owner/repo"}); err != nil {
		t.Fatal(err)
	}
	src := source{[]factory.Item{{Repo: "owner/repo", Num: 1, Kind: factory.KindIssue, Tier: factory.TierOwner, Title: "Incoming job",
		Body: "fix docs/readme.md", Labels: []string{"agent-ready", "size:S"},
		Triage: factory.Triage{StartJudged: true, StartMatched: []string{}}}}}
	if _, err := github.PollOnce(ctx, src, st, "", func() time.Time { return time.Unix(100, 0) }); err != nil {
		t.Fatal(err)
	}
	items, err := st.List()
	if err != nil || len(items) != 1 {
		t.Fatalf("items %+v %v", items, err)
	}
	arrived := factory.CopyStages(items[0].Stages)
	if !reflect.DeepEqual(arrived, factory.DefaultRecipe().For(factory.KindIssue)) {
		t.Fatalf("the issue arrived with\n%s\nnot the default recipe", strings.Join(factory.StageLines(arrived), "\n"))
	}

	f, err := pipeline.Parse("start:\n  labels: [agent-ready]\n")
	if err != nil {
		t.Fatal(err)
	}
	f.DefaultBudget = 1
	history, err := record.Open(st, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	started := 0
	in := intake.New(st, intake.Options{Record: history,
		Load:   func(string) (pipeline.File, error) { return f, nil },
		Launch: func(int) error { started++; return nil }})
	if err := in.Once(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get(items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if started != 1 {
		t.Fatalf("the rule did not start the job: %d starts, job %+v", started, got)
	}
	if !reflect.DeepEqual(got.Stages, arrived) {
		t.Fatalf("the rule's start runs\n%s\nwhere a person's start runs\n%s",
			strings.Join(factory.StageLines(got.Stages), "\n"), strings.Join(factory.StageLines(arrived), "\n"))
	}
}
