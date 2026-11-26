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
