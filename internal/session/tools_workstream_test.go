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
	reads   []string
}

func (f *fakeWorkstreamDoor) Workstream(context.Context) (factory.OrgWorkstream, error) {
	return f.ws, nil
}
func (f *fakeWorkstreamDoor) ReadPages(_ context.Context, pages []string) {
	f.reads = append(f.reads, pages...)
}
func (f *fakeWorkstreamDoor) WritePage(_ context.Context, page, title, body string) (string, error) {
	f.written = append(f.written, page+"|"+title+"|"+body)
	return "wrote it", nil
}
func (f *fakeWorkstreamDoor) Decide(_ context.Context, text string) (string, error) {
	f.decided = append(f.decided, text)
	return "proposed it", nil
}

// THE SERVER'S WORKSTREAM AGENT CARRIES ITS ONE TOOL AND NOTHING ELSE when no
// search pair is wired: no shell, no files, no loader that could put one back.
// And the manual names the tool, as it names every tool on every belt.
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

// THE SERVER'S WORKSTREAM AGENT CAN LOOK THINGS UP (finding #38): with a
// search pair wired, its belt carries web_search and web_fetch beside its
// workstream's tools, with a door that keeps tasks and files and one that does
// not, and gains nothing that writes, runs or reads the machine. Each half
// rides alone when only it is wired, and a page reaches the model through the
// fetcher it was handed.
func TestAWorkstreamAgentCarriesTheWebsReadingPair(t *testing.T) {
	names := func(agent *Agent) []string {
		agent.tools = agent.belt()
		out := []string{}
		for _, tool := range agent.offeredTools() {
			out = append(out, tool.Name)
		}
		return out
	}
	for _, door := range []WorkstreamDoor{&fakeWorkstreamDoor{}, &workingDoor{}} {
		fetch := &scriptedFetch{text: "Hall A seats 120. From $900 a day."}
		agent := &Agent{config: Config{Workstream: door, Workspace: t.TempDir(), ProfileDir: t.TempDir(), Interactive: true,
			SearchProvider: &scriptedSearch{}, SearchFetcher: fetch}}
		got := names(agent)
		if len(got) < 3 || got[0] != WorkstreamPagesTool || strings.Join(got[len(got)-2:], ",") != "web_search,web_fetch" {
			t.Fatalf("door %T: the server agent's belt is %v, want workstream_pages first and web_search,web_fetch last", door, got)
		}
		for _, name := range got {
			switch name {
			case "bash", "read", "write", "edit", "grep", "find", "ls", "load_capability", "propose_task", "change_setting":
				t.Fatalf("door %T: the server agent's belt gained %s: %v", door, name, got)
			}
		}
		// The page's address is in the person's words, so the leashed
		// web_fetch may read it (tools_web_leash.go).
		agent.keepPersonWordsLocked("Compare the halls, starting with https://venues.example/hall-a.")
		for _, tool := range agent.tools {
			if tool.Name != "web_fetch" {
				continue
			}
			out, failed, err := tool.Execute(context.Background(), json.RawMessage(`{"url":"https://venues.example/hall-a"}`))
			if err != nil || failed || !strings.Contains(out, "Hall A seats 120") {
				t.Fatalf("web_fetch answered %q failed=%v err=%v", out, failed, err)
			}
		}
		if strings.Join(fetch.fetched(), ",") != "https://venues.example/hall-a" {
			t.Fatalf("the fetcher was asked for %v", fetch.fetched())
		}
	}
	only := &Agent{config: Config{Workstream: &fakeWorkstreamDoor{}, Workspace: t.TempDir(), ProfileDir: t.TempDir(), Interactive: true,
		SearchFetcher: &scriptedFetch{}}}
	if got := names(only); strings.Join(got, ",") != WorkstreamPagesTool+",web_fetch" {
		t.Fatalf("a fetch-only pair gave the belt %v", got)
	}
}

func TestWorkstreamPagesReadsAndWritesThroughTheDoor(t *testing.T) {
	budget := 5.0
	door := &fakeWorkstreamDoor{ws: factory.OrgWorkstream{Name: "Webhook reliability", Stage: "building", BudgetUSD: &budget,
		Pages:     []factory.WorkstreamPage{{ID: "pg_1", Title: "Retries", Kind: "page", BodyMD: strings.Repeat("r", workstreamPageCut+10)}},
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
	if failed || !strings.Contains(out, "# Webhook reliability") || !strings.Contains(out, "Retries (page id pg_1, for your tools only)") ||
		!strings.Contains(out, "10 more bytes; ask page") || !strings.Contains(out, "- Use backoff. (kept, abir)") || !strings.Contains(out, "budget: $5.00") {
		t.Fatalf("read answered %q", out)
	}
	if out, failed = call(`{"ask":"page","page":"pg_1"}`); failed || !strings.HasSuffix(out, strings.Repeat("r", workstreamPageCut+10)) {
		t.Fatalf("page answered %q", out)
	}
	if _, failed = call(`{"ask":"page","page":"pg_9"}`); !failed {
		t.Fatal("an unknown page was read")
	}
	if strings.Join(door.reads, ";") != "pg_1;pg_1" {
		t.Fatalf("page reads were not recorded: %v", door.reads)
	}
	if _, failed = call(`{"ask":"write","body":"x"}`); !failed {
		t.Fatal("a write with no page was taken")
	}
	call(`{"ask":"write","page":"pg_1","body":"new words"}`)
	call(`{"ask":"new","title":"Runbook","body":"steps"}`)
	// A call admitted before the upgrade still runs; the verb is no longer offered (below).
	call(`{"ask":"decide","text":"Ship Friday."}`)
	if strings.Join(door.written, ";") != "pg_1||new words;|Runbook|steps" || strings.Join(door.decided, ";") != "Ship Friday." {
		t.Fatalf("door saw %v %v", door.written, door.decided)
	}
}

// solo-ios-app #4: a decision the agent proposed waited for a Keep no screen has, because Decisions shows only the
// decisions that recorded themselves. The tool no longer offers decide, and its words say there is nothing to keep.
func TestWorkstreamPagesOffersNoDecisionToKeep(t *testing.T) {
	schema := workstreamPagesSchemaJSON()
	if strings.Contains(schema, `"decide"`) || strings.Contains(schema, "With decide") {
		t.Fatalf("the schema still offers decide: %s", schema)
	}
	if strings.Contains(workstreamPagesDescription, "decide") || strings.Contains(workstreamPagesDescription, "a person keeps it") {
		t.Fatalf("the description still offers a decision to keep: %s", workstreamPagesDescription)
	}
	if !strings.Contains(workstreamPagesDescription, "Decisions are written down by themselves") {
		t.Fatalf("the description does not say how a decision is made: %s", workstreamPagesDescription)
	}
}
