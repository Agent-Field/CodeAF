package check_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/check"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

func TestSampleReportsTheRowWithAnInvalidID(t *testing.T) {
	def := schema.Definition{Files: map[string][]byte{
		schema.SampleFile: []byte(`{"records":{"company":[{"id":"company-one","name":"Example"}]}}`),
	}}
	_, err := check.Sample(def)
	if err == nil || !strings.Contains(err.Error(), "company row 1 needs a 26-character ULID") {
		t.Fatalf("sample problem = %v", err)
	}
}

func TestSampleReportsAllInvalidIDsTogether(t *testing.T) {
	def := schema.Definition{Files: map[string][]byte{
		schema.SampleFile: []byte(`{"records":{"company":[{"id":"00000000000000000000000001"}],"contact":[{"id":"00000000000000000000000001"},{"id":"contact-two"}]}}`),
	}}
	_, err := check.Sample(def)
	if err == nil || !strings.Contains(err.Error(), "Example contact row 1 reuses id") ||
		!strings.Contains(err.Error(), "Example contact row 2 needs a 26-character ULID") {
		t.Fatalf("sample problem = %v", err)
	}
}

func TestCheckMeasuresDeskLookInBothThemesAtBothWidths(t *testing.T) {
	def, err := schema.Load("../schema/testdata/crm")
	if err != nil {
		t.Fatal(err)
	}
	pipeline := check.Pipeline{
		Seed:   func(string, []byte, map[string][]map[string]any) error { return nil },
		DryRun: func(context.Context, string, string, schema.Definition) error { return nil },
	}
	if report := pipeline.Run(t.Context(), "draft", def, nil); !report.OK() || report.Look != "skipped (desk not running)" {
		t.Fatalf("Check without Desk: %v", report)
	}
	seen := map[check.LookViewport]int{}
	pipeline.Look = func(_ context.Context, draft, screen string, view check.LookViewport) ([]string, error) {
		seen[view]++
		if screen == "deals" && view.Theme == "light" && view.Width == 800 {
			return []string{"[deal-list] value truncates"}, nil
		}
		return nil, nil
	}
	report := pipeline.Run(t.Context(), "draft", def, nil)
	for _, theme := range []string{"dark", "light"} {
		for _, width := range []int{800, 1120} {
			if got := seen[check.LookViewport{Theme: theme, Width: width}]; got != len(def.ScreenFiles()) {
				t.Errorf("%s at %d got %d renders", theme, width, got)
			}
		}
	}
	if !slices.Contains(report.Codes(), "screen_layout") || !strings.Contains(report.String(), "light at 800 px") {
		t.Fatalf("Check must fail the measured problem: %v", report)
	}
}

func TestUnresponsiveDeskStopsCheckAfterTheFirstCaptureError(t *testing.T) {
	def, err := schema.Load("../schema/testdata/crm")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	report := (check.Pipeline{
		Seed:   func(string, []byte, map[string][]map[string]any) error { return nil },
		DryRun: func(context.Context, string, string, schema.Definition) error { return nil },
		Look: func(context.Context, string, string, check.LookViewport) ([]string, error) {
			calls++
			return nil, errors.New("renderer timed out")
		},
	}).Run(t.Context(), "draft", def, nil)
	if calls != 1 || report.OK() || len(report.Problems) != 1 || report.Problems[0].Code != "desk_render_unavailable" {
		t.Fatalf("connected renderer got %d requests with report %+v", calls, report)
	}
}

func TestCheckNamesMissingScreenControls(t *testing.T) {
	def, err := schema.Load("../schema/testdata/crm")
	if err != nil {
		t.Fatal(err)
	}
	def.Files[schema.PlanFile] = bytes.Replace(def.Files[schema.PlanFile], []byte(`"id": "deal"`), []byte(`"id": "deal", "create": true`), 1)
	def.Files[schema.PlanFile] = bytes.Replace(def.Files[schema.PlanFile], []byte(`"screens":`), []byte(`"actions":[{"id":"won-handoff","line":"Post handoff"}], "screens":`), 1)
	// Leave both commands declared and both runs scheduled, but remove the
	// Deals controls that expose those commands and create a deal.
	def.Files["screens/deals.json"] = []byte(`{"screen":"deals","title":"Deals","body":{"part":"RecordList","id":"deals","label":"Deals","bind":{"records":"deal"},"props":{"columns":["name"]}}}`)
	pipeline := check.Pipeline{
		Seed:   func(string, []byte, map[string][]map[string]any) error { return nil },
		DryRun: func(context.Context, string, string, schema.Definition) error { return nil },
	}
	report := pipeline.Run(t.Context(), "draft", def, nil)
	for _, label := range []string{"Draft follow-up", "Mark won", "Add deal", "Post handoff"} {
		if !strings.Contains(report.String(), label) {
			t.Errorf("Check didn't name %q: %v", label, report)
		}
	}
}

func TestCheckRejectsACreateButtonMissingRequiredInput(t *testing.T) {
	def, err := schema.Load("../schema/testdata/crm")
	if err != nil {
		t.Fatal(err)
	}
	def.Files[schema.PlanFile] = bytes.Replace(def.Files[schema.PlanFile], []byte(`"id": "deal"`), []byte(`"id": "deal", "create": true`), 1)
	screen := def.Files["screens/deals.json"]
	def.Files["screens/deals.json"] = bytes.Replace(screen, []byte(`"name": "$input.name", `), nil, 1)
	if report := check.Definition(def, nil); !slices.Contains(report.Codes(), "create_control_missing") {
		t.Fatalf("Check accepted a create button without the required name: %v", report)
	}
	def.Files["screens/deals.json"] = screen
	if report := check.Definition(def, nil); !report.OK() {
		t.Fatalf("Check rejected the working control: %v", report)
	}
}

func TestCheckRejectsAnUnboundDraftPlaceholder(t *testing.T) {
	def, err := schema.Load("../schema/testdata/crm")
	if err != nil {
		t.Fatal(err)
	}
	const file = "runs/follow-up.yaml"
	def.Files[file] = bytes.Replace(def.Files[file], []byte("Draft a short, plain follow-up for {{deal.name}} from the thread."), []byte("Dear [Name], thanks for your time. [Your name]"), 1)
	report := check.Definition(def, nil)
	if !slices.Contains(report.Codes(), "unbound_placeholder") {
		t.Fatalf("Check accepted a draft placeholder: %v", report)
	}
}

func TestCheckRequiresTheAssistantActionsItClaims(t *testing.T) {
	def, err := schema.Load("../schema/testdata/crm")
	if err != nil {
		t.Fatal(err)
	}
	plan := def.Files[schema.PlanFile]
	def.Files[schema.PlanFile] = bytes.Replace(plan, []byte(`"agent": { "line":`), []byte(`"agent": { "actions": ["mark-won", "reopen-deal"], "line":`), 1)
	report := check.Definition(def, nil)
	if !slices.Contains(report.Codes(), "agent_action_missing") || !strings.Contains(report.String(), "reopen-deal") {
		t.Fatalf("Check accepted an assistant promising a missing Action: %v", report)
	}
	def.Files[schema.PlanFile] = bytes.Replace(def.Files[schema.PlanFile], []byte(`, "reopen-deal"`), nil, 1)
	if report := check.Definition(def, nil); !report.OK() {
		t.Fatalf("Check failed the registered Mark Won Action: %v", report)
	}
}

func TestCheckRequiresOneGrantableActionForAMultiEffectCommand(t *testing.T) {
	def, err := schema.Load("../schema/testdata/crm")
	if err != nil {
		t.Fatal(err)
	}
	app := def.Files[schema.AppFile]
	def.Files[schema.AppFile] = bytes.Replace(app, []byte(`"set": { "stage": "Won" } } }`), []byte(`"set": { "stage": "Won" } } }, { "runs.start": "follow-up" }`), 1)
	if report := check.Definition(def, nil); !slices.Contains(report.Codes(), "command_action_missing") {
		t.Fatalf("Check accepted a multi-effect Mark Won command without a grantable Action: %v", report)
	}
}

func TestCheckReportsExampleDepthWithoutBlockingAPersonalApp(t *testing.T) {
	def, err := schema.Load("../schema/testdata/crm")
	if err != nil {
		t.Fatal(err)
	}
	pipeline := check.Pipeline{
		Seed:   func(string, []byte, map[string][]map[string]any) error { return nil },
		DryRun: func(context.Context, string, string, schema.Definition) error { return nil },
	}
	report := pipeline.Run(t.Context(), "draft", def, nil)
	if !report.OK() || report.Depth == nil || report.Depth.Screens != 3 || report.Depth.Records == 0 {
		t.Fatalf("Check report = %+v", report)
	}
	if report.Depth.Passed() || len(report.Depth.Warnings) == 0 {
		t.Fatalf("thin example must have a warning: %+v", report.Depth)
	}
}

func TestCheckRequiresARecordPageToHaveARecord(t *testing.T) {
	def, err := schema.Load("../schema/testdata/crm")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ bind, message string }{
		{"", "Bind this RecordPage to a record"},
		{`,"bind":{"record":"deal","type":"deal"}`, "A RecordPage needs a record ID"},
	} {
		def.Files["screens/broken.json"] = []byte(`{"screen":"broken","title":"Detail","body":{"part":"RecordPage","label":"Detail","props":{"fields":["name"]}` + test.bind + `}}`)
		report := check.Definition(def, nil)
		if report.OK() || !strings.Contains(report.String(), test.message) {
			t.Fatalf("Check accepted a detail page with no record: %v", report)
		}
	}

}

func TestCheckRejectsACompletionThatTriesToReadAURL(t *testing.T) {
	def, err := schema.Load("../schema/testdata/crm")
	if err != nil {
		t.Fatal(err)
	}
	def.Files["runs/read-policy.yaml"] = []byte(`run: read-policy
budget: {per_run: 0.10}
params:
  source_url: {type: text}
steps:
  - id: draft
    ai:
      prompt: Read the policy document at {{params.source_url}} and draft a lesson.
      output: {type: string}
      example: Example lesson
`)
	report := check.Definition(def, nil)
	if !slices.Contains(report.Codes(), "run_source_unavailable") {
		t.Fatalf("Check accepted a model step that asks to fetch a URL: %v", report)
	}
}

func TestCheckNamesRegisteredTriggerAndConnectorOperations(t *testing.T) {
	def, err := schema.Load("../schema/testdata/ncr")
	if err != nil {
		t.Fatal(err)
	}
	def.Files[schema.AppFile] = []byte(`{"id":"ncr","name":"NC Review","version":1,"kit":2,
	  "records":{"case":{"fields":{"title":{"type":"text"}}}},
	  "capabilities":{"records":{"case":"r"},"connectors":{"calendar":{"can":["events.create"]}}},
	  "triggers":[{"id":"unexpected","on":"records.created","run":"review"}]}`)
	report := check.Definition(def, nil)
	for _, want := range []string{"Registered calendar operations: create, free_busy, list.", "Use connector, record.before, record.changed, schedule, timer."} {
		if !strings.Contains(report.String(), want) {
			t.Fatalf("expected %q in %v", want, report)
		}
	}
}

func TestExampleDepthCountsAnApprovedDraftAndRelatedRecords(t *testing.T) {
	def := schema.Definition{Files: map[string][]byte{
		schema.AppFile: []byte(`{"id":"crm","name":"CRM","kit":2,"records":{
			"deal":{"fields":{"company":{"type":"ref","to":"company"}}},
			"company":{"fields":{"name":{"type":"text"}}}
		},"triggers":[{"id":"weekly","on":"schedule","run":"save"}]}`),
		"screens/queue.json":    []byte(`{"body":{"part":"RecordList","bind":{"records":"deal"}},"views":{"open":{"type":"deal"}}}`),
		"screens/detail.json":   []byte(`{"body":{"part":"RecordPage","bind":{"record":"$params.id","type":"deal"},"props":{"fields":["company"]}}}`),
		"screens/overview.json": []byte(`{"body":{"part":"Chart","bind":{"records":"deal"}}}`),
		"runs/save.yaml": []byte(`run: save
steps:
  - id: save
    gate: {approval: proof}
    records.create: {type: deal, set: {company: example}}
`),
	}}
	rows := map[string][]map[string]any{
		"company": {{"id": "00000000000000000000000001", "name": "Northwind"}},
	}
	for range 29 {
		rows["deal"] = append(rows["deal"], map[string]any{"company": "00000000000000000000000001"})
	}
	report := check.ExampleDepth(def, nil, rows)
	if !report.Passed() || report.Records != 30 || report.RelatedRecordPages != 1 || report.WriteBackActions != 1 {
		t.Fatalf("depth report: %+v", report)
	}
}
