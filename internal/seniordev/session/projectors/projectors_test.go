//go:build !windows

package projectors

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"testing"
)

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
	if want := (PartialRow{"messageID": "m1", "sessionID": "gone"}); !reflect.DeepEqual(warnings[0].Fields, want) {
		t.Fatalf("message warning fields = %#v", warnings[0].Fields)
	}
	if want := (PartialRow{"partID": "pt1", "messageID": "gone", "sessionID": "gone"}); !reflect.DeepEqual(warnings[1].Fields, want) {
		t.Fatalf("part warning fields = %#v", warnings[1].Fields)
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
