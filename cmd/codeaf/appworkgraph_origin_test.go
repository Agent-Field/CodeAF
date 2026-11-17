package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/workgraph"
)

func TestMapOriginResolvesPhysicalLinesToEntries(t *testing.T) {
	// A note, a malformed line and an assistant are not user entry counters.
	record := session.ReadTranscriptBytes([]byte("{\"type\":\"message\",\"role\":\"user\",\"content\":\"first\",\"timestamp\":\"2026-10-20T13:40:00Z\"}\n" +
		"bad\n{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"answer\"}\n" +
		"{\"type\":\"message\",\"role\":\"user\",\"content\":\"second\"}\n"))
	conversation := graphConversationOf("a", record)
	entries := appEntriesOf("a", appRecordItems(record), time.Time{}, nil)
	for _, entry := range entries {
		if entry.Kind != "user" {
			continue
		}
		line := 1
		if entry.Text == "second" {
			line = 4
		}
		if got := conversation.lines[line]; got.source.Anchor != entry.ID {
			t.Fatalf("line %d: %+v != %+v", line, got, entry)
		}
	}
	if at := conversation.lines[1].source.At; at == nil || at.Format("15:04") != "13:40" {
		t.Fatalf("time: %v", at)
	}
}

func TestMapOriginWireAvailabilityAndBriefLevels(t *testing.T) {
	backend, _, crew := graphFixture(t)
	dir, _ := workGraphs.locate(graphManager)
	journal := session.Place{Dir: dir}.Transcript()
	message := `{"type":"message","role":"user","content":"the person's request","timestamp":"2026-10-20T13:40:00Z"}` + "\n"
	if err := os.WriteFile(journal, []byte(message), 0600); err != nil {
		t.Fatal(err)
	}
	checkpoint := strings.Replace(checkpoint1, `"id":1,`, `"id":1,"origin_journal":`+quoteGraph(journal)+`,"origin_line":1,`, 1)
	if err := os.WriteFile(session.Place{Dir: dir}.Tasks(), []byte(checkpoint), 0600); err != nil {
		t.Fatal(err)
	}
	graph := func(scope workgraph.GraphScope) *workgraph.GraphResult {
		t.Helper()
		got, err := backend.WorkGraph(context.Background(), workgraph.GraphRequest{Scope: scope})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	agent := graph(workgraph.GraphScope{Kind: workgraph.ScopeAgent, ID: graphManager})
	if b := agent.About.BriefFact; b.Source == nil || b.Source.Anchor != "u0" || b.Text != "the person's request" {
		t.Fatalf("agent brief: %+v", b)
	}
	taskScope := workgraph.GraphScope{Kind: workgraph.ScopeTask, ID: graphManager + ":1"}
	task := graph(taskScope)
	if b := task.About.BriefFact; b.Source == nil || b.Source.Agent != graphManager || b.Source.Anchor != "u0" || b.Text != "b" {
		t.Fatalf("task brief: %+v", b)
	}
	if agent.Nodes[0].OriginSource == nil || agent.Nodes[1].Origin != "none" {
		t.Fatalf("task origins: %+v", agent.Nodes)
	}
	// A goal edited outside chat has no pointer, even when the manager has spoken.
	backend.crewExtraOnCreate(crew, graphManager, "goal from settings", nil)
	if b := graph(workgraph.GraphScope{Kind: workgraph.ScopeCrew, ID: crew}).About.BriefFact; b.Source != nil || b.Origin != "none" || b.Text != "goal from settings" {
		t.Fatalf("crew: %+v", b)
	}
	if err := os.Remove(journal); err != nil {
		t.Fatal(err)
	}
	removed := graph(taskScope)
	if b := removed.About.BriefFact; b.Source != nil || b.Origin != "removed" || b.Text != "b" {
		t.Fatalf("removed: %+v", b)
	}
	if removed.Revision == task.Revision {
		t.Fatal("removing the origin did not change the revision")
	}
	reader := graphOriginReader{by: map[string]graphConversation{}}
	source, origin, err := reader.source(&workgraph.TextAnchor{Agent: graphManager, Line: 1, Machine: "other-machine"})
	if err != nil || source != nil || origin != "offline" {
		t.Fatalf("offline: %+v %s %v", source, origin, err)
	}
}

func quoteGraph(s string) string { b, _ := json.Marshal(s); return string(b) }

func TestMapOriginCrewUsesSubmittedIdentity(t *testing.T) {
	record := session.ReadTranscriptBytes([]byte(`{"type":"message","role":"user","content":"earlier"}` + "\n" +
		`{"type":"message","role":"user","content":"goal","presentation":{"audience":"person","client_id":"goal-client"}}` + "\n"))
	c := graphConversationOf("manager", record)
	reader := graphOriginReader{by: map[string]graphConversation{"manager": c}}
	brief, err := reader.brief(graphPlan{scope: workgraph.GraphScope{Kind: workgraph.ScopeCrew}, briefText: "goal", briefAgent: "manager", briefClient: "goal-client"})
	if err != nil || brief.Source == nil || brief.Source.Anchor != "u1" {
		t.Fatalf("crew origin: %+v %v", brief, err)
	}
}

func TestMapOriginExternalEndpoints(t *testing.T) {
	backend, _, _ := graphFixture(t)
	a, err := backend.find(graphManager)
	if err != nil {
		t.Fatal(err)
	}
	backend.locked(func() { a.view.Title = "Backend" })
	res := &workgraph.GraphResult{Edges: []workgraph.GraphEdge{
		{To: "a:1", ToRef: workgraph.TaskRef{Owner: "a", Task: "1"}, Target: "node"},
		{To: "a:2", ToRef: workgraph.TaskRef{Owner: "a", Task: "2"}, Target: "missing"},
		{To: graphManager + ":1", ToRef: workgraph.TaskRef{Owner: graphManager, Task: "1"}, Target: "external"},
	}}
	backend.graphExternal(res)
	if len(res.External) != 3 || res.External[0].Status != "paged" || res.External[1].Status != "removed" || res.External[2].Status != "outside" || res.External[2].Where == "" {
		t.Fatalf("external: %+v", res.External)
	}
}

func TestMapOriginCompactionUsesIdentityAndRetainsOnlyWantedLines(t *testing.T) {
	const raw = `{"type":"message","role":"user","content":"same words","presentation":{"audience":"person","client_id":"one"}}
{"type":"message","role":"assistant","content":"answer"}
{"type":"message","role":"user","content":"same words","presentation":{"audience":"person","client_id":"two"}}
{"type":"message","role":"assistant","content":"answer"}
{"type":"compaction","window":2,"stubbed":2}
{"type":"message","role":"user","content":"rewritten","presentation":{"audience":"person","client_id":"two"}}
{"type":"message","role":"assistant","content":"answer"}
{"type":"message","role":"user","content":"next"}
`
	record := session.ReadTranscriptBytes([]byte(raw))
	c := graphConversationSelect("a", record, map[int]bool{6: true, 8: true}, "")
	if c.lines[6].source.Anchor != "u1" || c.lines[8].source.Anchor != "u2" {
		t.Fatalf("compacted origins: %+v", c.lines)
	}
	if len(c.lines) != 2 {
		t.Fatalf("retained unrelated history: %+v", c.lines)
	}
}
