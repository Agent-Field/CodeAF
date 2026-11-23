package serve_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

func TestWriteAccessRefusalIsDefinite(t *testing.T) {
	for _, door := range []string{"queue", "runner"} {
		t.Run(door, func(t *testing.T) {
			f := writeAccessJobs(t, door)
			it := f.create(t, factory.Item{Kind: factory.KindIssue, State: factory.StateNew, Tier: factory.TierStranger, Stages: []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true}}})
			w := f.post(t, it, "run", "", http.StatusConflict)
			var refusal struct {
				Error  string `json:"error"`
				Reason string `json:"reason"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &refusal); err != nil {
				t.Fatal(err)
			}
			if refusal.Error != (run.WriteAccessRefusal{}).Error() || refusal.Reason != "conflict" {
				t.Fatalf("refusal = %+v", refusal)
			}
		})
	}
}

func writeAccessJobs(t *testing.T, door string) *v2Jobs {
	t.Helper()
	f := newV2Jobs(t)
	if door == "runner" {
		f.h.Close()
		runner := run.New(run.Options{Store: f.st})
		f.seam.Launch = runner.Launch
		f.h = serve.New(f.seam, f.cfg)
	}
	return f
}

func TestStrangerStartRefusalStaysOnJobWithoutStarting(t *testing.T) {
	for _, door := range []string{"queue", "runner"} {
		for _, verb := range []string{"run", "start_now"} {
			t.Run(door+"/"+verb, func(t *testing.T) {
				f := writeAccessJobs(t, door)
				it := f.create(t, factory.Item{Kind: factory.KindIssue, State: factory.StateNew, Tier: factory.TierStranger, Stages: []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true}}})
				var first factory.Item
				for attempt := 0; attempt < 2; attempt++ {
					f.post(t, it, verb, "", http.StatusConflict)
					job := f.read(t, it)
					failure := job.Failure
					if failure == nil || failure.Step != "plan" || failure.Code != factory.CodeToolError || failure.Detail != (run.WriteAccessRefusal{}).Error() || failure.Next != factory.FailureStopped || failure.At.IsZero() {
						t.Fatalf("saved refusal = %+v", failure)
					}
					if job.Cost != 0 || failure.Attempts != 0 || failure.Rounds != 0 {
						t.Fatalf("refusal reported work: state=%s failure=%+v", job.State, failure)
					}
					saved, err := f.st.Get(it.ID)
					if err != nil {
						t.Fatal(err)
					}
					if saved.State != it.State || saved.Stream != nil {
						t.Fatalf("refusal started a run: state=%s stream=%+v", saved.State, saved.Stream)
					}
					if attempt > 0 && (!reflect.DeepEqual(saved.Activity, first.Activity) || !reflect.DeepEqual(saved.Notes, first.Notes)) {
						t.Fatal("repeated refusal stacked job history")
					}
					first = saved
				}
			})
		}
	}
}
