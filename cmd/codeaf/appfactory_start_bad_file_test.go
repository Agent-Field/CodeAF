package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/worker"
	"github.com/Agent-Field/codeaf/internal/remote"
)

func TestFactoryStartBadRepositoryFile(t *testing.T) {
	t.Run("safe diagnostics", func(t *testing.T) {
		fallback := pipeline.RepoFile + ": could not read repository rules; check the file's syntax and access"
		cases := []struct {
			err  error
			want string
		}{
			{nil, ""},
			{errors.New("open /private/checkout/.codeaf/factory.yaml: denied token=private-token\nstack text"), fallback},
			{errors.New("yaml: line 7: unknown anchor 'private-token' referenced"), pipeline.RepoFile + " line 7: could not read repository rules; check the file's syntax and access"},
			{errors.New("open /private/checkout: yaml: line 7: did not find expected key\nstack private-token"), pipeline.RepoFile + " line 7: did not find expected key"},
			{errors.New(strings.Repeat("private-token", 200)), fallback},
		}
		for _, tc := range cases {
			if got := pipeline.UnreadWhy(tc.err); got != tc.want || len(got) >= 160 || strings.ContainsAny(got, "\r\n") {
				t.Errorf("diagnostic %q, want %q", got, tc.want)
			}
		}
	})
	for _, road := range []string{"app", "chat", "no line", "house rules unread", "clean file without budget", "no repository file", "no repository file from chat", "not on this Mac yet", "not on this Mac yet from chat"} {
		t.Run(road, func(t *testing.T) {
			s := newLocalSetup(t)
			config.AppCredentialSource().Set("openrouter", "test-key-no-network")
			dir := t.TempDir()
			gitCheckout(t, dir, "acme/api")
			path := filepath.Join(dir, pipeline.RepoFile)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			text := "rules:\n  - keep tests\nstart: bad: yaml\n"
			if road == "clean file without budget" {
				text = "default_budget: 0\n"
			}
			if err := os.WriteFile(path, []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{"add", ".codeaf/factory.yaml"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "repository rules"}} {
				if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
					t.Fatalf("git fixture: %v: %s", err, out)
				}
			}
			read := func(context.Context, string, string) (pipeline.File, error) {
				if strings.HasPrefix(road, "no repository file") {
					f := pipeline.Defaults()
					f.Source, f.From = pipeline.SourceDefaults, factory.FromDefault
					return f, nil
				}
				if strings.HasPrefix(road, "not on this Mac yet") {
					return pipeline.Defaults(), fmt.Errorf("%s: %w", pipeline.RepoFile, pipeline.ErrNotHereYet)
				}
				b, err := os.ReadFile(path)
				if err != nil {
					return pipeline.Defaults(), err
				}
				f, err := pipeline.Parse(string(b))
				if road == "no line" {
					return f, errors.New("access denied")
				}
				if road == "clean file without budget" {
					f.DefaultBudget = 0
				}
				if road == "house rules unread" {
					return f, pipeline.ErrHouseRulesUnread
				}
				return f, err
			}
			s.backend.local.rules = read
			var launch func(int) error
			s.backend.local.seamHook = func(seam factory.Seam) factory.Seam {
				// The election lock below keeps the automatic worker off; a real
				// worker uses the scripted model after Start.
				seam.GitHub = func(context.Context) (factory.GitHubLink, error) {
					return factory.GitHubLink{Login: "owner"}, nil
				}
				st, err := store.Open(v3FactoryRoot())
				if err != nil {
					t.Fatal(err)
				}
				desk, err := lease.Open(st, lease.Options{})
				if err != nil {
					t.Fatal(err)
				}
				launch = lease.NewDoors(desk).WithStartRead(func(ctx context.Context, it factory.Item) (pipeline.File, error) { return read(ctx, it.Repo, dir) }).Launch
				seam.Launch = launch
				return seam
			}
			election, err := store.Open(v3FactoryRoot())
			if err != nil {
				t.Fatal(err)
			}
			release, ok := election.TryWorker()
			if !ok {
				t.Fatal("runner election lock unavailable")
			}
			t.Cleanup(release)
			status := s.start()
			floor := s.backend.local.host.floor
			if err := floor.SetCheckout("acme/api", dir); err != nil {
				t.Fatal(err)
			}
			id, err := floor.Add(context.Background(), factory.Item{Repo: "acme/api", Title: "Plan safely", Model: "openai/gpt-4.1", Tier: factory.TierOwner, Autonomy: factory.AutonomyAuto, Stages: []factory.Stage{
				{Name: "plan", Kind: factory.StageChat, On: true, Until: factory.UntilDone},
				{Name: factory.ApproveName, Kind: factory.StageGate, On: true},
				{Name: "write", Kind: factory.StageChat, On: true, Until: factory.UntilDone},
			}})
			if err != nil {
				t.Fatal(err)
			}
			params := map[string]any{"factory": status.Factory, "job": id}
			var before api.Job
			s.must("factory.job", params, &before)
			params["expect"] = map[string]any{"version": before.Version}
			if road == "house rules unread" || road == "clean file without budget" {
				_, code, msg := s.call("factory.job_start", params)
				if code == "" {
					t.Fatal("unsafe start succeeded")
				}
				want := "factory's own rules could not be read"
				if road == "clean file without budget" {
					want = "default_budget must be positive"
				}
				if !strings.Contains(msg, want) {
					t.Fatalf("refusal: %s: %s", code, msg)
				}
				var after api.Job
				s.must("factory.job", params, &after)
				if after.State != api.StateBacklog || after.Version != before.Version {
					t.Fatalf("refused start changed job: %+v", after)
				}
				return
			}
			if strings.HasSuffix(road, "chat") {
				if err := launch(id); err != nil {
					t.Fatal(err)
				}
			} else {
				var acted appwire.FactoryJobActed
				s.must("factory.job_start", params, &acted)
				if acted.Job != id {
					t.Fatalf("start receipt: %+v", acted)
				}
			}
			var got struct {
				api.Job
				StartRules struct {
					Unread  bool   `json:"unread"`
					Why     string `json:"unread_why"`
					File    string `json:"unread_file"`
					Line    *int   `json:"unread_line"`
					NoFile  bool   `json:"no_file"`
					Pending bool   `json:"pending"`
				} `json:"start_rules"`
			}
			s.must("factory.job", params, &got)
			if strings.HasPrefix(road, "no repository file") || strings.HasPrefix(road, "not on this Mac yet") {
				noFile := strings.HasPrefix(road, "no repository file")
				if got.StartRules.NoFile != noFile || got.StartRules.Pending == noFile || got.StartRules.Unread == noFile || strings.Contains(got.StartRules.Why, "syntax") {
					t.Fatalf("start rules: %+v", got.StartRules)
				}
				for _, a := range got.Desk.Activity {
					if strings.Contains(a.What, "unread") {
						t.Fatalf("a person is told the rules are unread: %q", a.What)
					}
				}
				return
			}
			wantWhy := ".codeaf/factory.yaml line 3: mapping values are not allowed in this context"
			if road == "no line" {
				wantWhy = pipeline.RepoFile + ": could not read repository rules; check the file's syntax and access"
			}
			if got.StartRules.File != pipeline.RepoFile || (road == "no line" && got.StartRules.Line != nil) || (road != "no line" && (got.StartRules.Line == nil || *got.StartRules.Line != 3)) {
				t.Errorf("structured read location: %+v", got.StartRules)
			}
			if !got.StartRules.Unread || got.StartRules.Why != wantWhy {
				t.Errorf("start rules: %+v", got.StartRules)
			}
			if got.Desk == nil || got.Desk.Cap != factory.DefaultJobBudget || got.Desk.Cap <= 0 {
				t.Fatalf("budget: %+v", got.Desk)
			}
			noted := false
			for _, a := range got.Desk.Activity {
				noted = noted || a.What == "Started with the repository's rules unread; its plan will wait for you"
			}
			if !noted {
				t.Errorf("Activity: %+v", got.Desk.Activity)
			}

			mirror, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			model := newStageModel(t)
			t.Setenv("CODEAF_BASE_URL", model.server.URL)
			t.Setenv("OPENROUTER_API_KEY", "test-key-no-network")
			t.Chdir(dir)
			freshEngineProcess(t)
			oldSettle := stageSettle
			stageSettle = 20 * time.Millisecond
			t.Cleanup(func() { stageSettle = oldSettle })
			process, err := openEngineProcess()
			if err != nil {
				t.Fatal(err)
			}
			launchConfig, err := openV3Launch(process, engineLaunchOptions(remote.Hello{Version: remote.Version, Workspace: dir}, dir, ""))
			if err != nil {
				t.Fatal(err)
			}
			chat := factoryrun.NewChatExecutor(factoryStageMaker(mirror, dir, "", launchConfig.Config))
			var writes atomic.Int32
			runner := factoryrun.New(factoryrun.Options{Store: mirror, RepoDir: func(string) string { return dir }, Exec: map[factory.StageKind]factoryrun.Executor{
				factory.StageChat: factoryrun.ExecutorFunc(func(ctx context.Context, job factoryrun.Job) (factory.StageResult, error) {
					if job.Stage.Name != "plan" {
						writes.Add(1)
					}
					return chat.Run(ctx, job)
				}),
			}})
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				defer close(done)
				worker.Run(ctx, worker.Options{Dial: func(int) *client.WorkerClient { return client.Open(status.URL, s.backend.local.host.token).Worker("") },
					Attach: api.WorkerAttach{Machine: "test-runner", Name: "test-runner", Handle: "owner", TakesUnassigned: map[string]bool{lease.EveryRepo: true}},
					Runner: runner, Mirror: mirror, Slots: 1, ReportEvery: 10 * time.Millisecond, BeatEvery: 20 * time.Millisecond})
			}()
			t.Cleanup(func() { cancel(); <-done })
			eventually(t, "the plan holds for a person", func() bool {
				var job api.Job
				s.must("factory.job", params, &job)
				return job.State == api.StateNeedsYou && job.RuleHold == factory.StartRulesUnreadHold
			})
			model.mu.Lock()
			callsAtHold := len(model.models)
			model.mu.Unlock()
			if callsAtHold == 0 {
				t.Fatal("plan never called the fake model")
			}
			for until := time.Now().Add(time.Second); time.Now().Before(until); {
				var held api.Job
				s.must("factory.job", params, &held)
				if held.State != api.StateNeedsYou || held.NeedsYou == nil || len(held.Questions) == 0 || !strings.Contains(held.Questions[0].Text, wantWhy) {
					t.Errorf("person must see unread reason: %+v", held.NeedsYou)
					break
				}
				if len(held.Steps) != 3 || held.Steps[0].State != string(factory.PhaseDone) || held.Steps[2].StartedAt != nil || writes.Load() != 0 {
					t.Fatal("later step started before consent")
				}
				model.mu.Lock()
				calls := len(model.models)
				model.mu.Unlock()
				if calls != callsAtHold {
					t.Fatal("fake model called while plan waits for consent")
				}
				time.Sleep(20 * time.Millisecond)
			}
			if writes.Load() != 0 {
				t.Fatal("work continued past the unread rules plan hold")
			}
			// The read must survive the worker's report and its API round trip.
			raw, code, msg := s.call("factory.job", params)
			if code != "" || json.Unmarshal(raw, &got) != nil || got.StartRules.Why != wantWhy || got.StartRules.File != pipeline.RepoFile || (road == "no line" && got.StartRules.Line != nil) || (road != "no line" && (got.StartRules.Line == nil || *got.StartRules.Line != 3)) {
				t.Fatalf("read after plan: %s %s %s", code, msg, raw)
			}
			var held api.Job
			s.must("factory.job", params, &held)
			params["expect"] = map[string]any{"version": held.Version}
			params["commit"] = held.HeadCommit
			var approved appwire.FactoryJobActed
			s.must("factory.job_approve", params, &approved)
			for deadline := time.Now().Add(factory.ApprovalUndo + 5*time.Second); func() bool { model.mu.Lock(); defer model.mu.Unlock(); return len(model.models) == callsAtHold }() && time.Now().Before(deadline); {
				time.Sleep(20 * time.Millisecond)
			}
			model.mu.Lock()
			callsAfterApproval := len(model.models)
			model.mu.Unlock()
			if writes.Load() == 0 || callsAfterApproval <= callsAtHold {
				t.Fatal("approval did not release the next step")
			}
		})
	}
}
