package store_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
)

func testStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	home := t.TempDir()
	return store.NewAt(home, nil), home
}

const (
	appJSON    = `{"id":"crm","name":"CRM","version":1,"kit":0,"agent":{"name":"CRM agent","file":"agent.md"},"records":{"contact":{"title":"name","fields":{"name":{"type":"text"}}}},"capabilities":{"records":{"contact":"rw"}}}`
	planJSON   = `{"ask":"Contact list","data":[{"id":"contact","line":"Contact names"}],"screens":[{"id":"main","line":"Contacts"}],"agent":{"line":"Manage contacts"}}`
	mainScreen = `{"screen":"main","title":"Contacts","body":{"part":"Stack"}}`
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func makeApp(t *testing.T, s *store.Store) store.Meta {
	t.Helper()
	draft, err := s.NewDraft("")
	if err != nil {
		t.Fatal(err)
	}
	write(t, draft.Dir(), "app.json", appJSON)
	write(t, draft.Dir(), "plan.json", planJSON)
	write(t, draft.Dir(), "agent.md", "Manage contacts.")
	write(t, draft.Dir(), "screens/main.json", mainScreen)
	meta, err := s.Promote(draft, "Keep CRM")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(draft.Dir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("draft after Keep: %v", err)
	}
	return meta
}

func TestAppKeepAndReadDefinition(t *testing.T) {
	s, home := testStore(t)
	meta := makeApp(t, s)
	got, err := s.Get(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Meta != meta || got.AgentMD != "Manage contacts." || len(got.Screens) != 1 || got.Screens[0] != "main" || got.Runs == nil {
		t.Fatalf("definition: %+v", got)
	}
	var app struct {
		ID      string
		Version int
	}
	if err := json.Unmarshal(got.App, &app); err != nil || app.ID != meta.ID || app.Version != 1 {
		t.Fatalf("definition identity: %+v, %v", app, err)
	}
	for _, file := range []string{"meta.json", "current/app.json", "versions/1/app.json"} {
		if _, err := os.Stat(filepath.Join(home, "apps", meta.ID, file)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAppConcurrentKeepRejectsStaleDraft(t *testing.T) {
	s, home := testStore(t)
	meta := makeApp(t, s)
	other := store.NewAt(home, nil)
	first, err := s.NewDraft(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := other.NewDraft(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	write(t, first.Dir(), "agent.md", "First edit")
	write(t, second.Dir(), "agent.md", "Second edit")
	results := make(chan error, 2)
	start := make(chan struct{})
	go func() {
		<-start
		_, err := s.Promote(first, "First edit")
		results <- err
	}()
	go func() {
		<-start
		_, err := other.Promote(second, "Second edit")
		results <- err
	}()
	close(start)
	a, b := <-results, <-results
	if !((a == nil && errors.Is(b, store.ErrConflict)) || (b == nil && errors.Is(a, store.ErrConflict))) {
		t.Fatalf("concurrent keeps: %v, %v", a, b)
	}
	versions, err := s.Versions(meta.ID)
	if err != nil || len(versions) != 2 || versions[0].N != 2 || versions[1].N != 1 {
		t.Fatalf("versions: %+v, %v", versions, err)
	}
	got, err := other.Get(meta.ID)
	if err != nil || got.Meta.Version != 2 || got.AgentMD != versions[0].Request {
		t.Fatalf("live app: %+v, %v", got, err)
	}
}

func TestAppRevertMakesNewVersion(t *testing.T) {
	s, home := testStore(t)
	meta := makeApp(t, s)
	original, err := s.Get(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.NewDraft(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	write(t, d.Dir(), "agent.md", "Use the pipeline.")
	write(t, d.Dir(), "screens/detail.json", `{"screen":"detail","title":"Contact","body":{"part":"Stack"}}`)
	write(t, d.Dir(), "plan.json", `{"ask":"Contact list","data":[{"id":"contact","line":"Contact names"}],"screens":[{"id":"main","line":"Contacts"},{"id":"detail","line":"Contact details"}],"agent":{"line":"Manage contacts"}}`)
	write(t, d.Dir(), "sample.db", "draft sample")
	if _, err := s.Promote(d, "Add pipeline\nMore details"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Revert(meta.ID, 1); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(meta.ID)
	if err != nil || got.Meta.Version != 3 || got.AgentMD != "Manage contacts." || len(got.Screens) != 1 {
		t.Fatalf("reverted app: %+v, %v", got, err)
	}
	if !bytes.Equal(got.App, original.App) {
		t.Fatalf("revert did not restore the original app manifest: %s", got.App)
	}
	if _, err := s.Screen(meta.ID, "detail"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("screen from later version: %v", err)
	}
	versions, err := s.Versions(meta.ID)
	if err != nil || len(versions) != 3 || versions[0].Summary != "Revert to version 1" ||
		versions[1].Summary != "Add pipeline" || versions[1].Request != "Add pipeline\nMore details" {
		t.Fatalf("versions: %+v, %v", versions, err)
	}
	path := filepath.Join(home, "apps", meta.ID, "versions", "2")
	if _, err := os.Stat(filepath.Join(path, "screens", "detail.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(path, "sample.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("published draft sample: %v", err)
	}
}

func TestAppInvalidDraftAndCurrentKeepLastValidVersion(t *testing.T) {
	s, home := testStore(t)
	meta := makeApp(t, s)
	d, err := s.NewDraft(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	write(t, d.Dir(), "screens/main.json", `{"screen":"main","title":"Contacts","body":{"part":"Unknown"}}`)
	if _, err := s.Promote(d, "Break main screen"); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid keep: %v", err)
	}
	if _, err := os.Stat(d.Dir()); err != nil {
		t.Fatal(err)
	}
	write(t, d.Dir(), "screens/main.json", mainScreen)
	write(t, d.Dir(), "agent.md", "Second version")
	if _, err := s.Promote(d, "Update agent"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "apps", meta.ID)
	write(t, filepath.Join(dir, "current"), "screens/main.json", `{"screen":"main","title":"Contacts","body":{"part":"Unknown"}}`)
	rows, err := s.List()
	if err != nil || len(rows) != 1 || rows[0].Status != "broken" {
		t.Fatalf("broken current status: %+v, %v", rows, err)
	}
	got, err := s.Get(meta.ID)
	if err != nil || got.Meta.Version != 2 || got.AgentMD != "Second version" {
		t.Fatalf("last valid snapshot: %+v, %v", got, err)
	}
	write(t, filepath.Join(dir, "versions", "2"), "screens/main.json", `{"screen":"main","title":"Contacts","body":{"part":"Unknown"}}`)
	got, err = s.Get(meta.ID)
	if err != nil || got.Meta.Version != 1 || got.AgentMD != "Manage contacts." {
		t.Fatalf("older valid snapshot: %+v, %v", got, err)
	}
	if _, err := s.NewDraft(meta.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("draft started from a superseded definition: %v", err)
	}
	if _, err := s.Revert(meta.ID, 2); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid historical version: %v", err)
	}
	if next, err := s.Revert(meta.ID, 1); err != nil || next.Version != 3 {
		t.Fatalf("repair: %+v, %v", next, err)
	}
}

func TestAppCorruptMetaDoesNotHideOtherApps(t *testing.T) {
	s, home := testStore(t)
	broken, good := makeApp(t, s), makeApp(t, s)
	write(t, filepath.Join(home, "apps", broken.ID), "meta.json", `{"id":`)
	rows, err := s.List()
	if err != nil || len(rows) != 1 || rows[0].ID != good.ID {
		t.Fatalf("list with corrupt metadata: %+v, %v", rows, err)
	}
	if _, err := s.Get(broken.ID); !errors.Is(err, store.ErrCorrupt) {
		t.Fatalf("corrupt get: %v", err)
	}
	if _, err := s.Delete(broken.ID); !errors.Is(err, store.ErrCorrupt) {
		t.Fatalf("corrupt delete: %v", err)
	}
}

func TestAppArchiveDeleteAndDiscardKeepThePersonsData(t *testing.T) {
	s, home := testStore(t)
	meta := makeApp(t, s)
	write(t, home, "data/data.db", "person's records")
	d, err := s.NewDraft(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Discard(d); err != nil {
		t.Fatal(err)
	}
	if err := s.Discard(d); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(d.Dir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("discarded directory: %v", err)
	}
	if m, err := s.Archive(meta.ID); err != nil || !m.Archived {
		t.Fatalf("archive: %+v, %v", m, err)
	}
	rows, err := s.List()
	if err != nil || len(rows) != 1 || rows[0].Status != "archived" {
		t.Fatalf("archived rows: %+v, %v", rows, err)
	}
	for range 2 {
		if m, err := s.Delete(meta.ID); err != nil || !m.Deleted {
			t.Fatalf("delete: %+v, %v", m, err)
		}
	}
	if rows, err := s.List(); err != nil || len(rows) != 0 {
		t.Fatalf("deleted rows: %+v, %v", rows, err)
	}
	if _, err := s.Get(meta.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted app: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "data", "data.db"))
	if err != nil || string(raw) != "person's records" {
		t.Fatalf("personal data: %q, %v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(home, "apps", meta.ID, "versions", "1", "app.json")); err != nil {
		t.Fatal(err)
	}
}

func TestAppInterruptedPublishKeepsCommittedVersion(t *testing.T) {
	s, home := testStore(t)
	meta := makeApp(t, s)
	dir := filepath.Join(home, "apps", meta.ID)
	// A process may stop after moving both directories and before committing meta.
	next := filepath.Join(dir, "versions", "2")
	current := filepath.Join(dir, "current")
	if err := os.CopyFS(next, os.DirFS(current)); err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile(filepath.Join(current, "app.json"))
	if err != nil {
		t.Fatal(err)
	}
	ahead := strings.NewReplacer(`"name": "CRM"`, `"name": "Uncommitted"`, `"version": 1`, `"version": 2`).Replace(string(committed))
	write(t, next, "app.json", ahead)
	write(t, current, "app.json", ahead)

	got, err := s.Get(meta.ID)
	if err != nil || got.Meta.Version != 1 || got.Meta.Name != "CRM" {
		t.Fatalf("read after interruption: %+v, %v", got, err)
	}
	versions, err := s.Versions(meta.ID)
	if err != nil || len(versions) != 1 || versions[0].N != 1 {
		t.Fatalf("history after interruption: %+v, %v", versions, err)
	}
	draft, err := s.NewDraft(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	write(t, draft.Dir(), "agent.md", "Committed edit.")
	promoted, err := s.Promote(draft, "Retry update")
	if err != nil || promoted.Version != 2 || promoted.Name != "CRM" {
		t.Fatalf("keep after interruption: %+v, %v", promoted, err)
	}
}

func TestAppDefinitionCannotLoadLinkedFilesOutsideDraft(t *testing.T) {
	s, home := testStore(t)
	d, err := s.NewDraft("")
	if err != nil {
		t.Fatal(err)
	}
	write(t, home, "data/private.md", "Private content.")
	target := filepath.Join(home, "data", "private.md")
	if err := os.Symlink(target, filepath.Join(d.Dir(), "agent.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	write(t, d.Dir(), "app.json", appJSON)
	write(t, d.Dir(), "plan.json", planJSON)
	write(t, d.Dir(), "screens/main.json", mainScreen)
	if _, err := s.Promote(d, "Keep"); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("keep with linked agent instructions: %v", err)
	}
	if rows, err := s.List(); err != nil || len(rows) != 0 {
		t.Fatalf("published draft with linked file: %+v, %v", rows, err)
	}
}

func TestAppCRMFixtureKeepsItsScreensAndRuns(t *testing.T) {
	s, _ := testStore(t)
	d, err := s.NewDraft("")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(d.Dir(), os.DirFS("testdata/crm")); err != nil {
		t.Fatal(err)
	}
	m, err := s.Promote(d, "Keep the CRM")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Meta.Icon != "people" || len(got.Screens) != 3 || len(got.Runs) != 2 {
		t.Fatalf("CRM definition: %+v", got)
	}
	if run, ok := got.Run("follow-up"); !ok || len(run) == 0 {
		t.Fatal("CRM follow-up run is missing")
	}
	var app schema.App
	if err := json.Unmarshal(got.App, &app); err != nil || app.ID != m.ID {
		t.Fatalf("CRM app ID: %q, %v", app.ID, err)
	}
}

func TestAppCanValidateSharedTypesFromThePersonsCatalog(t *testing.T) {
	app := `{"id":"hiring","name":"Hiring","version":1,"kit":0,"records":{"contact":{"uses":true,"title":"name","fields":{"role_applied":{"type":"text"}}}},"capabilities":{"records":{"contact":"r"}}}`
	plan := `{"ask":"Contacts","data":[{"id":"contact","line":"Use contacts","uses":true}],"screens":[{"id":"main","line":"Contacts"}]}`
	types := schema.TypeSet{"contact": {Owner: "crm", Fields: map[string]schema.Field{"name": {Type: "text"}}}}
	home := t.TempDir()
	s := store.NewAt(home, func(string) (schema.TypeSet, error) { return types, nil })
	d, err := s.NewDraft("")
	if err != nil {
		t.Fatal(err)
	}
	write(t, d.Dir(), "app.json", app)
	write(t, d.Dir(), "plan.json", plan)
	write(t, d.Dir(), "screens/main.json", mainScreen)

	if _, err := store.NewAt(home, nil).Promote(d, "Use contacts"); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("shared type without a catalog: %v", err)
	}
	m, err := s.Promote(d, "Use contacts")
	if err != nil {
		t.Fatal(err)
	}
	reader := store.NewAt(home, nil)
	if _, err := reader.Get(m.ID); err != nil {
		t.Fatal(err)
	}
	if next, err := reader.Revert(m.ID, 1); err != nil || next.Version != 2 {
		t.Fatalf("revert with saved type definitions: %+v, %v", next, err)
	}
}

func TestAppKeepsAgentInstructionsAtTheirDeclaredPath(t *testing.T) {
	s, _ := testStore(t)
	d, err := s.NewDraft("")
	if err != nil {
		t.Fatal(err)
	}
	write(t, d.Dir(), "app.json", strings.Replace(appJSON, "agent.md", "agents/contacts/agent.md", 1))
	write(t, d.Dir(), "plan.json", planJSON)
	write(t, d.Dir(), "screens/main.json", mainScreen)
	write(t, d.Dir(), "agents/contacts/agent.md", "Use the contacts view.")
	m, err := s.Promote(d, "Keep contacts")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(m.ID)
	if err != nil || got.AgentMD != "Use the contacts view." {
		t.Fatalf("agent instructions: %q, %v", got.AgentMD, err)
	}
}

func TestAppKeepRefusesUnsupportedDefinitionFiles(t *testing.T) {
	s, _ := testStore(t)
	m := makeApp(t, s)
	d, err := s.NewDraft(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	write(t, d.Dir(), "custom/anything.txt", "Scratch outside the definition.")
	if _, err := s.Promote(d, "Add custom output"); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("keep with unsupported output: %v", err)
	}
	got, err := s.Get(m.ID)
	if err != nil || got.Meta.Version != 1 {
		t.Fatalf("app after refused change: %+v, %v", got.Meta, err)
	}
}

func TestGalleryContentSurvivesKeepAndRevert(t *testing.T) {
	s, home := testStore(t)
	draft, err := s.NewDraft("")
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := schema.Load("../schema/testdata/month-end-close")
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range fixture.Files {
		write(t, draft.Dir(), name, string(raw))
	}
	meta, err := s.Promote(draft, "Keep month-end close")
	if err != nil {
		t.Fatal(err)
	}
	edited, err := s.NewDraft(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	write(t, edited.Dir(), "sheets/ledger.csv", "account,amount\nNostro USD,350000\n")
	if _, err = s.Promote(edited, "Update ledger"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Revert(meta.ID, 1); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"sheets/ledger.csv", "sheets/formulas.json"} {
		got, err := os.ReadFile(filepath.Join(home, "apps", meta.ID, "current", name))
		if err != nil || string(got) != string(fixture.Files[name]) {
			t.Fatalf("%s after Revert = %q, %v", name, got, err)
		}
	}
}
