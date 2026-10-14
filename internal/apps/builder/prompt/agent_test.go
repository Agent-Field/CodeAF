package prompt

import (
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"gopkg.in/yaml.v3"
)

// The day-one test questions live in front matter, which the runtime parser
// ignores, so the agent never treats them as instructions.
func TestExampleAgentKeepsTestQuestionsOutOfTheBody(t *testing.T) {
	raw, err := exampleFS.ReadFile("example/agent.md")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := spec.ParseAgent(raw)
	if err != nil {
		t.Fatal(err)
	}
	front := strings.SplitN(string(raw), "---\n", 3)[1]
	var meta struct {
		TestQuestions []string `yaml:"test_questions"`
	}
	if err := yaml.Unmarshal([]byte(front), &meta); err != nil {
		t.Fatal(err)
	}
	if len(meta.TestQuestions) != 2 {
		t.Fatalf("want 2 test_questions, got %v", meta.TestQuestions)
	}
	for _, q := range meta.TestQuestions {
		if strings.Contains(doc.Body, q) {
			t.Errorf("test question %q leaks into the body", q)
		}
	}
	for _, section := range []string{"## Jobs", "## How to answer", "## Never", "That changes the app", "Approval needed: send"} {
		if !strings.Contains(doc.Body, section) {
			t.Errorf("example body lacks %q", section)
		}
	}
	// The chat panel already shows answers as AI; a body that mentions the
	// label mark teaches the agent to open every answer with it.
	if strings.Contains(doc.Body, "· AI") {
		t.Error("example body must not tell the agent to mark answers \"· AI\"")
	}
	if lines := strings.Count(doc.Body, "\n") + 1; lines > agentBodyLines {
		t.Errorf("example body is %d lines, over %d", lines, agentBodyLines)
	}
}

func TestWriteRendersTheAgentJobOnlyForAgentCalls(t *testing.T) {
	in := fullInputs()
	got, err := Render(KindWrite, in)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"at most " + strconv.Itoa(agentBodyLines) + " lines", "test_questions", "least privilege", "Never mark answers"} {
		if !strings.Contains(got, want) {
			t.Errorf("WRITE lacks %q", want)
		}
	}
	in.Only = []string{"screens/deals.json"}
	got, err = Render(KindWrite, in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "test_questions") {
		t.Error("a screen-only WRITE must not carry the agent.md job")
	}
}
