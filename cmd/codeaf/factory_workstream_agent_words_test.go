package main

import (
	"regexp"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// machineryWords are the words no person may read from codeaf (CLAUDE.md's
// machinery-vocabulary law, and Workstreams v5's words rule). The server's
// agent says what its prompt says, so the prompt may not carry them either:
// on 2026-11-16 it told a person their workstream was "at stage shaping".
var machineryWords = regexp.MustCompile(`(?i)\b(auditor|verdict|verified|refuted|jobs?|stages?|waves?|go-aheads?|autonomy|proposals?|records?)\b`)

func TestServerAgentPromptKeepsMachineryOut(t *testing.T) {
	for _, work := range []bool{false, true} {
		prompt := factoryWorkstreamAgentSystem("Pantry", "priya", work)
		if hit := machineryWords.FindAllString(prompt, -1); len(hit) > 0 {
			t.Errorf("work=%v: the org workstream prompt says %q:\n%s", work, hit, prompt)
		}
		if !strings.Contains(prompt, "never by an id") {
			t.Errorf("work=%v: the prompt no longer tells the agent to keep ids out of its answers", work)
		}
	}
	if p := factoryWorkstreamAgentSystem("Pantry", "priya", true); !strings.Contains(p, "private to the one person") || !strings.Contains(p, "talking to priya") {
		t.Error("the prompt must say the conversation is private to the person it names (L1 F2)")
	}
	if !strings.Contains(factoryWorkstreamAgentSystem("Pantry", "priya", true), "ws_task_create") {
		t.Error("with the task tools, the prompt must send asked-for work to ws_task_create, not to a page")
	}
}

func TestWorkstreamReadNeverSaysTheStage(t *testing.T) {
	ws := factory.OrgWorkstream{ID: "ws_1", Name: "Pantry", Stage: "shaping",
		Pages:     []factory.WorkstreamPage{{ID: "pg_1", Title: "Overview", Kind: "page", BodyMD: "hello"}},
		Decisions: []factory.WorkstreamDecision{{ID: "dc_1", Text: "Ship it", State: "proposed", By: "priya"}}}
	got := session.WorkstreamAnswer(ws)
	if strings.Contains(got, "shaping") || machineryWords.MatchString(got) {
		t.Fatalf("read answers machinery words:\n%s", got)
	}
}
