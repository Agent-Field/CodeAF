package store_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/store"
)

func TestAppPublicationKeepsDataGrantsInStep(t *testing.T) {
	s, home := testStore(t)
	d, err := data.Open(filepath.Join(home, "records.db"), data.Options{NoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	s = s.WithData(func() (*data.Store, error) { return d, nil })
	meta := makeApp(t, s)
	scope := data.Scope{App: meta.ID}
	made, err := d.Create(scope, "contact", []map[string]any{{"name": "Ari"}})
	if err != nil {
		t.Fatal(err)
	}
	draft, err := s.NewDraft(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	write(t, draft.Dir(), "app.json", strings.Replace(appJSON, `"contact":"rw"`, `"contact":"r"`, 1))
	if _, err := s.Promote(draft, "Read-only contacts"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Create(scope, "contact", []map[string]any{{"name": "Paz"}}); err == nil {
		t.Fatal("publish left the old write grant in effect")
	}
	if _, err := s.Revert(meta.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Create(scope, "contact", []map[string]any{{"name": "Paz"}}); err != nil {
		t.Fatalf("revert did not restore the write grant: %v", err)
	}
	if _, err := d.Create(scope, "contact", []map[string]any{{"name": "Paz"}}); err != nil {
		t.Fatal(err)
	}
	bad, err := s.NewDraft(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	write(t, bad.Dir(), "app.json", strings.Replace(appJSON, `"name":{"type":"text"}`, `"name":{"type":"text","unique":true}`, 1))
	if _, err := s.Promote(bad, "Invalid change to existing data"); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("incompatible data definition publish = %v", err)
	}
	live, err := s.Get(meta.ID)
	if err != nil || live.Meta.Version != 3 {
		t.Fatalf("invalid publish replaced committed app: %+v, %v", live.Meta, err)
	}
	if _, err := s.Delete(meta.ID); err != nil {
		t.Fatal(err)
	}
	if d.HasApp(meta.ID) {
		t.Fatal("delete kept the app registered")
	}
	// Simulate the grants left by an older engine that kept the files.
	if err := d.RegisterApp(live.App); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncData(d); err != nil {
		t.Fatal(err)
	}
	if d.HasApp(meta.ID) {
		t.Fatal("startup restored grants from a deleted definition")
	}
	row, err := d.Get(data.Scope{}, "contact", made.Rows[0].ID(), false)
	if err != nil || row["name"] != "Ari" {
		t.Fatalf("delete lost personal data: %v, %v", row, err)
	}
}

func TestAppCanRepublishItsOwnSharedFields(t *testing.T) {
	s, home := testStore(t)
	d, err := data.Open(filepath.Join(home, "records.db"), data.Options{NoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	s = s.WithData(func() (*data.Store, error) { return d, nil })
	makeApp(t, s)
	draft, err := s.NewDraft("")
	if err != nil {
		t.Fatal(err)
	}
	raw := strings.Replace(appJSON, `"title":"name","fields":{"name":{"type":"text"}}`, `"uses":true,"title":"name","fields":{"memo":{"type":"text"}}`, 1)
	write(t, draft.Dir(), "app.json", raw)
	write(t, draft.Dir(), "plan.json", planJSON)
	write(t, draft.Dir(), "agent.md", "Manage shared contacts.")
	write(t, draft.Dir(), "screens/main.json", mainScreen)
	meta, err := s.Promote(draft, "Use contacts")
	if err != nil {
		t.Fatal(err)
	}
	edit, err := s.NewDraft(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	meta, err = s.Promote(edit, "Keep the app and its added field")
	if err != nil || meta.Version != 2 {
		t.Fatalf("Keep of shared contribution = %+v, %v", meta, err)
	}
}
