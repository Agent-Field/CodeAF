package appwire

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"
)

// fakeCollector is the fake engine with collections: it answers fixed values
// and records what it was asked.
type fakeCollector struct {
	*fakeEngine
	depth, limit int
	after        string
	added        []Member
	from, to     Ref
}

func (f *fakeCollector) ListCollections(holds *Ref, after string, limit int) (CollectionsResult, error) {
	f.after, f.limit = after, limit
	if after == "bogus" {
		return CollectionsResult{}, Errorf(CodeBadParams, "that page cursor was not given by this listing")
	}
	at := time.UnixMilli(1000).UTC()
	yes := holds != nil
	c := Collection{ID: "c1", Name: "Launch", CreatedAt: &at, Count: 2, LastAddedAt: &at}
	if holds != nil {
		c.Holds = &yes
	}
	return CollectionsResult{Collections: []Collection{c}, Next: "n1", Total: 3}, nil
}
func (f *fakeCollector) CreateCollection(name string) (Collection, error) {
	if name == "Taken" {
		return Collection{}, Errorf(CodeConflict, "another collection already has that name")
	}
	return Collection{ID: "c2", Name: name}, nil
}
func (f *fakeCollector) RenameCollection(id, name string) (Collection, error) {
	return Collection{ID: id, Name: name}, nil
}
func (f *fakeCollector) DuplicateCollection(id, name string) (Collection, error) {
	return Collection{ID: "c3", Name: name, Count: 2}, nil
}
func (f *fakeCollector) DeleteCollection(id string) error {
	if id != "c1" {
		return Errorf(CodeNotFound, "collection not found")
	}
	return nil
}
func (f *fakeCollector) AddToCollection(id string, refs []Member) (CollectionAddResult, error) {
	for _, r := range refs {
		if r.Kind == RefCollection && r.ID == id {
			return CollectionAddResult{}, Errorf(CodeCycle, "collection membership would form a cycle")
		}
	}
	f.added = refs
	return CollectionAddResult{Added: refs}, nil
}
func (f *fakeCollector) RemoveFromCollection(id string, refs []Ref) (CollectionRemoveResult, error) {
	return CollectionRemoveResult{}, nil
}
func (f *fakeCollector) OpenCollection(id string, depth, limit int, after string) (CollectionOpenResult, error) {
	f.depth, f.limit, f.after = depth, limit, after
	return CollectionOpenResult{Collection: Collection{ID: id, Name: "Launch"}, More: true, Next: "o1"}, nil
}
func (f *fakeCollector) ExpandCollection(id string, depth, limit int) (CollectionExpandResult, error) {
	f.depth, f.limit = depth, limit
	return CollectionExpandResult{Collection: Collection{ID: id, Name: "Launch"}, Unavailable: 1, Unread: 2,
		UnavailableRefs: []ExpandedRef{{Member: Member{Ref: Ref{Kind: RefCrew, ID: "k"}}, Target: Target{State: TargetUnknown, Reason: "the crew list could not be asked: busy"}}}}, nil
}
func (f *fakeCollector) RelinkCollection(id string, from, to Ref) (CollectionRelinkResult, error) {
	f.from, f.to = from, to
	if from.ID == "absent" {
		return CollectionRelinkResult{}, Errorf(CodeNotFound, "that reference is not in this collection")
	}
	at := time.UnixMilli(2000).UTC()
	return CollectionRelinkResult{Member: Member{Ref: to, AddedAt: &at}}, nil
}

func startCollectorWire(t *testing.T) (*wire, *fakeCollector) {
	t.Helper()
	engine := &fakeCollector{fakeEngine: &fakeEngine{}}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	served := make(chan error, 1)
	server := NewServer(outW)
	go func() {
		served <- server.Serve(context.Background(), inR, Hello{Version: "test", PID: 42, Home: "/h", Features: []string{"x"}}, engine)
		outW.Close()
	}()
	scanner := bufio.NewScanner(outR)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	t.Cleanup(func() { inW.Close() })
	return &wire{t: t, in: inW, lines: scanner, served: served, engine: engine.fakeEngine}, engine
}

func features(hello map[string]any) []any {
	f, _ := hello["data"].(map[string]any)["features"].([]any)
	return f
}

// THE FEATURE IS CARRIED BY AN ENGINE THAT SERVES IT, and only by one: an
// engine without collections answers its methods unknown_method.
func TestCollectionsFeatureFollowsTheEngine(t *testing.T) {
	w, _ := startCollectorWire(t)
	if got := features(w.next()); len(got) != 3 || got[0] != "x" || got[1] != FeatureAgentEffortRungs || got[2] != FeatureCollections {
		t.Fatalf("features = %v", got)
	}
	plain := startWire(t, &fakeEngine{})
	for _, f := range features(plain.next()) {
		if f == FeatureCollections {
			t.Fatal("an engine without collections claims them")
		}
	}
	plain.send(`{"id":1,"method":"collections.list"}`)
	if got := plain.next(); got["error"].(map[string]any)["code"] != CodeUnknownMethod {
		t.Fatalf("collections.list without the engine half = %v", got)
	}
}

// EVERY SHAPE AND BOUND IS CHECKED HERE before the engine is asked.
func TestCollectionsRefuseBadParams(t *testing.T) {
	w, _ := startCollectorWire(t)
	w.next()
	many := `[`
	for i := 0; i < CollectionRefsMax+1; i++ {
		if i > 0 {
			many += ","
		}
		many += `{"kind":"conversation","id":"c"}`
	}
	many += `]`
	for i, line := range []string{
		`{"id":1,"method":"collections.create","params":{}}`,
		`{"id":2,"method":"collections.create","params":{"name":"a\u0007b"}}`,
		`{"id":3,"method":"collections.rename","params":{"name":"x"}}`,
		`{"id":4,"method":"collections.add","params":{"id":"c1","refs":[]}}`,
		`{"id":5,"method":"collections.add","params":{"id":"c1","refs":` + many + `}}`,
		`{"id":6,"method":"collections.add","params":{"id":"c1","refs":[{"kind":"task","id":"1"}]}}`,
		`{"id":7,"method":"collections.add","params":{"id":"c1","refs":[{"kind":"task","id":"01","session_id":"a"}]}}`,
		`{"id":8,"method":"collections.add","params":{"id":"c1","refs":[{"kind":"artifact","id":"rel/path"}]}}`,
		`{"id":9,"method":"collections.add","params":{"id":"c1","refs":[{"kind":"standing","id":"s"}]}}`,
		`{"id":10,"method":"collections.add","params":{"id":"c1","refs":[{"kind":"crew","id":"k","session_id":"a"}]}}`,
		`{"id":11,"method":"collections.add","params":{"id":"c1","refs":[{"kind":"process","id":"p"}]}}`,
		`{"id":12,"method":"collections.add","params":{"id":"c1","refs":[{"kind":"memory","id":"m"}]}}`,
		`{"id":13,"method":"collections.remove","params":{"id":"c1"}}`,
		`{"id":14,"method":"collections.open","params":{"id":"c1","depth":3}}`,
		`{"id":15,"method":"collections.open","params":{"id":"c1","limit":0}}`,
		`{"id":16,"method":"collections.open","params":{"id":"c1","limit":501}}`,
		`{"id":17,"method":"collections.expand","params":{"id":"c1","depth":0}}`,
		`{"id":18,"method":"collections.expand","params":{"id":"c1","depth":4}}`,
		`{"id":19,"method":"collections.expand","params":{"id":"c1","limit":501}}`,
		`{"id":20,"method":"collections.list","params":{"holds":{"kind":"task","id":"x","session_id":"a"}}}`,
		`{"id":21,"method":"collections.delete","params":{}}`,
		`{"id":22,"method":"collections.add","params":{"id":"c1","refs":[{"kind":"conversation","id":"a","added_at":"1960-01-01T00:00:00Z"}]}}`,
	} {
		w.send(line)
		got := w.next()
		if got["id"] != float64(i+1) || got["error"] == nil || got["error"].(map[string]any)["code"] != CodeBadParams {
			t.Fatalf("%s = %v", line, got)
		}
	}
}

// THE ANSWERS ARE THE SHAPES THE PROTOCOL STATES, the defaults are applied
// before the engine is asked, and engine refusals keep their codes.
func TestCollectionsAnswerTheirShapes(t *testing.T) {
	w, engine := startCollectorWire(t)
	w.next()
	call := func(id int, line string) map[string]any {
		t.Helper()
		w.send(line)
		got := w.next()
		if got["id"] != float64(id) {
			t.Fatalf("%s = %v", line, got)
		}
		return got
	}
	got := call(1, `{"id":1,"method":"collections.list","params":{"holds":{"kind":"conversation","id":"a1"}}}`)
	list := got["result"].(map[string]any)["collections"].([]any)
	if c := list[0].(map[string]any); c["holds"] != true || c["count"] != float64(2) || c["created_at"] != "1970-01-01T00:00:01Z" {
		t.Fatalf("list = %v", got)
	}
	if got := call(2, `{"id":2,"method":"collections.create","params":{"name":"  Launch  "}}`); got["result"].(map[string]any)["collection"].(map[string]any)["name"] != "Launch" {
		t.Fatalf("create = %v", got)
	}
	if got := call(3, `{"id":3,"method":"collections.create","params":{"name":"Taken"}}`); got["error"].(map[string]any)["code"] != CodeConflict {
		t.Fatalf("taken = %v", got)
	}
	if got := call(4, `{"id":4,"method":"collections.add","params":{"id":"c1","refs":[{"kind":"collection","id":"c1"}]}}`); got["error"].(map[string]any)["code"] != CodeCycle {
		t.Fatalf("cycle = %v", got)
	}
	got = call(5, `{"id":5,"method":"collections.add","params":{"id":"c1","refs":[{"kind":"process","id":"a1:p1","session_id":"a1","added_at":"2026-10-15T10:00:00Z"}]}}`)
	result := got["result"].(map[string]any)
	if already, ok := result["already"].([]any); !ok || len(already) != 0 || len(engine.added) != 1 || engine.added[0].AddedAt == nil {
		t.Fatalf("add = %v, engine was given %+v", got, engine.added)
	}
	if got := call(6, `{"id":6,"method":"collections.remove","params":{"id":"c1","refs":[{"kind":"standing","id":"s"}]}}`); got["result"].(map[string]any)["removed"] == nil {
		t.Fatalf("remove = %v", got)
	}
	call(7, `{"id":7,"method":"collections.open","params":{"id":"c1"}}`)
	if engine.depth != 1 || engine.limit != CollectionOpenLimit {
		t.Fatalf("open defaults %d %d", engine.depth, engine.limit)
	}
	got = call(8, `{"id":8,"method":"collections.expand","params":{"id":"c1"}}`)
	if engine.depth != CollectionExpandMax || engine.limit != CollectionExpandDefault {
		t.Fatalf("expand defaults %d %d", engine.depth, engine.limit)
	}
	if refs, ok := got["result"].(map[string]any)["refs"].([]any); !ok || refs == nil {
		t.Fatalf("expand = %v", got)
	}
	if got := call(9, `{"id":9,"method":"collections.delete","params":{"id":"zz"}}`); got["error"].(map[string]any)["code"] != CodeNotFound {
		t.Fatalf("delete = %v", got)
	}
}

// The schema names this wave's methods and its event, and the target's state
// is one of its four words.
func TestTheSchemaDescribesCollections(t *testing.T) {
	out, err := SchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Methods map[string]json.RawMessage `json:"methods"`
		Events  map[string]json.RawMessage `json:"events"`
		Defs    map[string]struct {
			Properties map[string]map[string]any `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"collections.list", "collections.create", "collections.rename", "collections.duplicate", "collections.delete", "collections.add", "collections.remove", "collections.open", "collections.expand"} {
		if doc.Methods[name] == nil {
			t.Errorf("schema lacks %s", name)
		}
	}
	if doc.Events[EventCollectionChanged] == nil {
		t.Error("schema lacks collection.changed")
	}
	if words, _ := doc.Defs["Target"].Properties["state"]["enum"].([]any); len(words) != 4 {
		t.Errorf("target.state words = %v", words)
	}
}

// PAGING, HONEST EXPANSION AND RELINK ARE SHAPES THE WIRE CHECKS: a cursor
// and a list limit are bounded here, relink needs both refs and refuses
// adding the legacy kind, and every new field arrives as PROTOCOL.md says.
func TestCollectionsPagingExpandAndRelinkOnTheWire(t *testing.T) {
	w, engine := startCollectorWire(t)
	w.next()
	call := func(id int, line string) map[string]any {
		t.Helper()
		w.send(line)
		got := w.next()
		if got["id"] != float64(id) {
			t.Fatalf("%s = %v", line, got)
		}
		return got
	}
	code := func(got map[string]any) any {
		if e, ok := got["error"].(map[string]any); ok {
			return e["code"]
		}
		return nil
	}
	for i, line := range []string{
		`{"id":1,"method":"collections.list","params":{"limit":0}}`,
		`{"id":2,"method":"collections.list","params":{"limit":1001}}`,
		`{"id":3,"method":"collections.list","params":{"after":"has space"}}`,
		`{"id":4,"method":"collections.open","params":{"id":"c1","after":"a\u0007"}}`,
		`{"id":5,"method":"collections.relink","params":{"id":"c1","from":{"kind":"conversation","id":"a"}}}`,
		`{"id":6,"method":"collections.relink","params":{"id":"c1","to":{"kind":"conversation","id":"a"}}}`,
		`{"id":7,"method":"collections.relink","params":{"from":{"kind":"conversation","id":"a"},"to":{"kind":"conversation","id":"b"}}}`,
		`{"id":8,"method":"collections.relink","params":{"id":"c1","from":{"kind":"conversation","id":"a"},"to":{"kind":"standing","id":"s"}}}`,
		`{"id":9,"method":"collections.relink","params":{"id":"c1","from":{"kind":"conversation","id":"a"},"to":{"kind":"artifact","id":"rel"}}}`,
	} {
		if got := call(i+1, line); code(got) != CodeBadParams {
			t.Fatalf("%s = %v", line, got)
		}
	}
	got := call(10, `{"id":10,"method":"collections.list","params":{"after":"c0","limit":2}}`)
	result := got["result"].(map[string]any)
	if result["next"] != "n1" || result["total"] != float64(3) || engine.after != "c0" || engine.limit != 2 {
		t.Fatalf("list page = %v (engine after %q limit %d)", got, engine.after, engine.limit)
	}
	call(11, `{"id":11,"method":"collections.list"}`)
	if engine.after != "" || engine.limit != CollectionsListMax {
		t.Fatalf("list defaults after %q limit %d", engine.after, engine.limit)
	}
	if got := call(12, `{"id":12,"method":"collections.list","params":{"after":"bogus"}}`); code(got) != CodeBadParams {
		t.Fatalf("a foreign cursor = %v", got)
	}
	got = call(13, `{"id":13,"method":"collections.open","params":{"id":"c1","after":"o0"}}`)
	if r := got["result"].(map[string]any); r["next"] != "o1" || r["more"] != true || engine.after != "o0" {
		t.Fatalf("open page = %v", got)
	}
	got = call(14, `{"id":14,"method":"collections.expand","params":{"id":"c1"}}`)
	r := got["result"].(map[string]any)
	unavailable, _ := r["unavailable_refs"].([]any)
	if r["unavailable"] != float64(1) || r["unread"] != float64(2) || len(unavailable) != 1 {
		t.Fatalf("expand = %v", got)
	}
	if target := unavailable[0].(map[string]any)["target"].(map[string]any); target["state"] != TargetUnknown || target["reason"] == "" {
		t.Fatalf("unavailable target = %v", target)
	}
	got = call(15, `{"id":15,"method":"collections.relink","params":{"id":"c1","from":{"kind":"standing","id":"s"},"to":{"kind":"conversation","id":"b"}}}`)
	r = got["result"].(map[string]any)
	if m := r["member"].(map[string]any); m["id"] != "b" || m["added_at"] != "1970-01-01T00:00:02Z" || r["already"] != false || engine.from.Kind != RefStanding {
		t.Fatalf("relink = %v", got)
	}
	if got := call(16, `{"id":16,"method":"collections.relink","params":{"id":"c1","from":{"kind":"conversation","id":"absent"},"to":{"kind":"conversation","id":"b"}}}`); code(got) != CodeNotFound {
		t.Fatalf("relink of a non-member = %v", got)
	}
}

// The schema carries this wave's later fields: relink, the page cursor and
// total, a target's reason and expand's unavailable and unread counts.
func TestTheSchemaDescribesCollectionPagingAndRelink(t *testing.T) {
	out, err := SchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Methods map[string]struct {
			Params map[string]any `json:"params"`
			Result map[string]any `json:"result"`
		} `json:"methods"`
		Defs map[string]struct {
			Properties map[string]map[string]any `json:"properties"`
			Required   []string                  `json:"required"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.Methods["collections.relink"]; !ok {
		t.Fatal("schema lacks collections.relink")
	}
	for def, fields := range map[string][]string{
		"CollectionsResult":      {"collections", "next", "total"},
		"CollectionOpenResult":   {"next", "more"},
		"CollectionExpandResult": {"unavailable", "unavailable_refs", "unread", "missing", "more"},
		"CollectionRelinkResult": {"member", "already"},
		"Target":                 {"reason"},
	} {
		for _, f := range fields {
			if doc.Defs[def].Properties[f] == nil {
				t.Errorf("schema %s lacks %s", def, f)
			}
		}
	}
	for _, def := range []string{"CollectionsResult", "CollectionOpenResult"} {
		for _, r := range doc.Defs[def].Required {
			if r == "next" {
				t.Errorf("%s.next is required; it is present only when more remain", def)
			}
		}
	}
}
