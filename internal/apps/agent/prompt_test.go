package agent

import (
	"regexp"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func goldenBundle(t *testing.T) *spec.Bundle {
	t.Helper()
	files, _ := spec.GoldenCRM()
	b, err := spec.Decode(files)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

var aiWord = regexp.MustCompile(`\bAI\b`)

// The chat panel already shows the agent's answers as AI. Any line the
// runtime itself writes (protocol and tool help; field labels are the app's)
// that mentions AI must forbid marking, or the model opens every answer with
// "· AI —".
func TestRuntimePromptNeverAsksTheAgentToMarkItsText(t *testing.T) {
	text := protocol
	for _, tool := range []string{"records.query", "records.get", "records.create", "records.update", "records.delete", "runs.start", "gmail.send"} {
		text += "\n" + toolHelp(tool)
	}
	for _, line := range strings.Split(text, "\n") {
		if aiWord.MatchString(line) && !strings.Contains(strings.ToLower(line), "never") {
			t.Errorf("line asks for an AI mark: %q", line)
		}
	}
}

func TestRuntimePromptSetsHumanAnswerFormat(t *testing.T) {
	b := goldenBundle(t)
	for _, rt := range b.App.Records {
		for _, f := range rt.Fields {
			if f.Type == spec.FieldMoney || f.Type == spec.FieldDate {
				f.Label = "Due label"
				f.Currency = "EUR"
			}
		}
	}
	system := systemPrompt(b, []string{"records.query"})
	for _, want := range []string{"a table when you list several records", "never the raw key", "12 Oct 2026", "Never show ids, JSON or ISO timestamps", `"Due label"`, "(EUR)", `reply {"shape_this": true}`} {
		if !strings.Contains(system, want) {
			t.Errorf("system prompt lacks %q", want)
		}
	}
	if strings.Contains(system, "a company by its name") {
		t.Error("runtime prompt names another app's record types")
	}
}
