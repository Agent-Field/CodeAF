package check

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func TestSchemaIssueHintsAreExact(t *testing.T) {
	tests := []struct {
		issue spec.Issue
		want  []string
	}{
		{spec.Issue{File: "screens/a.json", Path: "/body/props/gap", Rule: "schema", Message: `value must be one of "sm", "md", "lg"`},
			[]string{`screens/a.json:/body/props/gap`, `"sm", "md", "lg"`}},
		{spec.Issue{File: "app.json", Path: "", Rule: "schema", Message: `additionalProperties 'colour' not allowed`},
			[]string{"remove 'colour'", "allowed: ", "records"}},
		{spec.Issue{File: "plan.json", Path: "/data/0/line", Rule: "schema", Message: "length must be <= 200, but got 215"},
			[]string{"at most 200", "now 215"}},
		{spec.Issue{File: "agent.md", Rule: "missing", Message: "file is required"}, []string{"write agent.md"}},
		{spec.Issue{File: "app.json", Path: "/records", Rule: "schema", Message: "missing properties: 'title'"}, []string{"add 'title' to app.json:/records"}},
	}
	for _, tt := range tests {
		got := issueHint(tt.issue)
		for _, w := range tt.want {
			if !strings.Contains(got, w) {
				t.Errorf("hint %q lacks %q", got, w)
			}
		}
		if strings.Contains(got, "internal/apps/spec") {
			t.Errorf("hint names a repo path: %q", got)
		}
	}
}

func TestKitHintsListChoices(t *testing.T) {
	kit, err := spec.LoadKit()
	if err != nil {
		t.Fatal(err)
	}
	if got := partNamesHint(kit); !strings.Contains(got, "Button") || !strings.Contains(got, "RecordList") {
		t.Errorf("parts hint %q", got)
	}
	button := kit.Part("Button")
	if got := propNamesHint(button); !strings.Contains(got, "kind") || !strings.Contains(got, "size") {
		t.Errorf("props hint %q", got)
	}
	if got := propValueHint(kit, button, "kind", button.Props["kind"]); !strings.Contains(got, "primary, secondary") {
		t.Errorf("enum hint %q", got)
	}
	list := kit.Part("RecordList")
	if got := bindKindsHint(list); !strings.Contains(got, "records") {
		t.Errorf("bind hint %q", got)
	}
}
