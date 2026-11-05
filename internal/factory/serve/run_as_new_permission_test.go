package serve_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

func TestRunAsNewPersonalJobRuns(t *testing.T) {
	f := newV2Jobs(t)
	f.h.Close()
	ran := make(chan struct{}, 1)
	runner := run.New(run.Options{Store: f.st, Exec: map[factory.StageKind]run.Executor{factory.StageCheck: run.ExecutorFunc(func(context.Context, run.Job) (factory.StageResult, error) {
		ran <- struct{}{}
		return factory.StageResult{Done: true}, nil
	})}})
	f.seam.NewJob = factory.LocalSeam(f.st, time.Now()).NewJob
	f.seam.Launch = runner.Launch
	f.h = serve.New(f.seam, f.cfg)
	source := f.create(t, factory.Item{Title: "personal copy", Kind: factory.KindIssue, Tier: factory.TierStranger, Origin: factory.OriginForge, Num: 12, State: factory.StateNew, Failure: &factory.Failure{Next: factory.FailureStopped}, Stages: []factory.Stage{{Name: "test", Kind: factory.StageCheck, On: true}}})
	w := f.post(t, source, "run-as-new", "", 201)
	var out struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("personal copy did not run")
	}
	for deadline := time.Now().Add(5 * time.Second); runner.Running(out.ID) && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	if runner.Running(out.ID) {
		t.Fatal("personal copy did not finish")
	}
	saved, err := f.st.Get(out.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Failure != nil || saved.State != factory.StateLanded {
		t.Fatalf("personal copy=%s refusal=%+v", saved.State, saved.Failure)
	}
}
