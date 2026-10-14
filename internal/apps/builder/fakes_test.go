package builder

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// memStore keeps one app's files and state in memory.
type memStore struct {
	mu    sync.Mutex
	state bridge.AppState
	files spec.Files
}

func (m *memStore) List(context.Context, svc.Caller) ([]bridge.AppSummary, error) { return nil, nil }
func (m *memStore) Definition(context.Context, string) (*spec.Bundle, error)      { return nil, nil }
func (m *memStore) Keep(context.Context, string) (bridge.AppSummary, error) {
	return bridge.AppSummary{}, nil
}
func (m *memStore) Discard(context.Context, string) error { return nil }

func (m *memStore) Create(context.Context, svc.Caller, string, string) (bridge.AppSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state, m.files = bridge.StatePlanning, spec.Files{}
	return bridge.AppSummary{ID: "crm", Name: "CRM", State: m.state}, nil
}

func (m *memStore) Get(context.Context, string) (bridge.AppDetail, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	files := map[string]string{}
	for p, d := range m.files {
		files[p] = string(d)
	}
	return bridge.AppDetail{AppSummary: bridge.AppSummary{ID: "crm", State: m.state}, Files: files}, nil
}

func (m *memStore) WriteFiles(_ context.Context, _ string, files spec.Files) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files = files
	return nil
}

func (m *memStore) SetState(_ context.Context, _ string, state bridge.AppState) error {
	m.mu.Lock()
	m.state = state
	m.mu.Unlock()
	return nil
}

func (m *memStore) current() (bridge.AppState, spec.Files) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state, m.files
}

// memHub records every event published on the build topic.
type memHub struct {
	mu      sync.Mutex
	builds  []bridge.BuildEvent
	results []ResultEvent
	drafts  []bridge.DraftEvent
}

func (h *memHub) Publish(topic string, data any) {
	if ev, ok := data.(bridge.DraftEvent); ok && topic == bridge.TopicDraft("crm") {
		h.mu.Lock()
		h.drafts = append(h.drafts, ev)
		h.mu.Unlock()
		return
	}
	if result, ok := data.(ResultEvent); ok {
		h.mu.Lock()
		h.results = append(h.results, result)
		h.mu.Unlock()
		data = result.BuildEvent
	}
	if ev, ok := data.(bridge.BuildEvent); ok && topic == bridge.TopicBuild("crm") {
		h.mu.Lock()
		h.builds = append(h.builds, ev)
		h.mu.Unlock()
	}
}

func (h *memHub) Subscribe(string) (<-chan svc.Event, func()) { return nil, func() {} }

func (h *memHub) events() []bridge.BuildEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]bridge.BuildEvent(nil), h.builds...)
}

// find returns the first event matching step and status.
func (h *memHub) find(step bridge.BuildStep, status bridge.BuildStatus) (bridge.BuildEvent, bool) {
	for _, ev := range h.events() {
		if ev.Step == step && ev.Status == status {
			return ev, true
		}
	}
	return bridge.BuildEvent{}, false
}

// scriptedChecks answers Check from a list of reports, one per round.
type scriptedChecks struct {
	mu      sync.Mutex
	reports []bridge.CheckReport
	seen    []spec.Files
}

func (c *scriptedChecks) Run(_ context.Context, _ string, files spec.Files, round int) (bridge.CheckReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = append(c.seen, files)
	return c.reports[min(round-1, len(c.reports)-1)], nil
}

type fakeSample struct{}

func (fakeSample) Generate(context.Context, *spec.Bundle) (map[string][]bridge.Record, error) {
	return map[string][]bridge.Record{"deal": {{ID: "d1", Type: "deal"}}}, nil
}

type fakeSeeder struct {
	mu     sync.Mutex
	seeded map[string][]bridge.Record
}

func (f *fakeSeeder) Seed(_ context.Context, _ string, rows map[string][]bridge.Record) error {
	f.mu.Lock()
	f.seeded = rows
	f.mu.Unlock()
	return nil
}

func (f *fakeSeeder) Query(context.Context, svc.Scope, bridge.QueryParams) (bridge.QueryResult, error) {
	panic("the Builder must not query records")
}
func (f *fakeSeeder) Get(context.Context, svc.Scope, bridge.GetRecordParams) (bridge.Record, error) {
	panic("the Builder must not read records")
}
func (f *fakeSeeder) Create(context.Context, svc.Scope, bridge.CreateRecordParams) (bridge.Record, error) {
	panic("the Builder must not write records")
}
func (f *fakeSeeder) Update(context.Context, svc.Scope, bridge.UpdateRecordParams) (bridge.UpdateRecordResult, error) {
	panic("the Builder must not write records")
}
func (f *fakeSeeder) Delete(context.Context, svc.Scope, bridge.DeleteRecordParams) (bridge.DeleteRecordResult, error) {
	panic("the Builder must not write records")
}

// scriptedModel answers by kind of call. It plays the golden CRM as the model.
type scriptedModel struct {
	mu      sync.Mutex
	prompts []string
	cost    float64
	plan    func(call int) string
	write   func(prompt string) string
	fixes   []string // replies to successive FIX calls
	calls   map[string]int
	hold    chan struct{} // when set, every call waits on it
}

func newScriptedModel() *scriptedModel {
	return &scriptedModel{cost: 0.01, calls: map[string]int{}}
}

func (m *scriptedModel) Complete(ctx context.Context, call svc.ModelCall) (svc.ModelReply, error) {
	text := call.Messages[0].Text
	m.mu.Lock()
	m.prompts = append(m.prompts, text)
	kind := callKind(text)
	n := m.calls[kind]
	m.calls[kind]++
	hold := m.hold
	m.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return svc.ModelReply{}, ctx.Err()
		}
	}
	return svc.ModelReply{Text: m.reply(kind, text, n), Cost: m.cost}, nil
}

func callKind(prompt string) string {
	switch {
	case strings.Contains(prompt, "This is the PLAN call"):
		return "plan"
	case strings.Contains(prompt, "This is the FIX call"):
		return "fix"
	default:
		return "write"
	}
}

func (m *scriptedModel) reply(kind, prompt string, n int) string {
	switch kind {
	case "plan":
		if m.plan != nil {
			return m.plan(n)
		}
		return goldenPlan()
	case "fix":
		return m.fixes[min(n, len(m.fixes)-1)]
	}
	if m.write != nil {
		return m.write(prompt)
	}
	return goldenFiles(onlyPaths(prompt))
}

// onlyPaths reads the "This call writes ONLY:" line out of a WRITE prompt.
func onlyPaths(prompt string) []string {
	_, after, _ := strings.Cut(prompt, "This call writes ONLY: ")
	line, _, _ := strings.Cut(after, ". Write no other file")
	var paths []string
	for _, p := range strings.Split(line, ",") {
		paths = append(paths, strings.TrimSpace(p))
	}
	return paths
}

func goldenPlan() string {
	files, _ := spec.GoldenCRM()
	return string(files[spec.PlanFile])
}

// goldenFiles is a WRITE reply holding the golden CRM's files at paths.
func goldenFiles(paths []string) string {
	golden, _ := spec.GoldenCRM()
	reply := map[string]string{}
	for _, p := range paths {
		reply[p] = string(golden[p])
	}
	out, _ := json.Marshal(reply)
	return string(out)
}

// rig wires a Service to the fakes above.
type rig struct {
	svc    *Service
	store  *memStore
	hub    *memHub
	model  *scriptedModel
	checks *scriptedChecks
	seeds  *fakeSeeder
}

func newRig(t *testing.T, reports ...bridge.CheckReport) *rig {
	t.Helper()
	if len(reports) == 0 {
		reports = []bridge.CheckReport{{Pass: true}}
	}
	r := &rig{store: &memStore{}, hub: &memHub{}, model: newScriptedModel(),
		checks: &scriptedChecks{reports: reports}, seeds: &fakeSeeder{}}
	all := &svc.Services{Store: r.store, Hub: r.hub, Model: r.model, Check: r.checks, Sample: fakeSample{}, Records: r.seeds}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r.svc = New(svc.Config{Ctx: ctx}, all)
	return r
}

func (r *rig) scope() svc.Scope { return svc.Scope{App: "crm", Caller: svc.Caller{Member: svc.Owner}} }

// waitFor polls until cond holds; the Builder works in the background.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (r *rig) waitState(t *testing.T, want bridge.AppState) {
	t.Helper()
	waitFor(t, "state "+string(want), func() bool { s, _ := r.store.current(); return s == want })
}

func (r *rig) create(t *testing.T, justBuild bool) {
	t.Helper()
	_, err := r.svc.Create(context.Background(), svc.Caller{Member: svc.Owner},
		bridge.CreateAppParams{Paragraph: "A CRM for our sales pipeline: companies, deals and contacts.", JustBuild: justBuild})
	if err != nil {
		t.Fatal(err)
	}
}
