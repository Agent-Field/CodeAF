package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Earlier committed work belongs to the job even when its replacement attempt
// makes no new edit. An empty branch still needs the person's decision.
func TestFactoryWriteUsesJobBaseAcrossAttempts(t *testing.T) {
	for _, action := range []string{"rerun", "try again", "resume after stop", "resume after reported stop", "empty rerun"} {
		t.Run(action, func(t *testing.T) {
			s := newLocalSetup(t)
			s.backend.emit = s.server
			t.Setenv(factoryBenchesEnv, "1")
			model := newStageModel(t)
			t.Setenv("OPENROUTER_API_KEY", "test-key")
			t.Setenv("CODEAF_BASE_URL", model.server.URL)
			g := newRunRig(t)
			freshEngineProcess(t)
			freshEngineFactoryStore(t)
			s.backend.open = appOpenInProcess
			s.backend.local.parent = func() session.Config {
				return session.Config{Workspace: g.workspace, Model: "test/model", OneModel: true, APIKey: "test-key", BaseURL: model.server.URL}
			}
			st := s.start()
			floor := s.backend.local.host.floor
			if err := floor.SetCheckout("acme/api", g.workspace); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			entered, ready := filepath.Join(dir, "entered"), filepath.Join(dir, "ready")
			command := "if ! test -f CHANGE.txt; then printf 'the requested change\n' > CHANGE.txt && git add CHANGE.txt && git -c user.name=t -c user.email=t@t -c commit.gpgsign=false commit -qm change; fi; git diff --exit-code HEAD"
			switch action {
			case "try again":
				command += fmt.Sprintf("; test -f %q", ready)
			case "resume after stop", "resume after reported stop":
				command += fmt.Sprintf("; touch %q; while ! test -f %q; do sleep 0.05; done", entered, ready)
			case "empty rerun":
				command = "true"
			}
			id, err := floor.Add(context.Background(), factory.Item{
				Repo: "acme/api", Num: 7, Kind: factory.KindIssue, Tier: factory.TierOwner, Cap: 10,
				Title: "Keep the job's earlier change", Autonomy: factory.AutonomyManual,
				Stages: []factory.Stage{
					{Name: "write", Kind: factory.StageCheck, On: true, Ask: command, Until: factory.UntilGreen, Max: 1},
					{Name: "approve-test", Kind: factory.StageGate, On: true},
					{Name: "test", Kind: factory.StageCheck, On: true, Ask: "true", Until: factory.UntilGreen, Max: 1},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			read := func() api.Job {
				var job api.Job
				s.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &job)
				return job
			}
			act := func(method string, extra map[string]any) {
				job := read()
				params := map[string]any{"factory": st.Factory, "job": id, "expect": map[string]any{"version": job.Version}}
				for k, v := range extra {
					params[k] = v
				}
				s.must(method, params, &appwire.FactoryJobActed{})
			}
			wait := func(what string, ok func(api.Job) bool) api.Job {
				t.Helper()
				deadline := time.Now().Add(15 * time.Second)
				for {
					job := read()
					if ok(job) {
						return job
					}
					if time.Now().After(deadline) {
						t.Fatalf("%s: state=%s needs=%+v steps=%+v", what, job.State, job.NeedsYou, job.Steps)
					}
					time.Sleep(20 * time.Millisecond)
				}
			}
			waiting := func(job api.Job) bool { return job.State == api.StateNeedsYou }
			gate := func(job api.Job) bool {
				return waiting(job) && job.Steps[0].State == "done" && job.Steps[1].State == "waiting"
			}
			act("factory.job_start", nil)
			if strings.HasPrefix(action, "resume after") {
				wait("earlier attempt commits before Stop", func(api.Job) bool { _, err := os.Stat(entered); return err == nil })
				if action == "resume after reported stop" {
					wait("first attempt is reported before Stop", func(job api.Job) bool { return job.Steps[0].State == "running" && job.Steps[0].Round > 0 })
				}
				t.Logf("before Stop: write=%s round=%d", read().Steps[0].State, read().Steps[0].Round)
				act("factory.job_step_stop", map[string]any{"step": "write"})
				wait("Stop ends the earlier attempt", func(job api.Job) bool { return job.Failure != nil && job.Failure.Code == factory.CodeStopped })
			} else {
				first := wait("first attempt finishes", waiting)
				if action == "try again" && (first.NeedsYou == nil || first.NeedsYou.Kind != factory.NeedsOutOfAttempts || first.Steps[0].State != "waiting") {
					t.Fatalf("first write did not reproduce a failed command: %+v", first)
				}
				if action == "rerun" && !gate(first) {
					t.Fatalf("write did not finish against its committed change: %+v", first)
				}
				if action == "empty rerun" && (first.Steps[0].State != "waiting" || first.Steps[1].State != "pending" || first.NeedsYou == nil || first.NeedsYou.Question == nil || !strings.Contains(first.NeedsYou.Question.Text, "the checkout has no change from where the run began")) {
					t.Fatalf("empty first attempt passed: %+v", first)
				}
			}
			if err := os.WriteFile(ready, nil, 0600); err != nil {
				t.Fatal(err)
			}
			before := read()
			act("factory.job_step_rerun", map[string]any{"step": "write"})
			after := wait("replacement attempt finishes", func(job api.Job) bool { return waiting(job) && job.Steps[0].Round > before.Steps[0].Round })
			if action == "empty rerun" {
				if after.Steps[0].State != "waiting" || after.Steps[1].State != "pending" || after.NeedsYou == nil || after.NeedsYou.Question == nil || !strings.Contains(after.NeedsYou.Question.Text, "the checkout has no change from where the run began") {
					t.Fatalf("empty rerun passed: %+v", after)
				}
			} else {
				if !gate(after) {
					t.Fatalf("earlier committed change was called empty on %s: %+v", action, after)
				}
				if after.HeadCommit == "" || before.HeadCommit != "" && after.HeadCommit != before.HeadCommit || after.Change == nil || len(after.Change.Files) != 1 || after.Change.Files[0].Path != "CHANGE.txt" {
					t.Fatalf("replacement did not retain the earlier change and commit: before=%s after=%s change=%+v", before.HeadCommit, after.HeadCommit, after.Change)
				}
			}
			t.Logf("%s: write=%s round=%d next=%s", action, after.Steps[0].State, after.Steps[0].Round, after.Steps[1].State)
		})
	}
}
