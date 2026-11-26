package runs_test

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
)

func TestSourceTriggeredRunKeepsCausationAcrossRestart(t *testing.T) {
	db, err := data.Open(filepath.Join(t.TempDir(), "data.db"), data.Options{NoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const app = `{"id":"crm","records":{"deal":{"title":"name","fields":{
		"name":{"type":"text"},"stage":{"type":"text"},"touched":{"type":"bool"}}}},
		"capabilities":{"records":{"deal":"rw"}}}`
	if err := db.RegisterApp([]byte(app)); err != nil {
		t.Fatal(err)
	}
	created, err := db.Create(data.Scope{}, "deal", []map[string]any{{"name": "Northstar"}})
	if err != nil {
		t.Fatal(err)
	}
	id := created.Rows[0].ID()
	if err := db.BindCommonType("deal", "deal"); err != nil {
		t.Fatal(err)
	}
	key := data.ExternalKey{Binding: "crm-sync", Object: "deals", Key: "northstar", Generation: 1}
	if _, err := db.LinkIdentity("deal", key, id); err != nil {
		t.Fatal(err)
	}
	adapter := runs.Data{Store: func() (*data.Store, error) { return db, nil }}
	cfg := runs.Config{
		Root: t.TempDir(), Records: adapter.Write, RecordInput: adapter.RecordInput,
		Load: func(_, _ string) (runs.Definition, error) {
			return runs.Parse([]byte(`
run: won
steps:
  - id: touch
    records.update: {id: $record.id, set: {touched: true}}
`))
		},
		Triggers: func() ([]runs.Trigger, error) {
			return []runs.Trigger{{App: "crm", ID: "won", Run: "won", On: "record.changed", Type: "deal",
				When: "changed('stage') && stage == 'Won'"}}, nil
		},
	}
	e, err := runs.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	if err := adapter.ConsumeOutbox(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	// An observation with no native contribution must still advance the cursor.
	if _, err := db.ObserveSource(data.SourceObservation{Key: key, Event: "freshness-only", Revision: "0"}); err != nil {
		t.Fatal(err)
	}
	observation := data.SourceObservation{Key: key, Event: "source-event-1", Revision: "1", NativeValues: true,
		CauseChain: append(slices.Repeat([]string{"earlier"}, 64), "refresh", "crm-sync"),
		Claims:     map[string]data.SourceFieldClaim{"stage": {Path: "stage", Value: "Won"}}}
	for range 2 {
		if _, err := db.ObserveSource(observation); err != nil {
			t.Fatal(err)
		}
	}
	// The live value has already moved on when the trigger reads the outbox.
	if _, err := db.Update(data.Scope{}, "deal", []string{id}, map[string]any{"stage": "Review"}, data.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.ConsumeOutbox(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	e, err = runs.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	after, err := db.Cursor()
	if err != nil {
		t.Fatal(err)
	}
	tick(t, e)
	if err := adapter.ConsumeOutbox(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	list, err := e.List("crm", "won")
	if err != nil || len(list) != 1 || list[0].State != runs.Done {
		t.Fatalf("runs = %+v, %v", list, err)
	}
	events, err := db.Outbox(after, 2)
	if err != nil || len(events) != 1 {
		t.Fatalf("run write events = %+v, %v", events, err)
	}
	want := append(slices.Repeat([]string{"earlier"}, 61), "refresh", "crm-sync", "run:"+list[0].ID)
	if !slices.Equal(events[0].CauseChain, want) || events[0].SourceEvent != "" {
		t.Fatalf("run write causation = %+v, want %v and an empty source event ID", events[0], want)
	}
}

func TestAppRunsDataAdapterScopesRefsAndCommittedRows(t *testing.T) {
	store, err := data.Open(filepath.Join(t.TempDir(), "data.db"), data.Options{NoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const app = `{"id":"crm","records":{"deal":{"title":"name","fields":{
		"name":{"type":"text"},"secret":{"type":"text","masked":true},
		"stage":{"type":"enum","values":["Lead","Won"]}}}},
		"capabilities":{"records":{"deal":"rw"}}}`
	if err := store.RegisterApp([]byte(app)); err != nil {
		t.Fatal(err)
	}
	created, err := store.Create(data.Scope{}, "deal", []map[string]any{{"name": "Northstar", "secret": "hidden", "stage": "Lead"}})
	if err != nil {
		t.Fatal(err)
	}
	id := created.Rows[0].ID()
	adapter := runs.Data{Store: func() (*data.Store, error) { return store, nil }}
	params, err := adapter.ResolveParams("crm", map[string]runs.Parameter{"deal": {Type: "ref", To: "deal"}}, map[string]any{"deal": id})
	if err != nil {
		t.Fatal(err)
	}
	row := params["deal"].(map[string]any)
	if row["name"] != "Northstar" || row["secret"] != nil {
		t.Fatalf("reference escaped scope: %+v", row)
	}
	raw := runs.Change{Type: "deal", ID: id, Before: map[string]any{"stage": "Lead", "secret": "old"}, After: map[string]any{"stage": "Won", "secret": "new"}}
	scoped, err := adapter.RecordInput("crm", raw)
	if err != nil || scoped.Before["secret"] != nil || scoped.After["secret"] != nil || scoped.After["stage"] != "Won" {
		t.Fatalf("trigger record escaped scope: %+v %v", scoped, err)
	}
	commit := make(chan data.Change, 1)
	stop := store.OnCommit(func(c data.Change) {
		if c.Actor == "run:r1" {
			commit <- c
		}
	})
	defer stop()
	_, err = adapter.Write(context.Background(), runs.RecordCall{
		Call: runs.Call{App: "crm", ID: "r1", Name: "records.update"}, Type: "deal",
		IDs: []string{id}, Set: map[string]any{"stage": "Won"},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-commit:
		if got.ByKind != data.ByFlow || len(got.Records) != 1 || got.Records[0].After["stage"] != "Won" {
			t.Fatalf("commit = %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("record commit hook did not fire")
	}
}

func TestAgentActionKeepsItsAuthorAndGrantAcrossRestart(t *testing.T) {
	db, err := data.Open(filepath.Join(t.TempDir(), "data.db"), data.Options{NoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.RegisterApp([]byte(`{"id":"crm","records":{"deal":{"title":"name","fields":{"name":{"type":"text"},"stage":{"type":"text"}}}},"capabilities":{"records":{"deal":"rw"}}}`)); err != nil {
		t.Fatal(err)
	}
	dbWriter := runs.Data{Store: func() (*data.Store, error) { return db, nil }}
	principal := data.AgentPrincipal{ID: "sales-assistant"}
	if err := db.SetAgentGrant("crm", principal, "actions", "make-deal", true); err != nil {
		t.Fatal(err)
	}
	allowed := true
	cfg := runs.Config{Root: t.TempDir(), Records: dbWriter.Write,
		CheckAgentAction: func(_ string, origin runs.AgentActionOrigin) error {
			if !allowed || origin.Principal != principal || origin.Action != "make-deal" {
				return errors.New("grant withdrawn")
			}
			return nil
		},
		Load: func(_, _ string) (runs.Definition, error) {
			return runs.Parse([]byte("run: create\nsteps:\n  - id: make\n    records.create: {type: deal, set: {name: Northwind}}\n"))
		},
	}
	engine, err := runs.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	origin := runs.AgentActionOrigin{Principal: principal, Action: "make-deal", Cause: "request-1", Version: 1}
	if _, err := engine.StartAgentAction("crm", "create", nil, origin); err != nil {
		t.Fatal(err)
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	engine, err = runs.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	tick(t, engine)
	page, err := db.Query(data.Scope{}, "deal", "", "", 10)
	if err != nil || len(page.Rows) != 1 {
		t.Fatalf("records = %#v, %v", page, err)
	}
	history, err := db.History(data.Scope{}, "deal", page.Rows[0].ID())
	if err != nil || len(history) != 1 || history[0].ByKind != data.ByAI || history[0].Actor != "agent:sales-assistant" || history[0].Cause != "request-1" {
		t.Fatalf("history = %#v, %v", history, err)
	}
	run, err := engine.StartAgentAction("crm", "create", nil, origin)
	if err != nil {
		t.Fatal(err)
	}
	allowed = false
	tick(t, engine)
	got, err := engine.Get(run.ID)
	if err != nil || got.State != runs.Incomplete || got.Error != "grant withdrawn" {
		t.Fatalf("revoked run = %#v, %v", got, err)
	}
}

func TestAgentPromptRendersAnUnsetDeclaredSubjectField(t *testing.T) {
	db, err := data.Open(filepath.Join(t.TempDir(), "data.db"), data.Options{NoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.RegisterApp([]byte(`{"id":"marketing","records":{"post":{"fields":{"text":{"type":"text"},"topic":{"type":"text"},"secret":{"type":"text","masked":true}}}},"capabilities":{"records":{"post":"rw"}}}`)); err != nil {
		t.Fatal(err)
	}
	created, err := db.Create(data.Scope{}, "post", []map[string]any{{"text": "Launch news", "secret": "hidden"}})
	if err != nil {
		t.Fatal(err)
	}
	const workflow = `
run: example
params: {post: {type: ref, to: post}}
steps:
  - id: reply
    agent: writer
    prompt: "Draft a reply to {{text}}. Topic: {{topic}}{{post.topic}}"
`
	adapter := runs.Data{Store: func() (*data.Store, error) { return db, nil }}
	e := newEngine(t, workflow, runs.Config{
		ResolveParams: adapter.ResolveParams,
		Agent: func(_ context.Context, call runs.AgentCall) (runs.Reply, error) {
			if call.Prompt != "Draft a reply to Launch news. Topic: " {
				t.Fatalf("prompt = %q", call.Prompt)
			}
			return runs.Reply{Output: "Reply"}, nil
		},
	})
	run, err := e.Start("marketing", "example", map[string]any{"post": created.Rows[0].ID()})
	if err != nil {
		t.Fatal(err)
	}
	tick(t, e)
	if got := state(t, e, run.ID); got.State != runs.Done {
		t.Fatalf("run ended at %s with %s", got.State, got.Error)
	}
}

func TestIntakeRunsStartOnceAndFillFieldsAsAI(t *testing.T) {
	db, err := data.Open(filepath.Join(t.TempDir(), "data.db"), data.Options{NoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const app = `{"id":"crm","records":{"contact":{"title":"name","fields":{
		"name":{"type":"text"},"summary":{"type":"text","ai":"Extract a short summary"}}}},
		"capabilities":{"records":{"contact":"rw"}}}`
	if err := db.RegisterApp([]byte(app)); err != nil {
		t.Fatal(err)
	}
	adapter := runs.Data{Store: func() (*data.Store, error) { return db, nil }}
	var calls []runs.AgentCall
	cfg := runs.Config{
		Root: t.TempDir(), Records: adapter.Write, RecordInput: adapter.RecordInput,
		Agent: func(_ context.Context, call runs.AgentCall) (runs.Reply, error) {
			calls = append(calls, call)
			return runs.Reply{Output: "Parsed from the attachment"}, nil
		},
		Load: func(app, run string) (runs.Definition, error) {
			if app != "crm" || run != "extract" {
				t.Fatalf("unexpected run %s/%s", app, run)
			}
			return runs.Parse([]byte(`
run: extract
steps:
  - id: read
    agent: self
    prompt: "Summarize the attachment from {{input.channel}}"
  - id: fill
    records.update: {id: $record.id, set: {summary: $steps.read.output}}
`))
		},
	}
	e, err := runs.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	if err := adapter.ConsumeOutbox(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	in := data.IntakeDelivery{App: "crm", Channel: "drop", Run: "extract", Type: "contact",
		Key: "dropped-1", CSV: []byte("name\nNew contact\nAnother contact\n")}
	made, err := db.Deliver(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.ConsumeOutbox(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	e, err = runs.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Deliver(in); err != nil {
		t.Fatal(err)
	}
	if err := adapter.ConsumeOutbox(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	tick(t, e)
	if len(calls) != 2 || made.Created != 2 {
		t.Fatalf("agent calls = %+v, delivery = %+v", calls, made)
	}
	seen := map[string]int{}
	for _, call := range calls {
		if call.IntakeType != "contact" || call.Prompt != "Summarize the attachment from drop" {
			t.Fatalf("agent call = %+v", call)
		}
		seen[call.IntakeRecord]++
	}
	for _, id := range made.Rows {
		history, err := db.History(data.Scope{}, "contact", id)
		if err != nil || seen[id] != 1 || len(history) != 2 || history[0].ByKind != data.ByAI ||
			history[0].After["summary"] != "Parsed from the attachment" || history[1].Cause != "channel:drop" {
			t.Fatalf("record %s: calls %d, history %+v, %v", id, seen[id], history, err)
		}
	}
}
