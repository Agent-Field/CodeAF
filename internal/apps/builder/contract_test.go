package builder_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
)

func TestPlanReservesExplicitRunIDs(t *testing.T) {
	jobs, err := builder.OpenJobs(builder.Options{
		Home: t.TempDir(),
		Model: func() (builder.Completer, error) {
			called := false
			return script(func(context.Context, []ai.Message, []ai.ToolDefinition) (ai.Message, error) {
				if !called {
					called = true
					return toolCalls(command("plan", map[string]any{"name": "Workbench", "plan": json.RawMessage(
						`{"ask":"Research and draft","data":[{"id":"item","line":"Items"}],"screens":[{"id":"items","line":"Items"}],"automation":[{"id":"collect-data","line":"Collect the data"}]}`),
					})), nil
				}
				return ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "The plan is ready."}}}, nil
			}), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer jobs.Close()
	got, err := jobs.Plan(t.Context(), builder.PlanRequest{Ask: "Use item as the type, items as the screen, and collect-data and ship-report as the run IDs."})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := schema.ParsePlan(got.Plan)
	if err != nil || !slices.ContainsFunc(plan.Automation, func(line schema.PlanLine) bool { return line.ID == "ship-report" }) {
		t.Fatalf("plan did not reserve the requested run: %+v, %v", plan.Automation, err)
	}
}

func TestBuildUsesNativeImportAndLifecycle(t *testing.T) {
	home := t.TempDir()
	definitions := store.NewAt(home, nil)
	const ask = "Import CSV into items. Start each item as Pending and use a lifecycle transition with ID publish to move it to Done."
	const outline = `{"ask":"Track items","data":[{"id":"item","line":"Items"}],"screens":[{"id":"items","line":"Items"}],"automation":[{"id":"csv-loader","line":"Import CSV into items"},{"id":"publish-item","line":"Move items to Done through a lifecycle transition"}]}`
	jobs, err := builder.OpenJobs(builder.Options{
		Home: home, Store: definitions,
		Compiler: fixtureCompiler{schema.Definition{Files: map[string][]byte{
			"screens/items.json": []byte(`{"screen":"items","title":"Items","body":{"id":"root","part":"Stack"}}`),
		}}},
		Model: func() (builder.Completer, error) {
			step := 0
			return script(func(_ context.Context, messages []ai.Message, _ []ai.ToolDefinition) (ai.Message, error) {
				step++
				switch step {
				case 1:
					return toolCalls(command("plan", map[string]any{"name": "Items", "plan": json.RawMessage(outline)})), nil
				case 2:
					return toolCalls(command("add-type", map[string]any{"name": "item", "definition": json.RawMessage(`{"fields":{"name":{"type":"text"},"status":{"type":"enum","values":["Pending","Done"],"default":"Pending"}}}`)})), nil
				case 3:
					if !strings.Contains(messages[0].Content[0].Text, "CURRENT PHASE: planning") {
						return toolCalls(command("markup", map[string]string{"screen": "items"})), nil
					}
				}
				return ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "Items are ready."}}}, nil
			}), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer jobs.Close()
	got, err := jobs.Plan(t.Context(), builder.PlanRequest{Ask: ask})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := schema.ParsePlan(got.Plan)
	if err != nil || len(plan.Automation) != 0 || len(plan.Builtins) == 0 {
		t.Fatalf("plan should use native behavior: %+v, %v", plan, err)
	}
	if err := jobs.Build(got.Draft); err != nil {
		t.Fatal(err)
	}
	waitState(t, jobs, got.Draft, builder.Ready)
	kept, err := jobs.Apply(got.Draft)
	if err != nil {
		t.Fatal(err)
	}
	live, err := definitions.Get(kept.ID)
	if err != nil {
		t.Fatal(err)
	}
	app, err := schema.ParseApp(live.App)
	if err != nil || app.Records["item"].Lifecycle == nil {
		t.Fatalf("missing native lifecycle: %+v, %v", app.Records["item"], err)
	}
	lifecycle := app.Records["item"].Lifecycle
	if lifecycle.Initial != "Pending" || len(lifecycle.Transitions) != 1 || lifecycle.Transitions[0].ID != "publish" {
		t.Fatalf("native lifecycle = %+v", lifecycle)
	}
}
