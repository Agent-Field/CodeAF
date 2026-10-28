package lease_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// THE FACTORY'S COPY GOES WITH THE JOB: a start read from the copy runs its
// steps, stamps the copy on the job (its record says so), and the copy crosses
// the wire whole, so a runner on another machine reads the same file. A later
// start under main's file takes it off again, and says so.
func TestALaunchCarriesTheFactorysCopyToAnyRunner(t *testing.T) {
	f := setup(t)
	saved := &factory.PipelineCopy{Repo: "acme/api", Text: "pipeline:\n  issue: [plan, write, review, proof]\nrules:\n  - the copy's rule\n",
		By: "test-owner", At: time.Date(2026, 10, 28, 9, 30, 0, 0, time.UTC)}
	read, err := pipeline.CopyFile(*saved)
	if err != nil {
		t.Fatal(err)
	}
	doors := lease.NewDoors(f.desk).WithStartRead(func(context.Context, string) (pipeline.File, error) { return read, nil })
	defaults := factory.CopyStages(factory.DefaultRecipe().For(factory.KindIssue))
	it := f.add(t, factory.Item{Kind: factory.KindIssue, State: factory.StateNew, Stages: defaults})
	if err := doors.Launch(it.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := f.store.Get(it.ID)
	if !got.PipelineCopy.Same(saved) {
		t.Fatalf("the job carries %+v", got.PipelineCopy)
	}
	if names := stageNames(got.Stages); strings.Join(names, ",") != "plan,write,review,proof" {
		t.Fatalf("the job runs %v", names)
	}
	said := ""
	for _, e := range got.Activity {
		said += e.What + "\n"
	}
	if !strings.Contains(said, "Runs this factory's copy of .codeaf/factory.yaml, saved 2026-10-28 09:30 UTC by @test-owner, not main's") {
		t.Fatalf("the record says:\n%s", said)
	}

	// Across the wire, as a runner on another machine pulls it.
	raw, err := json.Marshal(api.WorkerJob(got))
	if err != nil {
		t.Fatal(err)
	}
	var pulled api.Job
	if err := json.Unmarshal(raw, &pulled); err != nil {
		t.Fatal(err)
	}
	if back := api.ToItem(pulled); !reflect.DeepEqual(back.PipelineCopy, got.PipelineCopy) {
		t.Fatalf("the copy crossed as %+v", back.PipelineCopy)
	}
	// A READER'S JOB SAYS WHO SAVED IT AND WHEN, NOT THE FILE: a queue of
	// dozens of jobs never carries dozens of files.
	raw, err = json.Marshal(api.FromItem(got))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "the copy's rule") || !strings.Contains(string(raw), `"pipeline_copy":{"repo":"acme/api","by":"test-owner","at":"2026-10-28T09:30:00Z"}`) {
		t.Fatalf("a reader's job carries %s", raw)
	}

	// A later start under main's file takes it off.
	main, err := pipeline.Parse("pipeline:\n  issue: [plan, write, proof]\n")
	if err != nil {
		t.Fatal(err)
	}
	main.From = factory.FromMainBranch
	if err := f.store.Update(it.ID, func(it *factory.Item) error { it.State = factory.StateNew; it.Stream = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := lease.NewDoors(f.desk).WithStartRead(func(context.Context, string) (pipeline.File, error) { return main, nil }).Launch(it.ID); err != nil {
		t.Fatal(err)
	}
	again, _ := f.store.Get(it.ID)
	said = ""
	for _, e := range again.Activity {
		said += e.What + "\n"
	}
	if again.PipelineCopy != nil || !strings.Contains(said, "Runs main's .codeaf/factory.yaml again") {
		t.Fatalf("the copy stayed: %+v\n%s", again.PipelineCopy, said)
	}
}

// ONLY A JOB THAT NEVER RAN TAKES THE FILE A START READS NOW: one that ran
// keeps the copy it started under, whether the factory has since stopped its
// copy or saved another, and its record says nothing new; one that never ran,
// even with steps changed for it, takes the copy the start read.
func TestALaunchKeepsTheCopyAJobThatRanStartedUnder(t *testing.T) {
	f := setup(t)
	started := &factory.PipelineCopy{Repo: "acme/api", Text: "pipeline:\n  issue: [plan, write, proof]\nrules:\n  - the first copy's rule\n",
		By: "test-owner", At: time.Date(2026, 10, 28, 9, 30, 0, 0, time.UTC)}
	later := &factory.PipelineCopy{Repo: "acme/api", Text: "pipeline:\n  issue: [write, proof]\n", By: "test-owner", At: time.Date(2026, 10, 28, 11, 0, 0, 0, time.UTC)}
	main, err := pipeline.Parse("pipeline:\n  issue: [plan, write, review, proof]\n")
	if err != nil {
		t.Fatal(err)
	}
	main.From = factory.FromMainBranch
	saved, err := pipeline.CopyFile(*later)
	if err != nil {
		t.Fatal(err)
	}
	ran := []factory.Stage{{Name: "plan", Kind: factory.StageChat, Ask: "say how", On: true}, {Name: "write", Kind: factory.StageChat, Ask: "make it", On: true}, {Name: "proof", Kind: factory.StageChat, Ask: "prove it", Until: factory.UntilProven, On: true}}
	for name, read := range map[string]pipeline.File{"main's file now": main, "another copy now": saved} {
		t.Run(name, func(t *testing.T) {
			it := f.add(t, factory.Item{Kind: factory.KindIssue, State: factory.StateNew, Stages: factory.CopyStages(ran), PipelineCopy: started.Clone(),
				Stream: &factory.Stream{Phases: []factory.Phase{{Name: "plan", State: factory.PhaseDone}}}})
			if err := lease.NewDoors(f.desk).WithStartRead(func(context.Context, string) (pipeline.File, error) { return read, nil }).Launch(it.ID); err != nil {
				t.Fatal(err)
			}
			got, _ := f.store.Get(it.ID)
			if !got.PipelineCopy.Same(started) || strings.Join(stageNames(got.Stages), ",") != "plan,write,proof" {
				t.Fatalf("the job that ran now carries %+v and runs %v", got.PipelineCopy, stageNames(got.Stages))
			}
			for _, e := range got.Activity {
				if strings.Contains(e.What, factory.PipelineFile) {
					t.Fatalf("the record says %q", e.What)
				}
			}
		})
	}
	// Never ran, steps changed for it: its steps stay, the copy is the start's.
	it := f.add(t, factory.Item{Kind: factory.KindIssue, State: factory.StateNew, Stages: factory.CopyStages(ran), Adapted: []string{"you removed review"}})
	if err := lease.NewDoors(f.desk).WithStartRead(func(context.Context, string) (pipeline.File, error) { return saved, nil }).Launch(it.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := f.store.Get(it.ID)
	if !got.PipelineCopy.Same(later) || strings.Join(stageNames(got.Stages), ",") != "plan,write,proof" {
		t.Fatalf("the job that never ran carries %+v and runs %v", got.PipelineCopy, stageNames(got.Stages))
	}
}
