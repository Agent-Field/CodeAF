package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/manual"
)

type fakeWorkstreamDoor struct {
	ws      factory.OrgWorkstream
	written []string
	decided []string
}

func (f *fakeWorkstreamDoor) Workstream(context.Context) (factory.OrgWorkstream, error) { return f.ws, nil }
func (f *fakeWorkstreamDoor) WritePage(_ context.Context, page, title, body string) (string, error) {
	f.written = append(f.written, page+"|"+title+"|"+body)
	return "wrote it", nil
}
func (f *fakeWorkstreamDoor) Decide(_ context.Context, text string) (string, error) {
	f.decided = append(f.decided, text)
	return "proposed it", nil
}

// THE SERVER'S WORKSTREAM AGENT CARRIES ITS ONE TOOL AND NOTHING ELSE: no
// shell, no files, no loader that could put one back. And the manual names
// the tool, as it names every tool on every belt.
func TestAWorkstreamAgentCarriesOnlyItsWorkstream(t *testing.T) {
	door := &fakeWorkstreamDoor{}
	agent := &Agent{config: Config{Workstream: door, Workspace: t.TempDir(), ProfileDir: t.TempDir(), Interactive: true}}
	agent.tools = agent.belt()
	offered := agent.offeredTools()
	if len(offered) != 1 || offered[0].Name != WorkstreamPagesTool {
		names := []string{}
		for _, tool := range offered {
			names = append(names, tool.Name)
		}
		t.Fatalf("the server agent's belt is %v", names)
	}
	if !manual.Chat().Mentions(WorkstreamPagesTool) {
		t.Errorf("no chat manual page mentions the %s tool — add it to internal/manual/chat/", WorkstreamPagesTool)
	}
}

func TestWorkstreamPagesReadsAndWritesThroughTheDoor(t *testing.T) {
	budget := 5.0
	door := &fakeWorkstreamDoor{ws: factory.OrgWorkstream{Name: "Webhook reliability", Stage: "building", BudgetUSD: &budget,
		Pages: []factory.WorkstreamPage{{ID: "pg_1", Title: "Retries", Kind: "page", BodyMD: strings.Repeat("r", workstreamPageCut+10)}},
		Decisions: []factory.WorkstreamDecision{{ID: "dc_1", Text: "Use backoff.", State: "kept", By: "abir"}}}}
	tool := (&Agent{config: Config{Workstream: door}}).workstreamTool()
	call := func(args string) (string, bool) {
		t.Helper()
		out, failed, err := tool.Execute(context.Background(), json.RawMessage(args))
		if err != nil {
			t.Fatal(err)
		}
		return out, failed
	}
	out, failed := call(`{"ask":"read"}`)
	if failed || !strings.Contains(out, "# Webhook reliability") || !strings.Contains(out, "Retries (pg_1, page)") ||
		!strings.Contains(out, "10 more bytes; ask page") || !strings.Contains(out, "[kept] Use backoff. (abir)") || !strings.Contains(out, "budget: $5.00") {
		t.Fatalf("read answered %q", out)
	}
	if out, failed = call(`{"ask":"page","page":"pg_1"}`); failed || !strings.HasSuffix(out, strings.Repeat("r", workstreamPageCut+10)) {
		t.Fatalf("page answered %q", out)
	}
	if _, failed = call(`{"ask":"page","page":"pg_9"}`); !failed {
		t.Fatal("an unknown page was read")
	}
	if _, failed = call(`{"ask":"write","body":"x"}`); !failed {
		t.Fatal("a write with no page was taken")
	}
	call(`{"ask":"write","page":"pg_1","body":"new words"}`)
	call(`{"ask":"new","title":"Runbook","body":"steps"}`)
	call(`{"ask":"decide","text":"Ship Friday."}`)
	if strings.Join(door.written, ";") != "pg_1||new words;|Runbook|steps" || strings.Join(door.decided, ";") != "Ship Friday." {
		t.Fatalf("door saw %v %v", door.written, door.decided)
	}
}
