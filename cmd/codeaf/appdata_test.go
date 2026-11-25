package main

import (
	"encoding/json"
	"errors"
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
