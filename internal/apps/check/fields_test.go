package check

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestScreenQueryFieldReferences(t *testing.T) {
	for _, tc := range []struct {
		name, query, sort, group string
		hidden, pass             bool
	}{
		{name: "Reading list unknown field", query: "highly:true"},
		{name: "sort token", query: "stage:Proposal sort:-highly"},
		{name: "has filter", query: "has:highly"},
		{name: "is filter", query: "is:highly"},
		{name: "comparison filter", query: "highly>1"},
		{name: "Table sort", sort: "-highly"},
		{name: "Board group", group: "highly"},
		{name: "hidden screen binding", query: "highly:true", hidden: true},
		{name: "template field", query: "highly:{{params.status}}"},
		{name: "valid fields", query: "stage:-Lost sort:-amount", pass: true},
		{name: "metadata field", query: "id:d1", pass: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files, sample := fixture(t)
			var screen spec.Screen
			if err := json.Unmarshal(files["screens/pipeline.json"], &screen); err != nil {
				t.Fatal(err)
			}
			screen.Walk(func(n *spec.Node) {
				if n.Bind != nil && n.Bind.Records == "deal" {
					n.Bind.Query = tc.query
					if tc.hidden {
						n.When = "false"
					}
				}
			})
			if tc.sort != "" {
				screen.Body.Children = append(screen.Body.Children, &spec.Node{Part: "Table", Label: "Deals", Bind: &spec.Binding{Records: "deal"}, Props: map[string]any{"columns": []string{"name"}, "sort": tc.sort}})
			}
			if tc.group != "" {
				screen.Body.Children = append(screen.Body.Children, &spec.Node{Part: "Board", Label: "Deals", Bind: &spec.Binding{Records: "deal"}, Props: map[string]any{"groupBy": tc.group}})
			}
			raw, _ := json.Marshal(screen)
			files["screens/pipeline.json"] = raw
			report, err := New(svc.Config{}, services(sample)).Run(context.Background(), "crm", files, 1)
			if err != nil || report.Pass != tc.pass {
				t.Fatalf("report %+v, err %v", report, err)
			}
			if tc.pass {
				return
			}
			for _, issue := range report.Issues {
				if issue.File == "screens/pipeline.json" && strings.Contains(issue.Message, "highly") && strings.Contains(issue.Message, "fix:") && strings.Contains(issue.Message, "amount") && strings.Contains(issue.Message, "name") {
					return
				}
			}
			t.Fatalf("missing screen, field and valid-fields fix hint: %+v", report.Issues)
		})
	}
}

func TestRunAndTriggerParamFieldReferences(t *testing.T) {
	for _, tc := range []struct{ name, file, old, next, bad string }{
		{"condition param", "runs/follow-up.yaml", "- id: each", "- id: each\n    if: '$params.highly == true'", "highly"},
		{"unknown param", "runs/follow-up.yaml", "$params.deals", "$params.highly", "highly"},
		{"param field", "runs/follow-up.yaml", "$params.deals", "$params.deals.highly", "highly"},
		{"param template field", "runs/follow-up.yaml", "id:{{deal}}", "id:{{params.deals.highly}}", "highly"},
		{"param target", "runs/follow-up.yaml", "to: deal", "to: highly", "highly"},
		{"templated query field", "runs/follow-up.yaml", "id:{{deal}}", "highly:{{deal}}", "highly"},
		{"trigger param field", "app.json", "$record.id", "$record.highly", "highly"},
		{"trigger fields field", "app.json", "$record.id", "$record.fields.highly", "highly"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files, sample := fixture(t)
			change(t, files, tc.file, tc.old, tc.next)
			report, err := New(svc.Config{}, services(sample)).Run(context.Background(), "crm", files, 1)
			if err != nil || report.Pass {
				t.Fatalf("report %+v err %v", report, err)
			}
			for _, issue := range report.Issues {
				if issue.File == tc.file && strings.Contains(issue.Message, tc.bad) && strings.Contains(issue.Message, "fix:") {
					return
				}
			}
			t.Fatalf("missing field hint %+v", report.Issues)
		})
	}
}
