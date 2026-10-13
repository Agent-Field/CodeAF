package appapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	factoryapi "github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func bindTestFactory(t *testing.T, s *Server, home string) (*FactoryRegistry, *httptest.Server) {
	t.Helper()
	server := httptest.NewUnstartedServer(s)
	server.Start()
	t.Cleanup(server.Close)
	t.Cleanup(s.Close)
	f := &FactoryRegistry{Home: home, URL: server.URL, Token: testToken, Credentials: config.NewAppCredentials()}
	f.Solo = func(context.Context) (http.Handler, error) {
		st, err := store.Open(filepath.Join(home, "v3", "factory"))
		if err != nil {
			return nil, err
		}
		handler := serve.New(factory.LocalSeam(st, time.Now()), serve.Config{Store: st, Root: st.Root(), Mode: "solo", ServedBy: "app", AccessToken: testToken, Address: server.URL, Name: "Solo"})
		t.Cleanup(handler.Close)
		return handler, nil
	}
	if err := s.BindFactories(f); err != nil {
		t.Fatal(err)
	}
	return f, server
}
func TestFactorySoloMountRequiresBearerAndCreationRestores(t *testing.T) {
	home := t.TempDir()
	s := New(testToken, Options{})
	_, server := bindTestFactory(t, s, home)
	for _, path := range []string{"/v1/me", "/v1/queue"} {
		w := request(t, s, "GET", path, "", bearer(), "127.0.0.1:4000")
		if w.Code != 404 || !strings.Contains(w.Body.String(), "no_factory") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := request(t, s, "POST", "/app/v1/factories/solo", "{}", bearer(), "127.0.0.1:4000")
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var row Factory
	_ = json.Unmarshal(w.Body.Bytes(), &row)
	if row.Mode != "solo" || row.State != "ok" || row.URL != server.URL {
		t.Fatalf("solo: %+v", row)
	}
	for _, path := range []string{"/v1/about", "/v1/me", "/v1/queue"} {
		for _, headers := range []http.Header{nil, bearer()} {
			w = request(t, s, "GET", path, "", headers, "127.0.0.1:4000")
			want := 200
			if headers == nil {
				want = 401
			}
			if w.Code != want {
				t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
			}
		}
	}
	s.Close()
	next := New(testToken, Options{})
	_, _ = bindTestFactory(t, next, home)
	w = request(t, next, "GET", "/app/v1/factories", "", bearer(), "127.0.0.1:4000")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"mode":"solo"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestFactoryTeamAttachMultipleLeaveAndMemoryHandoff(t *testing.T) {
	s := New(testToken, Options{})
	f, _ := bindTestFactory(t, s, t.TempDir())
	team := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer team-secret" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/v1/me":
			write(w, 200, factoryapi.Me{Factory: factoryapi.Factory{Name: "Team"}, Mode: "team", Operator: true})
		case "/v1/queue":
			write(w, 200, factoryapi.Queue{Jobs: []factoryapi.Job{}})
		case "/v1/events":
			<-r.Context().Done()
		}
	}))
	defer team.Close()
	f.Client = team.Client()
	w := request(t, s, "POST", "/app/v1/factories", `{"url":"`+team.URL+`"}`, bearer(), "127.0.0.1:4000")
	if w.Code != 201 || !strings.Contains(w.Body.String(), "signed_out") {
		t.Fatal(w.Code, w.Body.String())
	}
	f.Credentials.SetFactorySession(team.URL, "team-secret")
	if err := s.RefreshFactories(context.Background()); err != nil {
		t.Fatal(err)
	}
	w = request(t, s, "GET", "/app/v1/factories", "", bearer(), "127.0.0.1:4000")
	if !strings.Contains(w.Body.String(), `"state":"ok"`) {
		t.Fatal(w.Body.String())
	}
	w = request(t, s, "POST", "/app/v1/factories", `{"url":"https://another.invalid"}`, bearer(), "127.0.0.1:4000")
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(f.Home, "v3", "factory", "attach.json"))
	if err != nil || strings.Contains(string(data), "team-secret") {
		t.Fatal(string(data), err)
	}
	rows, err := f.connections(nil)
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	w = request(t, s, "DELETE", "/app/v1/factories/"+factoryID(team.URL), "{}", bearer(), "127.0.0.1:4000")
	if w.Code != 200 || f.Credentials.FactorySession(team.URL) != "" {
		t.Fatal(w.Code, w.Body.String())
	}
	rows, _ = f.connections(nil)
	if len(rows) != 1 {
		t.Fatal(rows)
	}
}
func TestFactoryInvalidRequestsAndPowerForwarding(t *testing.T) {
	s := New(testToken, Options{})
	f, _ := bindTestFactory(t, s, t.TempDir())
	var asleep []bool
	f.Power = func(_ context.Context, sleep bool) error { asleep = append(asleep, sleep); return nil }
	for _, body := range []string{`{"state":"sleeping"}`, `{"state":"awake"}`} {
		w := request(t, s, "POST", "/app/v1/power", body, bearer(), "127.0.0.1:4000")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if len(asleep) != 2 || !asleep[0] || asleep[1] {
		t.Fatal(asleep)
	}
	for _, item := range []struct{ path, body string }{{"/app/v1/power", `{"state":"offline"}`}, {"/app/v1/power", `{"state":null}`}, {"/app/v1/factories", `{"url":"http://public.example"}`}, {"/app/v1/factories", `{"url":"https://name:secret@host"}`}, {"/app/v1/factories/solo", `{"extra":true}`}} {
		w := request(t, s, "POST", item.path, item.body, bearer(), "127.0.0.1:4000")
		if w.Code != 422 {
			t.Fatal(item, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/v1/runners/presence", "/v1/workers/pull"} {
		w := request(t, s, "POST", path, `{}`, nil, "127.0.0.1:4000")
		if w.Code != 404 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
func TestFactoryHandOffChoosesFactoryKeepsSourceAndSendsOnce(t *testing.T) {
	fixture := newAgentFixture(t)
	f, _ := bindTestFactory(t, fixture.server, fixture.home)
	agent := readAgent(t, fixture.request(t, "POST", "/app/v1/agents", `{"model":"test/model","start":"send","text":"Ship change"}`), 201)
	var calls atomic.Int32
	var created factoryapi.NewJob
	team := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/me":
			write(w, 200, factoryapi.Me{Factory: factoryapi.Factory{Name: "Team"}, Mode: "team"})
		case "/v1/queue":
			write(w, 200, factoryapi.Queue{Me: "person", Repos: []factoryapi.Repo{{Name: "org/repo", Checkout: fixture.home, On: true}}})
		case "/v1/jobs":
			calls.Add(1)
			_ = json.NewDecoder(r.Body).Decode(&created)
			write(w, 201, map[string]any{"id": 17})
		case "/v1/jobs/17":
			write(w, 200, factoryapi.Job{JobID: "repo#17"})
		case "/v1/events":
			<-r.Context().Done()
		}
	}))
	defer team.Close()
	f.Credentials.SetFactorySession(team.URL, "session")
	w := fixture.request(t, "POST", "/app/v1/factories", `{"url":"`+team.URL+`"}`)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = fixture.request(t, "POST", "/app/v1/agents/"+agent.ID+"/hand-off", "{}")
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var card HandOffCard
	_ = json.Unmarshal(w.Body.Bytes(), &card)
	if card.Repo != "org/repo" || card.Factory != factoryID(team.URL) {
		t.Fatal(card)
	}
	path := "/app/v1/agents/" + agent.ID + "/hand-off/" + card.ID + "/send"
	for i := 0; i < 2; i++ {
		w = fixture.request(t, "POST", path, "{}")
		if w.Code != 200 || !strings.Contains(w.Body.String(), "repo#17") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if calls.Load() != 1 || created.Source != "hand-off" || created.FromRef.ID != agent.ID || created.Assignee == nil || *created.Assignee != "person" {
		t.Fatal(calls.Load(), created)
	}
	w = fixture.request(t, "GET", "/app/v1/agents/"+agent.ID, "")
	if !strings.Contains(w.Body.String(), "Handed to the factory") {
		t.Fatal(w.Body.String())
	}
}

// The task lane owns its transition; factory tests supply the typed source
// rather than changing its routes or reading its private storage.
type factoryTaskFake struct {
	agent  string
	handed []SourceRef
}

func (f *factoryTaskFake) FactoryTask(context.Context, string) (TaskResponse, error) {
	return TaskResponse{Task: Task{ID: "t-work", AgentID: f.agent, Title: "Task work", Brief: "Finish this task"}}, nil
}
func (f *factoryTaskFake) HandedToFactory(_ context.Context, _ string, source SourceRef) error {
	f.handed = append(f.handed, source)
	return nil
}
func TestFactoryTaskCardUsesResourceSourceAndRejectsStaleAgent(t *testing.T) {
	fixture := newAgentFixture(t)
	f, _ := bindTestFactory(t, fixture.server, fixture.home)
	agent := readAgent(t, fixture.request(t, "POST", "/app/v1/agents", `{"start":"send","text":"Origin"}`), 201)
	source := &factoryTaskFake{agent: agent.ID}
	f.Tasks = source
	if w := fixture.request(t, "POST", "/app/v1/factories/solo", "{}"); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	w := fixture.request(t, "POST", "/app/v1/tasks/t-work/hand-off", "{}")
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var card HandOffCard
	_ = json.Unmarshal(w.Body.Bytes(), &card)
	if card.Title != "Task work" || card.Brief != "Finish this task" {
		t.Fatal(card)
	}
	w = fixture.request(t, "PATCH", "/app/v1/agents/"+agent.ID, `{"name":"Changed"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = fixture.request(t, "POST", "/app/v1/agents/"+agent.ID+"/hand-off/"+card.ID+"/send", "{}")
	if w.Code != 409 || !strings.Contains(w.Body.String(), "stale") {
		t.Fatal(w.Code, w.Body.String())
	}
	if len(source.handed) != 0 {
		t.Fatal("stale card stopped task")
	}
}

func TestFactoryStreamRelaysRunnerAndActivityAndKeepsCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer session" || r.Header.Get("Last-Event-ID") != "prior" {
			t.Error("stream lost credential or cursor")
		}
		_, _ = w.Write([]byte("id: next\nevent: runner.changed\ndata: {\"runner\":\"this-machine\"}\n\nid: last\nevent: activity.created\ndata: {\"id\":\"record\"}\n\n"))
	}))
	defer server.Close()
	source := &factoryEventSource{&FactoryInboxSource{ID: "team", URL: server.URL, Token: "session"}}
	cursor := "prior"
	events := []FactoryEvent{}
	source.followFactory(context.Background(), &cursor, func(event FactoryEvent) { events = append(events, event) })
	if cursor != "last" || len(events) != 2 || events[0].Type != "runner.changed" || events[1].Factory != "team" {
		t.Fatal(cursor, events)
	}
}

func TestFactoryNativeHandOffCreatesOneJobAndCompletesTaskSource(t *testing.T) {
	fixture := newAgentFixture(t)
	f, _ := bindTestFactory(t, fixture.server, fixture.home)
	agent := readAgent(t, fixture.request(t, "POST", "/app/v1/agents", `{"start":"send","text":"Original task"}`), 201)
	source := &factoryTaskFake{agent: agent.ID}
	f.Tasks = source
	if w := fixture.request(t, "POST", "/app/v1/factories/solo", "{}"); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	w := fixture.request(t, "POST", "/app/v1/tasks/t-work/hand-off", "{}")
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var card HandOffCard
	_ = json.Unmarshal(w.Body.Bytes(), &card)
	path := "/app/v1/agents/" + agent.ID + "/hand-off/" + card.ID + "/send"
	for i := 0; i < 2; i++ {
		w = fixture.request(t, "POST", path, "{}")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if len(source.handed) != 1 || source.handed[0].Kind != "job" || source.handed[0].Factory != "solo" {
		t.Fatal(source.handed)
	}
	if fixture.engines[0].stop != 0 {
		t.Fatal("task hand-off stopped the unrelated foreground reply")
	}
	w = fixture.request(t, "GET", "/v1/queue", "")
	var queue factoryapi.Queue
	_ = json.Unmarshal(w.Body.Bytes(), &queue)
	if w.Code != 200 || len(queue.Jobs) != 1 || queue.Jobs[0].Ask == nil || queue.Jobs[0].Ask.Source != "hand-off" {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestFactorySleepStopsProcessesOnceAndKeepsRerunReceipt(t *testing.T) {
	fixture, agent, engine, _ := processFixture(t)
	f, _ := bindTestFactory(t, fixture.server, fixture.home)
	f.Power = func(context.Context, bool) error { return nil }
	for i := 0; i < 2; i++ {
		w := fixture.request(t, "POST", "/app/v1/power", `{"state":"sleeping"}`)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if len(engine.canceled) != 1 || engine.rows[0].State != "stopped" {
		t.Fatal(engine.canceled, engine.rows)
	}
	w := fixture.request(t, "GET", "/app/v1/agents/"+agent.ID+"/entries", "")
	if strings.Count(w.Body.String(), "stopped while this Mac slept · Rerun") != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
}
