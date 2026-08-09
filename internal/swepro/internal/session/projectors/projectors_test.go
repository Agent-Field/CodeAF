package projectors

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

func TestProjectorRegistryOrder(t *testing.T) {
	want := []string{
		"session.created",
		"session.updated",
		"session.deleted",
		"message.updated",
		"message.removed",
		"message.part.removed",
		"message.part.updated",
		"session.next.agent.switched",
		"session.next.model.switched",
		"session.next.prompted",
		"session.next.synthetic",
		"session.next.shell.started",
		"session.next.shell.ended",
		"session.next.step.started",
		"session.next.step.ended",
		"session.next.step.failed",
		"session.next.text.started",
		"session.next.text.delta",
		"session.next.text.ended",
		"session.next.tool.input.started",
		"session.next.tool.input.delta",
		"session.next.tool.input.ended",
		"session.next.tool.called",
		"session.next.tool.success",
		"session.next.tool.failed",
		"session.next.reasoning.started",
		"session.next.reasoning.delta",
		"session.next.reasoning.ended",
		"session.next.retried",
		"session.next.compaction.started",
		"session.next.compaction.delta",
		"session.next.compaction.ended",
	}
	if got := projectorTypes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("projectors = %v, want %v", got, want)
	}
}

func TestLateForeignWritesWarnAndOtherConstraintsFail(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, ":memory:", BusyRetryOptions{Log: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE project (id text PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if err := ApplySchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO project VALUES ('p1')`); err != nil {
		t.Fatal(err)
	}
	warnings := []Warning{}
	store := NewStore(db, StoreOptions{
		Now:  func() int64 { return 1000 },
		Warn: func(warning Warning) { warnings = append(warnings, warning) },
	})

	for _, event := range []Event{
		{
			ID:   "e1",
			Type: EventMessageUpdated,
			Data: json.RawMessage(`{"sessionID":"gone","info":{"id":"m1","sessionID":"gone","time":{"created":1}}}`),
		},
		{
			ID:   "e2",
			Type: EventMessagePartUpdated,
			Data: json.RawMessage(`{"sessionID":"gone","time":1,"part":{"id":"pt1","messageID":"gone","sessionID":"gone","type":"text"}}`),
		},
	} {
		if err := store.Apply(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if warnings[0].Message != "ignored late message update" ||
		warnings[1].Message != "ignored late part update" {
		t.Fatalf("warning messages = %#v", warnings)
	}
	first, _ := jscompat.Stringify(warnings[0].Fields)
	second, _ := jscompat.Stringify(warnings[1].Fields)
	if string(first) != `{"messageID":"m1","sessionID":"gone"}` {
		t.Fatalf("message warning fields = %s", first)
	}
	if string(second) != `{"partID":"pt1","messageID":"gone","sessionID":"gone"}` {
		t.Fatalf("part warning fields = %s", second)
	}

	err = store.Apply(ctx, Event{
		ID:   "e3",
		Type: EventMessageUpdated,
		Data: json.RawMessage(`{"sessionID":"p1","info":{"id":"m2","sessionID":"p1","time":{}}}`),
	})
	if err == nil || err.Error() != "NOT NULL constraint failed: message.time_created" {
		t.Fatalf("non-foreign constraint error = %v", err)
	}
	if len(warnings) != 2 {
		t.Fatalf("non-foreign constraint emitted warning: %#v", warnings)
	}
}
