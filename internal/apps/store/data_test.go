package store_test

import (
	"errors"
	"os"
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
	raw := strings.Replace(appJSON, `"title":"name","fields":{"name":{"type":"text"}}`, `"uses":true,"title":"name","fields":{"memo":{"type":"text"}}`, 1)
	makeShared := func() store.Meta {
		t.Helper()
		draft, err := s.NewDraft("")
		if err != nil {
			t.Fatal(err)
		}
		write(t, draft.Dir(), "app.json", raw)
		write(t, draft.Dir(), "plan.json", planJSON)
		write(t, draft.Dir(), "agent.md", "Manage shared contacts.")
		write(t, draft.Dir(), "screens/main.json", mainScreen)
		meta, err := s.Promote(draft, "Use contacts")
		if err != nil {
			t.Fatal(err)
		}
		return meta
	}
	republish := func(app store.Meta, definition string) store.Meta {
		t.Helper()
		edit, err := s.NewDraft(app.ID)
		if err != nil {
			t.Fatal(err)
		}
		write(t, edit.Dir(), "app.json", definition)
		next, err := s.Promote(edit, "Keep shared contacts")
		if err != nil || next.Version != app.Version+1 {
			t.Fatalf("Keep of shared contribution = %+v, %v", next, err)
		}
		return next
	}
	first := republish(makeShared(), raw)
	republish(first, strings.Replace(raw, `"memo":{"type":"text"}`, "", 1))
	adopted := makeShared()
	republish(adopted, raw)
}

func TestPublishedAppUsesCommittedSnapshotAndGrantsAfterReopen(t *testing.T) {
	s, home := testStore(t)
	path := filepath.Join(home, "records.db")
	d, err := data.Open(path, data.Options{NoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	s = s.WithData(func() (*data.Store, error) { return d, nil })
	m := makeApp(t, s)
	// A filesystem edit with the same version must not replace the committed definition.
	dir := filepath.Join(home, "apps", m.ID, "current")
	kept, err := os.ReadFile(filepath.Join(dir, "app.json"))
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.ReplaceAll(string(kept), `"rw"`, `"r"`)
	if tampered == string(kept) {
		t.Fatal("fixture has no write grant")
	}
	write(t, dir, "app.json", tampered)
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	d, err = data.Open(path, data.Options{NoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SyncData(d); err != nil {
		t.Fatal(err)
	}
	def, err := s.Get(m.ID)
	if err != nil || !strings.Contains(string(def.App), `"rw"`) {
		t.Fatalf("public snapshot was replaced by current: %s, %v", def.App, err)
	}
	if _, err := d.Create(data.Scope{App: m.ID}, "contact", []map[string]any{{"name": "Ari"}}); err != nil {
		t.Fatalf("committed grant disappeared: %v", err)
	}
}
