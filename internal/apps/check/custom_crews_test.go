package check

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/custom"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestCustomCheckRule(t *testing.T) {
	cases := []struct {
		name, file, source string
		write, pass        bool
	}{
		{"good", "custom/summary.tsx", `import React from "react"; export default () => <div>Deals</div>;`, true, true},
		{"missing", "custom/summary.tsx", "", false, false},
		{"extension", "custom/summary.js", "export {};", false, false},
		{"outside custom", "summary.tsx", "export {};", false, false},
		{"traversal", "custom/../summary.tsx", "export {};", false, false},
		{"forbidden", "custom/summary.tsx", "export {};\nfetch('/records');", true, false},
		{"import", "custom/summary.tsx", `import "other";`, true, false},
		{"large", "custom/summary.tsx", strings.Repeat("x", custom.MaxFileBytes+1), true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files, sample := fixture(t)
			var screen spec.Screen
			if err := json.Unmarshal(files["screens/deal.json"], &screen); err != nil {
				t.Fatal(err)
			}
			screen.Body.Children = append(screen.Body.Children, &spec.Node{Part: "Custom", Props: map[string]any{"file": tc.file}})
			files["screens/deal.json"], _ = json.Marshal(screen)
			if tc.write {
				files[tc.file] = []byte(tc.source)
			}
			report, err := New(svc.Config{}, services(sample)).Run(context.Background(), "crm", files, 1)
			if err != nil || report.Pass != tc.pass {
				t.Fatalf("want pass=%v, report=%+v err=%v", tc.pass, report, err)
			}
			if !tc.pass {
				found := false
				for _, issue := range report.Issues {
					if issue.Rule == custom.Rule {
						found = true
						if issue.File == "custom/summary.tsx" && !strings.Contains(issue.Message, "line ") {
							t.Fatalf("missing line: %+v", issue)
						}
					}
				}
				if !found {
					t.Fatalf("no kit/custom issue: %+v", report.Issues)
				}
			}
		})
	}
}

func TestCustomCheckDoesNotMutateFiles(t *testing.T) {
	files, _ := fixture(t)
	files["custom/summary.tsx"] = []byte("export {};")
	definitions, issues := customDefinitions(files)
	if len(issues) != 0 || files["custom/summary.tsx"] == nil || definitions["custom/summary.tsx"] != nil {
		t.Fatalf("issues=%+v, files=%v, schema files=%v", issues, files.Paths(), definitions.Paths())
	}
	files["custom/summary.tsx"] = []byte("fetch('/records');")
	if issues := validateFiles(files); len(issues) == 0 || issues[0].Rule != custom.Rule {
		t.Fatalf("unused custom code bypasses check: %+v", issues)
	}
}
