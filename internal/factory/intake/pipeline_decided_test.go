package intake_test

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// INTAKE DECIDES A JOB'S PIPELINE FILE WHEN IT STAMPS THE JOB FIRST: a rule's
// start stamps the copy the factory keeps on the job with its steps, so the
// launch right after runs that file and not one saved in between; and a job
// already decided is read through its own file (Options.LoadJob), never the
// factory's read of its repository now.
func TestIntakeDecidesTheJobsPipelineFileWithItsSteps(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"owner/repo"}); err != nil {
		t.Fatal(err)
	}
	kept := &factory.PipelineCopy{Repo: "owner/repo", Text: "start:\n  labels: [agent-ready]\npipeline:\n  issue: [write, proof]\n", By: "test-owner", At: time.Date(2026, 10, 28, 9, 30, 0, 0, time.UTC)}
	copied, err := pipeline.CopyFile(*kept)
	if err != nil {
		t.Fatal(err)
	}
	main, err := pipeline.Parse("start:\n  labels: [agent-ready]\npipeline:\n  issue: [plan, write, review, proof]\n")
	if err != nil {
		t.Fatal(err)
	}
	add := func(it factory.Item) factory.Item {
		it.Repo, it.Kind, it.State, it.Tier, it.Labels = "owner/repo", factory.KindIssue, factory.StateNew, factory.TierOwner, []string{"agent-ready"}
		it.Triage = factory.Triage{StartJudged: true, StartMatched: []string{}}
		it.Stages = factory.CopyStages(factory.DefaultRecipe().For(factory.KindIssue))
		got, err := st.Create(it)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	fresh := add(factory.Item{Num: 1, Title: "fresh"})
	onMain := add(factory.Item{Num: 2, Title: "decided on main", PipelineDecided: true})
	history, err := record.Open(st, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	in := intake.New(st, intake.Options{Record: history,
		Load: func(string) (pipeline.File, error) { return copied, nil },
		LoadJob: func(it factory.Item) (pipeline.File, error) {
			if it.PipelineIsDecided() && it.PipelineCopy == nil {
				return main, nil
			}
			return copied, nil
		},
		Launch: func(int) error { return nil }})
	if err := in.Once(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(fresh.ID)
	if !got.PipelineCopy.Same(kept) || !got.PipelineDecided || stageNames(got.Stages) != "write proof" {
		t.Fatalf("fresh: copy %+v decided %v steps %s", got.PipelineCopy, got.PipelineDecided, stageNames(got.Stages))
	}
	got, _ = st.Get(onMain.ID)
	if got.PipelineCopy != nil || !got.PipelineDecided || stageNames(got.Stages) != "plan write review proof" {
		t.Fatalf("decided on main: copy %+v steps %s", got.PipelineCopy, stageNames(got.Stages))
	}
}

func stageNames(stages []factory.Stage) string {
	out := ""
	for i, s := range stages {
		if i > 0 {
			out += " "
		}
		out += s.Name
	}
	return out
}
