package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func goldenVerdict(t *testing.T, mutate func(spec.Files)) verdict {
	t.Helper()
	kit, err := spec.LoadKit()
	if err != nil {
		t.Fatal(err)
	}
	files, _ := spec.GoldenCRM()
	if mutate != nil {
		mutate(files)
	}
	return verify(files, kit)
}

func TestGoldenCRMPasses(t *testing.T) {
	v := goldenVerdict(t, nil)
	if !v.pass() {
		t.Fatalf("golden CRM has issues: %v", v.Issues)
	}
}

func TestBindingIssues(t *testing.T) {
	tests := []struct {
		name, path, from, to, want string
	}{
		{"unknown record type", "screens/contacts.json", `"records": "contact"`, `"records": "person"`, `unknown record type "person"`},
		{"unknown field", "screens/contacts.json", `"columns": ["name", "email", "company"]`, `"columns": ["name", "mail"]`, `no field "mail"`},
		{"unknown part", "screens/deal.json", `"part": "ApprovalCard"`, `"part": "Gauge"`, `"Gauge" is not in the kit`},
		{"unknown screen", "screens/deal.json", `"open": "pipeline"`, `"open": "board"`, `no such screen`},
		{"unknown ref target", "app.json", `"to": "contact"`, `"to": "person"`, `unknown record type "person"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := goldenVerdict(t, func(f spec.Files) {
				f[tt.path] = []byte(strings.Replace(string(f[tt.path]), tt.from, tt.to, 1))
			})
			if !strings.Contains(strings.Join(v.Issues, "\n"), tt.want) {
				t.Fatalf("issues %v lack %q", v.Issues, tt.want)
			}
		})
	}
}

func TestFirstObjectSkipsProse(t *testing.T) {
	obj, err := firstObject("here you go:\n```json\n{\"data\":[1]}\n```", "data")
	if err != nil || obj["data"] == nil {
		t.Fatalf("got %v, %v", obj, err)
	}
}
