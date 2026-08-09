package plannertranslate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/baked"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/agentjson"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/scheduler"
)

func TestDAGSchemaDefaultsAndStrictDependencies(t *testing.T) {
	valid := json.RawMessage(`{"tasks":[{"taskKey":"a","title":"A","description":"description long enough"}]}`)
	parsed := (DAGSchema{}).SafeParse(valid)
	if !parsed.Success() {
		t.Fatalf("valid DAG rejected: %#v", parsed.Issues)
	}
	if parsed.Data.Tasks[0].Kind != "code" ||
		len(parsed.Data.Tasks[0].Tags) != 0 ||
		len(parsed.Data.Tasks[0].Deps) != 0 {
		t.Fatalf("defaults not applied: %#v", parsed.Data.Tasks[0])
	}

	invalid := json.RawMessage(`{"tasks":[{"taskKey":"a","title":"A","description":"description long enough","deps":[{"from_task":"b","kind":"feeds_into","extra":true}]}]}`)
	if got := (DAGSchema{}).SafeParse(invalid); got.Success() {
		t.Fatal("strict dependency object accepted an extra key")
	}
}

func TestDispatchPlannerTranslateUsesAgentJSONAndPreservesArtifact(t *testing.T) {
	workspace := t.TempDir()
	output := filepath.Join(workspace, "custom", "dag.json")
	var captured agentjson.Request
	deps := agentjson.Dependencies{
		Resolver: agentjson.ResolverFunc(func(tier baked.Tier) []string {
			if tier != baked.TierHigh {
				t.Fatalf("tier=%s", tier)
			}
			return []string{"openrouter/model"}
		}),
		Client: agentjson.ClientFunc(func(_ context.Context, request agentjson.Request) error {
			captured = request
			if err := os.WriteFile(output, []byte(
				`{"tasks":[{"taskKey":"a","title":"A","description":"description long enough","kind":"code","tags":[],"deps":[]}]}`,
			), 0o644); err != nil {
				return err
			}
			return nil
		}),
		NewID: func(prefix string) string { return prefix + "-fixture" },
	}
	got, err := DispatchPlannerTranslate(context.Background(), DispatchPlannerTranslateInput{
		Workspace: workspace, ParentSessionID: "parent", OutputPath: &output,
	}, deps)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got.UsedFallback || !got.FirstTry || len(got.Data.Tasks) != 1 {
		t.Fatalf("result=%#v", got)
	}
	if captured.Agent != "planner-translate" ||
		captured.TaskPrompt != BuildPrompt(output, nil, nil) ||
		captured.Phase != agentjson.PhaseMain {
		t.Fatalf("request drift: %#v", captured)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("preserveOnSuccess did not keep artifact: %v", err)
	}
}

func TestSchedulerServicePropagatesTransientPlannerFailure(t *testing.T) {
	// Round-2 frontier retry contract: a dispatch failure remains an error for
	// the scheduler, rather than looking like a completed tick with no residual.
	service := &SchedulerService{AgentJSON: agentjson.Dependencies{}}
	_, err := service.RunFrontierTick(context.Background(), scheduler.FrontierTickInput{
		Workspace: t.TempDir(), Tick: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "nil model resolver") {
		t.Fatalf("frontier planner failure = %v", err)
	}
}

func TestParseArchitectureToDAGAndDeadASCIIEdgeFallback(t *testing.T) {
	writeArchitecture := func(t *testing.T, text string) string {
		t.Helper()
		workspace := t.TempDir()
		dir := filepath.Join(workspace, ".codeaf", "plan")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "architecture.md"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return workspace
	}

	workspace := writeArchitecture(t, strings.Join([]string{
		"# Architecture",
		"## Components",
		"### 1. `lib/types` — Core types",
		"Foundational values.",
		"**Dependencies**: none",
		"",
		"### 2. Reader (io/reader/)",
		"Reads input.",
		"**Dependencies**: lib/types/",
		"",
		"## Module Dependency Graph",
		"1. **lib/types/** has no dependencies none.",
		"2. **io/reader/** depends on lib/types.",
		"",
		"## End",
	}, "\n"))
	dag := ParseArchitectureToDAG(workspace)
	if dag == nil || len(dag.Tasks) != 2 {
		t.Fatalf("dag=%#v", dag)
	}
	if dag.Tasks[0].TaskKey != "lib-types" || dag.Tasks[1].TaskKey != "io-reader" ||
		len(dag.Tasks[1].Deps) != 1 || dag.Tasks[1].Deps[0].FromTask != "lib-types" {
		t.Fatalf("unexpected tasks: %#v", dag.Tasks)
	}

	asciiOnly := writeArchitecture(t, strings.Join([]string{
		"## Components",
		"### 1. `lib/types` — Core types",
		"Base.",
		"",
		"### 2. `cli/main` — CLI",
		"Entry.",
		"",
		"## Module Dependency Graph",
		"```",
		"cli/main.go",
		"  └── lib/types.go",
		"```",
	}, "\n"))
	parsed := ParseArchitectureToDAG(asciiOnly)
	if parsed == nil || len(parsed.Tasks) != 2 || len(parsed.Tasks[1].Deps) != 0 {
		t.Fatalf("dead ASCII fallback behavior changed: %#v", parsed)
	}
}

func TestApplyTranslatedDAGFrontierAndTopoOrder(t *testing.T) {
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	db := plandb.GetPlanDB()
	project := db.Init("planner-translate")
	root, err := db.AddTask(plandb.AddTaskInput{Title: "root", Project: project.ID})
	if err != nil {
		t.Fatal(err)
	}
	residual := json.RawMessage(`"Run integration checks."`)
	result := ApplyTranslatedDAG(ApplyTranslatedDAGInput{
		DAG: DAGData{Tasks: []DAGTask{
			{
				TaskKey: "down", Title: "Downstream", Kind: "code",
				Description: "Implement downstream after upstream.",
				Tags:        []string{"agent:fixer"}, Deps: []DAGDep{{FromTask: "up", Kind: "feeds_into"}},
			},
			{
				TaskKey: "up", Title: "Upstream", Kind: "code",
				Description: "Implement the upstream contract first.",
				Tags:        []string{"agent:fixer"}, Deps: []DAGDep{},
			},
		}, Residual: residual},
		ProjectID: project.ID, RootTaskID: root.ID,
	})
	if !result.OK || result.TaskCount != 2 || result.EdgeCount != 1 ||
		len(result.InsertedTaskIDs) != 2 {
		t.Fatalf("result=%#v", result)
	}
	first := db.GetTask(result.InsertedTaskIDs[0])
	second := db.GetTask(result.InsertedTaskIDs[1])
	if first == nil || second == nil || first.Title != "Upstream" || second.Title != "Downstream" {
		t.Fatalf("topological insert order drifted: %#v %#v", first, second)
	}
	contexts := db.ListContexts(&plandb.ListContextsFilter{
		Project: project.ID, Kind: "residual", TaskID: root.ID,
	})
	if len(contexts) != 1 || contexts[0].Content != "Run integration checks." {
		t.Fatalf("residual contexts=%#v", contexts)
	}
}

func TestApplyTranslatedDAGRejectsEmptyDanglingAndCycles(t *testing.T) {
	tests := []struct {
		name   string
		tasks  []DAGTask
		reason string
	}{
		{name: "empty", tasks: []DAGTask{}, reason: "DAG had zero tasks"},
		{name: "dangling", tasks: []DAGTask{{
			TaskKey: "a", Title: "A", Description: "description long enough",
			Deps: []DAGDep{{FromTask: "missing", Kind: "feeds_into"}},
		}}, reason: "dangling edge"},
		{name: "cycle", tasks: []DAGTask{
			{TaskKey: "a", Title: "A", Description: "description long enough", Deps: []DAGDep{{FromTask: "b", Kind: "feeds_into"}}},
			{TaskKey: "b", Title: "B", Description: "description long enough", Deps: []DAGDep{{FromTask: "a", Kind: "feeds_into"}}},
		}, reason: "cycle detected"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ApplyTranslatedDAGWithRunner(ApplyTranslatedDAGInput{
				DAG: DAGData{Tasks: test.tasks}, RootTaskID: "root",
			}, PlanDBRunnerFunc(func([]string) plandb.RunResult {
				t.Fatal("runner called for rejected DAG")
				return plandb.RunResult{}
			}))
			if got.OK || got.Reason == nil || !strings.Contains(*got.Reason, test.reason) {
				t.Fatalf("result=%#v", got)
			}
		})
	}
}

func TestApplyKeepsDependentAfterUpstreamInsertFailure(t *testing.T) {
	calls := [][]string{}
	runner := PlanDBRunnerFunc(func(argv []string) plandb.RunResult {
		calls = append(calls, append([]string(nil), argv...))
		if len(argv) > 2 && argv[2] == "Upstream" {
			return plandb.RunResult{Code: 1, Stderr: []byte("boom")}
		}
		return plandb.RunResult{Stdout: []byte(`{"id":"down-id"}`)}
	})
	got := ApplyTranslatedDAGWithRunner(ApplyTranslatedDAGInput{
		DAG: DAGData{Tasks: []DAGTask{
			{TaskKey: "up", Title: "Upstream", Kind: "code", Description: "upstream description", Tags: []string{}, Deps: []DAGDep{}},
			{TaskKey: "down", Title: "Downstream", Kind: "code", Description: "downstream description", Tags: []string{}, Deps: []DAGDep{{FromTask: "up", Kind: "feeds_into"}}},
		}}, RootTaskID: "root",
	}, runner)
	if !got.OK || got.TaskCount != 1 || got.Reason == nil ||
		*got.Reason != "partial: 2/2 tasks failed to insert" {
		t.Fatalf("result=%#v", got)
	}
	if len(calls) != 2 || containsArg(calls[1], "--dep") {
		t.Fatalf("dependent should be inserted without its missing edge: %#v", calls)
	}
}

func TestApplyDropsPriorityAndDuplicatesRepeatedTaskKeys(t *testing.T) {
	priority := jscompat.JSNumber(99)
	calls := [][]string{}
	runner := PlanDBRunnerFunc(func(argv []string) plandb.RunResult {
		calls = append(calls, append([]string(nil), argv...))
		id := "id-" + itoa(len(calls))
		return plandb.RunResult{Stdout: []byte(`{"id":"` + id + `"}`)}
	})
	got := ApplyTranslatedDAGWithRunner(ApplyTranslatedDAGInput{
		DAG: DAGData{Tasks: []DAGTask{
			{TaskKey: "same", Title: "First", Kind: "code", Description: "first description long enough", Tags: []string{}, Priority: &priority, Deps: []DAGDep{}},
			{TaskKey: "same", Title: "Second", Kind: "code", Description: "second description long enough", Tags: []string{}, Priority: &priority, Deps: []DAGDep{}},
		}}, RootTaskID: "root",
	}, runner)
	if !got.OK || got.TaskCount != 2 || len(calls) != 2 {
		t.Fatalf("result=%#v calls=%#v", got, calls)
	}
	for _, call := range calls {
		if len(call) < 3 || call[2] != "Second" {
			t.Fatalf("duplicate key did not duplicate last task: %#v", calls)
		}
		if containsArg(call, "--priority") {
			t.Fatalf("source unexpectedly forwarded priority: %#v", call)
		}
	}
}

func containsArg(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
