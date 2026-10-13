package records

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type definitionStore struct {
	svc.Store
	def *spec.Bundle
}

func (s definitionStore) Definition(context.Context, string) (*spec.Bundle, error) { return s.def, nil }

type captureHub struct {
	mu     sync.Mutex
	events []svc.Event
}

func (h *captureHub) Publish(topic string, data any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, svc.Event{Topic: topic, Data: data})
}
func (*captureHub) Subscribe(string) (<-chan svc.Event, func()) {
	return make(chan svc.Event), func() {}
}
func setup(t *testing.T) (*Service, svc.Scope, *captureHub, map[string][]bridge.Record) {
	t.Helper()
	files, sample := spec.GoldenCRM()
	def, err := spec.Decode(files)
	if err != nil {
		t.Fatal(err)
	}
	var rows map[string][]bridge.Record
	if err := json.Unmarshal(sample, &rows); err != nil {
		t.Fatal(err)
	}
	hub := &captureHub{}
	all := &svc.Services{Store: definitionStore{def: def}, Hub: hub}
	s := New(svc.Config{Root: t.TempDir()}, all)
	if err := os.MkdirAll(filepath.Join(s.cfg.Root, "crm", "definition"), 0700); err != nil {
		t.Fatal(err)
	}
	scope := svc.Scope{App: "crm", Data: svc.DataSample, Caller: svc.Caller{Member: svc.Owner}}
	if err := s.Seed(context.Background(), scope.App, rows); err != nil {
		t.Fatal(err)
	}
	hub.events = nil
	return s, scope, hub, rows
}
func needCode(t *testing.T, err error, code bridge.ErrorCode) {
	t.Helper()
	var e *bridge.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("error = %v, want %s", err, code)
	}
}
func read(t *testing.T, s *Service, scope svc.Scope, typ, id string) bridge.Record {
	t.Helper()
	row, err := s.Get(context.Background(), scope, bridge.GetRecordParams{Type: typ, ID: id})
	if err != nil {
		t.Fatal(err)
	}
	return row
}
func TestCRUDHistoryAndIsolation(t *testing.T) {
	s, scope, hub, _ := setup(t)
	ctx := context.Background()
	real := scope
	real.Data = svc.DataReal
	page, err := s.Query(ctx, real, bridge.QueryParams{Type: "deal"})
	if err != nil || page.Total != 0 {
		t.Fatalf("real should start empty: %+v %v", page, err)
	}
	set := map[string]any{"name": "Cedar Works"}
	company, err := s.Create(ctx, real, bridge.CreateRecordParams{Type: "company", Set: set})
	if err != nil {
		t.Fatal(err)
	}
	set["name"] = "mutated caller map"
	if company.Fields["owner"] != "me" || company.Version != 1 {
		t.Fatalf("defaults = %+v", company)
	}
	readback := read(t, s, real, "company", company.ID)
	if readback.Fields["name"] != "Cedar Works" {
		t.Fatal("caller mutated persisted fields")
	}
	updated, err := s.Update(ctx, real, bridge.UpdateRecordParams{Type: "company", ID: company.ID, Set: map[string]any{"name": "Cedar Systems"}})
	if err != nil || updated.Rows[0].Version != 2 {
		t.Fatalf("update = %+v %v", updated, err)
	}
	if _, err := s.Delete(ctx, real, bridge.DeleteRecordParams{Type: "company", ID: company.ID}); err != nil {
		t.Fatal(err)
	}
	_, err = s.Get(ctx, real, bridge.GetRecordParams{Type: "company", ID: company.ID})
	needCode(t, err, bridge.CodeNotFound)
	db, err := sql.Open("sqlite", filepath.Join(s.cfg.Root, "crm", "records.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT op,by,before,after FROM _history ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for i, want := range []string{"create", "update", "delete"} {
		if !rows.Next() {
			t.Fatal("missing history")
		}
		var op, by string
		var before, after sql.NullString
		if err := rows.Scan(&op, &by, &before, &after); err != nil {
			t.Fatal(err)
		}
		if op != want || by != "me" || before.Valid != (i > 0) || after.Valid != (i < 2) {
			t.Fatalf("history: %s %s %v %v", op, by, before, after)
		}
	}
	if rows.Next() || rows.Err() != nil {
		t.Fatal("unexpected history")
	}
	page, err = s.Query(ctx, scope, bridge.QueryParams{Type: "company"})
	if err != nil || page.Total != 5 {
		t.Fatalf("sample was changed: %+v %v", page, err)
	}
	if len(hub.events) != 3 {
		t.Fatalf("events = %d", len(hub.events))
	}
	for _, event := range hub.events {
		if event.Topic != bridge.TopicRecords("crm", "company") {
			t.Fatal(event.Topic)
		}
	}
}
func TestComputedAndTransactionalBatches(t *testing.T) {
	s, scope, hub, _ := setup(t)
	ctx := context.Background()
	before := read(t, s, scope, "deal", "dl_northwind")
	if before.Fields["weighted"] != float64(24000) {
		t.Fatalf("weighted = %v", before.Fields["weighted"])
	}
	_, err := s.Update(ctx, scope, bridge.UpdateRecordParams{Type: "deal", ID: "dl_northwind", Set: map[string]any{"weighted": 1}})
	needCode(t, err, bridge.CodeInvalid)
	_, err = s.Update(ctx, scope, bridge.UpdateRecordParams{Type: "deal", IDs: []string{"dl_northwind", "absent"}, Set: map[string]any{"stage": "Won"}})
	needCode(t, err, bridge.CodeNotFound)
	after := read(t, s, scope, "deal", "dl_northwind")
	if !reflect.DeepEqual(before, after) || len(hub.events) != 0 {
		t.Fatal("partial batch escaped its transaction")
	}
	_, err = s.Update(ctx, scope, bridge.UpdateRecordParams{Type: "deal", ID: "dl_northwind", Set: map[string]any{"stage": "Won"}})
	if err != nil {
		t.Fatal(err)
	}
	after = read(t, s, scope, "deal", "dl_northwind")
	if after.Fields["weighted"] != float64(48000) {
		t.Fatal(after)
	}
	// A person's edit pins an AI value by clearing its label.
	after.AI = []string{"likely"}
	rows := map[string][]bridge.Record{"deal": {after}}
	_, _, _, original := setup(t)
	rows["company"], rows["contact"] = original["company"], original["contact"]
	if err := s.Seed(ctx, "crm", rows); err != nil {
		t.Fatal(err)
	}
	_, err = s.Update(ctx, scope, bridge.UpdateRecordParams{Type: "deal", ID: after.ID, Set: map[string]any{"likely": "Maybe"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(read(t, s, scope, "deal", after.ID).AI) != 0 {
		t.Fatal("person did not pin AI field")
	}
}
func TestValidationRelationsAndUniqueConstraints(t *testing.T) {
	s, scope, hub, _ := setup(t)
	ctx := context.Background()
	tests := []struct {
		name, typ, id string
		set           map[string]any
		code          bridge.ErrorCode
	}{
		{"unknown", "deal", "dl_hooli", map[string]any{"x": "oops"}, bridge.CodeInvalid},
		{"enum", "deal", "dl_hooli", map[string]any{"stage": "Outside"}, bridge.CodeInvalid},
		{"money", "deal", "dl_hooli", map[string]any{"amount": "1 OR 1=1"}, bridge.CodeInvalid},
		{"required", "deal", "dl_hooli", map[string]any{"name": nil}, bridge.CodeInvalid},
		{"reference", "deal", "dl_hooli", map[string]any{"company": "absent"}, bridge.CodeInvalid},
		{"refs", "deal", "dl_hooli", map[string]any{"contacts": []string{"absent"}}, bridge.CodeInvalid},
		{"date", "deal", "dl_hooli", map[string]any{"close_on": "2026-02-30"}, bridge.CodeInvalid},
		{"email", "contact", "ct_priya", map[string]any{"email": "not email"}, bridge.CodeInvalid},
		{"url", "company", "co_globex", map[string]any{"domain": "javascript:alert(1)"}, bridge.CodeInvalid},
		{"unique", "company", "co_globex", map[string]any{"name": "Northwind"}, bridge.CodeConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := s.Update(ctx, scope, bridge.UpdateRecordParams{Type: test.typ, ID: test.id, Set: test.set})
			needCode(t, err, test.code)
		})
	}
	if len(hub.events) != 0 {
		t.Fatal("rejected writes emitted events")
	}
	_, err := s.Delete(ctx, scope, bridge.DeleteRecordParams{Type: "company", ID: "co_northwind"})
	needCode(t, err, bridge.CodeConflict)
	_, err = s.Delete(ctx, scope, bridge.DeleteRecordParams{Type: "deal", IDs: []string{"dl_hooli", "absent"}})
	needCode(t, err, bridge.CodeNotFound)
	_ = read(t, s, scope, "deal", "dl_hooli")
	for _, p := range []bridge.UpdateRecordParams{{Type: "deal"}, {Type: "deal", ID: "a", IDs: []string{"b"}}, {Type: "deal", IDs: []string{"a", "a"}}} {
		_, err := s.Update(ctx, scope, p)
		needCode(t, err, bridge.CodeInvalid)
	}
}
func TestSeedAtomicReplacementAndIndexedColumns(t *testing.T) {
	s, scope, _, original := setup(t)
	ctx := context.Background()
	broken := map[string][]bridge.Record{"deal": {{ID: "bad", Fields: map[string]any{"name": "Bad", "company": "absent"}}}}
	needCode(t, s.Seed(ctx, "crm", broken), bridge.CodeInvalid)
	page, err := s.Query(ctx, scope, bridge.QueryParams{Type: "deal"})
	if err != nil || page.Total != 6 {
		t.Fatalf("failed seed erased rows: %+v %v", page, err)
	}
	if err := s.Seed(ctx, "crm", original); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.cfg.Root, "crm", "records.db")); !os.IsNotExist(err) {
		t.Fatal("seed touched real database")
	}
	db, err := sql.Open("sqlite", filepath.Join(s.cfg.Root, "crm", "sample.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var ddl string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='records_deal'`).Scan(&ddl); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='index_deal_stage'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("missing generated index: %s %v", ddl, err)
	}
	if err := s.Seed(ctx, "crm", map[string][]bridge.Record{}); err != nil {
		t.Fatal(err)
	}
	page, err = s.Query(ctx, scope, bridge.QueryParams{Type: "deal"})
	if err != nil || page.Total != 0 {
		t.Fatal("seed did not replace absent types")
	}
}
func TestRulesDependenciesAndClock(t *testing.T) {
	typ := &spec.RecordType{Fields: map[string]*spec.Field{
		"amount": {Type: spec.FieldMoney}, "a_total": {Type: spec.FieldMoney, Computed: "z_double + 1.0"}, "z_double": {Type: spec.FieldMoney, Computed: "amount * 2.0"}, "age": {Type: spec.FieldNumber, Computed: "days_since(created_at)"}, "date": {Type: spec.FieldDatetime, Computed: "now()"},
	}}
	rules, err := NewRules(typ)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	row, err := rules.Prepare(bridge.Record{ID: "one", CreatedAt: "2026-10-10T00:00:00Z", Fields: map[string]any{"amount": 10}}, "me", at, true)
	if err != nil {
		t.Fatal(err)
	}
	if row.Fields["a_total"] != float64(21) || row.Fields["age"] != float64(3) {
		t.Fatal(row.Fields)
	}
	typ.Fields["z_double"].Computed = "a_total"
	_, err = NewRules(typ)
	needCode(t, err, bridge.CodeInvalid)
	s, scope, hub, original := setup(t)
	def := s.all.Store.(definitionStore).def
	def.App.Records["deal"].Fields["age"] = &spec.Field{Type: spec.FieldNumber, Computed: "days_since(created_at)"}
	for i := range original["deal"] {
		original["deal"][i].CreatedAt = "2026-10-10T00:00:00Z"
	}
	if err := s.Seed(context.Background(), scope.App, original); err != nil {
		t.Fatal(err)
	}
	hub.events = nil
	if err := s.Refresh(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	if row := read(t, s, scope, "deal", "dl_hooli"); row.Fields["age"] != float64(3) {
		t.Fatal(row.Fields)
	}
	if len(hub.events) != 6 {
		t.Fatalf("clock events = %d", len(hub.events))
	}
	if err := s.Refresh(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	if len(hub.events) != 6 {
		t.Fatal("same clock wrote again")
	}
}

func TestConcurrentWritesPersistAcrossRestart(t *testing.T) {
	s, scope, hub, _ := setup(t)
	scope.Data = svc.DataReal
	start := make(chan struct{})
	results := make(chan error, 16)
	for i := range 16 {
		go func() {
			<-start
			_, err := s.Create(context.Background(), scope, bridge.CreateRecordParams{Type: "company", Set: map[string]any{"name": string(rune('A' + i))}})
			results <- err
		}()
	}
	close(start)
	for range 16 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	restarted := New(s.cfg, s.all)
	page, err := restarted.Query(context.Background(), scope, bridge.QueryParams{Type: "company"})
	if err != nil || page.Total != 16 || len(hub.events) != 16 {
		t.Fatalf("restart: %v %v events=%d", page, err, len(hub.events))
	}
}
func TestStoredQueryPagingCountsAndDatetimes(t *testing.T) {
	s, scope, _, _ := setup(t)
	ctx := context.Background()
	first, err := s.Query(ctx, scope, bridge.QueryParams{Type: "deal", Query: "stage:-Lost sort:-amount", Limit: 2, CountBy: "stage"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 6 || len(first.Rows) != 2 || first.Next == "" || first.Counts["Proposal"] != 2 {
		t.Fatal(first)
	}
	second, err := s.Query(ctx, scope, bridge.QueryParams{Type: "deal", Query: "stage:-Lost sort:-amount", Limit: 2, After: first.Next, CountBy: "stage"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Total != 6 || second.Rows[0].ID == first.Rows[0].ID || !reflect.DeepEqual(first.Counts, second.Counts) {
		t.Fatal(second)
	}
	for _, p := range []bridge.QueryParams{{Type: "deal", Limit: 101}, {Type: "deal", After: "malformed"}, {Type: "deal", CountBy: "amount"}, {Type: `deal; DROP TABLE records_deal`}, {Type: "deal", Query: "sort:unknown"}} {
		if _, err := s.Query(ctx, scope, p); err == nil {
			t.Fatal("accepted invalid query", p)
		}
	}
	_, err = s.Update(ctx, scope, bridge.UpdateRecordParams{Type: "deal", ID: "dl_hooli", Set: map[string]any{"last_touch": "2026-10-13T02:00:00.100+02:00"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Query(ctx, scope, bridge.QueryParams{Type: "deal", Query: "last_touch>2026-10-13T00:00:00Z"})
	if err != nil || len(result.Rows) != 1 || result.Rows[0].ID != "dl_hooli" {
		t.Fatalf("datetime: %+v %v", result, err)
	}
}
func TestAgentLabelsAndPins(t *testing.T) {
	s, scope, hub, original := setup(t)
	ctx := context.Background()
	original["deal"][0].AI = []string{"likely"}
	if err := s.Seed(ctx, "crm", original); err != nil {
		t.Fatal(err)
	}
	hub.events = nil
	_, err := s.Update(ctx, scope, bridge.UpdateRecordParams{Type: "deal", ID: "dl_northwind", Set: map[string]any{"likely": "Maybe"}})
	if err != nil {
		t.Fatal(err)
	}
	agent := scope
	agent.Caller.Agent = true
	result, err := s.Update(ctx, agent, bridge.UpdateRecordParams{Type: "deal", ID: "dl_northwind", Set: map[string]any{"likely": "Unlikely", "name": "Updated by agent"}})
	if err != nil {
		t.Fatal(err)
	}
	row := result.Rows[0]
	if row.Fields["likely"] != "Maybe" || !reflect.DeepEqual(row.AI, []string{"name"}) {
		t.Fatal(row)
	}
	row.Fields["name"] = "mutated result"
	diff := hub.events[len(hub.events)-1].Data.(bridge.RecordDiff)
	if diff.Record.Fields["name"] != "Updated by agent" {
		t.Fatal("result mutated a published event")
	}
}

func TestComputedReferencesCountAndOptionalInputs(t *testing.T) {
	typ := &spec.RecordType{Fields: map[string]*spec.Field{
		"amount": {Type: spec.FieldMoney}, "contacts": {Type: spec.FieldPeople},
		"total":   {Type: spec.FieldMoney, Computed: "$record.amount * 2.0"},
		"owner":   {Type: spec.FieldPerson, Computed: "$me"},
		"instant": {Type: spec.FieldDatetime, Computed: "$now"},
		"count":   {Type: spec.FieldNumber, Computed: "count(contacts)"},
		"literal": {Type: spec.FieldText, Computed: "'$me'"},
	}}
	rules, err := NewRules(typ)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	row, err := rules.Prepare(bridge.Record{Fields: map[string]any{"contacts": []string{"me"}}}, "owner", at, true)
	if err != nil {
		t.Fatal(err)
	}
	if row.Fields["total"] != nil || row.Fields["owner"] != "owner" || row.Fields["count"] != float64(1) || row.Fields["literal"] != "$me" {
		t.Fatal(row.Fields)
	}
}
