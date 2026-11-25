package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// dataBackend is a backend whose codeaf home is a fresh folder holding the
// CRM as a kept app.
func dataBackend(t *testing.T) (*appBackend, *appRecorder) {
	t.Helper()
	rec := newAppRecorder()
	backend := newFakeBackend(rec, nil)
	t.Cleanup(func() { backend.Close() })
	root := t.TempDir()
	raw, err := os.ReadFile(filepath.Join("testdata", "fluidapps", "crm-app.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "apps", "crm", "current")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	backend.data.home = root
	return backend, rec
}

// THE WIRE REACHES THE STORE WITH THE KEPT APPS REGISTERED: a screen's call
// for the CRM is allowed, another app's is refused with the capability it
// would need in the detail, and a live query's diff arrives as data.changed.
func TestAppDataDoor(t *testing.T) {
	backend, rec := dataBackend(t)
	crm := appwire.DataScope{App: "crm"}
	if _, err := backend.DataSubscribe(appwire.DataSubscribeParams{DataScope: crm, Sub: "deals", Type: "deal", Query: "-stage:Lost"}); err != nil {
		t.Fatal(err)
	}
	made, err := backend.DataCreate(appwire.DataCreateParams{DataScope: crm, Type: "deal", Set: map[string]any{"name": "Northwind", "amount": 48000.0}})
	if err != nil {
		t.Fatal(err)
	}
	if owner, _ := made.Rows[0]["owner"].(string); made.Batch == "" || len(owner) != 26 {
		t.Fatalf("create: %+v", made)
	}
	rec.waitFor(t, 5*time.Second, func(event string, d any) bool {
		c, ok := d.(appwire.DataChanged)
		return event == appwire.EventDataChanged && ok && c.Sub == "deals" && c.Total == 1 && len(c.Upserts) == 1
	})

	_, err = backend.DataQuery(appwire.DataQueryParams{DataScope: appwire.DataScope{App: "ghost"}, Type: "deal"})
	var wire *appwire.Error
	if !errors.As(err, &wire) || wire.Code != appwire.CodeNotAllowed {
		t.Fatalf("ghost: %v", err)
	}
	raw, _ := json.Marshal(wire)
	if string(raw) != `{"code":"not_allowed","message":"ghost may not read deal records","detail":{"need":"records.deal.r"}}` {
		t.Fatalf("refusal on the wire: %s", raw)
	}

	_, err = backend.DataCreate(appwire.DataCreateParams{Type: "deal", Set: map[string]any{"amount": 1.0}})
	if !errors.As(err, &wire) || wire.Code != appwire.CodeBadParams || wire.Detail.(map[string]string)["field"] != "name" {
		t.Fatalf("invalid: %v", err)
	}

	undo, err := backend.DataUndo(appwire.DataUndoParams{Batch: made.Batch})
	if err != nil || len(undo.Removes) != 1 {
		t.Fatalf("undo: %+v %v", undo, err)
	}
	types, err := backend.DataTypes(appwire.DataScope{})
	if err != nil || len(types.Types) != 3 || types.Types[0].CreatedByApp != "crm" || types.Types[0].ID == "" {
		t.Fatalf("types: %+v %v", types, err)
	}
	if err := backend.DataUnsubscribe("deals"); err != nil {
		t.Fatal(err)
	}

	csv := filepath.Join(t.TempDir(), "c.csv")
	_ = os.WriteFile(csv, []byte("name,email\nAna,ana@x.example\n"), 0o600)
	preview, err := backend.DataImportCSV(appwire.DataImportParams{Type: "contact", Path: csv, DryRun: true})
	if err != nil || preview.Preview == nil || preview.Preview.Valid != 1 || preview.Created != nil {
		t.Fatalf("dry run: %+v %v", preview, err)
	}
	real, err := backend.DataImportCSV(appwire.DataImportParams{Type: "contact", Path: csv})
	if err != nil || real.Created == nil || *real.Created != 1 {
		t.Fatalf("import: %+v %v", real, err)
	}
	if _, err := backend.DataImportCSV(appwire.DataImportParams{Type: "contact", Path: "c.csv"}); err == nil {
		t.Fatal("a relative path was read")
	}
}

func TestAppDataSubscribeWireCarriesSnapshotAndInvalidationCursor(t *testing.T) {
	b, _ := dataBackend(t)
	created, err := b.DataCreate(appwire.DataCreateParams{Type: "deal", Set: map[string]any{"name": "Ari"}})
	if err != nil {
		t.Fatal(err)
	}
	w := newActionWire(t, b)
	var watched struct {
		Cursor   int64                    `json:"cursor"`
		Snapshot *appwire.DataQueryResult `json:"snapshot"`
	}
	raw, wireErr := w.call("data.subscribe", map[string]any{"type": "deal", "sub": "deals"})
	if wireErr != nil {
		t.Fatal(wireErr)
	}
	if err := json.Unmarshal(raw, &watched); err != nil {
		t.Fatal(err)
	}
	if watched.Cursor <= 0 || watched.Snapshot == nil || watched.Snapshot.Total != 1 ||
		watched.Snapshot.Rows[0]["id"] != created.Rows[0]["id"] {
		t.Fatalf("subscribe reply: %+v", watched)
	}
	watched.Snapshot = nil
	raw, wireErr = w.call("data.subscribe", map[string]any{"type": "deal", "sub": "count", "invalidate_only": true})
	if wireErr != nil {
		t.Fatal(wireErr)
	}
	if err := json.Unmarshal(raw, &watched); err != nil {
		t.Fatal(err)
	}
	if watched.Cursor <= 0 || watched.Snapshot != nil {
		t.Fatalf("invalidate-only reply: %+v", watched)
	}
}

func TestAppDataSubscribeWireResetHasNoRowPayload(t *testing.T) {
	b, _ := dataBackend(t)
	in, requests := io.Pipe()
	replies, out := io.Pipe()
	t.Cleanup(func() {
		_ = requests.Close()
		_ = replies.Close()
	})
	server := appwire.NewServer(out)
	b.emit = server
	go func() {
		defer out.Close()
		_ = server.Serve(t.Context(), in, appwire.Hello{Version: "test"}, b)
	}()
	frames := make(chan map[string]json.RawMessage, 16)
	go func() {
		defer close(frames)
		scan := bufio.NewScanner(replies)
		for scan.Scan() {
			var frame map[string]json.RawMessage
			if json.Unmarshal(scan.Bytes(), &frame) == nil {
				frames <- frame
			}
		}
	}()
	next := func(match func(map[string]json.RawMessage) bool) map[string]json.RawMessage {
		t.Helper()
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		for {
			select {
			case f, ok := <-frames:
				if !ok {
					t.Fatal("wire closed before the expected frame")
				}
				if match(f) {
					return f
				}
			case <-deadline.C:
				t.Fatal("wire did not send the expected frame")
			}
		}
	}
	next(func(f map[string]json.RawMessage) bool { return string(f["event"]) == `"hello"` })
	if _, err := io.WriteString(requests, `{"id":42,"method":"data.subscribe","params":{"type":"deal","sub":"total","invalidate_only":true}}`+"\n"); err != nil {
		t.Fatal(err)
	}
	reply := next(func(f map[string]json.RawMessage) bool { return string(f["id"]) == "42" })
	var receipt appwire.DataSubscribeResult
	if err := json.Unmarshal(reply["result"], &receipt); err != nil {
		t.Fatal(err)
	}
	if _, err := b.DataCreate(appwire.DataCreateParams{Type: "deal", Set: map[string]any{"name": "Protected payload"}}); err != nil {
		t.Fatal(err)
	}
	event := next(func(f map[string]json.RawMessage) bool { return string(f["event"]) == `"data.reset"` })
	var reset appwire.DataReset
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(event["data"], &reset); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(event["data"], &fields); err != nil || len(fields) != 2 ||
		reset.Sub != "total" || reset.Cursor <= receipt.Cursor {
		t.Fatalf("unexpected reset: %s (%v)", event["data"], err)
	}
}

type pausedDataWire struct {
	started chan struct{}
	reset   chan struct{}
	release chan struct{}
}

func (w *pausedDataWire) Emit(event string, body any) {
	w.EmitContext(context.Background(), event, body)
}

func (w *pausedDataWire) EmitContext(ctx context.Context, event string, _ any) {
	if event == appwire.EventDataReset {
		close(w.reset)
		return
	}
	if event == appwire.EventDataChanged {
		select {
		case w.started <- struct{}{}:
		default:
		}
		select {
		case <-ctx.Done():
		case <-w.release:
		}
	}
}

func TestAppDataSlowWireDoesNotBlockPolicyCommit(t *testing.T) {
	b, _ := dataBackend(t)
	wire := &pausedDataWire{started: make(chan struct{}, 1), reset: make(chan struct{}), release: make(chan struct{})}
	defer close(wire.release)
	b.emit = wire
	if _, err := b.DataSubscribe(appwire.DataSubscribeParams{Sub: "deals", Type: "deal"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.DataCreate(appwire.DataCreateParams{Type: "deal", Set: map[string]any{"name": "Ari"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-wire.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the data event never reached the wire")
	}
	db, err := b.dataStore("")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- db.InvalidatePolicy("") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the paused wire blocked the policy commit")
	}
	select {
	case <-wire.reset:
	case <-time.After(5 * time.Second):
		t.Fatal("the policy reset did not replace the blocked event")
	}
}
