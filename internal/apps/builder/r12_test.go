package builder

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/check"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

func r12Draft(t *testing.T, name string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile("testdata/r12/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var files map[string]string
	if err := json.Unmarshal(raw, &files); err != nil {
		t.Fatal(err)
	}
	return files
}

func TestR12LandingDepthUsesOpenedQueries(t *testing.T) {
	files := r12Draft(t, "client-desk")
	// Reproduce the client queue first, then make the view selective. The
	// other 24 records and the other clients cannot satisfy this view.
	plan, _ := schema.ParsePlan([]byte(files[schema.PlanFile]))
	plan.Screens[0], plan.Screens[2] = plan.Screens[2], plan.Screens[0]
	if err := saveJSON(files, schema.PlanFile, plan); err != nil {
		t.Fatal(err)
	}
	screen, _ := schema.ParseScreen([]byte(files["screens/clients.json"]))
	query := screen.Views[screen.Body.Bind.View]
	query.Filter = []schema.QueryPredicate{{Field: "name", Op: "ne", Value: "Acme Studio"}}
	screen.Views[screen.Body.Bind.View] = query
	if err := saveJSON(files, "screens/clients.json", screen); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	before, _ := check.Sample(definition(files))
	if count, err := fillLandingExamples(files, nil, now); err != nil || count != check.MinLandingRows {
		t.Fatalf("want %d distinct clients through the saved query, got %d (%v)", check.MinLandingRows, count, err)
	}
	after, err := check.Sample(definition(files))
	if err != nil || len(after["client"]) != check.MinLandingRows+1 || !reflect.DeepEqual(before["deliverable"], after["deliverable"]) {
		t.Fatalf("repair should fill only the opening view: %v (%v)", after["client"], err)
	}
	prior := files[schema.SampleFile]
	if _, err := fillLandingExamples(files, nil, now); err != nil || files[schema.SampleFile] != prior {
		t.Fatalf("second pass changed examples: %v", err)
	}
}

func TestR12RunWriteReportsTheKitShapeAndRepairsAnAlias(t *testing.T) {
	author := authoring{runShapeRejected: map[string]bool{}}
	meeting := r12Draft(t, "meeting-copilot")
	write := func(files map[string]string, id, text string) (map[string]string, error) {
		raw, _ := json.Marshal(fileCommand{ID: id, Text: text})
		return author.edit(t.Context(), "set_run", files, nil, raw)
	}
	if _, err := write(meeting, "process-transcript", meeting["runs/process-transcript.yaml"]); err == nil ||
		!strings.Contains(err.Error(), "/steps/0") || !strings.Contains(err.Error(), "Kit example:") {
		t.Fatalf("want a write-time error and a single kit shape: %v", err)
	}
	feedback := r12Draft(t, "feedback-roadmap")
	const path = "runs/ingest-quotes.yaml"
	// R12's shape findings include a record key on records.update. The
	// original saved quote update gives us its exact target and payload.
	raw := strings.Replace(feedback[path], "id: $params.quote.id", "record: $params.quote.id", 1)
	if _, err := write(feedback, "ingest-quotes", raw); err == nil || !strings.Contains(err.Error(), `"records.update"`) {
		t.Fatalf("want the records.update example on the first rejection: %v", err)
	}
	next, err := write(feedback, "ingest-quotes", raw)
	if err != nil {
		t.Fatal(err)
	}
	run, err := schema.ParseRun([]byte(next[path]))
	if err != nil || run.Steps[1].RecordsUpdate.ID != "$params.quote.id" {
		t.Fatalf("want the same record update after repair: %#v, %v", run.Steps, err)
	}
}

func TestR12TriggerBindsTheQuoteWithoutAProvidedParameter(t *testing.T) {
	files := r12Draft(t, "feedback-roadmap")
	const path = "runs/ingest-quotes.yaml"
	hasFinding := func(report schema.Report) bool {
		return slices.ContainsFunc(report.Problems, func(p schema.Problem) bool {
			return p.File == path && p.Path == "/steps/1/records.update/id" && p.Code == schema.CodeTriggerInput
		})
	}
	if !hasFinding(schema.Validate(definition(files), nil)) {
		t.Fatal("the saved run's unfilled quote parameter needs a finding at the update")
	}
	app, _ := schema.ParseApp([]byte(files[schema.AppFile]))
	if _, err := normalizeRunInputs(files, app); err != nil {
		t.Fatal(err)
	}
	run, err := schema.ParseRun([]byte(files[path]))
	if err != nil || !run.Params["quote"].FromTriggerRecord() || run.Params["themes"].Default != "status:New" {
		t.Fatalf("want a typed arrival input and the original theme query: %+v (%v)", run.Params, err)
	}
	if hasFinding(schema.Validate(definition(files), nil)) {
		t.Fatal("the compiled run still asks an arrival to supply a manual parameter")
	}
}

func TestR12BareClientListGetsAWorkspace(t *testing.T) {
	files := r12Draft(t, "client-desk")
	plan, _ := schema.ParsePlan([]byte(files[schema.PlanFile]))
	plan.Screens[0], plan.Screens[2] = plan.Screens[2], plan.Screens[0]
	if err := saveJSON(files, schema.PlanFile, plan); err != nil {
		t.Fatal(err)
	}
	hasFinding := func() bool {
		return slices.ContainsFunc(check.LookFindings(definition(files), nil), func(p schema.Problem) bool {
			return p.Code == "look_landing_without_detail" && p.File == "screens/clients.json"
		})
	}
	if !hasFinding() {
		t.Fatal("Check should flag the saved landing list")
	}
	if _, err := composeWorkScreens(files, nil); err != nil {
		t.Fatal(err)
	}
	screen, _ := schema.ParseScreen([]byte(files["screens/clients.json"]))
	list := mainCollection(screen.Body)
	if hasFinding() || list == nil || !check.WorkspaceFollows(screen.Body, list.ID) ||
		!nodeHasID(screen.Body, list.ID+"-record") {
		t.Fatalf("expected the original client query beside a selected client workspace: %+v", screen.Body)
	}
}

func TestR12MissingRunsDoNotLeavePromisesOrTriggers(t *testing.T) {
	for name, missing := range map[string]string{"ae-account-desk": "desk-brief", "csm-renewal-desk": "weekly-digest"} {
		t.Run(name, func(t *testing.T) {
			files := r12Draft(t, name)
			path := "runs/" + missing + ".yaml"
			delete(files, path)
			notes, err := reconcileRunLinks(files)
			if err != nil || !slices.ContainsFunc(notes, func(p schema.Problem) bool { return p.File == path }) {
				t.Fatalf("want an explicit note for the missing run: %+v (%v)", notes, err)
			}
			app, _ := schema.ParseApp([]byte(files[schema.AppFile]))
			plan, _ := schema.ParsePlan([]byte(files[schema.PlanFile]))
			if hasPlanLine(plan.Automation, missing) || slices.ContainsFunc(app.Triggers, func(t schema.Trigger) bool { return t.Run == missing }) {
				t.Fatal("missing run still has a live or planned entry")
			}
			again, err := reconcileRunLinks(files)
			if err != nil || len(again) != 0 {
				t.Fatalf("second pass changed the draft: %+v, %v", again, err)
			}
		})
	}
}

func TestR12TriggerAndManualReadDifferentQuoteIDs(t *testing.T) {
	files := r12Draft(t, "feedback-roadmap")
	app, _ := schema.ParseApp([]byte(files[schema.AppFile]))
	raw, changed, err := schema.BindRunTrigger(app, []byte(files["runs/ingest-quotes.yaml"]))
	if err != nil || !changed {
		t.Fatalf("compile arrival binding: %v", err)
	}
	steps, err := runs.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	engine, err := runs.Open(runs.Config{
		Root: t.TempDir(),
		Load: func(_, _ string) (runs.Definition, error) { return steps, nil },
		Triggers: func() ([]runs.Trigger, error) {
			return []runs.Trigger{{App: app.ID, ID: "new-quote", On: "record.changed", Type: "feedback", Run: "ingest-quotes"}}, nil
		},
		ResolveParams: func(_ string, _ map[string]runs.Parameter, provided map[string]any) (map[string]any, error) {
			id, _ := provided["quote"].(string)
			return map[string]any{"quote": map[string]any{"id": id, "text": "Bulk export stalls", "source": "Support", "author": "Lena"}, "themes": []any{}}, nil
		},
		Agent: func(_ context.Context, _ runs.AgentCall) (runs.Reply, error) {
			return runs.Reply{Output: `{"theme_id":"theme-1","mentions":4,"new_theme":""}`}, nil
		},
		Records: func(_ context.Context, call runs.RecordCall) (runs.Reply, error) {
			if call.Type == "feedback" {
				ids = append(ids, call.IDs[0])
			}
			return runs.Reply{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	if err := engine.RecordChanged(runs.Change{Type: "feedback", ID: "arrival", After: map[string]any{"text": "Bulk export stalls"}}); err != nil {
		t.Fatal(err)
	}
	if err := engine.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Start(app.ID, "ingest-quotes", map[string]any{"quote": "manual"}); err != nil {
		t.Fatal(err)
	}
	if err := engine.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids, []string{"arrival", "manual"}) {
		t.Fatalf("automation updated %v", ids)
	}
}
