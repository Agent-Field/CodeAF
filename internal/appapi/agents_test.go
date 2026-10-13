package appapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

type fakeAgentEngine struct {
	mu     sync.Mutex
	model  string
	title  string
	needs  bool
	stop   int
	closed int
	text   string
	events chan session.Event
	tasks  []session.PlanTaskRow
	budget float64
}

func (f *fakeAgentEngine) Submit(_ context.Context, text string) (<-chan session.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.text = text
	f.events = make(chan session.Event, 8)
	return f.events, nil
}
func (f *fakeAgentEngine) StopWork() error                 { f.mu.Lock(); defer f.mu.Unlock(); f.stop++; return nil }
func (f *fakeAgentEngine) Model() string                   { f.mu.Lock(); defer f.mu.Unlock(); return f.model }
func (f *fakeAgentEngine) SetModel(model string)           { f.mu.Lock(); defer f.mu.Unlock(); f.model = model }
func (f *fakeAgentEngine) SetReasoningFor(string, string)  {}
func (f *fakeAgentEngine) SetSpendRail(usd float64) error  { f.budget = usd; return nil }
func (f *fakeAgentEngine) SetApprovalPosture(string) error { return nil }
func (f *fakeAgentEngine) Rename(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.title = name
	return nil
}
func (f *fakeAgentEngine) NeedsPerson() bool                  { f.mu.Lock(); defer f.mu.Unlock(); return f.needs }
func (f *fakeAgentEngine) Transcript() []session.DisplayEntry { return nil }
func (f *fakeAgentEngine) PlanTasks() []session.PlanTaskRow   { return f.tasks }
func (f *fakeAgentEngine) Usage() session.Usage               { return session.Usage{} }

type agentFixture struct {
	registry *AgentRegistry
	server   *Server
	home     string
	engines  []*fakeAgentEngine
	opens    []AgentOpenRequest
}

func newAgentFixture(t *testing.T) *agentFixture {
	t.Helper()
	home := t.TempDir()
	f := &agentFixture{home: home}
	f.registry = NewAgentRegistry(home, func(_ context.Context, ask AgentOpenRequest) (AgentConnection, error) {
		f.opens = append(f.opens, ask)
		file := ask.SessionFile
		if file == "" {
			id := fmt.Sprintf("%016x", len(f.opens))
			dir := filepath.Join(home, "v3", "places", "app", id)
			if err := os.MkdirAll(dir, 0700); err != nil {
				return AgentConnection{}, err
			}
			file = filepath.Join(dir, "transcript.jsonl")
			if err := os.WriteFile(file, []byte(""), 0600); err != nil {
				return AgentConnection{}, err
			}
			if err := session.SaveMeta(dir, session.Meta{ID: id, Workspace: home, Model: "test/model", Created: time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)}); err != nil {
				return AgentConnection{}, err
			}
		}
		model := ask.Model
		if model == "" {
			model = "test/model"
		}
		engine := &fakeAgentEngine{model: model}
		f.engines = append(f.engines, engine)
		return AgentConnection{Engine: engine, SessionFile: file, Workspace: home, Close: func() { engine.mu.Lock(); engine.closed++; engine.mu.Unlock() }}, nil
	})
	f.registry.now = func() time.Time { return time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC) }
	f.server = New(testToken, Options{Agents: f.registry})
	t.Cleanup(f.registry.Close)
	return f
}
func (f *agentFixture) request(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return request(t, f.server, method, path, body, bearer(), "127.0.0.1:4000")
}
func readAgent(t *testing.T, w *httptest.ResponseRecorder, status int) Agent {
	t.Helper()
	if w.Code != status {
		t.Fatalf("want %d, got %d: %s", status, w.Code, w.Body)
	}
	var agent Agent
	if err := json.Unmarshal(w.Body.Bytes(), &agent); err != nil {
		t.Fatal(err)
	}
	if agent.ID == "" {
		t.Fatalf("missing identity: %s", w.Body)
	}
	return agent
}

func TestAgentsCreateListGetPatchAndStableReopen(t *testing.T) {
	f := newAgentFixture(t)
	a := readAgent(t, f.request(t, "POST", "/app/v1/agents", `{"start":"empty","model":"test/other","effort":"high","budget":{"usd":"8.00"},"approvals":"everything"}`), 201)
	if a.State != "idle" || a.Name != "" || a.Budget.USD != "8.00" || a.Model != "test/other" || a.Effort != "high" {
		t.Fatalf("wrong defaults: %+v", a)
	}
	if f.opens[0].Approvals != "everything" || f.opens[0].Budget.USD != "8.00" {
		t.Fatalf("engine lost creation settings: %+v", f.opens)
	}
	a = readAgent(t, f.request(t, "PATCH", "/app/v1/agents/"+a.ID, `{"name":"Investigate billing","pinned":true,"model":"test/new","effort":"low","budget":{"usd":"9.00"},"expect":{"version":1,"state":"idle"}}`), 200)
	if a.Name != "Investigate billing" || !a.Pinned || a.Model != "test/new" || a.Budget.USD != "9.00" || a.Version != 2 {
		t.Fatalf("patch: %+v", a)
	}
	if f.engines[0].title != a.Name || f.engines[0].budget != 9 {
		t.Fatal("patch did not reach engine")
	}
	w := f.request(t, "GET", "/app/v1/agents?q=billing+state:idle+pinned:true&state=open", "")
	var list struct {
		Agents []Agent `json:"agents"`
	}
	json.Unmarshal(w.Body.Bytes(), &list)
	if w.Code != 200 || len(list.Agents) != 1 || list.Agents[0].ID != a.ID {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	get := f.request(t, "GET", "/app/v1/agents/"+a.ID, "")
	readAgent(t, get, 200)
	if !strings.Contains(get.Body.String(), `"snapshot"`) {
		t.Fatal("no snapshot")
	}
	closed := readAgent(t, f.request(t, "POST", "/app/v1/agents/"+a.ID+"/close", `{}`), 200)
	if closed.State != "closed" || closed.ClosedAt == nil {
		t.Fatalf("close: %+v", closed)
	}
	archived := readAgent(t, f.request(t, "POST", "/app/v1/agents/"+a.ID+"/archive", `{}`), 200)
	if archived.State != "archived" || archived.ArchivedAt == nil {
		t.Fatalf("archive: %+v", archived)
	}
	f.registry.Close()
	second := NewAgentRegistry(f.home, nil)
	defer second.Close()
	server := New(testToken, Options{Agents: second})
	restored := readAgent(t, request(t, server, "POST", "/app/v1/agents/"+a.ID+"/reopen", `{}`, bearer(), "127.0.0.1:4000"), 200)
	if restored.ID != a.ID || restored.Name != a.Name || !restored.Pinned || restored.State != "idle" || restored.ClosedAt != nil || restored.ArchivedAt != nil {
		t.Fatalf("reopen lost history settings: %+v", restored)
	}
	if _, err := os.Stat(f.opens[0].SessionFile); err == nil {
		t.Fatal("fixture's initial request should not already name a file")
	}
	records, err := os.ReadFile(filepath.Join(f.home, "v3", "app", "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	var disk agentDisk
	json.Unmarshal(records, &disk)
	if _, err := os.Stat(disk.Records[a.ID].SessionFile); err != nil {
		t.Fatal("lifecycle removed transcript")
	}
}

func TestAgentsSendNamesAndCloseNeedsExplicitStop(t *testing.T) {
	f := newAgentFixture(t)
	text := "  Investigate\n billing timeout with 多字节 characters and context  "
	a := readAgent(t, f.request(t, "POST", "/app/v1/agents", `{"start":"send","text":`+strconvQuote(text)+`}`), 201)
	if a.Name != AgentName(text) || len([]rune(a.Name)) > 40 || a.State != "running" || a.LastMessageAt == nil || a.Effort != "medium" || a.Budget.USD != "5.00" {
		t.Fatalf("send: %+v", a)
	}
	readAgent(t, f.request(t, "GET", "/app/v1/agents/"+a.ID, ""), 200)
	w := f.request(t, "POST", "/app/v1/agents/"+a.ID+"/close", `{}`)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"running":{"processes":0,"tasks":0}`) || f.engines[0].stop != 0 {
		t.Fatalf("close guard: %d %s", w.Code, w.Body)
	}
	closed := readAgent(t, f.request(t, "POST", "/app/v1/agents/"+a.ID+"/close", `{"stop_running":true}`), 200)
	if closed.State != "closed" || f.engines[0].stop != 1 || f.engines[0].closed != 1 {
		t.Fatal("confirmed close did not stop and release engine")
	}
	close(f.engines[0].events)
	reopened := readAgent(t, f.request(t, "POST", "/app/v1/agents/"+a.ID+"/reopen", `{}`), 200)
	if reopened.State != "idle" {
		t.Fatal("late turn completion reopened or failed resource")
	}
}
func strconvQuote(s string) string { b, _ := json.Marshal(s); return string(b) }

func TestAgentStatesPrecedenceAndRestartReceipt(t *testing.T) {
	f := newAgentFixture(t)
	a := readAgent(t, f.request(t, "POST", "/app/v1/agents", `{"start":"empty"}`), 201)
	for _, state := range []string{"running", "needs_you", "failed", "idle"} {
		f.engines[0].mu.Lock()
		f.engines[0].needs = state == "needs_you"
		f.engines[0].mu.Unlock()
		if err := f.registry.UpdateState(a.ID, state); err != nil {
			t.Fatal(err)
		}
		got := readAgent(t, f.request(t, "GET", "/app/v1/agents/"+a.ID, ""), 200)
		if got.State != state {
			t.Fatalf("want %s, got %s", state, got.State)
		}
	}
	if err := f.registry.UpdateState(a.ID, "bogus"); err == nil {
		t.Fatal("unknown state accepted")
	}
	if err := f.registry.UpdateState(a.ID, "running"); err != nil {
		t.Fatal(err)
	}
	f.registry.Close()
	restarted := NewAgentRegistry(f.home, nil)
	defer restarted.Close()
	got := readAgent(t, request(t, New(testToken, Options{Agents: restarted}), "GET", "/app/v1/agents/"+a.ID, "", bearer(), "127.0.0.1:4000"), 200)
	if got.State != "idle" || got.Receipt != "Stopped · codeaf restarted" {
		t.Fatalf("restart: %+v", got)
	}
}

func TestAgentStaleAndInvalidRoutesCannotMutate(t *testing.T) {
	f := newAgentFixture(t)
	a := readAgent(t, f.request(t, "POST", "/app/v1/agents", `{"start":"empty"}`), 201)
	for _, body := range []string{`{"name":"new","expect":{"version":0}}`, `{"name":"new","expect":{"state":"running"}}`} {
		w := f.request(t, "PATCH", "/app/v1/agents/"+a.ID, body)
		if w.Code != 409 || !strings.Contains(w.Body.String(), `"reason":"stale"`) {
			t.Fatalf("stale: %s", w.Body)
		}
	}
	for _, body := range []string{"", `null`, `[]`, `{} {}`, `{"start":"unknown"}`, `{"start":"send"}`, `{"start":"empty","text":"hello"}`, `{"start":"empty","workspace":"/tmp"}`, `{"start":"empty","effort":"max"}`, `{"start":"empty","budget":{"usd":"5"}}`, `{"start":"empty","model":null}`, `{"start":"empty","approvals":"yolo"}`, `{"start":"empty","budget":{"usd":"0.00"}}`} {
		w := f.request(t, "POST", "/app/v1/agents", body)
		if w.Code != 422 {
			t.Fatalf("invalid create accepted %q: %d %s", body, w.Code, w.Body)
		}
	}
	for _, name := range []string{"", strings.Repeat("字", 41), "line\nbreak"} {
		w := f.request(t, "PATCH", "/app/v1/agents/"+a.ID, `{"name":`+strconvQuote(name)+`}`)
		if w.Code != 422 {
			t.Fatalf("bad rename accepted: %s", w.Body)
		}
	}
	for _, suffix := range []string{"archive", "reopen"} {
		w := f.request(t, "POST", "/app/v1/agents/"+a.ID+"/"+suffix, `{}`)
		if w.Code != 409 {
			t.Fatalf("invalid transition accepted: %s", w.Body)
		}
	}
	for _, path := range []string{"/app/v1/agents", "/app/v1/agents/" + a.ID, "/app/v1/agents/" + a.ID + "/close", "/app/v1/agents/" + a.ID + "/archive", "/app/v1/agents/" + a.ID + "/reopen"} {
		w := request(t, f.server, "DELETE", path, `{}`, nil, "127.0.0.1:4000")
		if w.Code != 401 {
			t.Fatalf("unguarded %s: %d", path, w.Code)
		}
		w = f.request(t, "DELETE", path, `{}`)
		if w.Code != 405 || w.Header().Get("Allow") == "" {
			t.Fatalf("method %s: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/app/v1/agents/missing", "/app/v1/agents/" + a.ID + "/delete", "/app/v1/agents/" + a.ID + "/close/extra"} {
		w := f.request(t, "GET", path, "")
		if w.Code != 404 {
			t.Fatalf("unknown route accepted: %d %s", w.Code, w.Body)
		}
	}
	for _, q := range []string{"state:bogus", "since:bad", "pinned:maybe", "idle+unknown:value", "state:closed+unknown:value"} {
		w := f.request(t, "GET", "/app/v1/agents?q="+q, "")
		if w.Code != 422 {
			t.Fatalf("bad query accepted %s: %d %s", q, w.Code, w.Body)
		}
	}
	if len(f.opens) != 1 {
		t.Fatal("invalid requests started engines")
	}
}

func TestAgentPOSTIdempotencyAcrossRegistryInstances(t *testing.T) {
	f := newAgentFixture(t)
	headers := bearer()
	headers.Set("Idempotency-Key", "create-once")
	one := request(t, f.server, "POST", "/app/v1/agents", `{"start":"empty"}`, headers, "127.0.0.1:4000")
	first := readAgent(t, one, 201)
	secondRegistry := NewAgentRegistry(f.home, func(context.Context, AgentOpenRequest) (AgentConnection, error) {
		t.Fatal("replay opened another engine")
		return AgentConnection{}, nil
	})
	// Both launches share a clock so the replay does not expire with the date.
	secondRegistry.now = f.registry.now
	defer secondRegistry.Close()
	two := request(t, New(testToken, Options{Agents: secondRegistry}), "POST", "/app/v1/agents", `{"start":"empty"}`, headers, "127.0.0.1:4000")
	if one.Body.String() != two.Body.String() || two.Code != 201 {
		t.Fatalf("replay differed: %s / %s", one.Body, two.Body)
	}
	headers.Set("Idempotency-Key", "close-once")
	one = request(t, f.server, "POST", "/app/v1/agents/"+first.ID+"/close", `{}`, headers, "127.0.0.1:4000")
	two = request(t, f.server, "POST", "/app/v1/agents/"+first.ID+"/close", `{}`, headers, "127.0.0.1:4000")
	if one.Body.String() != two.Body.String() || one.Code != 200 {
		t.Fatal("close replay changed version or timestamp")
	}
}

func TestAgentRegistryMergesConcurrentWritersAndTUIHistory(t *testing.T) {
	f := newAgentFixture(t)
	a := readAgent(t, f.request(t, "POST", "/app/v1/agents", `{"start":"empty"}`), 201)
	second := NewAgentRegistry(f.home, nil)
	defer second.Close()
	server := New(testToken, Options{Agents: second})
	readAgent(t, request(t, server, "PATCH", "/app/v1/agents/"+a.ID, `{"pinned":true}`, bearer(), "127.0.0.1:4000"), 200)
	got := readAgent(t, f.request(t, "PATCH", "/app/v1/agents/"+a.ID, `{"name":"Kept pin"}`), 200)
	if !got.Pinned {
		t.Fatal("writer overwrote another launch's pin")
	}
	dir := filepath.Join(f.home, "v3", "places", "app", "2222222222222222")
	os.MkdirAll(dir, 0700)
	session.SaveMeta(dir, session.Meta{ID: "2222222222222222", Title: "TUI history", Workspace: f.home, Created: time.Now(), LastUserAt: time.Now(), Model: "test/model"})
	os.WriteFile(filepath.Join(dir, "transcript.jsonl"), nil, 0600)
	w := f.request(t, "GET", "/app/v1/agents?q=TUI", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "2222222222222222") {
		t.Fatalf("shared TUI history missing: %s", w.Body)
	}
}

func TestAgentNameUsesUnicodeCharactersAndNoAI(t *testing.T) {
	for _, test := range []struct{ in, want string }{{"", ""}, {"  first\n words\tstay  ", "first words stay"}, {strings.Repeat("字", 41), strings.Repeat("字", 40)}, {"Fix a broken login", "Fix a broken login"}} {
		if got := AgentName(test.in); got != test.want {
			t.Fatalf("name %q: %q", test.in, got)
		}
	}
}

var _ AgentEngine = (*fakeAgentEngine)(nil)

func TestAgentLifecycleEventsAndCloseWorkAndCrewRules(t *testing.T) {
	f := newAgentFixture(t)
	headers := bearer()
	headers.Set("Idempotency-Key", "one")
	response := request(t, f.server, "POST", "/app/v1/agents", `{"start":"empty"}`, headers, "127.0.0.1:4000")
	agent := readAgent(t, response, 201)
	request(t, f.server, "POST", "/app/v1/agents", `{"start":"empty"}`, headers, "127.0.0.1:4000")
	batch, _, _, _, _ := f.server.Events().read(0)
	if len(batch) != 1 || batch[0].kind != EventAgentCreated || batch[0].agent != agent.ID {
		t.Fatalf("create/replay events: %+v", batch)
	}
	f.engines[0].tasks = []session.PlanTaskRow{{Status: "running"}}
	failed := f.request(t, "POST", "/app/v1/agents/"+agent.ID+"/close", `{}`)
	if failed.Code != 409 || !strings.Contains(failed.Body.String(), `"tasks":1`) {
		t.Fatalf("running task guard: %d %s", failed.Code, failed.Body)
	}
	f.engines[0].tasks = nil
	f.registry.mu.Lock()
	err := f.registry.disk(func(d *agentDisk) error {
		record := d.Records[agent.ID]
		record.Agent.CrewID = "crew"
		d.Records[agent.ID] = record
		return nil
	}, true)
	f.registry.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	failed = f.request(t, "POST", "/app/v1/agents/"+agent.ID+"/close", `{"stop_running":true}`)
	if failed.Code != 409 || !strings.Contains(failed.Body.String(), "remove this agent from its crew") {
		t.Fatalf("crew guard: %s", failed.Body)
	}
	f.registry.mu.Lock()
	err = f.registry.disk(func(d *agentDisk) error {
		record := d.Records[agent.ID]
		record.Agent.CrewID = ""
		d.Records[agent.ID] = record
		return nil
	}, true)
	f.registry.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"close", "archive", "reopen"} {
		readAgent(t, f.request(t, "POST", "/app/v1/agents/"+agent.ID+"/"+action, `{}`), 200)
	}
	batch, _, _, _, _ = f.server.Events().read(0)
	// Activity has its own transition tests; this gate counts agent publications.
	agentEvents := batch[:0]
	for _, event := range batch {
		if event.kind != EventActivity {
			agentEvents = append(agentEvents, event)
		}
	}
	batch = agentEvents
	if len(batch) != 4 {
		t.Fatalf("failed actions or replay published changes: %d", len(batch))
	}
	for _, event := range batch[1:] {
		if event.kind != EventAgentChanged || event.agent != agent.ID {
			t.Fatalf("lifecycle event: %+v", event)
		}
	}
}
