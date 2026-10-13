package check

import (
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func TestAgentToolsFollowCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name   string
		access spec.Access
		tools  []string
		rule   string
	}{
		{"rw complete", spec.AccessReadWrite, []string{"records.query", "records.get", "records.create", "records.update", "records.delete"}, ""},
		{"rw missing delete", spec.AccessReadWrite, []string{"records.query", "records.get", "records.create", "records.update"}, "agent/records"},
		{"rw missing create", spec.AccessReadWrite, []string{"records.query", "records.get", "records.update", "records.delete"}, "agent/records"},
		{"read only", spec.AccessRead, []string{"records.query", "records.get"}, ""},
		{"read cannot write", spec.AccessRead, []string{"records.query", "records.get", "records.create"}, "agent/capability"},
		{"ungranted connector", spec.AccessRead, []string{"records.query", "records.get", "gmail.send"}, "agent/capability"},
		{"definition tool", spec.AccessRead, []string{"records.query", "records.get", "builder.write"}, "agent/capability"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &spec.Bundle{App: &spec.App{Records: map[string]*spec.RecordType{"bug": {}}, Capabilities: spec.Capabilities{Records: map[string]spec.Access{"bug": tc.access}}}, Agent: &spec.AgentDoc{Tools: tc.tools}}
			issues := agentCapabilities(b)
			if tc.rule == "" && len(issues) > 0 {
				t.Fatalf("issues: %v", issues)
			}
			if tc.rule != "" && !slices.ContainsFunc(issues, func(i Issue) bool { return i.Rule == tc.rule }) {
				t.Fatalf("missing %s: %v", tc.rule, issues)
			}
		})
	}
}
