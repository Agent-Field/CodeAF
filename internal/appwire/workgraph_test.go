package appwire

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/Agent-Field/codeaf/internal/workgraph"
)

// graphEngine serves work.graph from a real snapshot built over three tasks, so
// the roundtrip below is the engine's own paging, cursor and bounds and not a
// canned answer.
type graphEngine struct {
	*fakeEngine
	snap *workgraph.Snapshot
	err  error
	ctx  context.Context
}

func (g *graphEngine) WorkGraph(ctx context.Context, req workgraph.GraphRequest) (*workgraph.GraphResult, error) {
	g.ctx = ctx
	if g.err != nil {
		return nil, g.err
	}
	return g.snap.Answer(ctx, req)
}

func buildGraph(t *testing.T) *workgraph.Snapshot {
	t.Helper()
	node := func(seq uint64) workgraph.Fact {
		ref := workgraph.TaskRef{Owner: "agent", Task: string(rune('0' + seq))}
		return workgraph.Fact{Node: workgraph.GraphNode{ID: ref.ID(), Ref: ref, Seq: seq, Kind: "task", Title: "t", State: workgraph.StateQueued, Links: "recorded"}}
	}
	second := node(2)
	second.Waits = []workgraph.Wait{{On: workgraph.TaskRef{Owner: "agent", Task: "1"}, Via: workgraph.ViaRecord}}
	snap, err := workgraph.Build(context.Background(), workgraph.Input{
		Scope: workgraph.GraphScope{Kind: workgraph.ScopeAgent, ID: "agent"}, Revision: "r1",
		Facts: []workgraph.Fact{node(1), second, node(3)}, Owners: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func startGraphWire(t *testing.T, engine Engine) *wire {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	served := make(chan error, 1)
	server := NewServer(outW)
	go func() {
		served <- server.Serve(context.Background(), inR, Hello{Version: "test"}, engine)
		outW.Close()
	}()
	scanner := bufio.NewScanner(outR)
	scanner.Buffer(make([]byte, 4<<20), 4<<20)
	t.Cleanup(func() { inW.Close() })
	return &wire{t: t, in: inW, lines: scanner, served: served}
}

// WORK.GRAPH IS ANNOUNCED BY AN ENGINE THAT SERVES IT, answers a real page with
// its edge and a cursor that continues, refuses a tampered cursor as bad_params,
// carries a deadline into the engine, and is unknown to an engine that cannot.
func TestWorkGraphRoundtrip(t *testing.T) {
	engine := &graphEngine{fakeEngine: &fakeEngine{}, snap: buildGraph(t)}
	w := startGraphWire(t, engine)
	found := false
	for _, f := range features(w.next()) {
		found = found || f == FeatureWorkGraph
	}
	if !found {
		t.Fatal("hello does not announce work.graph")
	}
	w.send(`{"id":1,"method":"work.graph","params":{"scope":{"kind":"agent","id":"agent"},"limit":2}}`)
	first := decodeGraph(t, w.next())
	if first.Contract != workgraph.Contract || len(first.Nodes) != 2 || first.Next == "" || first.Totals.Nodes != 3 || len(first.Edges) != 1 {
		t.Fatalf("first page = %+v", first)
	}
	if first.Edges[0].From != "agent:2" || first.Edges[0].To != "agent:1" {
		t.Fatalf("direction must be dependent -> prerequisite: %+v", first.Edges[0])
	}
	if dl, ok := engine.ctx.Deadline(); !ok || dl.IsZero() {
		t.Fatal("the engine was handed no deadline")
	}
	cursor, _ := json.Marshal(first.Next)
	w.send(`{"id":2,"method":"work.graph","params":{"scope":{"kind":"agent","id":"agent"},"limit":2,"cursor":` + string(cursor) + `}}`)
	if second := decodeGraph(t, w.next()); len(second.Nodes) != 1 || second.Next != "" {
		t.Fatalf("second page = %+v", second)
	}
	w.send(`{"id":3,"method":"work.graph","params":{"scope":{"kind":"agent","id":"agent"},"cursor":"tampered"}}`)
	if got := w.next(); got["error"].(map[string]any)["code"] != CodeBadParams {
		t.Fatalf("tampered cursor = %v", got)
	}
	w.send(`{"id":4,"method":"work.graph","params":{"scope":{"kind":"nowhere","id":"x"}}}`)
	if got := w.next(); got["error"].(map[string]any)["code"] != CodeBadParams {
		t.Fatalf("bad scope = %v", got)
	}
	w.send(`{"id":5,"method":"work.graph","params":{"scope":{"kind":"agent","id":"agent"},"view":"metrics","if_revision":"` + first.Revision + `"}}`)
	if got := decodeGraph(t, w.next()); !got.Unchanged || len(got.Nodes) != 0 {
		t.Fatalf("unchanged answer = %+v", got)
	}
}

func TestWorkGraphErrorsKeepTheirWireCode(t *testing.T) {
	for want, err := range map[string]error{
		CodeNotFound: workgraph.ErrNotFound,
		CodeEngine:   context.DeadlineExceeded,
		CodeBusy:     Errorf(CodeBusy, "busy"),
	} {
		engine := &graphEngine{fakeEngine: &fakeEngine{}, err: err}
		_, got := methods["work.graph"].call(engine, json.RawMessage(`{"scope":{"kind":"agent","id":"a"}}`))
		if w, ok := got.(*Error); !ok || w.Code != want {
			t.Errorf("%v became %v, want %s", err, got, want)
		}
	}
	if _, err := methods["work.graph"].call(&fakeEngine{}, json.RawMessage(`{"scope":{"kind":"agent","id":"a"}}`)); err == nil || err.(*Error).Code != CodeUnknownMethod {
		t.Fatalf("an engine that does not serve it answered %v", err)
	}
}

func decodeGraph(t *testing.T, line map[string]any) workgraph.GraphResult {
	t.Helper()
	if line["error"] != nil {
		t.Fatalf("error: %v", line["error"])
	}
	raw, _ := json.Marshal(line["result"])
	var out workgraph.GraphResult
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
