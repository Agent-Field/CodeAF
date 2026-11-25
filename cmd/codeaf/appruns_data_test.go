package main

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
)

func TestAppRunsWakesFromLiveDataCommit(t *testing.T) {
	place := data.NewPlace(t.TempDir(), data.Options{NoRefresh: true})
	defer place.Close()
	records := runs.Data{Store: place.Main}
	db, err := place.Main()
	if err != nil {
		t.Fatal(err)
	}
	const app = `{"id":"crm","records":{"deal":{"title":"name","fields":{
		"name":{"type":"text"},"stage":{"type":"text"},"touched":{"type":"bool"}}}},
		"capabilities":{"records":{"deal":"rw"}}}`
	if err := db.RegisterApp([]byte(app)); err != nil {
		t.Fatal(err)
	}
	deal, err := db.Create(data.Scope{}, "deal", []map[string]any{{"name": "Northstar", "stage": "Lead"}})
	if err != nil {
		t.Fatal(err)
	}
	id := deal.Rows[0].ID()
	started := make(chan string, 1)
	e, err := runs.Open(runs.Config{
		Root: t.TempDir(), Records: records.Write, RecordInput: records.RecordInput,
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
		Changed: func(ev runs.ChangeEvent) {
			if ev.State == runs.Running && ev.Step == "touch" {
				select {
				case started <- ev.ID:
				default:
				}
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	ctx, cancel := context.WithCancel(t.Context())
	stop, done := watchAppRunCommits(ctx, place, e)
	defer func() {
		cancel()
		stop()
		<-done
	}()
	_, err = db.Update(data.Scope{}, "deal", []string{id}, map[string]any{"stage": "Won"}, data.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var runID string
	select {
	case runID = <-started:
	case <-ctx.Done():
		t.Fatal("the commit did not start a run")
	}
	if err := e.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := e.Get(runID)
	if err != nil || got.State != runs.Done {
		t.Fatalf("run = %+v, %v", got, err)
	}
	row, err := db.Get(data.Scope{}, "deal", id, false)
	if err != nil || row["touched"] != true {
		t.Fatalf("row = %+v, %v", row, err)
	}
}
