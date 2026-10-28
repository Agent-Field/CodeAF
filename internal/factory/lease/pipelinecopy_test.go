package lease_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// THE FACTORY'S COPY GOES WITH THE JOB: a first start read from the copy runs
// its steps, decides the copy on the job (its record says so), and the copy
// crosses the wire whole, so a runner on another machine reads the same file.
// THE DECISION IS FOR THE JOB'S LIFE: a later start, after the factory stopped
// its copy, keeps it, and says nothing new.
func TestALaunchCarriesTheFactorysCopyToAnyRunner(t *testing.T) {
	f := setup(t)
	saved := &factory.PipelineCopy{Repo: "acme/api", Text: "pipeline:\n  issue: [plan, write, review, proof]\nrules:\n  - the copy's rule\n",
		By: "test-owner", At: time.Date(2026, 10, 28, 9, 30, 0, 0, time.UTC)}
	read, err := pipeline.CopyFile(*saved)
	if err != nil {
		t.Fatal(err)
	}
	doors := lease.NewDoors(f.desk).WithStartRead(func(context.Context, factory.Item) (pipeline.File, error) { return read, nil })
	defaults := factory.CopyStages(factory.DefaultRecipe().For(factory.KindIssue))
	it := f.add(t, factory.Item{Kind: factory.KindIssue, State: factory.StateNew, Stages: defaults})
	if err := doors.Launch(it.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := f.store.Get(it.ID)
	if !got.PipelineCopy.Same(saved) || !got.PipelineDecided {
		t.Fatalf("the job carries %+v (decided %v)", got.PipelineCopy, got.PipelineDecided)
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
	if back := api.ToItem(pulled); !reflect.DeepEqual(back.PipelineCopy, got.PipelineCopy) || !back.PipelineDecided {
		t.Fatalf("the copy crossed as %+v (decided %v)", back.PipelineCopy, back.PipelineDecided)
	}
	// A READER'S JOB SAYS WHO SAVED IT AND WHEN, NOT THE FILE: a queue of
	// dozens of jobs never carries dozens of files.
	raw, err = json.Marshal(api.FromItem(got))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "the copy's rule") || !strings.Contains(string(raw), `"pipeline_copy":{"repo":"acme/api","by":"test-owner","at":"2026-10-28T09:30:00Z"},"pipeline_decided":true`) {
		t.Fatalf("a reader's job carries %s", raw)
	}

	// A later start, the factory reading main's file now, keeps the copy.
	main, err := pipeline.Parse("pipeline:\n  issue: [plan, write, proof]\n")
	if err != nil {
		t.Fatal(err)
	}
	main.From = factory.FromMainBranch
	if err := f.store.Update(it.ID, func(it *factory.Item) error { it.State = factory.StateNew; it.Stream = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := lease.NewDoors(f.desk).WithStartRead(func(context.Context, factory.Item) (pipeline.File, error) { return main, nil }).Launch(it.ID); err != nil {
		t.Fatal(err)
	}
	again, _ := f.store.Get(it.ID)
	notes := 0
	for _, e := range again.Activity {
		if strings.Contains(e.What, factory.PipelineFile) {
			notes++
		}
	}
	if !again.PipelineCopy.Same(saved) || notes != 1 {
		t.Fatalf("the copy changed: %+v\n%+v", again.PipelineCopy, again.Activity)
	}
}

// A JOB'S FILE IS DECIDED ONCE: one decided on a copy keeps it, whether the
// factory has since stopped its copy or saved another, and its record says
// nothing new; one decided on main's file stays on main's file when the
// factory saves a copy afterwards; one never decided, even with steps changed
// for it, takes the copy its first start read; and a first start whose read of
// main failed decides main's file.
func TestAJobsPipelineFileIsDecidedOnce(t *testing.T) {
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
	launch := func(t *testing.T, id int, read pipeline.File, readErr error) factory.Item {
		t.Helper()
		if err := lease.NewDoors(f.desk).WithStartRead(func(context.Context, factory.Item) (pipeline.File, error) { return read, readErr }).Launch(id); err != nil {
			t.Fatal(err)
		}
		got, _ := f.store.Get(id)
		return got
	}
	ran := []factory.Stage{{Name: "plan", Kind: factory.StageChat, Ask: "say how", On: true}, {Name: "write", Kind: factory.StageChat, Ask: "make it", On: true}, {Name: "proof", Kind: factory.StageChat, Ask: "prove it", Until: factory.UntilProven, On: true}}
	for name, read := range map[string]pipeline.File{"main's file now": main, "another copy now": saved} {
		t.Run("a copy kept, "+name, func(t *testing.T) {
			it := f.add(t, factory.Item{Kind: factory.KindIssue, State: factory.StateNew, Stages: factory.CopyStages(ran), PipelineCopy: started.Clone(), PipelineDecided: true,
				Stream: &factory.Stream{Phases: []factory.Phase{{Name: "plan", State: factory.PhaseDone}}}})
			got := launch(t, it.ID, read, nil)
			if !got.PipelineCopy.Same(started) || strings.Join(stageNames(got.Stages), ",") != "plan,write,proof" {
				t.Fatalf("the job now carries %+v and runs %v", got.PipelineCopy, stageNames(got.Stages))
			}
			for _, e := range got.Activity {
				if strings.Contains(e.What, factory.PipelineFile) {
					t.Fatalf("the record says %q", e.What)
				}
			}
		})
	}
	t.Run("main's file kept", func(t *testing.T) {
		it := f.add(t, factory.Item{Kind: factory.KindIssue, State: factory.StateNew, Stages: factory.CopyStages(ran), PipelineDecided: true})
		if got := launch(t, it.ID, saved, nil); got.PipelineCopy != nil || !got.PipelineDecided {
			t.Fatalf("the job decided on main now carries %+v", got.PipelineCopy)
		}
	})
	t.Run("never decided, steps changed for it", func(t *testing.T) {
		it := f.add(t, factory.Item{Kind: factory.KindIssue, State: factory.StateNew, Stages: factory.CopyStages(ran), Adapted: []string{"you removed review"}})
		got := launch(t, it.ID, saved, nil)
		if !got.PipelineCopy.Same(later) || !got.PipelineDecided || strings.Join(stageNames(got.Stages), ",") != "plan,write,proof" {
			t.Fatalf("the job carries %+v and runs %v", got.PipelineCopy, stageNames(got.Stages))
		}
	})
	t.Run("never decided, main unread", func(t *testing.T) {
		it := f.add(t, factory.Item{Kind: factory.KindIssue, State: factory.StateNew, Stages: factory.CopyStages(ran)})
		got := launch(t, it.ID, pipeline.Defaults(), errors.New("factory.yaml: github is not connected"))
		if got.PipelineCopy != nil || !got.PipelineDecided {
			t.Fatalf("the job carries %+v (decided %v)", got.PipelineCopy, got.PipelineDecided)
		}
	})
}
