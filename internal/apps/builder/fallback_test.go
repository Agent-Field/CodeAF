package builder

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/check"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type fixtureSample struct{ rows map[string][]bridge.Record }

func (s fixtureSample) Generate(context.Context, *spec.Bundle) (map[string][]bridge.Record, error) {
	return s.rows, nil
}

func fallbackRig(t *testing.T, badScreen bool) *rig {
	t.Helper()
	r := newRig(t)
	golden, sampleJSON := spec.GoldenCRM()
	golden[spec.AgentFile] = []byte(strings.Replace(string(golden[spec.AgentFile]), "records.update, gmail", "records.update, records.delete, gmail", 1))
	// The file schema accepts the query shape; Check must reject its field
	// before the run needs a live engine.
	golden["runs/follow-up.yaml"] = []byte(strings.Replace(string(golden["runs/follow-up.yaml"]), "id:{{deal}}", "highly:{{deal}}", 1))
	if badScreen {
		golden["screens/pipeline.json"] = []byte(strings.Replace(string(golden["screens/pipeline.json"]), "stage:-Lost sort:-amount", "highly:yes", 1))
	}
	r.model.write = func(text string) string {
		reply := map[string]string{}
		for _, file := range onlyPaths(text) {
			reply[file] = string(golden[file])
		}
		raw, _ := json.Marshal(reply)
		return string(raw)
	}
	reply, _ := json.Marshal(map[string]string{"runs/follow-up.yaml": string(golden["runs/follow-up.yaml"])})
	r.model.fixes = []string{string(reply)}
	var rows map[string][]bridge.Record
	if err := json.Unmarshal(sampleJSON, &rows); err != nil {
		t.Fatal(err)
	}
	r.svc.all.Sample = fixtureSample{rows}
	r.svc.all.Check = check.New(svc.Config{}, r.svc.all)
	r.svc.root = t.TempDir()
	return r
}

func waitFinished(t *testing.T, r *rig) {
	t.Helper()
	waitFor(t, "build completion", func() bool {
		b, err := r.svc.buildFor(context.Background(), "crm")
		if err != nil {
			return false
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		return !b.working
	})
}

func TestRunFallbackWithScriptedModel(t *testing.T) {
	for _, tc := range []struct {
		name   string
		screen bool
		want   bridge.AppState
	}{
		{"run only", false, bridge.StatePreview},
		{"screen and run", true, bridge.StateFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := fallbackRig(t, tc.screen)
			r.create(t, true)
			waitFinished(t, r)
			state, files := r.store.current()
			if state != tc.want {
				t.Fatalf("state %s, want %s; report %+v", state, tc.want, r.hub.events()[len(r.hub.events())-1].Check)
			}
			result, err := r.svc.Result(context.Background(), r.scope())
			if err != nil {
				t.Fatal(err)
			}
			if tc.screen {
				if len(result.LeftOut) != 0 || files["runs/follow-up.yaml"] == nil {
					t.Fatal("screen failure must not omit automations")
				}
				last := r.hub.events()[len(r.hub.events())-1]
				if last.Step != bridge.StepCheck || last.Status != bridge.BuildFailed {
					t.Fatalf("final event %+v", last)
				}
				return
			}
			if r.model.calls["fix"] != 2 {
				t.Fatalf("FIX calls %d", r.model.calls["fix"])
			}
			if len(result.LeftOut) != 1 || result.LeftOut[0].File != "runs/follow-up.yaml" || !strings.Contains(result.LeftOut[0].Reason, "highly") {
				t.Fatalf("result %+v", result)
			}
			if files["runs/follow-up.yaml"] != nil {
				t.Fatal("bad run remains")
			}
			b, err := spec.Decode(files)
			if err != nil {
				t.Fatal(err)
			}
			if len(b.App.Triggers) != 0 || len(b.Plan.Automation) != 0 {
				t.Fatalf("references remain %+v %+v", b.App.Triggers, b.Plan.Automation)
			}
			report, err := r.svc.all.Check.Run(context.Background(), "crm", files, 3)
			if err != nil || !report.Pass {
				t.Fatalf("reduced app: %+v, %v", report, err)
			}
			r.hub.mu.Lock()
			events := append([]ResultEvent(nil), r.hub.results...)
			r.hub.mu.Unlock()
			if len(events) == 0 || !reflect.DeepEqual(events[len(events)-1].LeftOut, result.LeftOut) {
				t.Fatalf("result events %+v", events)
			}
			raw, _ := json.Marshal(events[len(events)-1])
			if !strings.Contains(string(raw), `"left_out"`) || !strings.Contains(string(raw), `"step":"preview"`) {
				t.Fatalf("wire event %s", raw)
			}
			restarted := New(svc.Config{Root: r.svc.root}, r.svc.all)
			detail, err := r.svc.AppDetail(context.Background(), r.scope())
			if err != nil {
				t.Fatal(err)
			}
			detailJSON, _ := json.Marshal(detail)
			if !strings.Contains(string(detailJSON), `"left_out"`) || !strings.Contains(string(detailJSON), `"files"`) {
				t.Fatalf("detail %s", detailJSON)
			}
			got, err := restarted.Result(context.Background(), r.scope())
			if err != nil || !reflect.DeepEqual(got, result) {
				t.Fatalf("after restart %+v %v", got, err)
			}
		})
	}
}

func TestRetryWritesAndFixesOnlyLeftOutRun(t *testing.T) {
	r := fallbackRig(t, false)
	r.create(t, true)
	waitFinished(t, r)
	_, before := r.store.current()
	r.svc = New(svc.Config{Root: r.svc.root}, r.svc.all)
	r.model.mu.Lock()
	r.model.prompts = nil
	r.model.calls = map[string]int{}
	r.model.mu.Unlock()
	// WRITE still returns a broken run. FIX also tries to overwrite the screen;
	// the scoped retry must ignore that unrelated file.
	fixed, _ := spec.GoldenCRM()
	fixedRun := []byte("run: follow-up\nsteps:\n  - id: load\n    records.query: {type: deal, query: 'name:North*'}\n")
	fixed["runs/follow-up.yaml"] = fixedRun
	reply, _ := json.Marshal(map[string]string{"runs/follow-up.yaml": string(fixedRun), "screens/pipeline.json": "{}"})
	r.model.fixes = []string{string(reply)}
	// A missing run engine makes behaviour skipped; fields and references still
	// receive the full schema, kit and sample-render Check.
	if err := r.svc.RetryRun(context.Background(), r.scope(), "runs/follow-up.yaml"); err != nil {
		t.Fatal(err)
	}
	waitFinished(t, r)
	state, after := r.store.current()
	if state != bridge.StatePreview {
		t.Fatalf("state %s: %+v", state, r.hub.events())
	}
	result, err := r.svc.Result(context.Background(), r.scope())
	if err != nil || len(result.LeftOut) != 0 {
		t.Fatalf("result %+v %v", result, err)
	}
	if string(after["runs/follow-up.yaml"]) != string(fixedRun) {
		t.Fatal("run was not repaired")
	}
	if len(before) >= len(after) || string(after["screens/pipeline.json"]) == "{}" {
		t.Fatal("retry lost working files")
	}
	for _, text := range r.model.prompts {
		switch callKind(text) {
		case "write":
			if !reflect.DeepEqual(onlyPaths(text), []string{"runs/follow-up.yaml"}) {
				t.Fatalf("unscoped WRITE %s", text)
			}
		case "fix":
			if !strings.Contains(text, "Change ONLY runs/follow-up.yaml") {
				t.Fatal("unscoped FIX")
			}
		default:
			t.Fatal("retry called PLAN")
		}
	}
	if r.model.calls["write"] != 1 || r.model.calls["fix"] != 1 {
		t.Fatalf("calls %+v", r.model.calls)
	}
}

func TestRunFailureEligibility(t *testing.T) {
	files := spec.Files{"runs/a.yaml": []byte("invalid")}
	for _, tc := range []struct {
		name   string
		issues []spec.Issue
		want   bool
	}{
		{"empty", nil, false},
		{"run", []spec.Issue{{File: "runs/a.yaml", Message: "bad"}}, true},
		{"records", []spec.Issue{{File: "app.json"}}, false},
		{"screen", []spec.Issue{{File: "screens/a.json"}}, false},
		{"missing file", []spec.Issue{{File: "runs/b.yaml"}}, false},
		{"mixed", []spec.Issue{{File: "runs/a.yaml"}, {File: "app.json"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := len(runFailures(files, bridge.CheckReport{Issues: tc.issues})) > 0; got != tc.want {
				t.Fatalf("eligible %v", got)
			}
		})
	}
}

func TestScreenFailureNeverFallsBack(t *testing.T) {
	r := newRig(t, bridge.CheckReport{Issues: []spec.Issue{{File: "screens/pipeline.json", Rule: "schema/query", Message: `unknown field "highly"; fix: valid fields: name, stage, amount`}}})
	r.model.fixes = []string{goldenFiles([]string{"screens/pipeline.json"})}
	r.create(t, true)
	waitFinished(t, r)
	state, files := r.store.current()
	if state != bridge.StateFailed || files["runs/follow-up.yaml"] == nil {
		t.Fatalf("state %s, files %v", state, files.Paths())
	}
	result, err := r.svc.Result(context.Background(), r.scope())
	if err != nil || len(result.LeftOut) != 0 {
		t.Fatalf("result %+v %v", result, err)
	}
}

func TestOmitRunsRemovesAgentToolAndPreservesOtherActions(t *testing.T) {
	files, _ := spec.GoldenCRM()
	files[spec.AgentFile] = []byte(strings.Replace(string(files[spec.AgentFile]), "tools: [", "tools: [runs.start, ", 1))
	var app map[string]any
	_ = json.Unmarshal(files[spec.AppFile], &app)
	commands := app["commands"].([]any)
	commands = append(commands, map[string]any{"id": "mixed", "label": "Mixed", "do": []any{map[string]any{"runs.start": "follow-up"}, map[string]any{"agent.ask": "Hello"}}})
	app["commands"] = commands
	files[spec.AppFile], _ = json.Marshal(app)
	next, err := omitRuns(files, []LeftOutAutomation{{File: "runs/follow-up.yaml"}})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := spec.ParseAgent(next[spec.AgentFile])
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range agent.Tools {
		if tool == "runs.start" {
			t.Fatal("agent retains unavailable runs.start")
		}
	}
	before, _ := spec.ParseAgent(files[spec.AgentFile])
	if before.Body != agent.Body {
		t.Fatal("agent instructions changed")
	}
	b, err := spec.Decode(next)
	if err != nil {
		t.Fatal(err)
	}
	mixed := b.App.Commands[len(b.App.Commands)-1]
	if mixed.ID != "mixed" || len(mixed.Do) != 1 || mixed.Do[0].Verb() != spec.VerbAgentAsk {
		t.Fatalf("mixed command %+v", mixed)
	}
}

type failingRunEngine struct{ svc.Runs }

func (failingRunEngine) Start(context.Context, svc.Scope, bridge.StartRunParams) (bridge.RunState, error) {
	return bridge.RunState{Status: bridge.RunFailed, Fake: true, Error: "automation could not complete"}, nil
}

func TestFallbackHandlesRunSchemaAndBehaviourFailures(t *testing.T) {
	for _, tc := range []struct {
		name, run, reason string
		behaviour         bool
	}{
		{name: "schema", run: "run: follow-up\nsteps:\n  - id: send\n    gate: {approval: send, who: $me, title: Send}\n", reason: "schema"},
		{name: "behaviour", behaviour: true, reason: "automation could not complete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := fallbackRig(t, false)
			run := tc.run
			if tc.behaviour {
				files, _ := spec.GoldenCRM()
				run = string(files["runs/follow-up.yaml"])
				r.svc.all.Runs = failingRunEngine{}
			}
			originalWrite := r.model.write
			r.model.write = func(text string) string {
				var files map[string]string
				_ = json.Unmarshal([]byte(originalWrite(text)), &files)
				if _, ok := files["runs/follow-up.yaml"]; ok {
					files["runs/follow-up.yaml"] = run
				}
				raw, _ := json.Marshal(files)
				return string(raw)
			}
			fix, _ := json.Marshal(map[string]string{"runs/follow-up.yaml": run})
			r.model.fixes = []string{string(fix)}
			r.create(t, true)
			waitFinished(t, r)
			state, _ := r.store.current()
			result, err := r.svc.Result(context.Background(), r.scope())
			if state != bridge.StatePreview || err != nil || len(result.LeftOut) != 1 || !strings.Contains(result.LeftOut[0].Reason, tc.reason) {
				t.Fatalf("state %s, result %+v, error %v", state, result, err)
			}
		})
	}
}
