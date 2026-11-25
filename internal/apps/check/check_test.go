package check_test

import (
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

func TestSampleReportsIDsSharedByDifferentTypes(t *testing.T) {
	def := schema.Definition{Files: map[string][]byte{
		schema.SampleFile: []byte(`{"records":{"company":[{"id":"00000000000000000000000001"}],"contact":[{"id":"00000000000000000000000001"}]}}`),
	}}
	_, err := check.Sample(def)
	if err == nil || !strings.Contains(err.Error(), "Example contact row 1 reuses id") {
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
