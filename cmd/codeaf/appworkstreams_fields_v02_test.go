package main

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/workstream"
)

// WP-F2 (a): real plan_set/get dispatch preserves draft fields and refuses atomically.
func TestAppWorkstreamDraftFieldsWireRoundTrip(t *testing.T) {
	b, rec, _, _ := workstreamBackend(t)
	w := workstreamCreateForTest(t, b, "Draft wire", workstreamJobRef("7"))
	connection := workstreamWireConnect(t, &workstreamWireBackend{appBackend: b})
	item := map[string]any{"kind": "draft", "draft_id": "d", "title": "Draft", "size": "M", "budget_usd": 25.0, "source": map[string]any{"kind": "message", "id": "m", "chat_id": "chat", "label": "Scope"}}
	params := map[string]any{"id": w.ID, "base_version": 0, "milestones": []any{}, "waves": []any{map[string]any{"n": 1, "auto_start": false, "items": []any{item}}}}
	set := workstreamWireResult(t, connection.call(t, "workstream.plan_set", params))
	get := workstreamWireResult(t, connection.call(t, "workstream.plan_get", map[string]any{"id": w.ID}))
	if !reflect.DeepEqual(set, get) {
		t.Fatalf("round trip: %v %v", set, get)
	}
	draft := get.(map[string]any)["plan"].(map[string]any)["waves"].([]any)[0].(map[string]any)["items"].([]any)[0]
	want := workstreamWireDecode(t, json.RawMessage(`{"kind":"draft","draft_id":"d","title":"Draft","size":"M","budget_usd":25,"source":{"kind":"message","id":"m","chat_id":"chat","label":"Scope"}}`))
	if !reflect.DeepEqual(draft, want) {
		t.Fatalf("draft fields: %v", draft)
	}
	before := len(workstreamEvents(rec))
	for _, invalid := range []map[string]any{
		{"kind": "job", "factory": "f", "job": 7, "size": "S"},
		{"kind": "job", "factory": "f", "job": 7, "budget_usd": 1},
		{"kind": "job", "factory": "f", "job": 7, "source": map[string]any{"kind": "chat", "id": "c"}},
		{"kind": "draft", "draft_id": "d", "title": "Draft", "size": "XL"},
		{"kind": "draft", "draft_id": "d", "title": "Draft", "budget_usd": 0},
		{"kind": "draft", "draft_id": "d", "title": "Draft", "budget_usd": -1},
		{"kind": "draft", "draft_id": "d", "title": "Draft", "budget_usd": 10000.01},
		{"kind": "draft", "draft_id": "d", "title": "Draft", "source": map[string]any{"kind": "message", "id": "m"}},
	} {
		params["base_version"] = 1
		params["waves"] = []any{map[string]any{"n": 1, "auto_start": false, "items": []any{invalid}}}
		frame := connection.call(t, "workstream.plan_set", params)
		if failure, ok := frame["error"].(map[string]any); !ok || failure["code"] != "bad_params" {
			t.Fatalf("refusal: %v", frame)
		}
		after := workstreamWireResult(t, connection.call(t, "workstream.plan_get", map[string]any{"id": w.ID}))
		if !reflect.DeepEqual(after, get) {
			t.Fatalf("refusal changed plan: %v", after)
		}
	}
	_, err := b.WorkstreamPlanSet(appwire.WorkstreamPlanSetParams{ID: w.ID, BaseVersion: 1, Milestones: []appwire.WorkstreamMilestone{}, Waves: []appwire.WorkstreamWave{{N: 1, Items: []appwire.WorkstreamPlanItem{{Kind: "draft", DraftID: workstreamPtr("d"), Title: workstreamPtr("Draft"), BudgetUSD: workstreamPtr(math.NaN())}}}}})
	workstreamCheckError(t, err, appwire.CodeBadParams)
	after := workstreamWireResult(t, connection.call(t, "workstream.plan_get", map[string]any{"id": w.ID}))
	if !reflect.DeepEqual(after, get) {
		t.Fatalf("NaN refusal changed plan: %v", after)
	}
	if len(workstreamEvents(rec)) != before {
		t.Fatal("refused plan emitted a change")
	}
}

// WP-F2 (b): save/create source belongs to that version, and absent source clears it.
func TestAppWorkstreamPageSourcesThroughWire(t *testing.T) {
	b, _, _, _ := workstreamBackend(t)
	w := workstreamCreateForTest(t, b, "Page source")
	connection := workstreamWireConnect(t, &workstreamWireBackend{appBackend: b})
	source := map[string]any{"kind": "message", "id": "message", "chat_id": "chat", "label": "Scope"}
	doc := map[string]any{"type": "doc", "content": []any{}}
	created := workstreamWireResult(t, connection.call(t, "page.create", map[string]any{"workstream_id": w.ID, "title": "Notes", "source": source, "doc": doc})).(map[string]any)["page"].(map[string]any)
	id := created["id"]
	if !reflect.DeepEqual(created["updated_from"], source) {
		t.Fatalf("create source: %v", created)
	}
	saved := workstreamWireResult(t, connection.call(t, "page.save", map[string]any{"id": id, "base_version": 1, "doc": doc, "source": source})).(map[string]any)["page"].(map[string]any)
	if !reflect.DeepEqual(saved["updated_from"], source) {
		t.Fatalf("save source: %v", saved)
	}
	last := workstreamWireResult(t, connection.call(t, "page.save", map[string]any{"id": id, "base_version": 2, "doc": doc})).(map[string]any)["page"].(map[string]any)
	if _, exists := last["updated_from"]; exists {
		t.Fatalf("source retained: %v", last)
	}
	history := workstreamWireResult(t, connection.call(t, "page.history", map[string]any{"id": id})).(map[string]any)["versions"].([]any)
	if len(history) != 3 {
		t.Fatalf("history: %v", history)
	}
	if _, exists := history[0].(map[string]any)["from"]; exists {
		t.Fatalf("source retained in history: %v", history)
	}
	for _, version := range history[1:] {
		if !reflect.DeepEqual(version.(map[string]any)["from"], source) {
			t.Fatalf("version source: %v", version)
		}
	}
	before := last
	for _, method := range []string{"page.save", "page.create"} {
		params := map[string]any{"id": id, "base_version": 3, "doc": doc, "workstream_id": w.ID, "title": "Invalid", "source": map[string]any{"kind": "message", "id": "m"}}
		frame := connection.call(t, method, params)
		if failure, ok := frame["error"].(map[string]any); !ok || failure["code"] != "bad_params" {
			t.Fatalf("invalid source: %v", frame)
		}
	}
	current := workstreamWireResult(t, connection.call(t, "page.get", map[string]any{"id": id})).(map[string]any)["page"]
	if !reflect.DeepEqual(current, before) {
		t.Fatalf("invalid source changed page: %v", current)
	}
}

// WP-F2 (c): list gets both conversation identities from existing row data.
func TestAppWorkstreamListConversationIDsThroughWire(t *testing.T) {
	b, _, _, origin := workstreamBackend(t)
	first, err := b.WorkstreamCreate(appwire.WorkstreamCreateParams{Name: "With origin", OriginChatID: &origin.ID})
	if err != nil {
		t.Fatal(err)
	}
	second := workstreamCreateForTest(t, b, "Without origin")
	connection := workstreamWireConnect(t, &workstreamWireBackend{appBackend: b})
	rows := workstreamWireResult(t, connection.call(t, "workstream.list", map[string]any{})).(map[string]any)["workstreams"].([]any)
	if len(rows) != 2 {
		t.Fatalf("rows: %v", rows)
	}
	for _, raw := range rows {
		row := raw.(map[string]any)
		get := workstreamWireResult(t, connection.call(t, "workstream.get", map[string]any{"id": row["id"]})).(map[string]any)["workstream"].(map[string]any)
		if row["agent_id"] == "" || row["agent_id"] != get["agent_id"] {
			t.Fatalf("agent id: %v %v", row, get)
		}
		if row["id"] == first.Workstream.ID && row["origin_chat_id"] != origin.ID {
			t.Fatalf("origin: %v", row)
		}
		if row["id"] == second.ID {
			if _, exists := row["origin_chat_id"]; exists {
				t.Fatalf("absent origin: %v", row)
			}
		}
	}
}

// WP-F2 (e): the adapter resolves real sources before any draft can be added.
func TestAppWorkstreamAgentDraftSourceResolution(t *testing.T) {
	b, _, w, door := workstreamAgentFixture(t)
	ctx := context.Background()
	page, err := b.WorkstreamPageCreate(appwire.WorkstreamPageCreateParams{WorkstreamID: w.ID, Title: "Spec"})
	if err != nil {
		t.Fatal(err)
	}
	for _, asked := range []*workstream.Source{nil, {Kind: "page", ID: page.Page.ID}} {
		source, err := door.Source(ctx, asked)
		if err != nil {
			t.Fatal(err)
		}
		want := &workstream.Source{Kind: "chat", ID: w.AgentID}
		if asked != nil {
			want = asked
		}
		if !reflect.DeepEqual(source, want) {
			t.Fatalf("source: %+v", source)
		}
		plan, err := door.Plan(ctx)
		if err != nil {
			t.Fatal(err)
		}
		n := len(plan.Waves) + 1
		plan.Waves = append(plan.Waves, workstream.Wave{N: n, Items: []workstream.PlanItem{{Kind: "draft", DraftID: source.Kind, Title: "Draft", Source: source}}})
		saved, err := door.SetPlan(ctx, plan.Version, plan)
		if err != nil || !reflect.DeepEqual(saved.Waves[n-1].Items[0].Source, want) {
			t.Fatalf("saved source: %+v %v", saved, err)
		}
	}
	before, err := door.Plan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []*workstream.Source{{Kind: "page", ID: "missing"}, {Kind: "factory_job", ID: "99", Factory: "f"}, {Kind: "url", ID: "file:///tmp/source"}, {Kind: "page", ID: page.Page.ID, ChatID: "chat"}} {
		if _, err := door.Source(ctx, source); err == nil {
			t.Fatalf("accepted source: %+v", source)
		}
	}
	after, err := door.Plan(ctx)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("source refusal changed plan: %+v %v", after, err)
	}
}

// WP-F2 (e), (f): agent reads include proposed and kept go-ahead structures.
func TestAppWorkstreamAgentReadsGoAheadRecords(t *testing.T) {
	b, _, w, door := workstreamAgentFixture(t)
	ctx := context.Background()
	ask := session.WorkstreamGoAheadAsk{Scope: "job", Factory: "f", Job: 7, CapUSD: 10, EndsOn: b.now().AddDate(0, 0, 89).Format("2006-01-02"), Why: "Review"}
	proposal, err := door.Propose(ctx, session.WorkstreamProposal{GoAhead: &ask})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"", "goahead"} {
		raw, err := door.Read(ctx, session.WorkstreamReadAsk{What: "records", Kind: kind})
		var records appwire.WorkstreamRecordListResult
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &records); err != nil || len(records.Records) != 1 || records.Records[0].Kind != "goahead" || records.Records[0].GoAhead == nil {
			t.Fatalf("read: %s %v", raw, err)
		}
	}
	kept, err := b.WorkstreamRecordKeep(appwire.WorkstreamRecordKeepParams{ID: proposal.ID})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := door.Read(ctx, session.WorkstreamReadAsk{What: "records"})
	var records appwire.WorkstreamRecordListResult
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &records); err != nil || len(records.Records) != 1 || !reflect.DeepEqual(records.Records[0], kept.Record) {
		t.Fatalf("kept read: %s %v", raw, err)
	}
	grant, err := b.WorkstreamGoAheadList(appwire.WorkstreamGoAheadListParams{WorkstreamID: w.ID})
	if err != nil || len(grant.GoAheads) != 1 || grant.GoAheads[0].EndsAt.Hour() != 23 || grant.GoAheads[0].EndsAt.Minute() != 59 || grant.GoAheads[0].EndsAt.Second() != 59 {
		t.Fatalf("end of day: %+v %v", grant, err)
	}
	ask.EndsOn = b.now().AddDate(0, 0, 90).Format("2006-01-02")
	if _, err := door.Propose(ctx, session.WorkstreamProposal{GoAhead: &ask}); err == nil {
		t.Fatal("90-day end accepted")
	}
}

// WP-F2 (d), (f): real wire dispatch exposes grants, undo and compatibility filters.
func TestAppWorkstreamGoAheadRecordsThroughWire(t *testing.T) {
	b, rec, _, _ := workstreamBackend(t)
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	b.now = func() time.Time { return now }
	w := workstreamCreateForTest(t, b, "Record wire", workstreamJobRef("7"))
	connection := workstreamWireConnect(t, &workstreamWireBackend{appBackend: b})
	proposal := map[string]any{"scope": "job", "factory": "f", "job": 7, "cap_usd": 12.5, "ends_at": now.Add(time.Hour).Format(time.RFC3339)}
	raw := workstreamWireResult(t, connection.call(t, "record.propose", map[string]any{"workstream_id": w.ID, "kind": "goahead", "text": "Why", "goahead": proposal})).(map[string]any)["record"].(map[string]any)
	id := raw["id"]
	if raw["kind"] != "goahead" || raw["state"] != "proposed" || raw["text"] != "Why" {
		t.Fatalf("proposal: %v", raw)
	}
	for _, kind := range []string{"decision", "question"} {
		workstreamWireResult(t, connection.call(t, "record.propose", map[string]any{"workstream_id": w.ID, "kind": kind, "text": kind}))
	}
	for _, tc := range []struct {
		kind    string
		include *bool
		count   int
	}{{"", nil, 2}, {"", workstreamPtr(false), 2}, {"", workstreamPtr(true), 3}, {"goahead", nil, 1}, {"question", workstreamPtr(true), 1}} {
		params := map[string]any{"workstream_id": w.ID}
		if tc.kind != "" {
			params["kind"] = tc.kind
		}
		if tc.include != nil {
			params["include_goaheads"] = *tc.include
		}
		rows := workstreamWireResult(t, connection.call(t, "record.list", params)).(map[string]any)["records"].([]any)
		if len(rows) != tc.count {
			t.Fatalf("filter %+v: %v", tc, rows)
		}
		for _, row := range rows {
			kind := row.(map[string]any)["kind"]
			if tc.kind != "" && kind != tc.kind || tc.kind == "" && (tc.include == nil || !*tc.include) && kind == "goahead" {
				t.Fatalf("filter %+v: %v", tc, rows)
			}
		}
	}
	kept := workstreamWireResult(t, connection.call(t, "record.keep", map[string]any{"id": id})).(map[string]any)["record"].(map[string]any)
	grants := workstreamWireResult(t, connection.call(t, "goahead.list", map[string]any{"workstream_id": w.ID})).(map[string]any)["goaheads"].([]any)
	if len(grants) != 1 || grants[0].(map[string]any)["id"] != kept["goahead_id"] {
		t.Fatalf("keep: %v %v", kept, grants)
	}
	before := len(workstreamEvents(rec))
	again := workstreamWireResult(t, connection.call(t, "record.keep", map[string]any{"id": id})).(map[string]any)["record"]
	grants = workstreamWireResult(t, connection.call(t, "goahead.list", map[string]any{"workstream_id": w.ID})).(map[string]any)["goaheads"].([]any)
	if !reflect.DeepEqual(again, kept) || len(grants) != 1 || len(workstreamEvents(rec)) != before {
		t.Fatal("repeat keep changed state")
	}
	for _, kind := range []string{"", "decision", "goahead"} {
		params := map[string]any{"id": id, "text": "Replacement"}
		if kind != "" {
			params["kind"] = kind
		}
		frame := connection.call(t, "record.supersede", params)
		if failure, ok := frame["error"].(map[string]any); !ok || failure["code"] != "bad_params" {
			t.Fatalf("supersede: %v", frame)
		}
	}
	dropped := workstreamWireResult(t, connection.call(t, "record.drop", map[string]any{"id": id})).(map[string]any)["record"].(map[string]any)
	grants = workstreamWireResult(t, connection.call(t, "goahead.list", map[string]any{"workstream_id": w.ID, "ended": true})).(map[string]any)["goaheads"].([]any)
	if dropped["state"] != "dropped" || dropped["goahead_id"] != kept["goahead_id"] || len(grants) != 1 || grants[0].(map[string]any)["end_reason"] != "revoked" {
		t.Fatalf("drop: %v %v", dropped, grants)
	}
	newKeep := workstreamWireResult(t, connection.call(t, "record.keep", map[string]any{"id": id})).(map[string]any)["record"].(map[string]any)
	if newKeep["goahead_id"] == kept["goahead_id"] {
		t.Fatal("re-keep reused the grant")
	}
	workstreamWireResult(t, connection.call(t, "record.drop", map[string]any{"id": id}))
	workstreamWireResult(t, connection.call(t, "workstream.mark_done", map[string]any{"id": w.ID}))
	frame := connection.call(t, "record.keep", map[string]any{"id": id})
	if failure, ok := frame["error"].(map[string]any); !ok || failure["code"] != "conflict" {
		t.Fatalf("keep done: %v", frame)
	}
	frame = connection.call(t, "record.propose", map[string]any{"workstream_id": w.ID, "kind": "goahead", "text": "Why", "goahead": proposal})
	if failure, ok := frame["error"].(map[string]any); !ok || failure["code"] != "conflict" {
		t.Fatalf("propose done: %v", frame)
	}
}
