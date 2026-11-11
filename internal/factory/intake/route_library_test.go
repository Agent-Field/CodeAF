package intake_test

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/route"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// A ROUTING RULE MAY NAME A PIPELINE ONLY THE FACTORY'S LIBRARY HAS: the job
// it routes runs the library's steps, the same resolution a Start uses
// ([pipeline.File.Resolve]).
func TestARoutedJobRunsALibraryPipelineTheRepositoryLacks(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"acme/demo"}); err != nil {
		t.Fatal(err)
	}
	rule := route.Rule{Name: "jira FL", When: route.Match{Sources: []string{"jira"}},
		Then: route.Action{Repo: "acme/demo", Pipeline: "triage", Start: route.StartBacklog}}
	if _, err := st.SetRoutes([]route.Rule{rule}, factory.RulesChange{}, nil); err != nil {
		t.Fatal(err)
	}
	job, err := st.Create(factory.Item{Origin: "jira", SourceRef: "jira:FL-8", Kind: factory.KindIssue, Tier: factory.TierOwner, State: factory.StateNew,
		Title: "Checkout button is grey", Body: "fix docs/readme.md", Created: time.Unix(100, 0)})
	if err != nil {
		t.Fatal(err)
	}
	repo, err := pipeline.Parse(routedFile)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := pipeline.ParseLibrary("pipeline:\n  triage: [reproduce, proof]\nsteps:\n  reproduce:\n    prompt: reproduce it first\n")
	if err != nil {
		t.Fatal(err)
	}
	history, err := record.Open(st, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	in := intake.New(st, intake.Options{Record: history,
		Load:   func(string) (pipeline.File, error) { return pipeline.WithLibrary(repo, lib), nil },
		Launch: func(int) error { return nil }})
	if err := in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Route != "jira FL" || stageNames(got.Stages) != "reproduce proof" || got.Stages[0].Ask != "reproduce it first" {
		t.Fatalf("routed job %+v stages %s", got, stageNames(got.Stages))
	}
}
