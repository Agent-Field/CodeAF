package builder

import (
	"encoding/json"
	"slices"
	"strings"
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
	if len(got.Actions) != 1 || got.Actions[0].Line != plan.Actions[0].Line || len(got.Builtins) != 1 || got.Builtins[0].Line != plan.Actions[0].Line ||
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
	if err := plannedWork(plan.Ask, schema.Plan{}).compileCapabilities(&plan, files); err != nil {
		t.Fatal(err)
	}
	run, err := schema.ParseRun([]byte(files["runs/draft.yaml"]))
	if err != nil || run.Steps[0].Steps[0].Gate == nil || run.Steps[0].Steps[0].Gate.Approval != "proof" ||
		files["runs/read.yaml"] != read {
		t.Fatalf("selected approval = %s, %v", files["runs/draft.yaml"], err)
	}
}

func TestWriterCanCompleteAnApprovalWithItsOwnPlanID(t *testing.T) {
	plan := schema.Plan{Ask: "Draft and ask for approval before saving", Automation: []schema.PlanLine{
		{ID: "draft", Line: "Draft a note"}, {ID: "review", Line: "Review notes"},
	}}
	files := map[string]string{}
	work := plannedWork(plan.Ask, schema.Plan{})
	if err := saveJSON(files, schema.PlanFile, plan); err != nil {
		t.Fatal(err)
	}
	if err := work.apply(files); err != nil {
		t.Fatal(err)
	}
	unresolved, _ := schema.ParsePlan([]byte(files[schema.PlanFile]))
	if len(unresolved.Builtins) != 1 || unresolved.Builtins[0].Run != "" {
		t.Fatalf("expected one approval with no target yet: %+v", unresolved.Builtins)
	}
	plan.Builtins = []schema.PlanBuiltin{{ID: "approve-draft", Kind: schema.BuiltinApproval, Run: "draft"}}
	if err := saveJSON(files, schema.PlanFile, plan); err != nil {
		t.Fatal(err)
	}
	if err := work.apply(files); err != nil {
		t.Fatal(err)
	}
	got, _ := schema.ParsePlan([]byte(files[schema.PlanFile]))
	if len(got.Builtins) != 1 || got.Builtins[0].Run != "draft" {
		t.Fatalf("completed approval plan = %+v", got.Builtins)
	}
}

func TestPersonCanRemoveANativeCapability(t *testing.T) {
	plan := schema.Plan{Ask: "Import CSV into items and move them through a lifecycle", Data: []schema.PlanLine{{ID: "item", Line: "Items"}}}
	files := map[string]string{}
	work := plannedWork(plan.Ask, schema.Plan{})
	if err := saveJSON(files, schema.PlanFile, plan); err != nil {
		t.Fatal(err)
	}
	if err := work.apply(files); err != nil {
		t.Fatal(err)
	}
	got, _ := schema.ParsePlan([]byte(files[schema.PlanFile]))
	if len(got.Builtins) != 2 || len(got.Actions) != 2 {
		t.Fatalf("native work missing from the visible plan: %+v", got)
	}
	remove := plannedWork("Please remove CSV import.", got)
	if err := remove.apply(files); err != nil {
		t.Fatal(err)
	}
	got, _ = schema.ParsePlan([]byte(files[schema.PlanFile]))
	if len(got.Builtins) != 1 || got.Builtins[0].Kind != schema.BuiltinLifecycle ||
		len(got.Actions) != 1 || got.Actions[0].ID != got.Builtins[0].ID {
		t.Fatalf("expected only the lifecycle selection to remain: %+v", got)
	}
}

func TestOutlineKeepsOnlySelectedBuiltinsAndNamesBoundSchedules(t *testing.T) {
	const outline = `{"ask":"Review posts each morning","data":[{"id":"post","line":"Posts"}],
"screens":[{"id":"posts","line":"Posts"}],"automation":[{"id":"review","line":"Review a post"}],
"builtins":[{"id":"review","kind":"agent"},{"id":"compose","kind":"generate-text"},{"id":"schedule","kind":"approval","run":"review"}]}`
	files := map[string]string{
		schema.PlanFile:     outline,
		schema.AppFile:      `{"records":{"post":{"fields":{}}},"triggers":[{"id":"each-morning","on":"schedule","run":"morning"}]}`,
		"runs/morning.yaml": "run: morning\ntitle: Review this morning's posts\n",
	}
	work := plannedWork("Review posts each morning", schema.Plan{})
	if err := work.apply(files); err != nil {
		t.Fatal(err)
	}
	got, _ := schema.ParsePlan([]byte(files[schema.PlanFile]))
	if len(got.Builtins) != 0 || len(got.Automation) != 2 || got.Automation[0].ID != "review" ||
		got.Automation[1].ID != "morning" || got.Automation[1].Line != "Review this morning's posts" {
		t.Fatalf("compiled outline = %+v", got)
	}
}

func TestOrdinaryCreationUsesTheRecordFormAndPreservesRequestedRuns(t *testing.T) {
	const outline = `{"ask":"Add and edit shared contacts","data":[{"id":"contact","line":"Contacts","uses":true}],
"screens":[{"id":"contacts","line":"Contacts"}],
"automation":[{"id":"add-contact","line":"Add a shared contact"},{"id":"create-contact","line":"Create a contact"}],
"actions":[{"id":"add-contact","line":"Add a shared contact"}]}`
	files := map[string]string{
		schema.PlanFile:            outline,
		schema.AppFile:             `{"records":{"contact":{"uses":true,"fields":{}}},"capabilities":{"records":{"contact":"rw"}}}`,
		"runs/add-contact.yaml":    "run: add-contact",
		"runs/create-contact.yaml": "run: create-contact",
	}
	work := plannedWork("Add and edit shared contacts. Use run ID create-contact.", schema.Plan{})
	if err := work.apply(files); err != nil {
		t.Fatal(err)
	}
	plan, err := schema.ParsePlan([]byte(files[schema.PlanFile]))
	if err != nil || !plan.Data[0].Create || !plan.Data[0].Uses || len(plan.Actions) != 0 ||
		len(plan.Automation) != 1 || plan.Automation[0].ID != "create-contact" ||
		files["runs/add-contact.yaml"] != "" || files["runs/create-contact.yaml"] == "" {
		t.Fatalf("compiled plan = %+v, %v", plan, err)
	}
}

func TestRecordFormSelectionKeepsReadOnlyAndApprovalRuns(t *testing.T) {
	for _, tc := range []struct{ ask, access string }{
		{"Add a shared contact", "r"},
		{"Add a shared contact. Approve before saving.", "rw"},
	} {
		t.Run(tc.ask, func(t *testing.T) {
			files := map[string]string{
				schema.PlanFile: `{"ask":"Add shared contacts","data":[{"id":"contact","line":"Contacts","uses":true}],
"automation":[{"id":"add-contact","line":"Add a shared contact"}]}`,
				schema.AppFile:          `{"records":{"contact":{"uses":true,"fields":{}}},"capabilities":{"records":{"contact":"` + tc.access + `"}}}`,
				"runs/add-contact.yaml": "run: add-contact\nsteps:\n  - id: save\n    records.create: {type: contact, set: {name: Example}}\n",
			}
			work := plannedWork(tc.ask, schema.Plan{})
			if err := work.apply(files); err != nil {
				t.Fatal(err)
			}
			plan, _ := schema.ParsePlan([]byte(files[schema.PlanFile]))
			run, err := schema.ParseRun([]byte(files["runs/add-contact.yaml"]))
			if err != nil || run.Run != "add-contact" || len(plan.Automation) != 1 || plan.Data[0].Create {
				t.Fatalf("expected the original run to survive: %+v, %v", plan, err)
			}
			if tc.access == "rw" && (run.Steps[0].Gate == nil || run.Steps[0].Gate.Approval != "proof") {
				t.Fatalf("expected a gate before saving: %+v", run)
			}
		})
	}
}

func TestRepairGetsTheSafeReferenceParameterFinding(t *testing.T) {
	report := briefReport(&schema.Report{Problems: []schema.Problem{
		{Code: "run_failed", File: "runs/morning.yaml", Message: "Could not start: parameter posts: a refs parameter needs record ids or a record query"},
		{Code: "run_failed", File: "runs/another.yaml", Message: "A sample value may be private."},
	}})
	if !strings.Contains(report.Problems[0].Message, "query string") || strings.Contains(report.Problems[1].Message, "private") {
		t.Fatalf("repair report = %+v", report.Problems)
	}
}
