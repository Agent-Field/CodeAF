package crewlink

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestCrewLinks(t *testing.T) {
	tests := []struct {
		name string
		fn   func(*testing.T)
	}{
		{"history stays newest first", testLinkHistory},
		{"access follows the caller", testLinkAccess},
		{"finish and reopen keep the same links", testLinkFinish},
		{"a crew spans apps", testLinkAcrossApps},
		{"rejects a bad target", testLinkInvalid},
		{"detail is the live record", testLinkDetail},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.fn(t) })
	}
}

func testLinkHistory(t *testing.T) {
	s, hub := newLinks(t, dealWorld("crm"))
	writeTeam(t, s.root, "pricing", "Pricing push")
	ctx := context.Background()
	first, err := s.Add(ctx, scope("crm"), bridge.AddLinkParams{Crew: "pricing", Type: "deal", ID: "north"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(ctx, scope("crm"), bridge.AddLinkParams{Crew: "review", Type: "deal", ID: "north"}); err != nil {
		t.Fatal(err)
	}
	got := linksForRecord(t, s, ctx, "crm", "deal", "north")
	if len(got) != 2 || got[0].Crew != "review" || got[1].Crew != "pricing" || got[1].CrewName != "Pricing push" {
		t.Fatalf("order=%+v", got)
	}
	if err := s.Remove(ctx, scope("crm"), bridge.RemoveLinkParams{Crew: "pricing", Type: "deal", ID: "north"}); err != nil {
		t.Fatal(err)
	}
	got = linksForRecord(t, s, ctx, "crm", "deal", "north")
	if len(got) != 2 || got[1].State != bridge.CrewLinkUnlinked {
		t.Fatalf("history=%+v", got)
	}
	again, err := s.Add(ctx, scope("crm"), bridge.AddLinkParams{Crew: "pricing", Type: "deal", ID: "north"})
	if err != nil || again.State != bridge.CrewLinkOpen {
		t.Fatalf("relink=%+v %v", again, err)
	}
	if _, err := s.Add(ctx, scope("crm"), bridge.AddLinkParams{Crew: "pricing", Type: "deal", ID: "north"}); codeOf(err) != bridge.CodeConflict {
		t.Fatal(err)
	}
	if codeOf(s.Remove(ctx, scope("crm"), bridge.RemoveLinkParams{Crew: "missing", Type: "deal", ID: "north"})) != bridge.CodeNotFound {
		t.Fatal("missing remove")
	}
	raw, err := os.ReadFile(filepath.Join(s.root, "crm", "links.json"))
	if err != nil || !json.Valid(raw) {
		t.Fatal(err)
	}
	index, err := os.ReadFile(s.indexPath())
	if err != nil || !json.Valid(index) || !strings.Contains(string(index), `"pricing"`) {
		t.Fatal(err)
	}
	temps, _ := filepath.Glob(filepath.Join(s.root, "crm", ".tmp-*"))
	if len(temps) != 0 {
		t.Fatal(temps)
	}
	if len(hub.events) == 0 || hub.events[0].Topic != bridge.TopicLinks("crm") {
		t.Fatalf("publish=%+v", hub.events)
	}
	if _, ok := hub.events[0].Data.(bridge.CrewLink); !ok {
		t.Fatalf("event=%T", hub.events[0].Data)
	}
	if first.Label != "Northwind · team plan" || first.AppName != "CRM" || first.Access != bridge.CrewLinkAccessOK {
		t.Fatalf("first=%+v", first)
	}
}

func testLinkAccess(t *testing.T) {
	w := dealWorld("crm")
	s, _ := newLinks(t, w)
	ctx := context.Background()
	if _, err := s.Add(ctx, scope("crm"), bridge.AddLinkParams{Crew: "pricing", Type: "deal", ID: "north"}); err != nil {
		t.Fatal(err)
	}
	w.hide["crm"] = true
	if got := linksForRecord(t, s, ctx, "crm", "deal", "north"); got[0].Access != bridge.CrewLinkNoAccess {
		t.Fatalf("hidden=%s", got[0].Access)
	}
	w.hide["crm"] = false
	w.caps.deny["deal"] = true
	if got := linksForRecord(t, s, ctx, "crm", "deal", "north"); got[0].Access != bridge.CrewLinkNoAccess {
		t.Fatalf("denied=%s", got[0].Access)
	}
	w.caps.deny["deal"] = false
	delete(w.rows.rows, "crm/deal/north")
	if got := linksForRecord(t, s, ctx, "crm", "deal", "north"); got[0].Access != bridge.CrewLinkDeleted || got[0].Label != "Northwind · team plan" {
		t.Fatalf("deleted=%+v", got[0])
	}
	w.rows.rows["crm/deal/north"] = deal("north", "Northwind · team plan", "Proposal", 48000)
	w.apps["crm"] = bridge.AppDetail{AppSummary: bridge.AppSummary{ID: "crm", Name: "CRM", State: bridge.StateArchived}}
	if got := linksForRecord(t, s, ctx, "crm", "deal", "north"); got[0].Access != bridge.CrewLinkArchived {
		t.Fatalf("archived=%s", got[0].Access)
	}
	delete(w.apps, "crm")
	if got := linksForRecord(t, s, ctx, "crm", "deal", "north"); got[0].Access != bridge.CrewLinkDeleted {
		t.Fatalf("gone=%s", got[0].Access)
	}
}

func testLinkFinish(t *testing.T) {
	s, hub := newLinks(t, dealWorld("crm"))
	ctx := context.Background()
	add := func(id string) {
		t.Helper()
		if _, err := s.Add(ctx, scope("crm"), bridge.AddLinkParams{Crew: "pricing", Type: "deal", ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	add("north")
	add("globex")
	if err := s.Remove(ctx, scope("crm"), bridge.RemoveLinkParams{Crew: "pricing", Type: "deal", ID: "globex"}); err != nil {
		t.Fatal(err)
	}
	before := len(hub.events)
	if err := s.Finish(ctx, "pricing"); err != nil {
		t.Fatal(err)
	}
	got := linksForCrew(t, s, ctx, "pricing")
	states := map[string]bridge.CrewLinkState{}
	for _, link := range got {
		states[link.ID] = link.State
	}
	if states["north"] != bridge.CrewLinkFinished || states["globex"] != bridge.CrewLinkUnlinked {
		t.Fatalf("states=%v", states)
	}
	if len(hub.events) != before+1 {
		t.Fatalf("finish events=%d", len(hub.events)-before)
	}
	if err := s.Reopen(ctx, "pricing"); err != nil {
		t.Fatal(err)
	}
	got = linksForCrew(t, s, ctx, "pricing")
	states = map[string]bridge.CrewLinkState{}
	for _, link := range got {
		states[link.ID] = link.State
	}
	if states["north"] != bridge.CrewLinkOpen || states["globex"] != bridge.CrewLinkUnlinked {
		t.Fatalf("reopen=%v", states)
	}
}

func testLinkAcrossApps(t *testing.T) {
	w := dealWorld("crm")
	w.apps["slides"] = bridge.AppDetail{AppSummary: bridge.AppSummary{ID: "slides", Name: "Slides", State: bridge.StateKept}}
	w.defs["slides"] = w.defs["crm"]
	w.rows.rows["slides/deal/deck"] = deal("deck", "Q4 pricing deck", "Proposal", 0)
	s, _ := newLinks(t, w)
	ctx := context.Background()
	if _, err := s.Add(ctx, scope("crm"), bridge.AddLinkParams{Crew: "pricing", Type: "deal", ID: "north"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(ctx, scope("slides"), bridge.AddLinkParams{Crew: "pricing", Type: "deal", ID: "deck"}); err != nil {
		t.Fatal(err)
	}
	got := linksForCrew(t, s, ctx, "pricing")
	if len(got) != 2 || got[0].App != "slides" || got[1].App != "crm" || got[0].AppName != "Slides" {
		t.Fatalf("across=%+v", got)
	}
}

func testLinkInvalid(t *testing.T) {
	s, _ := newLinks(t, dealWorld("crm"))
	ctx := context.Background()
	for _, p := range []bridge.AddLinkParams{{Type: "deal", ID: "north"}, {Crew: "pricing", ID: "north"}, {Crew: "pricing", Type: "deal", ID: "a/b"}, {Crew: "../x", Type: "deal", ID: "north"}} {
		if _, err := s.Add(ctx, scope("crm"), p); codeOf(err) != bridge.CodeInvalid {
			t.Fatalf("params=%+v err=%v", p, err)
		}
	}
	if _, err := s.ForCrew(ctx, editor(), bridge.LinksForCrewParams{}); codeOf(err) != bridge.CodeInvalid {
		t.Fatal(err)
	}
}

func testLinkDetail(t *testing.T) {
	def := dealDef()
	tests := []struct {
		name, label, detail string
		amount              float64
		def                 *spec.Bundle
	}{
		{"proposal", "Northwind · team plan", "Proposal · $48,000", 48000, def},
		{"zero", "Northwind · team plan", "Proposal", 0, def},
		{"cents", "Northwind · team plan", "Proposal · $14.40", 14.4, def},
		{"plain fields", "Northwind · team plan", "Proposal · $9,000", 9000, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			label, detail := Present(tt.def, deal("north", "Northwind · team plan", "Proposal", tt.amount))
			if label != tt.label || detail != tt.detail {
				t.Fatalf("label=%q detail=%q", label, detail)
			}
		})
	}
	if RecordRef("crm", "deal", "north") != "app:crm/deal/north" || RecordHref("crm", "deal", "north") != "codeaf://apps/crm/deal/north" {
		t.Fatal("ref")
	}
	app, typ, id, ok := ParseRecordRef("app:crm/deal/north")
	if !ok || app != "crm" || typ != "deal" || id != "north" {
		t.Fatalf("parse %s %s %s %v", app, typ, id, ok)
	}
}

func linksForRecord(t *testing.T, s *Service, ctx context.Context, app, typ, id string) []bridge.CrewLink {
	t.Helper()
	result, err := s.ForRecord(ctx, scope(app), bridge.LinksForRecordParams{Type: typ, ID: id})
	return listed(t, result, err)
}

func linksForCrew(t *testing.T, s *Service, ctx context.Context, crew string) []bridge.CrewLink {
	t.Helper()
	result, err := s.ForCrew(ctx, editor(), bridge.LinksForCrewParams{Crew: crew})
	return listed(t, result, err)
}

func listed(t *testing.T, result bridge.ListLinksResult, err error) []bridge.CrewLink {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if result.Links == nil {
		t.Fatal("null links")
	}
	return result.Links
}

type linkWorld struct {
	apps map[string]bridge.AppDetail
	hide map[string]bool
	defs map[string]*spec.Bundle
	rows *memRecords
	caps *memCaps
}

func dealWorld(app string) *linkWorld {
	w := &linkWorld{
		apps: map[string]bridge.AppDetail{},
		hide: map[string]bool{},
		defs: map[string]*spec.Bundle{},
		rows: &memRecords{rows: map[string]bridge.Record{}},
		caps: &memCaps{deny: map[string]bool{}},
	}
	w.apps[app] = bridge.AppDetail{AppSummary: bridge.AppSummary{ID: app, Name: "CRM", State: bridge.StateKept}}
	w.defs[app] = dealDef()
	w.rows.rows[app+"/deal/north"] = deal("north", "Northwind · team plan", "Proposal", 48000)
	w.rows.rows[app+"/deal/globex"] = deal("globex", "Globex · renewal", "Proposal", 48000)
	return w
}

func newLinks(t *testing.T, w *linkWorld) (*Service, *memHub) {
	t.Helper()
	profile := t.TempDir()
	hub := &memHub{}
	all := &svc.Services{Store: &memStore{w: w}, Records: w.rows, Caps: w.caps, Hub: hub}
	s := New(svc.Config{Root: filepath.Join(profile, "apps")}, all)
	cur := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time {
		cur = cur.Add(time.Second)
		return cur
	}
	return s, hub
}

func writeTeam(t *testing.T, root, id, name string) {
	t.Helper()
	raw := []byte(`{"version":2,"teams":[{"id":"` + id + `","name":"` + name + `"}]}`)
	if err := os.WriteFile(filepath.Join(filepath.Dir(root), "teams.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func dealDef() *spec.Bundle {
	return &spec.Bundle{App: &spec.App{Records: map[string]*spec.RecordType{
		"deal": {Title: "name", Fields: map[string]*spec.Field{
			"name":     {Type: spec.FieldText},
			"likely":   {Type: spec.FieldEnum},
			"stage":    {Type: spec.FieldEnum},
			"amount":   {Type: spec.FieldMoney},
			"weighted": {Type: spec.FieldMoney, Computed: "amount"},
		}},
	}}}
}

func deal(id, name, stage string, amount float64) bridge.Record {
	return bridge.Record{ID: id, Type: "deal", Fields: map[string]any{"name": name, "stage": stage, "likely": "Likely", "amount": amount}}
}

func editor() svc.Caller { return svc.Caller{Member: "me", Role: svc.RoleEditor} }

func scope(app string) svc.Scope {
	return svc.Scope{App: app, Caller: editor(), Data: svc.DataReal}
}

type memStore struct{ w *linkWorld }

func (m *memStore) List(context.Context, svc.Caller) ([]bridge.AppSummary, error) {
	var rows []bridge.AppSummary
	for id, detail := range m.w.apps {
		if m.w.hide[id] {
			continue
		}
		rows = append(rows, detail.AppSummary)
	}
	return rows, nil
}
func (m *memStore) Get(_ context.Context, app string) (bridge.AppDetail, error) {
	detail, ok := m.w.apps[app]
	if !ok {
		return bridge.AppDetail{}, bridge.Errorf(bridge.CodeNotFound, "no app "+app)
	}
	return detail, nil
}
func (m *memStore) Definition(_ context.Context, app string) (*spec.Bundle, error) {
	return m.w.defs[app], nil
}
func (m *memStore) Create(context.Context, svc.Caller, string, string) (bridge.AppSummary, error) {
	return bridge.AppSummary{}, bridge.NotBuilt("store")
}
func (m *memStore) WriteFiles(context.Context, string, spec.Files) error {
	return bridge.NotBuilt("store")
}
func (m *memStore) SetState(context.Context, string, bridge.AppState) error {
	return bridge.NotBuilt("store")
}
func (m *memStore) Keep(context.Context, string) (bridge.AppSummary, error) {
	return bridge.AppSummary{}, bridge.NotBuilt("store")
}
func (m *memStore) Discard(context.Context, string) error { return bridge.NotBuilt("store") }

type memRecords struct{ rows map[string]bridge.Record }

func (m *memRecords) Get(_ context.Context, scope svc.Scope, p bridge.GetRecordParams) (bridge.Record, error) {
	row, ok := m.rows[scope.App+"/"+p.Type+"/"+p.ID]
	if !ok {
		return bridge.Record{}, bridge.Errorf(bridge.CodeNotFound, "no "+p.Type+" "+p.ID)
	}
	return row, nil
}
func (m *memRecords) Query(context.Context, svc.Scope, bridge.QueryParams) (bridge.QueryResult, error) {
	return bridge.QueryResult{}, bridge.NotBuilt("records")
}
func (m *memRecords) Create(context.Context, svc.Scope, bridge.CreateRecordParams) (bridge.Record, error) {
	return bridge.Record{}, bridge.NotBuilt("records")
}
func (m *memRecords) Update(context.Context, svc.Scope, bridge.UpdateRecordParams) (bridge.UpdateRecordResult, error) {
	return bridge.UpdateRecordResult{}, bridge.NotBuilt("records")
}
func (m *memRecords) Delete(context.Context, svc.Scope, bridge.DeleteRecordParams) (bridge.DeleteRecordResult, error) {
	return bridge.DeleteRecordResult{}, bridge.NotBuilt("records")
}
func (m *memRecords) Seed(context.Context, string, map[string][]bridge.Record) error {
	return bridge.NotBuilt("records")
}

type memCaps struct{ deny map[string]bool }

func (m *memCaps) Allow(_ context.Context, _ svc.Scope, _ string, raw json.RawMessage) error {
	var p struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(raw, &p)
	if m.deny[p.Type] {
		return bridge.Errorf(bridge.CodeDenied, "no access")
	}
	return nil
}

type memHub struct{ events []svc.Event }

func (h *memHub) Publish(topic string, data any) {
	h.events = append(h.events, svc.Event{Topic: topic, Data: data})
}
func (h *memHub) Subscribe(string) (<-chan svc.Event, func()) {
	return make(chan svc.Event), func() {}
}
