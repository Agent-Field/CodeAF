package runs

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/caps"
	"github.com/Agent-Field/codeaf/internal/apps/realtime"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type harness struct {
	ctx    context.Context
	cancel context.CancelFunc
	root   string
	eng    *engine
	all    *svc.Services
	bundle *spec.Bundle
	recs   *memRecords
	model  *fakeModel
	clock  *lockedTime
	runs   *Service
	inbox  *Approvals
	tools  *Connectors
}

type lockedTime struct {
	mu sync.Mutex
	t  time.Time
}

func (c *lockedTime) get() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *lockedTime) set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	root := t.TempDir()
	files, sample := spec.GoldenCRM()
	bundle, err := spec.Decode(files)
	if err != nil {
		t.Fatal(err)
	}
	var seeded map[string][]bridge.Record
	if err := json.Unmarshal(sample, &seeded); err != nil {
		t.Fatal(err)
	}
	recs := newMemRecords(seeded)
	model := &fakeModel{text: `{"subject":"Hi","body":"Hello"}`}
	all := &svc.Services{
		Store:   &fixStore{bundle: bundle, state: bridge.StateKept},
		Records: recs,
		Model:   model,
		Hub:     realtime.New(svc.Config{}, nil),
	}
	all.Caps = caps.New(svc.Config{Root: root}, all)
	recs.hub = all.Hub
	h := &harness{
		ctx: ctx, cancel: cancel, root: root, all: all, bundle: bundle,
		recs: recs, model: model, clock: &lockedTime{t: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)},
	}
	h.eng = shared(svc.Config{Root: root, Ctx: ctx}, all)
	h.eng.now.Store(func() time.Time { return h.clock.get() })
	h.eng.sleeper.Store(func(context.Context, time.Duration) error { return nil })
	h.runs = &Service{eng: h.eng}
	h.inbox = &Approvals{eng: h.eng}
	h.tools = &Connectors{eng: h.eng}
	t.Cleanup(func() {
		h.runs.Close()
		h.cancel()
	})
	return h
}

func (h *harness) scope() svc.Scope {
	return svc.Scope{App: "crm", Caller: svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}, Data: svc.DataReal}
}

func (h *harness) reopen(t *testing.T) {
	t.Helper()
	h.runs.Close()
	h.cancel()
	ctx, cancel := context.WithCancel(context.Background())
	h.ctx, h.cancel = ctx, cancel
	h.eng = newEngine(svc.Config{Root: h.root, Ctx: ctx}, h.all)
	h.eng.now.Store(func() time.Time { return h.clock.get() })
	h.eng.sleeper.Store(func(context.Context, time.Duration) error { return nil })
	enginesMu.Lock()
	engines[h.root] = h.eng
	enginesMu.Unlock()
	h.runs = &Service{eng: h.eng}
	h.inbox = &Approvals{eng: h.eng}
	h.tools = &Connectors{eng: h.eng}
}

type fixStore struct {
	bundle *spec.Bundle
	state  bridge.AppState
}

func (s *fixStore) List(context.Context, svc.Caller) ([]bridge.AppSummary, error) {
	return []bridge.AppSummary{s.summary()}, nil
}

func (s *fixStore) summary() bridge.AppSummary {
	return bridge.AppSummary{ID: s.bundle.App.ID, Name: s.bundle.App.Name, State: s.state}
}

func (s *fixStore) Get(_ context.Context, app string) (bridge.AppDetail, error) {
	if app != s.bundle.App.ID {
		return bridge.AppDetail{}, bridge.Errorf(bridge.CodeNotFound, "no app "+app)
	}
	return bridge.AppDetail{AppSummary: s.summary()}, nil
}

func (s *fixStore) Create(context.Context, svc.Caller, string, string) (bridge.AppSummary, error) {
	return bridge.AppSummary{}, bridge.NotBuilt("store")
}
func (s *fixStore) Definition(_ context.Context, app string) (*spec.Bundle, error) {
	if app != s.bundle.App.ID {
		return nil, bridge.Errorf(bridge.CodeNotFound, "no app "+app)
	}
	return s.bundle, nil
}
func (s *fixStore) WriteFiles(context.Context, string, spec.Files) error {
	return bridge.NotBuilt("store")
}
func (s *fixStore) SetState(context.Context, string, bridge.AppState) error {
	return bridge.NotBuilt("store")
}
func (s *fixStore) Keep(context.Context, string) (bridge.AppSummary, error) {
	return bridge.AppSummary{}, bridge.NotBuilt("store")
}
func (s *fixStore) Discard(context.Context, string) error { return bridge.NotBuilt("store") }

type fakeModel struct {
	mu    sync.Mutex
	text  string
	cost  float64
	errs  []error
	calls int
	last  svc.ModelCall
}

func (f *fakeModel) Complete(_ context.Context, call svc.ModelCall) (svc.ModelReply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.calls
	f.calls++
	f.last = call
	if i < len(f.errs) && f.errs[i] != nil {
		return svc.ModelReply{}, f.errs[i]
	}
	text := f.text
	if text == "" {
		text = `{"subject":"Hi","body":"Hello"}`
	}
	return svc.ModelReply{Text: text, Cost: f.cost}, nil
}

type memRecords struct {
	mu   sync.Mutex
	rows map[string]map[string]bridge.Record
	hub  svc.Hub
	next int
}

func newMemRecords(seeded map[string][]bridge.Record) *memRecords {
	m := &memRecords{rows: map[string]map[string]bridge.Record{}}
	for recordType, list := range seeded {
		table := map[string]bridge.Record{}
		for _, row := range list {
			row.Type = recordType
			if row.Version == 0 {
				row.Version = 1
			}
			table[row.ID] = row
		}
		m.rows[recordType] = table
	}
	return m
}

func (m *memRecords) Query(_ context.Context, _ svc.Scope, p bridge.QueryParams) (bridge.QueryResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	table, ok := m.rows[p.Type]
	if !ok {
		return bridge.QueryResult{}, bridge.Errorf(bridge.CodeNotFound, "no record type "+p.Type)
	}
	rows := make([]bridge.Record, 0, len(table))
	for _, row := range table {
		rows = append(rows, row)
	}
	return bridge.QueryResult{Rows: rows, Total: len(rows)}, nil
}

func (m *memRecords) Get(_ context.Context, _ svc.Scope, p bridge.GetRecordParams) (bridge.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[p.Type][p.ID]
	if !ok {
		return bridge.Record{}, bridge.Errorf(bridge.CodeNotFound, "no "+p.Type+" "+p.ID)
	}
	return row, nil
}

func (m *memRecords) Create(_ context.Context, scope svc.Scope, p bridge.CreateRecordParams) (bridge.Record, error) {
	m.mu.Lock()
	table := m.rows[p.Type]
	if table == nil {
		table = map[string]bridge.Record{}
		m.rows[p.Type] = table
	}
	m.next++
	row := bridge.Record{ID: p.Type + "_new", Type: p.Type, Version: 1, Fields: p.Set}
	if _, exists := table[row.ID]; exists {
		row.ID = p.Type + "_new2"
	}
	table[row.ID] = row
	m.mu.Unlock()
	m.publish(scope, bridge.RecordDiff{Op: bridge.DiffUpsert, Type: p.Type, ID: row.ID, Record: &row, By: scope.Caller.Member})
	return row, nil
}

func (m *memRecords) Update(_ context.Context, scope svc.Scope, p bridge.UpdateRecordParams) (bridge.UpdateRecordResult, error) {
	m.mu.Lock()
	table := m.rows[p.Type]
	var written []bridge.Record
	for _, id := range idList(p.ID, p.IDs) {
		row, ok := table[id]
		if !ok {
			m.mu.Unlock()
			return bridge.UpdateRecordResult{}, bridge.Errorf(bridge.CodeNotFound, "no "+p.Type+" "+id)
		}
		if row.Fields == nil {
			row.Fields = map[string]any{}
		}
		for key, value := range p.Set {
			row.Fields[key] = value
		}
		row.Version++
		table[id] = row
		written = append(written, row)
	}
	m.mu.Unlock()
	for i := range written {
		m.publish(scope, bridge.RecordDiff{Op: bridge.DiffUpsert, Type: p.Type, ID: written[i].ID, Record: &written[i], By: scope.Caller.Member})
	}
	return bridge.UpdateRecordResult{Rows: written}, nil
}

func (m *memRecords) Delete(_ context.Context, scope svc.Scope, p bridge.DeleteRecordParams) (bridge.DeleteRecordResult, error) {
	m.mu.Lock()
	table := m.rows[p.Type]
	var deleted []string
	for _, id := range idList(p.ID, p.IDs) {
		if _, ok := table[id]; ok {
			delete(table, id)
			deleted = append(deleted, id)
		}
	}
	m.mu.Unlock()
	for _, id := range deleted {
		m.publish(scope, bridge.RecordDiff{Op: bridge.DiffDelete, Type: p.Type, ID: id, By: scope.Caller.Member})
	}
	return bridge.DeleteRecordResult{IDs: deleted}, nil
}

func (m *memRecords) Seed(context.Context, string, map[string][]bridge.Record) error { return nil }

func (m *memRecords) publish(scope svc.Scope, diff bridge.RecordDiff) {
	if m.hub != nil {
		m.hub.Publish(bridge.TopicRecords(scope.App, diff.Type), diff)
	}
}

func idList(one string, many []string) []string {
	if one != "" {
		return append([]string{one}, many...)
	}
	return many
}

func findStep(steps []bridge.StepState, id string) (bridge.StepState, bool) {
	for _, step := range steps {
		if step.ID == id {
			return step, true
		}
		if found, ok := findStep(step.Rounds, id); ok {
			return found, true
		}
	}
	return bridge.StepState{}, false
}
