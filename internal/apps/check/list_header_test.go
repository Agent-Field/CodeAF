package check

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestRecordListHeaderParts(t *testing.T) {
	for _, tc := range []struct {
		name, rule, path string
		change           func(*spec.Screen)
	}{
		{name: "board 08"},
		{name: "header navigation", change: func(s *spec.Screen) { s.Nav.Props["placement"] = "header" }},
		{name: "table sort field", change: func(s *spec.Screen) {
			s.Body.Children[0].Children[0] = &spec.Node{Part: "Table", Label: "Deals table", Props: map[string]any{"columns": []any{"name"}, "sort": "-amount"}}
		}},
		{name: "table sort unknown field", rule: "kit/bind", path: "/body/children/0/children/0/props/sort", change: func(s *spec.Screen) {
			s.Body.Children[0].Children[0] = &spec.Node{Part: "Table", Label: "Deals table", Props: map[string]any{"columns": []any{"name"}, "sort": "missing"}}
		}},
		{name: "navigation placement", rule: "kit/parts", path: "/nav/props/placement", change: func(s *spec.Screen) { s.Nav.Props["placement"] = "sidebar" }},
		{name: "filter labels", rule: "kit/parts", path: "/body/children/0/children/0/props/labels", change: func(s *spec.Screen) { s.Body.Children[0].Children[0].Props["labels"] = []any{true} }},
		{name: "filter AI flag", rule: "kit/parts", path: "/body/children/0/children/0/props/ai", change: func(s *spec.Screen) { s.Body.Children[0].Children[0].Props["ai"] = "yes" }},
		{name: "stat field required", rule: "kit/parts", path: "/body/children/0/children/1/props/field", change: func(s *spec.Screen) { delete(s.Body.Children[0].Children[1].Props, "field") }},
		{name: "stat field exists", rule: "kit/bind", path: "/body/children/0/children/1/props/field", change: func(s *spec.Screen) { s.Body.Children[0].Children[1].Props["field"] = "missing" }},
		{name: "stat records binding", rule: "kit/bind", path: "/body/children/0/children/1/bind", change: func(s *spec.Screen) { s.Body.Children[0].Children[1].Bind = &spec.Binding{Selection: "deals"} }},
		{name: "subtitle noun required", rule: "kit/parts", path: "/body/children/0/children/2/props/noun", change: func(s *spec.Screen) { delete(s.Body.Children[0].Children[2].Props, "noun") }},
		{name: "subtitle hint text", rule: "kit/parts", path: "/body/children/0/children/2/props/hint", change: func(s *spec.Screen) { s.Body.Children[0].Children[2].Props["hint"] = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files, sample := fixture(t)
			bundle, err := spec.Decode(files)
			if err != nil {
				t.Fatal(err)
			}
			if tc.change != nil {
				tc.change(bundle.Screens["pipeline"])
			}
			files["screens/pipeline.json"], err = json.Marshal(bundle.Screens["pipeline"])
			if err != nil {
				t.Fatal(err)
			}
			report, err := New(svc.Config{}, services(sample)).Run(context.Background(), "crm", files, 1)
			if err != nil {
				t.Fatal(err)
			}
			if tc.rule == "" {
				if !report.Pass {
					t.Fatalf("header rejected: %+v", report.Issues)
				}
				return
			}
			for _, issue := range report.Issues {
				if issue.File == "screens/pipeline.json" && issue.Rule == tc.rule && issue.Path == tc.path {
					return
				}
			}
			t.Fatalf("want %s at %s; got %+v", tc.rule, tc.path, report.Issues)
		})
	}
}
