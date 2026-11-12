package main

import (
	"context"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
)

func TestAppFactoryRunAsNew(t *testing.T) {
	for _, ending := range []string{"failed", "stopped", "closed", "done", "running", "repo off", "no checkout", "launch refused"} {
		t.Run(ending, func(t *testing.T) {
			t.Setenv("OPENROUTER_API_KEY", "sk-test-not-real")
			s := newLocalSetup(t)
			// Connect the fixture identity after serving, so local_start cannot
			// turn on an automatic worker. Launch still uses the real lease desk.
			var connected atomic.Bool
			hook := s.backend.local.seamHook
			s.backend.local.seamHook = func(seam factory.Seam) factory.Seam {
				seam = hook(seam)
				seam.ConnectGitHub = func(context.Context, string) error { return nil }
				seam.GitHub = func(context.Context) (factory.GitHubLink, error) {
					if !connected.Load() {
						return factory.GitHubLink{}, nil
					}
					return factory.GitHubLink{Login: "owner"}, nil
				}
				if ending == "launch refused" {
					seam.Launch = func(int) error { return factoryrun.AlreadyRunning("fixture") }
				}
				return seam
			}
			st := s.start()
			if st.Runner {
				t.Fatal("fixture started an automatic runner")
			}
			connected.Store(true)
			// Connecting invalidates the host's cached anonymous identity. This
			// fixture connect does not turn the app's automatic runner on.
			if err := s.backend.local.host.seam.ConnectGitHub(context.Background(), "fixture"); err != nil {
				t.Fatal(err)
			}
			if ending != "no checkout" {
				if err := s.backend.local.host.floor.SetCheckout("acme/api", t.TempDir()); err != nil {
					t.Fatal(err)
				}
			}
			source := factory.Item{Tier: factory.TierOwner, Repo: "acme/api", Num: 42, Kind: factory.KindIssue, Origin: factory.OriginForge,
				Title: "Fix auto-save $5", Body: "  security, two review rounds\nKeep  these words.\n", URL: "https://github.com/acme/api/issues/42",
				Ask:  &factory.JobAsk{Text: "  cheaper; ship it; plan first\n", Source: factory.AskApp},
				Talk: "old-conversation", Head: "old-branch",
				State: factory.StateNew, Autonomy: factory.AutonomyManual, Cap: 20,
				Stages:      []factory.Stage{{Name: "write", Kind: factory.StageChat, On: true, Ask: "Keep the words", Model: "test/model"}},
				PullRequest: &factory.PullRequest{Number: 90, State: factory.PROpen},
				Stream:      &factory.Stream{Spent: 3}, Failure: &factory.Failure{Step: "write", Code: "failed", Next: "stopped"},
			}
			switch ending {
			case "stopped":
				source.Failure.Code = "stopped"
			case "closed":
				source.Failure = nil
				source.DoneHow = "closed_locally"
			case "done":
				source.Failure = nil
				source.State = factory.StateShipped
				source.PullRequest.State = factory.PRMerged
			case "running":
				source.Failure = nil
				source.State = factory.StateRunning
			}
			source.DecidePipeline(&factory.PipelineCopy{Repo: source.Repo, Text: "issue:\n  - write: Keep the words\n", By: "owner"}, source.Created)
			old, err := s.backend.local.host.floor.Create(source)
			if err != nil {
				t.Fatal(err)
			}
			read := func(id int) map[string]any {
				var j map[string]any
				s.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &j)
				return j
			}
			before := read(old.ID)
			if ending == "repo off" {
				s.must("factory.repo_set", map[string]any{"factory": st.Factory, "repo": source.Repo, "on": false}, nil)
				before = read(old.ID)
			}
			params := map[string]any{"factory": st.Factory, "job": old.ID, "expect": map[string]any{"version": before["version"]}}
			if ending == "stopped" {
				delete(params, "expect")
			}
			if ending == "running" || ending == "repo off" || ending == "no checkout" || ending == "launch refused" {
				s.mustFail("factory.job_run_as_new", params, appwire.CodeConflict)
				s.mustFail("factory.job", map[string]any{"factory": st.Factory, "job": old.ID + 1}, appwire.CodeNotFound)
			} else {
				can := before["can"].(map[string]any)
				if can["run_as_new"] != true {
					t.Errorf("ended job can.run_as_new = %v", can["run_as_new"])
				}
				var acted appwire.FactoryJobActed
				s.must("factory.job_run_as_new", params, &acted)
				if acted.Job <= old.ID {
					t.Fatalf("new job = %d; old = %d", acted.Job, old.ID)
				}
				got := read(acted.Job)
				if got["from_job"] != float64(old.ID) {
					t.Errorf("from_job = %v", got["from_job"])
				}
				var job api.Job
				s.must("factory.job", map[string]any{"factory": st.Factory, "job": acted.Job}, &job)
				if job.Title != source.Title || job.Ask == nil || job.Ask.Text != source.Ask.Text || job.Budget != 20 || job.PR != nil || job.Failure != nil || job.Cost != 0 || job.Num != 0 || job.Autonomy != source.Autonomy || job.Repo != source.Repo || job.SourceIssue != source.URL || job.Desk == nil || job.Desk.Body != source.Body || job.Desk.Head != "" || job.Desk.Talk != "" {
					t.Errorf("new job did not copy only the request: %+v", job)
				}
				if job.State != "queued" {
					t.Errorf("new job state = %q", job.State)
				}
				if len(job.Steps) != 1 || job.Steps[0].UseModel != "test/model" || !job.PipelineDecided || job.PipelineCopy == nil {
					t.Errorf("steps or decided pipeline lost: %+v", job)
				}
				if !strings.Contains(s.hello, "factory.job_run_as_new") {
					t.Error("hello lacks factory.job_run_as_new")
				}
				var activity appwire.FactoryActivity
				s.must("factory.activity", map[string]any{"factory": st.Factory}, &activity)
				linked := map[int]bool{}
				for _, e := range activity.Events {
					if e.Kind == "run_as_new" && e.Handle != "" {
						linked[e.Job] = true
					}
				}
				if !linked[old.ID] || !linked[acted.Job] {
					t.Errorf("missing paired receipts: %+v", activity.Events)
				}
			}
			if after := read(old.ID); !reflect.DeepEqual(before, after) {
				t.Errorf("old job changed:\nbefore %+v\nafter %+v", before, after)
			}
		})
	}
}
