package spec

import (
	"strings"
	"testing"
)

func TestRunReferenceSchema(t *testing.T) {
	for _, tc := range []struct {
		name, steps string
		valid       bool
	}{
		{"param template", `[{"id":"load","records.query":{"type":"bug","query":"id:{{params.bug}}"}}]`, true},
		{"bare param template", `[{"id":"load","records.query":{"type":"bug","query":"id:{{bug}}"}}]`, false},
		{"bare typed param", `[{"id":"save","records.update":{"type":"bug","id":"$bug","set":{"status":"new"}}}]`, false},
		{"undeclared param", `[{"id":"save","records.update":{"type":"bug","id":"$params.missing","set":{}}}]`, false},
		{"loop variable", `[{"id":"each","loop":{"over":"$params.bug","as":"bug","max":2},"steps":[{"id":"load","records.query":{"type":"bug","query":"id:{{bug}}"}}]}]`, true},
		{"escaped loop scope", `[{"id":"load","records.query":{"type":"bug","query":"id:{{bug}}"}}]`, false},
		{"malformed template", `[{"id":"load","records.query":{"type":"bug","query":"id:{{params.bug"}}]`, false},
		{"gate and action", `[{"id":"save","gate":{"approval":"start","who":"$me"},"records.update":{"type":"bug","id":"$params.bug","set":{"status":"won't fix"}}}]`, true},
		{"standalone gate", `[{"id":"approve","gate":{"approval":"start"}}]`, false},
		{"two actions", `[{"id":"load","wait":{"timeout":"1s"},"records.query":{"type":"bug"}}]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(`{"run":"triage","params":{"bug":{"type":"ref","to":"bug"}},"steps":` + tc.steps + `}`)
			issues := validateFile("runs/triage.yaml", data)
			if (len(issues) == 0) != tc.valid {
				t.Fatalf("issues: %v", issues)
			}
			for _, issue := range issues {
				if issue.Rule == "schema/run-reference" && !strings.Contains(issue.Message, "{{params.<name>}}") {
					t.Fatalf("no namespace repair instruction: %v", issue)
				}
			}
		})
	}
}
