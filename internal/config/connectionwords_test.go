package config

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/modelsource"
)

func TestCodexConnectionWordKeepsIncompleteAccountDetailsEmpty(t *testing.T) {
	connected := modelsource.Outcome{Kind: modelsource.OutcomeConnected, Refreshed: true}
	for _, testCase := range []struct {
		email string
		plan  string
		want  string
	}{
		{email: "person@example.com", plan: "pro", want: "codex connected · person@example.com · pro plan"},
		{email: "person@example.com", want: "codex connected"},
		{plan: "pro", want: "codex connected"},
		{want: "codex connected"},
	} {
		if got := CodexConnectionWord(testCase.email, testCase.plan, connected); got != testCase.want {
			t.Errorf("CodexConnectionWord(%q, %q) = %q, want %q", testCase.email, testCase.plan, got, testCase.want)
		}
	}
}
