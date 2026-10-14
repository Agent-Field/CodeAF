package check

import (
	"slices"
	"strings"
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

func TestAgentToolHintsNameTheExactFix(t *testing.T) {
	b := &spec.Bundle{
		App: &spec.App{
			Records: map[string]*spec.RecordType{"bug": {}},
			Capabilities: spec.Capabilities{
				Records:    map[string]spec.Access{"bug": spec.AccessRead},
				Connectors: map[string]spec.Connector{"gmail": {Can: []string{"draft"}}},
			},
		},
		Agent: &spec.AgentDoc{Tools: []string{"records.query", "records.create", "gmail.send", "runs.start", "builder.write"}},
	}
	hints := map[string]string{}
	for _, issue := range agentCapabilities(b) {
		hints[issue.Path] = issue.FixHint
	}
	for path, want := range map[string]string{
		"/tools/1": "make the record type rw",
		"/tools/2": `add "send" to app.json capabilities.connectors.gmail.can`,
		"/tools/3": "write the runs/<id>.yaml",
		"/tools/4": "never from tools that change the app",
		"/tools":   "add records.get to agent.md tools",
	} {
		if !strings.Contains(hints[path], want) {
			t.Errorf("%s hint %q lacks %q", path, hints[path], want)
		}
	}
	if !strings.HasPrefix(hints["/tools/1"], "remove records.create from agent.md tools") {
		t.Errorf("hint must say what to remove: %q", hints["/tools/1"])
	}
}

func TestAgentBodyMustNotMarkAnswers(t *testing.T) {
	for _, tc := range []struct {
		body string
		flag bool
	}{
		{"- Mark anything you rank or guess with \"· AI\". A person's edit always wins.", true},
		{"Start each answer with · AI.", true},
		{"- Never mark answers \"· AI\"; the panel already shows them as AI.", false},
		{"- Mark answers as AI; the panel does.", false},
	} {
		b := &spec.Bundle{App: &spec.App{}, Agent: &spec.AgentDoc{Body: "You help.\n" + tc.body}}
		got := slices.ContainsFunc(agentCapabilities(b), func(i Issue) bool { return i.Rule == "agent/answer-mark" })
		if got != tc.flag {
			t.Errorf("%q: flagged=%v, want %v", tc.body, got, tc.flag)
		}
	}
}
