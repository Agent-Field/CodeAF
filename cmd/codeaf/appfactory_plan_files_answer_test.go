package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestAppFactoryPlanFilesAnswer(t *testing.T) {
	t.Run("saved results and worker reports", func(t *testing.T) {
		for _, files := range []string{`["README.md"]`, `[]`, `["../bad"]`, `["README.md","auth/login.go"]`} {
			var result factory.StageResult
			if err := json.Unmarshal([]byte(`{"Done":true,"Files":`+files+`,"Output":"Files I will touch: only README.md."}`), &result); err != nil {
				t.Fatal(err)
			}
			report := factory.RunReport{Stages: []factory.StageReport{{Name: "plan", State: factory.PhaseDone, Round: 1, Result: &result}}}
			wire := api.FromRunReport(report)
			raw, err := json.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(raw, &wire); err != nil {
				t.Fatal(err)
			}
			back := api.ToWorkerReport(wire)
			if !reflect.DeepEqual(back.Stages[0].Result.Files, result.Files) {
				t.Errorf("worker report lost files: %s", raw)
			}
			item := factory.Item{Stream: &factory.Stream{Phases: []factory.Phase{{Name: "plan", State: factory.PhaseDone, Round: 1, Result: &result}}}}
			raw, err = json.Marshal(item)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(raw, &item); err != nil {
				t.Fatal(err)
			}
			pull := api.WorkerJob(item)
			if got := api.ToItem(pull).Stream.Phases[0].Result.Files; !reflect.DeepEqual(got, result.Files) {
				t.Errorf("worker pull lost files: %v", got)
			}
		}
		legacy := factory.Item{Stream: &factory.Stream{Phases: []factory.Phase{{Name: "plan", State: factory.PhaseDone, Result: &factory.StageResult{Done: true, Output: "Files: README.md"}}}}}
		if got := factory.PlanFilesOf(legacy); !reflect.DeepEqual(got, []string{"README.md"}) {
			t.Errorf("saved legacy plan = %v", got)
		}
	})
	t.Run("structured path limits", func(t *testing.T) {
		for _, files := range [][]string{
			{"README.md", "../auth/login.go"}, {"/README.md"}, {"auth/**"}, {"auth/"}, {"C:\u003aREADME.md"}, {"docs/\u202efile.md"},
			{strings.Repeat("x", factory.PlanPathMost+1)}, make([]string, factory.PlanFilesMost+1),
		} {
			if got := (factory.StageResult{Files: files, Output: "Files: README.md"}).PlanFiles(); got != nil {
				t.Errorf("invalid list returned files: %v", got)
			}
		}
		files := make([]string, factory.PlanFilesMost)
		for i := range files {
			files[i] = fmt.Sprintf("docs/%d.md", i)
		}
		if got := (factory.StageResult{Files: files}).PlanFiles(); len(got) != factory.PlanFilesMost {
			t.Errorf("maximum list = %d files", len(got))
		}
		path := strings.Repeat("x", factory.PlanPathMost)
		if got := (factory.StageResult{Files: []string{path}}).PlanFiles(); !reflect.DeepEqual(got, []string{path}) {
			t.Error("maximum path rejected")
		}
	})

	const prose = "Files I will touch: only README.md. I will not touch auth/** or anything under cmd/."
	for _, tc := range []struct {
		name, fields, output, hold string
		files                      []string
		repair                     bool
	}{
		{name: "structured safe", fields: `,"files":["README.md"]`, output: prose, files: []string{"README.md"}},
		{name: "structured protected", fields: `,"files":["auth/login.go"]`, output: prose, files: []string{"auth/login.go"}, hold: "protected · auth/** · auth/login.go"},
		{name: "prose alone", output: prose, hold: "protected · auth/** · " + factory.PlanNoFiles},
		{name: "invalid list", fields: `,"files":["../README.md"]`, output: prose, hold: "protected · auth/** · " + factory.PlanNoFiles},
		{name: "conflicting declaration", fields: `,"files":["README.md"]`, output: "Files: auth/login.go", hold: "protected · auth/** · " + factory.PlanNoFiles},
		{name: "legacy declaration", output: "Files: README.md", files: []string{"README.md"}},
		{name: "null list cannot fall back", fields: `,"files":null`, output: "Files: README.md", hold: "protected · auth/** · " + factory.PlanNoFiles},
		{name: "empty list cannot fall back", fields: `,"files":[]`, output: "Files: README.md", hold: "protected · auth/** · " + factory.PlanNoFiles},
		{name: "repair missing list", output: prose, files: []string{"README.md"}, repair: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newLocalSetup(t)
			g := newRunRig(t)
			var mu sync.Mutex
			var requests []string
			var reports int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
					http.Error(w, "no catalog", 503)
					return
				}
				raw, _ := io.ReadAll(r.Body)
				var req recordedRequest
				_ = json.Unmarshal(raw, &req)
				hasStage := false
				for _, tool := range req.Tools {
					hasStage = hasStage || tool.Function.Name == "stage_result"
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if !hasStage {
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
					return
				}
				mu.Lock()
				requests = append(requests, string(raw))
				reports++
				fields := tc.fields
				if tc.repair && reports > 1 {
					fields = `,"files":["README.md"]`
				}
				mu.Unlock()
				output, _ := json.Marshal(tc.output)
				args, _ := json.Marshal(`{"done":true,"output":` + string(output) + fields + `}`)
				fmt.Fprint(w, `data: {"id":"s","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"stage_result","arguments":`+string(args)+`}}]},"finish_reason":null}]}`+"\n\n"+`data: {"id":"s","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\ndata: [DONE]\n\n")
			}))
			t.Cleanup(server.Close)
			t.Setenv("CODEAF_BASE_URL", server.URL)
			t.Setenv("OPENROUTER_API_KEY", "test-key")
			config.AppCredentialSource().Set("openrouter", "test-key")
			freshEngineProcess(t)
			proc, err := openEngineProcess()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(proc.closeAll)
			launch, err := openV3Launch(proc, engineLaunchOptions(remote.Hello{Version: remote.Version, Workspace: g.workspace}, g.workspace, ""))
			if err != nil {
				t.Fatal(err)
			}
			launch.Config.OneModel = true
			s.backend.local.parent = func() session.Config { return launch.Config }
			var rules pipeline.File
			rules.Start.Never = []string{"auth/**"}
			s.backend.local.rules = func(context.Context, string, string) (pipeline.File, error) { return rules, nil }
			stageSettle = 20 * time.Millisecond
			t.Cleanup(func() { stageSettle = unattendedRunSettleTick })
			st := s.start()
			s.mu.Lock()
			hello := s.lines[0]
			s.mu.Unlock()
			if !strings.Contains(hello, appwire.FeatureFactoryPlanFilesAnswer) {
				t.Errorf("hello lacks structured plan feature: %s", hello)
			}
			floor := s.backend.local.host.floor
			if err := floor.SetCheckout("acme/api", g.workspace); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(t.TempDir(), "build-started")
			id, err := floor.Add(context.Background(), factory.Item{Title: "Plan a readme edit", Repo: "acme/api", Tier: factory.TierOwner, Cap: 5, Autonomy: factory.AutonomyAuto, Stages: []factory.Stage{
				{Name: "plan", Kind: factory.StageChat, On: true, Until: "done", Ask: "Plan the README edit."},
				{Name: factory.ApproveName, Kind: factory.StageGate, On: true},
				{Name: "build", Kind: factory.StageCheck, On: true, Ask: "touch " + marker},
				{Name: "finish", Kind: factory.StageGate, On: true, Ask: "Wait for owner", Who: "someone-else"},
			}})
			if err != nil {
				t.Fatal(err)
			}
			var job api.Job
			params := map[string]any{"factory": st.Factory, "job": id}
			s.must("factory.job", params, &job)
			s.must("factory.job_start", map[string]any{"factory": st.Factory, "job": id, "expect": map[string]any{"version": job.Version}}, &appwire.FactoryJobActed{})
			eventually(t, "plan gate or build", func() bool {
				s.must("factory.job", params, &job)
				return job.RuleHold != "" || len(job.Steps) > 2 && job.Steps[2].State == "done"
			})
			if job.RuleHold != tc.hold {
				t.Errorf("hold = %q; want %q; Auto: %s", job.RuleHold, tc.hold, job.AutonomyWhy)
			}
			if !reflect.DeepEqual(job.PlannedFiles, tc.files) {
				t.Errorf("planned_files = %v; want %v", job.PlannedFiles, tc.files)
			}
			if got := factory.PlanFilesOf(api.ToItem(job)); !reflect.DeepEqual(got, tc.files) {
				t.Errorf("wire round trip files = %v; want %v", got, tc.files)
			}
			if tc.hold != "" && job.Steps[2].State != "pending" {
				t.Errorf("build started despite hold: %+v", job.Steps[2])
			}
			if strings.HasSuffix(tc.hold, factory.PlanNoFiles) && (!strings.Contains(job.AutonomyWhy, "didn't give the factory a file list it can check against auth/**") || strings.Contains(job.AutonomyWhy, "didn't list its files")) {
				t.Errorf("unknown-list words contradict prose: %q", job.AutonomyWhy)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(requests) == 0 || !strings.Contains(requests[0], "fill files with every repository-relative file") {
				t.Errorf("plan request did not carry briefPlanFiles")
			}
			var first struct {
				Tools []struct {
					Function struct {
						Name       string `json:"name"`
						Parameters struct {
							Required []string `json:"required"`
						} `json:"parameters"`
					} `json:"function"`
				} `json:"tools"`
			}
			if len(requests) > 0 {
				_ = json.Unmarshal([]byte(requests[0]), &first)
			}
			required := false
			for _, tool := range first.Tools {
				if tool.Function.Name == "stage_result" {
					for _, field := range tool.Function.Parameters.Required {
						required = required || field == "files"
					}
				}
			}
			if !required {
				t.Error("plan answer schema does not require files")
			}
			if tc.repair && reports != 2 {
				t.Errorf("repair reports = %d; want 2", reports)
			}
			if !tc.repair && reports > 2 {
				t.Errorf("unbounded repair: %d reports", reports)
			}
		})
	}
}
