package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
)

// THE MANAGER'S EDIT OF A RUNNING JOB REACHES ITS WORKER: it goes through the
// door a person's edit does, so the worker running the job from its own copy
// is withdrawn and pulls the edited steps, and a report from that old copy
// can no longer write its steps back over the edit.
func TestTheManagersEditOfARunningJobIsNotLostToItsWorker(t *testing.T) {
	st, web, _ := talkLab(t)
	ctx := context.Background()
	talk := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(talk, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stage := func(name string) factory.Stage {
		return factory.Stage{Name: name, Kind: factory.StageChat, Ask: "do " + name, Until: "done", On: true}
	}
	id, err := st.Add(ctx, factory.Item{Repo: "web", Title: "fix the ledger", Kind: factory.KindIssue, Tier: factory.TierOwner, Talk: talk,
		Stages: []factory.Stage{stage("plan"), stage("write"), stage("review")}})
	if err != nil {
		t.Fatal(err)
	}
	desk, err := lease.Open(st, lease.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.NewDoors(desk).Launch(id); err != nil {
		t.Fatal(err)
	}
	held, err := desk.Pull(lease.Worker{ID: "w1", Handle: "sam", TakesUnassigned: map[string]bool{lease.EveryRepo: true}})
	if err != nil || held.Job != id {
		t.Fatalf("pulled %+v %v", held, err)
	}
	// The worker's copy, as it pulled the job.
	mirror, err := st.Get(id)
	if err != nil || mirror.Stream == nil {
		t.Fatalf("pulled %+v %v", mirror, err)
	}

	door := runDoor(st, web, "")
	if _, _, err := door.EditRun(ctx, talk, factory.RunEdit{By: factory.ByManager, Ask: map[string]string{"review": "read it for security"}, Why: "the person asked"}); err != nil {
		t.Fatal(err)
	}
	// The worker reports a reshaped pipeline from the copy it runs.
	report := factory.RunReport{Pipeline: &factory.PipelineChange{Stages: mirror.Stages, Phases: mirror.Stream.Phases}}
	_ = desk.Apply(held.ID, "w1", false, func(it *factory.Item) error { return factoryrun.ApplyReport(it, report, time.Now()) })

	saved, err := st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if i := factory.StageIndex(saved.Stages, "review"); i < 0 || saved.Stages[i].Ask != "read it for security" {
		t.Fatalf("the manager's edit was lost: %+v", saved.Stages)
	}
	if live, err := desk.HasLiveHold(id); err != nil || live {
		t.Fatalf("the worker still holds the job it runs from an old copy (%v)", err)
	}
}
