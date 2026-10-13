package shape

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type testHub struct{ events []svc.Event }

func (h *testHub) Publish(topic string, data any) {
	h.events = append(h.events, svc.Event{Topic: topic, Data: data})
}
func (*testHub) Subscribe(string) (<-chan svc.Event, func()) { return make(chan svc.Event), func() {} }

type applyFixture struct {
	service *Service
	store   *store.Service
	records *records.Service
	scope   svc.Scope
	hub     *testHub
	files   spec.Files
	root    string
}

func newApplyFixture(t *testing.T, count int) applyFixture {
	t.Helper()
	ctx := context.Background()
	cfg := svc.Config{Root: t.TempDir()}
	hub := &testHub{}
	all := &svc.Services{Hub: hub}
	st := store.NewWithOptions(cfg, all, store.Options{WithoutDemo: true})
	all.Store = st
	rec := records.New(cfg, all)
	all.Records = rec
	scope := svc.Scope{Caller: svc.Caller{Member: "me", Role: svc.RoleEditor}, Data: svc.DataReal}
	summary, err := st.Create(ctx, scope.Caller, "CRM", "A sales pipeline")
	if err != nil {
		t.Fatal(err)
	}
	scope.App = summary.ID
	files := spec.Files{
		spec.AppFile:            []byte(`{"id":"crm","name":"CRM","space":"personal","version":0,"kit":2,"agent":{"name":"CRM agent","file":"agent.md"},"records":{"deal":{"label":"Deal","plural":"deals","title":"name","fields":{"name":{"type":"text","required":true},"value":{"type":"money","label":"Value","index":true},"source":{"type":"text","label":"Source"}}}},"capabilities":{"records":{"deal":"rw"}},"budget":{"month":20,"currency":"USD"}}`),
		spec.PlanFile:           []byte(`{"ask":"A sales pipeline","name":"CRM","data":[{"id":"deal","line":"deals"}],"automation":[],"screens":[{"id":"pipeline","line":"Pipeline"}],"agent":{"id":"agent","line":"CRM agent"},"needs":"computed"}`),
		spec.AgentFile:          []byte("---\nname: CRM agent\ntools: []\n---\nHelp with deals.\n"),
		"screens/pipeline.json": []byte(`{"screen":"pipeline","title":"Pipeline","body":{"part":"RecordList","id":"deals","label":"Deals","bind":{"records":"deal"},"props":{"columns":["name","value"]}}}`),
	}
	if err := st.WriteFiles(ctx, scope.App, files); err != nil {
		t.Fatal(err)
	}
	if err := st.SetState(ctx, scope.App, bridge.StateBuilding); err != nil {
		t.Fatal(err)
	}
	if err := st.SetState(ctx, scope.App, bridge.StatePreview); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Keep(ctx, scope.App); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < count; i++ {
		set := map[string]any{"name": fmt.Sprintf("Deal %d", i), "value": float64(i + 1)}
		if i < 204 {
			set["source"] = "Referral"
		}
		if _, err := rec.Create(ctx, scope, bridge.CreateRecordParams{Type: "deal", Set: set}); err != nil {
			t.Fatal(err)
		}
	}
	detail, err := st.Get(ctx, scope.App)
	if err != nil {
		t.Fatal(err)
	}
	service := New(cfg, all)
	service.now = func() time.Time { return time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC) }
	return applyFixture{service, st, rec, scope, hub, detailFiles(detail), cfg.Root}
}

func writeApplyDraft(t *testing.T, f applyFixture, files spec.Files, requests ...string) {
	t.Helper()
	d := openDraft(t, f.service, f.scope)
	f.service.mu.Lock()
	defer f.service.mu.Unlock()
	for i, request := range requests {
		id := fmt.Sprint(i)
		change := bridge.Change{ID: id, N: i + 1, Request: request, State: "checking", Check: &bridge.CheckReport{Pass: true}}
		if err := d.AddChange(change, nil); err != nil {
			t.Fatal(err)
		}
		if err := d.ApplyFiles(id, files); err != nil {
			t.Fatal(err)
		}
	}
}

func editFiles(t *testing.T, files spec.Files, edit func(map[string]any)) spec.Files {
	t.Helper()
	copy := spec.Files{}
	for name, data := range files {
		copy[name] = data
	}
	var app map[string]any
	if err := json.Unmarshal(copy[spec.AppFile], &app); err != nil {
		t.Fatal(err)
	}
	edit(app)
	data, err := json.MarshalIndent(app, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	copy[spec.AppFile] = data
	return copy
}
func dealFields(app map[string]any) map[string]any {
	return app["records"].(map[string]any)["deal"].(map[string]any)["fields"].(map[string]any)
}
func codeIs(t *testing.T, err error, code bridge.ErrorCode) {
	t.Helper()
	var e *bridge.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("error=%v, want %s", err, code)
	}
}

func TestApplyRenameRemoveAndUndo(t *testing.T) {
	ctx := context.Background()
	f := newApplyFixture(t, 312)
	files := editFiles(t, f.files, func(app map[string]any) {
		fields := dealFields(app)
		amount := fields["value"].(map[string]any)
		amount["alias"] = "value"
		amount["label"] = "Amount"
		fields["amount"] = amount
		delete(fields, "value")
		delete(fields, "source")
	})
	files["screens/pipeline.json"] = []byte(`{"screen":"pipeline","title":"Pipeline","body":{"part":"RecordList","id":"deals","label":"Deals","bind":{"records":"deal","query":"value>0"},"props":{"columns":["name","amount"]}}}`)
	writeApplyDraft(t, f, files, "rename Value to Amount", "remove the Source field")
	preview, err := f.service.Preview(ctx, f.scope, bridge.ShapePreviewParams{})
	if err != nil {
		t.Fatal(err)
	}
	if preview.From != 1 || preview.To != 2 || preview.RecordsChecked != 312 || preview.Audience != "only you" || preview.NeedsChanged {
		t.Fatalf("preview=%+v", preview)
	}
	if len(preview.Migration) != 2 || preview.Migration[0].Op != "rename" || preview.Migration[0].Affected != 312 || preview.Migration[1].Affected != 204 {
		t.Fatalf("migration=%+v", preview.Migration)
	}
	if preview.Migration[0].Note != "nothing deleted · old name kept as alias · value: filters and runs keep working" || preview.Migration[1].Note != "hidden, restorable · values stay stored · Restore v1 brings it back" {
		t.Fatalf("notes=%+v", preview.Migration)
	}
	var replayVersion int
	f.service.replayHook = func(app string, version int) {
		if app != f.scope.App {
			t.Fatal(app)
		}
		replayVersion = version
	}
	summary, err := f.service.Apply(ctx, f.scope, bridge.ShapeApplyParams{Base: 1})
	if err != nil || summary.Version != 2 {
		t.Fatalf("apply=%+v,%v", summary, err)
	}
	if replayVersion != 2 {
		t.Fatal("replay hook not called")
	}
	dir, _ := f.service.candidateDir(f.scope)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("draft still exists")
	}
	versions, err := f.service.ListVersions(ctx, f.scope, bridge.VersionsListParams{})
	if err != nil || len(versions.Versions) != 2 || !versions.Versions[0].Current || versions.Versions[0].By != "me" || versions.Versions[0].Changes != 2 || !reflect.DeepEqual(versions.Versions[0].Requests, []string{"rename Value to Amount", "remove the Source field"}) {
		t.Fatalf("versions=%+v,%v", versions, err)
	}
	found := false
	for _, event := range f.hub.events {
		if event.Topic == bridge.TopicVersion(f.scope.App) {
			got := event.Data.(bridge.VersionEvent)
			found = got.Version == 2 && got.By == "me"
		}
	}
	if !found {
		t.Fatal("no version event")
	}
	for _, query := range []string{"value>0", "amount>0"} {
		result, err := f.records.Query(ctx, f.scope, bridge.QueryParams{Type: "deal", Query: query})
		if err != nil || result.Total != 312 {
			t.Fatalf("query %s=%+v,%v", query, result, err)
		}
		if _, exists := result.Rows[0].Fields["source"]; exists {
			t.Fatal("removed field exposed")
		}
		if result.Rows[0].Fields["amount"] != result.Rows[0].Fields["value"] {
			t.Fatal("alias value differs")
		}
	}
	stored, err := records.ShapeSnapshot(ctx, filepath.Join(f.root, f.scope.App, "records.db"))
	if err != nil {
		t.Fatal(err)
	}
	withSource := 0
	for _, row := range stored["deal"] {
		if row.Fields["source"] != nil {
			withSource++
		}
	}
	if withSource != 204 {
		t.Fatalf("stored sources=%d", withSource)
	}
	result, err := f.records.Query(ctx, f.scope, bridge.QueryParams{Type: "deal", Query: "value>0", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	id := result.Rows[0].ID
	if _, err := f.records.Update(ctx, f.scope, bridge.UpdateRecordParams{Type: "deal", ID: id, Set: map[string]any{"value": 999.0}}); err != nil {
		t.Fatal(err)
	}
	updated, err := f.records.Get(ctx, f.scope, bridge.GetRecordParams{Type: "deal", ID: id})
	if err != nil || updated.Fields["amount"] != 999.0 || updated.Fields["value"] != 999.0 {
		t.Fatalf("updated=%+v,%v", updated, err)
	}
	if _, err := f.records.Create(ctx, f.scope, bridge.CreateRecordParams{Type: "deal", Set: map[string]any{"name": "new", "amount": 12.0}}); err != nil {
		t.Fatal(err)
	}
	diff, err := f.service.DiffVersion(ctx, f.scope, bridge.VersionsDiffParams{Version: 2})
	if err != nil || len(diff.Files) != 2 {
		t.Fatalf("diff=%+v,%v", diff, err)
	}
	_, err = f.service.RestoreVersion(ctx, f.scope, bridge.VersionsRestoreParams{Version: 1})
	codeIs(t, err, bridge.CodeConflict)
	undo, err := f.service.RestoreVersion(ctx, f.scope, bridge.VersionsRestoreParams{Version: 1, Confirm: true})
	if err != nil || undo.Version != 3 {
		t.Fatalf("undo=%+v,%v", undo, err)
	}
	restored, err := f.store.GetVersion(ctx, f.scope.App, 3)
	if err != nil {
		t.Fatal(err)
	}
	a, b := detailFiles(restored), f.files
	var oldApp, newApp map[string]any
	json.Unmarshal(a[spec.AppFile], &newApp)
	json.Unmarshal(b[spec.AppFile], &oldApp)
	delete(newApp, "version")
	delete(oldApp, "version")
	if !reflect.DeepEqual(newApp, oldApp) {
		t.Fatal("restored shape differs")
	}
	for name, data := range b {
		if name != spec.AppFile && string(a[name]) != string(data) {
			t.Fatalf("restore changed %s", name)
		}
	}
	health, err := f.service.Health(ctx, f.scope, bridge.AppsHealthParams{})
	if err != nil || health.Broken != nil {
		t.Fatalf("health=%+v,%v", health, err)
	}
}

func TestApplyConflictAndUnready(t *testing.T) {
	for _, test := range []struct {
		name  string
		base  int
		state string
	}{{"stale", 0, "ready"}, {"checking", 1, "checking"}, {"failed", 1, "ready"}} {
		t.Run(test.name, func(t *testing.T) {
			f := newApplyFixture(t, 0)
			writeApplyDraft(t, f, f.files, "a change")
			f.service.mu.Lock()
			d, err := f.service.draftFor(f.scope)
			if err != nil {
				t.Fatal(err)
			}
			d.State = test.state
			if test.name == "failed" {
				d.Changes[0].State = "failed"
			}
			if err := d.persist(); err != nil {
				t.Fatal(err)
			}
			f.service.mu.Unlock()
			_, err = f.service.Apply(context.Background(), f.scope, bridge.ShapeApplyParams{Base: test.base})
			codeIs(t, err, bridge.CodeConflict)
			versions, _ := f.store.Versions(context.Background(), f.scope.App)
			if len(versions) != 1 {
				t.Fatal("conflict wrote version")
			}
		})
	}
}

func TestBrokenRenderAndRevertClears(t *testing.T) {
	ctx := context.Background()
	f := newApplyFixture(t, 3)
	files := editFiles(t, f.files, func(app map[string]any) { dealFields(app)["value"].(map[string]any)["type"] = "text" })
	writeApplyDraft(t, f, files, "make Value text")
	summary, err := f.service.Apply(ctx, f.scope, bridge.ShapeApplyParams{Base: 1})
	if err != nil || summary.Version != 2 {
		t.Fatal(summary, err)
	}
	health, err := f.service.Health(ctx, f.scope, bridge.AppsHealthParams{})
	if err != nil || health.Broken == nil {
		t.Fatal(health, err)
	}
	if health.Broken.LastGood != 1 || health.Broken.Affected != 3 || health.Broken.File != "screens/pipeline.json" || health.Broken.Request != "make Value text" {
		t.Fatalf("broken=%+v", health.Broken)
	}
	if _, err := f.records.Query(ctx, f.scope, bridge.QueryParams{Type: "deal"}); err != nil {
		t.Fatal("broken app stopped serving:", err)
	}
	restarted := New(svc.Config{Root: f.root}, f.service.all)
	health, err = restarted.Health(ctx, f.scope, bridge.AppsHealthParams{})
	if err != nil || health.Broken == nil {
		t.Fatal("health not durable", health, err)
	}
	summary, err = f.service.RestoreVersion(ctx, f.scope, bridge.VersionsRestoreParams{Version: 1})
	if err != nil || summary.Version != 3 {
		t.Fatal(summary, err)
	}
	health, err = f.service.Health(ctx, f.scope, bridge.AppsHealthParams{})
	if err != nil || health.Broken != nil || health.Version != 3 {
		t.Fatal(health, err)
	}
}

func TestSummaryAliasAndMigrationTypes(t *testing.T) {
	f := newApplyFixture(t, 1)
	for _, summary := range []string{"alias value → amount", "rename Value to Amount"} {
		files := editFiles(t, f.files, func(app map[string]any) {
			fields := dealFields(app)
			fields["amount"] = fields["value"]
			delete(fields, "value")
			fields["extra"] = map[string]any{"type": "text", "default": "hello"}
			fields["source"].(map[string]any)["type"] = "number"
		})
		files, err := annotatedRenames(f.files, files, []bridge.Change{{Summary: summary}})
		if err != nil {
			t.Fatal(err)
		}
		lines, checked, err := f.service.migration(context.Background(), f.scope.App, f.files, files, 1)
		if err != nil || checked != 1 || len(lines) != 3 || lines[0].Op != "rename" || lines[1].Op != "add" || lines[2].Op != "retype" {
			t.Fatalf("%s: %+v,%d,%v", summary, lines, checked, err)
		}
	}
}

func TestDryRunFailureLeavesVersionAndRecords(t *testing.T) {
	f := newApplyFixture(t, 2)
	files := editFiles(t, f.files, func(app map[string]any) { dealFields(app)["source"].(map[string]any)["unique"] = true })
	writeApplyDraft(t, f, files, "make Source unique")
	_, err := f.service.Apply(context.Background(), f.scope, bridge.ShapeApplyParams{Base: 1})
	if err == nil {
		t.Fatal("duplicate data passed migration dry run")
	}
	versions, err := f.store.Versions(context.Background(), f.scope.App)
	if err != nil || len(versions) != 1 {
		t.Fatal("dry run wrote a version", versions, err)
	}
	rows, err := f.records.Query(context.Background(), f.scope, bridge.QueryParams{Type: "deal"})
	if err != nil || rows.Total != 2 {
		t.Fatal("dry run changed real records", rows, err)
	}
}

func TestApplyEmptyRecordsAndLazyDefault(t *testing.T) {
	f := newApplyFixture(t, 0)
	files := editFiles(t, f.files, func(app map[string]any) {
		dealFields(app)["extra"] = map[string]any{"type": "text", "default": "hello"}
	})
	writeApplyDraft(t, f, files, "add Extra")
	summary, err := f.service.Apply(context.Background(), f.scope, bridge.ShapeApplyParams{Base: 1})
	if err != nil || summary.Version != 2 {
		t.Fatal(summary, err)
	}
	health, err := f.service.Health(context.Background(), f.scope, bridge.AppsHealthParams{})
	if err != nil || health.Broken != nil {
		t.Fatal("empty records marked broken", health, err)
	}
}

func TestNewRecordsUseOldAliasAndHiddenValuesSurviveUpdates(t *testing.T) {
	f := newApplyFixture(t, 1)
	files := editFiles(t, f.files, func(app map[string]any) {
		fields := dealFields(app)
		field := fields["value"].(map[string]any)
		field["alias"] = "value"
		fields["amount"] = field
		delete(fields, "value")
		delete(fields, "source")
		fields["extra"] = map[string]any{"type": "text", "default": "hello"}
	})
	files["screens/pipeline.json"] = []byte(`{"screen":"pipeline","title":"Pipeline","body":{"part":"RecordList","id":"deals","label":"Deals","bind":{"records":"deal"},"props":{"columns":["name","amount","extra"]}}}`)
	writeApplyDraft(t, f, files, "rename Value and hide Source")
	if _, err := f.service.Apply(context.Background(), f.scope, bridge.ShapeApplyParams{Base: 1}); err != nil {
		t.Fatal(err)
	}
	result, err := f.records.Query(context.Background(), f.scope, bridge.QueryParams{Type: "deal"})
	if err != nil || len(result.Rows) != 1 || result.Rows[0].Fields["extra"] != "hello" {
		t.Fatal(result, err)
	}
	if _, err := f.records.Update(context.Background(), f.scope, bridge.UpdateRecordParams{Type: "deal", ID: result.Rows[0].ID, Set: map[string]any{"name": "edited", "amount": 25.0}}); err != nil {
		t.Fatal(err)
	}
	stored, err := records.ShapeSnapshot(context.Background(), filepath.Join(f.root, f.scope.App, "records.db"))
	if err != nil || stored["deal"][0].Fields["source"] != "Referral" {
		t.Fatal("hidden value lost", stored, err)
	}
	row, err := f.records.Create(context.Background(), f.scope, bridge.CreateRecordParams{Type: "deal", Set: map[string]any{"name": "new", "value": 50.0}})
	if err != nil {
		t.Fatal(err)
	}
	result, err = f.records.Query(context.Background(), f.scope, bridge.QueryParams{Type: "deal", Query: "value>49"})
	if err != nil || result.Total != 1 || result.Rows[0].ID != row.ID {
		t.Fatal("old alias did not find new row", result, err)
	}
}

func TestVersionRequestsPreserveParagraphsAndMember(t *testing.T) {
	f := newApplyFixture(t, 0)
	f.scope.Caller.Member = "raj"
	requests := []string{"add a column\nwith a second paragraph", "change the title"}
	writeApplyDraft(t, f, f.files, requests...)
	summary, err := f.service.Apply(context.Background(), f.scope, bridge.ShapeApplyParams{Base: 1})
	if err != nil || summary.Version != 2 {
		t.Fatal(summary, err)
	}
	result, err := f.service.ListVersions(context.Background(), f.scope, bridge.VersionsListParams{})
	if err != nil || result.Versions[0].By != "raj" || !reflect.DeepEqual(result.Versions[0].Requests, requests) {
		t.Fatal(result, err)
	}
}

func TestApplyFinishesJournalAndNewDraftPreservesScratch(t *testing.T) {
	ctx := context.Background()
	f := newApplyFixture(t, 1)
	other := f.scope
	other.Caller.Member = "raj"
	openDraft(t, f.service, other)
	files := editFiles(t, f.files, func(app map[string]any) {
		delete(dealFields(app), "source")
	})
	writeApplyDraft(t, f, files, "hide Source")
	f.service.replayHook = func(app string, version int) {
		got, err := f.service.Get(ctx, f.scope, bridge.ShapeGetParams{})
		if err != nil || got.Draft != nil {
			t.Fatalf("applying member's draft survived: %+v, %v", got, err)
		}
		got, err = f.service.Get(ctx, other, bridge.ShapeGetParams{})
		if err != nil || got.Draft == nil || got.Draft.Base != 1 {
			t.Fatalf("other member's replay journal disappeared: %+v, %v", got, err)
		}
		if app != f.scope.App || version != 2 {
			t.Fatalf("replay = %s v%d", app, version)
		}
	}
	if _, err := f.service.Apply(ctx, f.scope, bridge.ShapeApplyParams{Base: 1}); err != nil {
		t.Fatal(err)
	}
	restarted := New(svc.Config{Root: f.root}, f.service.all)
	got, err := restarted.Get(ctx, f.scope, bridge.ShapeGetParams{})
	if err != nil || got.Draft != nil {
		t.Fatalf("applied draft returned after restart: %+v, %v", got, err)
	}
	opened, err := f.service.Open(ctx, f.scope, bridge.ShapeOpenParams{})
	if err != nil || opened.Base != 2 || len(opened.Changes) != 0 {
		t.Fatalf("next draft = %+v, %v", opened, err)
	}
	scratch := f.scope
	scratch.Data = svc.DataDraft
	rows, err := f.records.Query(ctx, scratch, bridge.QueryParams{Type: "deal"})
	if err != nil || len(rows.Rows) != 1 {
		t.Fatalf("scratch records = %+v, %v", rows, err)
	}
	id := rows.Rows[0].ID
	if _, err := f.records.Update(ctx, scratch, bridge.UpdateRecordParams{Type: "deal", ID: id, Set: map[string]any{"name": "scratch edit"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Open(ctx, f.scope, bridge.ShapeOpenParams{}); err != nil {
		t.Fatal(err)
	}
	row, err := f.records.Get(ctx, scratch, bridge.GetRecordParams{Type: "deal", ID: id})
	if err != nil || row.Fields["name"] != "scratch edit" {
		t.Fatalf("reopen lost a scratch write after migration: %+v, %v", row, err)
	}
	row, err = f.records.Get(ctx, f.scope, bridge.GetRecordParams{Type: "deal", ID: id})
	if err != nil || row.Fields["name"] != "Deal 0" {
		t.Fatalf("scratch write reached real records: %+v, %v", row, err)
	}
}
