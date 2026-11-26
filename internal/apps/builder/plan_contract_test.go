package builder

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

func TestRunReservationSurvivesEditsAndAllowsRequestedRemoval(t *testing.T) {
	plan := schema.Plan{Ask: "Research and draft", Automation: []schema.PlanLine{
		{ID: "research", Line: "Research the story"}, {ID: "draft", Line: "Draft the story"},
	}}
	files := map[string]string{"runs/research.yaml": "run: research", "runs/draft.yaml": "run: draft"}
	args := json.RawMessage(`{"id":"draft"}`)
	edit := plannedWork("Add a title", plan)
	if err := edit.checkEdit("remove_run", args, files); err == nil {
		t.Fatal("the writer can remove a reserved run")
	}
	revised := plan
	revised.Automation = plan.Automation[:1]
	if err := saveJSON(files, schema.PlanFile, revised); err != nil {
		t.Fatal(err)
	}
	if err := edit.apply(files); err != nil {
		t.Fatal(err)
	}
	got, _ := schema.ParsePlan([]byte(files[schema.PlanFile]))
	if len(got.Automation) != 2 {
		t.Fatalf("a plan rewrite lost a reserved run: %+v", got.Automation)
	}
	remove := plannedWork("Remove the run draft.", got)
	if err := remove.checkEdit("remove_run", args, files); err != nil {
		t.Fatal(err)
	}
	if err := remove.apply(files); err != nil {
		t.Fatal(err)
	}
	got, _ = schema.ParsePlan([]byte(files[schema.PlanFile]))
	if len(got.Automation) != 1 || got.Automation[0].ID != "research" || files["runs/draft.yaml"] != "" ||
		files["runs/research.yaml"] == "" {
		t.Fatalf("expected only the draft run to be removed: %+v", files)
	}
}

func TestBuiltInKeepsPlanLineAndWriterSettings(t *testing.T) {
	plan := schema.Plan{
		Ask:      "Mark an item finished through its lifecycle",
		Data:     []schema.PlanLine{{ID: "item", Line: "Items"}},
		Actions:  []schema.PlanLine{{ID: "finish", Line: "Mark the item finished through its lifecycle"}},
		Builtins: []schema.PlanBuiltin{{ID: "finish", Kind: schema.BuiltinLifecycle, Record: "item", Transition: "finish"}},
	}
	files := map[string]string{
		schema.AppFile:     `{"records":{"item":{"fields":{"status":{"type":"enum","values":["Open","Closed"]}},"lifecycle":{"field":"status","initial":"Open","transitions":[{"id":"close","from":["Open"],"to":"Closed"}]}}}}`,
		"runs/finish.yaml": "run: finish",
	}
	if err := saveJSON(files, schema.PlanFile, plan); err != nil {
		t.Fatal(err)
	}
	work := plannedWork(plan.Ask, schema.Plan{})
	if err := work.apply(files); err != nil {
		t.Fatal(err)
	}
	got, _ := schema.ParsePlan([]byte(files[schema.PlanFile]))
	if len(got.Actions) != 0 || len(got.Builtins) != 1 || got.Builtins[0].Line != plan.Actions[0].Line ||
		got.Builtins[0].Transition != "finish" || files["runs/finish.yaml"] != "" {
		t.Fatalf("builtin conversion = %+v", got)
	}
	app, err := schema.ParseApp([]byte(files[schema.AppFile]))
	if err != nil || !slices.ContainsFunc(app.Records["item"].Lifecycle.Transitions, func(tr schema.RecordTransition) bool {
		return tr.ID == "finish" && tr.To == "Closed"
	}) {
		t.Fatalf("native transition = %+v, %v", app.Records["item"].Lifecycle, err)
	}
}

func TestLocalApprovalSelectsAndGatesTheChosenRun(t *testing.T) {
	plan := schema.Plan{
		Ask:        "I approve before it saves",
		Automation: []schema.PlanLine{{ID: "draft", Line: "Draft and save a note"}, {ID: "read", Line: "Read notes"}},
		Builtins:   []schema.PlanBuiltin{{ID: schema.BuiltinApproval, Kind: schema.BuiltinApproval, Run: "draft"}},
	}
	files := map[string]string{
		"runs/draft.yaml": "run: draft\nsteps:\n  - id: each-note\n    loop: {over: $params.notes, as: note, max: 3}\n    steps:\n      - id: save\n        records.create: {type: note, set: {title: Draft}}\n",
		"runs/read.yaml":  "run: read\nsteps:\n  - id: look\n    gate: {approval: start}\n",
	}
	read := files["runs/read.yaml"]
	if err := compileCapabilities(plan.Ask, &plan, nil, nil, files); err != nil {
		t.Fatal(err)
	}
	run, err := schema.ParseRun([]byte(files["runs/draft.yaml"]))
	if err != nil || run.Steps[0].Steps[0].Gate == nil || run.Steps[0].Steps[0].Gate.Approval != "proof" ||
		files["runs/read.yaml"] != read {
		t.Fatalf("selected approval = %s, %v", files["runs/draft.yaml"], err)
	}
}
