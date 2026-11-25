package runs_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
)

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
