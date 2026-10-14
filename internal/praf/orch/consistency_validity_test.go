package orch

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/praf/reasoners"
	"github.com/Agent-Field/codeaf/internal/praf/schemas"
)

func TestConsistencyVerdictsRequireFindingDetailsOnlyWhenTheyClaimADefect(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(map[string]any)
		invalid bool
		count   int
	}{
		{"grounded defect", func(map[string]any) {}, false, 1},
		{"property holds", func(v map[string]any) { v["holds"] = true; delete(v, "body"); delete(v, "file_path") }, false, 0},
		{"live draft without details", func(v map[string]any) {
			v["title"] = "exploratory internal note: property holds; holds=true"
			delete(v, "body")
			delete(v, "file_path")
			delete(v, "line_start")
			delete(v, "line_end")
		}, true, 0},
		{"blank explanation", func(v map[string]any) { v["body"] = " \n" }, true, 0},
		{"missing location", func(v map[string]any) { v["file_path"] = " " }, true, 0},
		{"missing line", func(v map[string]any) { v["line_start"] = 0 }, true, 0},
		{"reversed lines", func(v map[string]any) { v["line_end"] = 1 }, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := degradationOrchestrator(t)
			o.prData.ChangedFiles = []schemas.ChangedFile{{Path: "a.go", Patch: "@@ -1 +1 @@\n-old\n+new"}}
			o.rfns.extractOblig = func(context.Context, reasoners.Deps, reasoners.ExtractObligationsInput) (map[string]any, error) {
				return map[string]any{"obligations": []any{map[string]any{"property": "keys agree"}}}, nil
			}
			o.rfns.verifyOblig = func(context.Context, reasoners.Deps, reasoners.VerifyObligationInput) (map[string]any, error) {
				v := map[string]any{"holds": false, "title": "keys disagree", "body": "Admission and completion use different keys.", "file_path": "a.go", "line_start": 3, "line_end": 4}
				tc.change(v)
				return v, nil
			}
			findings, err := o.runConsistencyVerify(context.Background(), nil)
			if (err != nil) != tc.invalid || len(findings) != tc.count {
				t.Fatalf("findings=%+v, err=%v; invalid=%v count=%d", findings, err, tc.invalid, tc.count)
			}
		})
	}
}
