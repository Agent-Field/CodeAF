package main

// WORK.GRAPH ON THE REAL BACKEND, THROUGH THE REAL WIRE. Two conversations are
// real folders under a real places root, each with the checkpoint the engine
// writes; a crew holds them; and a request line goes through appwire's server
// into appBackend and comes back as the bytes the app would read.

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/workgraph"
)

const (
	graphManager = "00000000000000c1"
	graphMember  = "00000000000000c2"
)

// checkpoint1 is the manager's tasks.json: task 1 landed, task 2 is a queued
// sub-task of it that waits (depends_on) on it.
const checkpoint1 = `{"type":"tasks","version":1,"seq":2,"nodes":[
 {"id":1,"title":"Design the schema","brief":"b","acceptance":"a","state":"done","merge":"merged",
  "changed":["db/schema.sql","/etc/hosts"],"admittedAt":"2026-10-01T09:00:00Z","startedAt":"2026-10-01T09:00:05Z",
  "endedAt":"2026-10-01T09:30:00Z","stateAt":"2026-10-01T09:30:00Z","costUsd":0.12,"input":1000,"output":200},
 {"id":2,"title":"Write migrations","brief":"b","acceptance":"a","state":"queued","depends_on":[1],"parent":1,"depth":2,
  "admittedAt":"2026-10-01T09:31:00Z","stateAt":"2026-10-01T09:31:00Z"}]}`

// checkpoint2 is the member's: its only task waits on the manager's task 1, a
// link recorded across conversations.
const checkpoint2 = `{"type":"tasks","version":1,"seq":1,"nodes":[
 {"id":1,"title":"Build the API","brief":"b","acceptance":"a","state":"queued",
  "waits_external":[{"session":"` + graphManager + `","task":1}],"admittedAt":"2026-10-01T09:05:00Z","stateAt":"2026-10-01T09:05:00Z"}]}`

type graphWire struct {
	t     *testing.T
	in    io.Writer
	lines *bufio.Scanner
	id    int
}

func (w *graphWire) call(params string) (result json.RawMessage, wireErr map[string]any) {
	w.t.Helper()
	w.id++
	line := `{"id":` + string(rune('0'+w.id)) + `,"method":"work.graph","params":` + params + "}\n"
	if _, err := io.WriteString(w.in, line); err != nil {
		w.t.Fatal(err)
	}
	if !w.lines.Scan() {
		w.t.Fatal("the wire ended")
	}
	var out struct {
		Result json.RawMessage `json:"result"`
		Error  map[string]any  `json:"error"`
	}
	if err := json.Unmarshal(w.lines.Bytes(), &out); err != nil {
		w.t.Fatalf("not JSON: %s", w.lines.Text())
	}
	return out.Result, out.Error
}

func (w *graphWire) graph(params string) workgraph.GraphResult {
	w.t.Helper()
	raw, bad := w.call(params)
	if bad != nil {
		w.t.Fatalf("work.graph %s: %v", params, bad)
	}
	var res workgraph.GraphResult
	if err := json.Unmarshal(raw, &res); err != nil {
		w.t.Fatal(err)
	}
	return res
}

func graphFixture(t *testing.T) (*appBackend, *graphWire, string) {
	t.Helper()
	t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), "state"))
	bucket, err := v3ProjectDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dirs := map[string]string{
		graphManager: writeV3Session(t, bucket, graphManager, "lead", time.Now()),
		graphMember:  writeV3Session(t, bucket, graphMember, "build", time.Now()),
	}
	for id, doc := range map[string]string{graphManager: checkpoint1, graphMember: checkpoint2} {
		if err := os.WriteFile(session.Place{Dir: dirs[id]}.Tasks(), []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	backend := newFakeBackend(newAppRecorder(), func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	})
	t.Cleanup(func() { backend.Close() })
	backend.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	backend.profileDir = t.TempDir()
	crew := teamstore.NewID()
	sub := teamstore.NewID()
	err = teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		f.Teams = append(f.Teams, teamstore.Team{ID: crew, Name: "harbor", Made: time.Now()}, teamstore.Team{ID: sub, Name: "api", Parent: crew, Made: time.Now()})
		for _, id := range []string{graphManager, graphMember} {
			transcript := session.Place{Dir: dirs[id]}.Transcript()
			member := teamstore.Member{Key: transcript, File: transcript, Where: "/w", Word: id, Handle: id}
			if err := f.AddMember(crew, member); err != nil {
				return err
			}
		}
		member := teamstore.Member{Key: session.Place{Dir: dirs[graphMember]}.Transcript(), File: session.Place{Dir: dirs[graphMember]}.Transcript(), Where: "/w", Word: "api", Handle: "api"}
		return f.AddMember(sub, member)
	})
	if err != nil {
		t.Fatal(err)
	}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	server := appwire.NewServer(outW)
	go func() {
		_ = server.Serve(context.Background(), inR, appwire.Hello{Version: "test"}, backend)
		outW.Close()
	}()
	scanner := bufio.NewScanner(outR)
	scanner.Buffer(make([]byte, 4<<20), 4<<20)
	t.Cleanup(func() { inW.Close() })
	if !scanner.Scan() {
		t.Fatal("no hello")
	}
	return backend, &graphWire{t: t, in: inW, lines: scanner}, crew
}

func TestWorkGraphOnTheRealBackendThroughTheWire(t *testing.T) {
	_, w, crew := graphFixture(t)

	// A CREW SCOPE reads every member's conversation: three tasks, the real
	// edges between them, and the true ownership of each.
	res := w.graph(`{"scope":{"kind":"crew","id":"` + crew + `"}}`)
	if res.Totals.Nodes != 3 || !res.Coverage.Complete || res.Coverage.Owners != 2 {
		t.Fatalf("crew totals = %+v coverage = %+v", res.Totals, res.Coverage)
	}
	edges := map[string]workgraph.GraphEdge{}
	for _, e := range res.Edges {
		edges[e.From+">"+e.To] = e
	}
	local := edges[graphManager+":2>"+graphManager+":1"]
	across := edges[graphMember+":1>"+graphManager+":1"]
	if local.Via != "record" || local.Target != "node" || across.Via != "external" || across.Target != "node" || across.ToState != "done" {
		t.Fatalf("edges = %+v", edges)
	}
	byID := map[string]workgraph.GraphNode{}
	for _, n := range res.Nodes {
		byID[n.ID] = n
	}
	landed := byID[graphManager+":1"]
	if landed.Landing == nil || landed.Landing.Outcome != "merged" || landed.Files.Total != 1 || landed.Files.Omitted != 1 || landed.Spend == nil || landed.Spend.Basis != "priced" {
		t.Fatalf("landed task = %+v", landed)
	}
	if n := byID[graphManager+":2"]; n.Parent != graphManager+":1" || n.ParentState != "node" || n.WaitsOnTotal != 1 {
		t.Fatalf("sub-task = %+v", n)
	}
	if n := byID[graphMember+":1"]; n.Crew == nil || len(n.Crew.Path) != 2 || n.Crew.Crew == crew {
		t.Fatalf("the member's task sits in the sub-crew: %+v", n.Crew)
	}

	// THE PEOPLE, THE CLUSTERS AND THE FIGURES come from the same snapshot.
	people := w.graph(`{"scope":{"kind":"crew","id":"` + crew + `"},"view":"people"}`)
	if len(people.Crews) != 2 || len(people.Members) != 3 {
		t.Fatalf("people = %d crews %d members", len(people.Crews), len(people.Members))
	}
	metrics := w.graph(`{"scope":{"kind":"crew","id":"` + crew + `"},"view":"metrics"}`).Metrics
	if metrics == nil || metrics.Tasks.Value != 3 || metrics.LandedTasks.Value != 1 || !metrics.Spend.Known || metrics.Spend.Value != 0.12 || metrics.Subtasks.Value != 1 {
		t.Fatalf("metrics = %+v", metrics)
	}

	// AN AGENT SCOPE resolves the same link as an external target with the
	// state the other conversation's file says.
	agent := w.graph(`{"scope":{"kind":"agent","id":"` + graphMember + `"}}`)
	if len(agent.Edges) != 1 || agent.Edges[0].Target != "external" || agent.Edges[0].ToState != "done" || agent.Nodes[0].ID != graphMember+":1" {
		t.Fatalf("agent graph = %+v", agent)
	}

	// A TASK SCOPE is that task's own sub-tasks.
	task := w.graph(`{"scope":{"kind":"task","id":"` + graphManager + `:1"}}`)
	if task.About.Root == nil || task.About.Root.ID != graphManager+":1" || len(task.Nodes) != 1 || task.Nodes[0].ID != graphManager+":2" {
		t.Fatalf("task graph = %+v", task)
	}
}

func TestWorkGraphUnchangedPagingAndRefusals(t *testing.T) {
	_, w, crew := graphFixture(t)
	scope := `{"kind":"crew","id":"` + crew + `"}`
	first := w.graph(`{"scope":` + scope + `,"limit":2}`)
	if len(first.Nodes) != 2 || first.Next == "" || first.Totals.Nodes != 3 {
		t.Fatalf("first page = %+v", first)
	}
	cursor, _ := json.Marshal(first.Next)
	if next := w.graph(`{"scope":` + scope + `,"limit":2,"cursor":` + string(cursor) + `}`); len(next.Nodes) != 1 || next.Next != "" {
		t.Fatalf("second page = %+v", next)
	}
	builds, _ := workGraphs.cache.Stats()
	if got := w.graph(`{"scope":` + scope + `,"if_revision":"` + first.Revision + `"}`); !got.Unchanged || len(got.Nodes) != 0 {
		t.Fatalf("an unmoved revision answered %+v", got)
	}
	if after, _ := workGraphs.cache.Stats(); after != builds {
		t.Fatal("an unchanged answer built a snapshot")
	}
	if _, bad := w.call(`{"scope":` + scope + `,"cursor":"tampered"}`); bad == nil || bad["code"] != appwire.CodeBadParams {
		t.Fatalf("tampered cursor = %v", bad)
	}
	if _, bad := w.call(`{"scope":{"kind":"crew","id":"nope"}}`); bad == nil || bad["code"] != appwire.CodeNotFound {
		t.Fatalf("unknown crew = %v", bad)
	}
	if _, bad := w.call(`{"scope":{"kind":"agent","id":"00000000000000ff"}}`); bad == nil || bad["code"] != appwire.CodeNotFound {
		t.Fatalf("unknown agent = %v", bad)
	}
	if _, bad := w.call(`{"scope":{"kind":"task","id":"../x:1"}}`); bad == nil || bad["code"] != appwire.CodeBadParams {
		t.Fatalf("unsafe task id = %v", bad)
	}
}
