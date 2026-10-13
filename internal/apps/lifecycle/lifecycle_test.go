package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/sample"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type fakeClock struct {
	mu    sync.Mutex
	at    time.Time
	ticks chan time.Time
}

func (c *fakeClock) Now() time.Time                       { c.mu.Lock(); defer c.mu.Unlock(); return c.at }
func (c *fakeClock) After(time.Duration) <-chan time.Time { return c.ticks }
func (c *fakeClock) advance(d time.Duration)              { c.mu.Lock(); c.at = c.at.Add(d); c.mu.Unlock() }

type activityLog struct {
	lines  []string
	scopes []svc.Scope
}

func (a *activityLog) Write(_ context.Context, scope svc.Scope, line string) error {
	a.lines = append(a.lines, line)
	a.scopes = append(a.scopes, scope)
	return nil
}

type testStore struct {
	root  string
	apps  map[string]bridge.AppDetail
	files map[string]spec.Files
	calls []string
	fail  string
}

func newStore(t *testing.T, root string) *testStore {
	t.Helper()
	files, _ := spec.GoldenCRM()
	def, err := spec.Decode(files)
	if err != nil {
		t.Fatal(err)
	}
	def.App.Version = 12
	data, _ := json.Marshal(def.App)
	files[spec.AppFile] = data
	s := &testStore{root: root, apps: map[string]bridge.AppDetail{}, files: map[string]spec.Files{}}
	s.apps["crm"] = bridge.AppDetail{AppSummary: bridge.AppSummary{ID: "crm", Name: "CRM", State: bridge.StateKept, Version: 12, Space: "personal"}}
	if err := s.persist("crm", files); err != nil {
		t.Fatal(err)
	}
	return s
}
func (s *testStore) persist(id string, files spec.Files) error {
	s.files[id] = files
	detail := s.apps[id]
	detail.Files = map[string]string{}
	for name, data := range files {
		detail.Files[name] = string(data)
		path := filepath.Join(s.root, id, "definition", name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			return err
		}
	}
	s.apps[id] = detail
	return nil
}
func (s *testStore) List(context.Context, svc.Caller) ([]bridge.AppSummary, error) {
	var apps []bridge.AppSummary
	for _, app := range s.apps {
		apps = append(apps, app.AppSummary)
	}
	return apps, nil
}
func (s *testStore) Get(_ context.Context, id string) (bridge.AppDetail, error) {
	if detail, ok := s.apps[id]; ok {
		return detail, nil
	}
	return bridge.AppDetail{}, notFound(id)
}
func (s *testStore) Definition(_ context.Context, id string) (*spec.Bundle, error) {
	if files, ok := s.files[id]; ok {
		return spec.Decode(files)
	}
	return nil, notFound(id)
}
func (s *testStore) Create(_ context.Context, caller svc.Caller, name, description string) (bridge.AppSummary, error) {
	id := fmt.Sprintf("copy-%d", len(s.apps))
	app := bridge.AppSummary{ID: id, Name: name, Description: description, Space: "personal", State: bridge.StatePlanning}
	s.apps[id] = bridge.AppDetail{AppSummary: app}
	s.calls = append(s.calls, "create")
	return app, nil
}
func (s *testStore) WriteFiles(_ context.Context, id string, files spec.Files) error {
	s.calls = append(s.calls, "write")
	if s.fail == "write" {
		return errors.New("write failed")
	}
	return s.persist(id, files)
}
func (s *testStore) SetState(_ context.Context, id string, state bridge.AppState) error {
	s.calls = append(s.calls, "state")
	app := s.apps[id]
	app.State = state
	s.apps[id] = app
	return nil
}
func (s *testStore) Keep(_ context.Context, id string) (bridge.AppSummary, error) {
	s.calls = append(s.calls, "keep")
	if s.fail == "keep" {
		return bridge.AppSummary{}, errors.New("keep failed")
	}
	app := s.apps[id]
	if app.State != bridge.StatePreview {
		return bridge.AppSummary{}, errors.New("copy skipped preview")
	}
	app.State, app.Version = bridge.StateKept, 1
	s.apps[id] = app
	return app.AppSummary, nil
}
func (s *testStore) Discard(_ context.Context, id string) error {
	s.calls = append(s.calls, "discard")
	if s.fail == "discard" {
		return errors.New("discard failed")
	}
	delete(s.apps, id)
	delete(s.files, id)
	return os.RemoveAll(filepath.Join(s.root, id))
}

type fixture struct {
	s     *Service
	all   *svc.Services
	base  *testStore
	clock *fakeClock
	log   *activityLog
	scope svc.Scope
}

func setup(t *testing.T) fixture {
	t.Helper()
	root := t.TempDir()
	base := newStore(t, root)
	all := &svc.Services{Store: Wrap(base)}
	clock := &fakeClock{at: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC), ticks: make(chan time.Time)}
	cfg := svc.Config{Root: root}
	s := NewWithClock(cfg, all, clock)
	all.Crews.Lifecycle = s
	log := &activityLog{}
	all.Crews.Activity = log
	all.Sample = sample.New(cfg, all)
	all.Records = records.New(cfg, all)
	return fixture{s: s, all: all, base: base, clock: clock, log: log, scope: svc.Scope{App: "crm", Caller: svc.Caller{Member: "mona", Role: svc.RoleEditor}, Data: svc.DataReal}}
}
func requireCode(t *testing.T, err error, code bridge.ErrorCode) {
	t.Helper()
	var wire *bridge.Error
	if !errors.As(err, &wire) || wire.Code != code {
		t.Fatalf("error = %v; want %s", err, code)
	}
}

func TestLifecycleActions(t *testing.T) {
	for _, tc := range []struct {
		name       string
		action     func(f fixture) error
		state      bridge.AppState
		code       bridge.ErrorCode
		visible    int
		activities int
	}{
		{name: "archive hides from list", action: func(f fixture) error { _, err := f.s.Archive(context.Background(), f.scope); return err }, state: bridge.StateArchived, visible: 0, activities: 1},
		{name: "restore returns", action: func(f fixture) error {
			if _, err := f.s.Archive(context.Background(), f.scope); err != nil {
				return err
			}
			_, err := f.s.Restore(context.Background(), f.scope)
			return err
		}, state: bridge.StateKept, visible: 1, activities: 2},
		{name: "wrong name", action: func(f fixture) error {
			return f.s.Delete(context.Background(), f.scope, bridge.DeleteAppParams{Confirm: "crm"})
		}, code: bridge.CodeInvalid, state: bridge.StateKept, visible: 1},
		{name: "member delete denied", action: func(f fixture) error {
			app := f.base.apps["crm"]
			app.Space = "acme"
			f.base.apps["crm"] = app
			return f.s.Delete(context.Background(), f.scope, bridge.DeleteAppParams{Confirm: "CRM"})
		}, code: bridge.CodeDenied, state: bridge.StateKept, visible: 1},
		{name: "delete hides", action: func(f fixture) error {
			return f.s.Delete(context.Background(), f.scope, bridge.DeleteAppParams{Confirm: "CRM"})
		}, state: bridge.StateDeleted, visible: 0, activities: 1},
		{name: "restore deleted", action: func(f fixture) error {
			if err := f.s.Delete(context.Background(), f.scope, bridge.DeleteAppParams{Confirm: "CRM"}); err != nil {
				return err
			}
			_, err := f.s.Restore(context.Background(), f.scope)
			return err
		}, state: bridge.StateKept, visible: 1, activities: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			err := tc.action(f)
			if tc.code != "" {
				requireCode(t, err, tc.code)
			} else if err != nil {
				t.Fatal(err)
			}
			apps, err := f.all.Store.List(context.Background(), f.scope.Caller)
			if err != nil || len(apps) != tc.visible {
				t.Fatalf("list = %v, %v", apps, err)
			}
			detail, err := f.all.Store.Get(context.Background(), "crm")
			if tc.state == bridge.StateDeleted {
				requireCode(t, err, bridge.CodeNotFound)
				_, err = f.all.Store.Definition(context.Background(), "crm")
				requireCode(t, err, bridge.CodeNotFound)
			} else if err != nil || detail.State != bridge.StateKept {
				t.Fatalf("get = %v, %v", detail, err)
			}
			lifecycle, err := f.s.List(context.Background(), f.scope.Caller)
			if err != nil {
				t.Fatal(err)
			}
			if tc.state == bridge.StateArchived && (len(lifecycle.Archived) != 1 || lifecycle.Archived[0].App.State != bridge.StateArchived) {
				t.Fatalf("archived state = %v", lifecycle)
			}
			if len(f.log.lines) != tc.activities {
				t.Fatalf("activity = %v", f.log.lines)
			}
			if tc.state == bridge.StateArchived && f.log.lines[0] != "@mona archived CRM · Restore" {
				t.Fatalf("archive activity = %v", f.log.lines)
			}
		})
	}
}

func TestRoles(t *testing.T) {
	for _, tc := range []struct {
		name, space     string
		caller          svc.Caller
		archive, delete bool
	}{
		{"personal viewer holds every role", "personal", svc.Caller{Member: "raj", Role: svc.RoleViewer}, true, true},
		{"workspace editor may archive", "acme", svc.Caller{Member: "mona", Role: svc.RoleEditor}, true, false},
		{"workspace user", "acme", svc.Caller{Member: "raj", Role: svc.RoleUser}, false, false},
		{"workspace viewer", "acme", svc.Caller{Member: "jm", Role: svc.RoleViewer}, false, false},
		{"agent", "personal", svc.Caller{Member: "mona", Role: svc.RoleEditor, Agent: true}, false, false},
		{"anonymous", "personal", svc.Caller{Role: svc.RoleEditor}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			app := f.base.apps["crm"]
			app.Space = tc.space
			f.base.apps["crm"] = app
			f.scope.Caller = tc.caller
			_, err := f.s.Archive(context.Background(), f.scope)
			if tc.archive {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				requireCode(t, err, bridge.CodeDenied)
			}
			err = f.s.Delete(context.Background(), f.scope, bridge.DeleteAppParams{Confirm: "CRM"})
			if tc.delete {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				requireCode(t, err, bridge.CodeDenied)
			}
		})
	}
}

func TestPurgeRetention(t *testing.T) {
	for _, tc := range []struct {
		name    string
		elapsed time.Duration
		gone    bool
	}{
		{"before deadline", retention - time.Nanosecond, false},
		{"at 30 days", retention, true},
		{"after deadline", retention + time.Hour, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			if err := f.s.Delete(ctx, f.scope, bridge.DeleteAppParams{Confirm: "CRM"}); err != nil {
				t.Fatal(err)
			}
			result, err := f.s.List(ctx, f.scope.Caller)
			if err != nil || len(result.Deleted) != 1 {
				t.Fatalf("lifecycle = %v, %v", result, err)
			}
			if result.Deleted[0].PurgeAt != stamp(f.clock.Now().Add(retention)) {
				t.Fatal("wrong purge_at")
			}
			marker := filepath.Join(f.base.root, "crm", "records.db")
			if err := os.WriteFile(marker, []byte("retained"), 0600); err != nil {
				t.Fatal(err)
			}
			f.clock.advance(tc.elapsed)
			if err := f.s.Purge(ctx); err != nil {
				t.Fatal(err)
			}
			_, err = os.Stat(marker)
			if os.IsNotExist(err) != tc.gone {
				t.Fatalf("records retained = %v", err)
			}
			_, err = f.base.Get(ctx, "crm")
			if tc.gone {
				requireCode(t, err, bridge.CodeNotFound)
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStartupAndDailyPurge(t *testing.T) {
	for _, daily := range []bool{false, true} {
		t.Run(fmt.Sprintf("daily=%v", daily), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := setup(t)
				if err := f.s.Delete(context.Background(), f.scope, bridge.DeleteAppParams{Confirm: "CRM"}); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				all := &svc.Services{Store: Wrap(f.base)}
				if !daily {
					f.clock.advance(retention)
				}
				s := NewWithClock(svc.Config{Root: f.base.root, Ctx: ctx}, all, f.clock)
				if daily {
					synctest.Wait()
					f.clock.advance(retention)
					f.clock.ticks <- f.clock.Now()
					synctest.Wait()
				}
				_, err := f.base.Get(context.Background(), "crm")
				requireCode(t, err, bridge.CodeNotFound)
				if err := s.Purge(context.Background()); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

func TestCopyFilesAndNoRealRecords(t *testing.T) {
	for _, name := range []string{"", "My pipeline"} {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			sourceApp := f.base.apps["crm"]
			sourceApp.Space = "acme"
			f.base.apps["crm"] = sourceApp
			sourceDefinition, err := f.base.Definition(ctx, "crm")
			if err != nil {
				t.Fatal(err)
			}
			sourceDefinition.App.Space = "acme"
			data, err := json.Marshal(sourceDefinition.App)
			if err != nil {
				t.Fatal(err)
			}
			f.base.files["crm"][spec.AppFile] = data
			if err := f.base.persist("crm", f.base.files["crm"]); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(f.base.root, "crm", "settings.json"), []byte(`{"month":50}`), 0600); err != nil {
				t.Fatal(err)
			}
			sourceFiles := make(spec.Files)
			for path, data := range f.base.files["crm"] {
				sourceFiles[path] = append([]byte(nil), data...)
			}
			sourceScope := f.scope
			if _, err := f.all.Records.Create(ctx, sourceScope, bridge.CreateRecordParams{Type: "company", Set: map[string]any{"name": "Private company"}}); err != nil {
				t.Fatal(err)
			}
			f.scope.Caller.Role = svc.RoleViewer
			copy, err := f.s.Copy(ctx, f.scope, bridge.CopyAppParams{Name: name})
			if err != nil {
				t.Fatal(err)
			}
			wantName := name
			if wantName == "" {
				wantName = "CRM (my copy)"
			}
			if copy.Name != wantName || copy.Space != "personal" || copy.State != bridge.StateKept || copy.Version != 1 {
				t.Fatalf("copy = %+v", copy)
			}
			def, err := f.all.Store.Definition(ctx, copy.ID)
			if err != nil {
				t.Fatal(err)
			}
			if def.App.ID != copy.ID || def.App.Space != "personal" || def.App.Version != 1 {
				t.Fatalf("copy identity = %+v", def.App)
			}
			for path, data := range sourceFiles {
				if path != spec.AppFile && !reflect.DeepEqual(data, def.Files[path]) {
					t.Errorf("file changed: %s", path)
				}
			}
			if !reflect.DeepEqual(sourceFiles, f.base.files["crm"]) {
				t.Fatal("source files changed")
			}
			if _, err := os.Stat(filepath.Join(f.base.root, copy.ID, "settings.json")); !os.IsNotExist(err) {
				t.Fatalf("copy inherited settings overrides: %v", err)
			}

			copyScope := svc.Scope{App: copy.ID, Caller: f.scope.Caller, Data: svc.DataReal}
			for typ := range def.App.Records {
				real, err := f.all.Records.Query(ctx, copyScope, bridge.QueryParams{Type: typ})
				if err != nil || real.Total != 0 {
					t.Fatalf("real %s = %+v, %v", typ, real, err)
				}
				copyScope.Data = svc.DataSample
				seeded, err := f.all.Records.Query(ctx, copyScope, bridge.QueryParams{Type: typ})
				if err != nil || seeded.Total == 0 {
					t.Fatalf("sample %s = %+v, %v", typ, seeded, err)
				}
				copyScope.Data = svc.DataReal
			}
			def.App.Budget.Month++
			source, _ := f.base.Definition(ctx, "crm")
			if def.App.Budget.Month == source.App.Budget.Month {
				t.Fatal("copy shares budget")
			}
			if strings.Join(f.base.calls, ",") != "create,write,state,keep" {
				t.Fatalf("store calls = %v", f.base.calls)
			}
			if len(f.log.lines) != 1 || f.log.scopes[0].App != copy.ID {
				t.Fatalf("copy activity = %v", f.log)
			}
		})
	}
}

func TestCopyFailureCleansUp(t *testing.T) {
	for _, phase := range []string{"write", "keep"} {
		t.Run(phase, func(t *testing.T) {
			f := setup(t)
			f.base.fail = phase
			_, err := f.s.Copy(context.Background(), f.scope, bridge.CopyAppParams{})
			if err == nil {
				t.Fatal("copy unexpectedly succeeded")
			}
			if len(f.base.apps) != 1 || len(f.log.lines) != 0 {
				t.Fatal("failed copy left an app or activity")
			}
			if _, err := os.Stat(filepath.Join(f.base.root, "copy-1")); !os.IsNotExist(err) {
				t.Fatalf("failed copy folder survived: %v", err)
			}
		})
	}
}

func TestTriggerGate(t *testing.T) {
	for _, action := range []string{"live", "archived", "restored", "deleted"} {
		t.Run(action, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			if action == "archived" || action == "restored" {
				if _, err := f.s.Archive(ctx, f.scope); err != nil {
					t.Fatal(err)
				}
			}
			if action == "restored" {
				if _, err := f.s.Restore(ctx, f.scope); err != nil {
					t.Fatal(err)
				}
			}
			if action == "deleted" {
				if err := f.s.Delete(ctx, f.scope, bridge.DeleteAppParams{Confirm: "CRM"}); err != nil {
					t.Fatal(err)
				}
			}
			gate := Gate(f.all, nil)
			if got := gate.Allow("crm", "follow-up", f.clock.Now()); got != (action == "live" || action == "restored") {
				t.Fatalf("Allow = %v", got)
			}
			if got := f.s.Archived("crm"); got != (action == "archived") {
				t.Fatalf("Archived = %v", got)
			}
		})
	}
}

func TestStateSurvivesRestart(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		t.Run(fmt.Sprintf("deleted=%v", deleted), func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			if deleted {
				if err := f.s.Delete(ctx, f.scope, bridge.DeleteAppParams{Confirm: "CRM"}); err != nil {
					t.Fatal(err)
				}
			} else if _, err := f.s.Archive(ctx, f.scope); err != nil {
				t.Fatal(err)
			}
			all := &svc.Services{Store: Wrap(f.base)}
			restarted := NewWithClock(svc.Config{Root: f.base.root}, all, f.clock)
			apps, err := all.Store.List(ctx, f.scope.Caller)
			if err != nil || len(apps) != 0 {
				t.Fatalf("restart list = %v, %v", apps, err)
			}
			result, err := restarted.List(ctx, f.scope.Caller)
			if err != nil || len(result.Archived)+len(result.Deleted) != 1 {
				t.Fatalf("restart lifecycle = %v, %v", result, err)
			}
			if _, err := restarted.Restore(ctx, f.scope); err != nil {
				t.Fatal(err)
			}
			detail, err := all.Store.Get(ctx, "crm")
			if err != nil || detail.State != bridge.StateKept || detail.Version != 12 {
				t.Fatalf("restored = %v, %v", detail, err)
			}
		})
	}
}

func TestLifecycleListingAndExpiredRestore(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		deleted, expired, workspace bool
		archived, visibleDeleted    int
	}{
		{name: "archive", archived: 1},
		{name: "deleted personal", deleted: true, visibleDeleted: 1},
		{name: "deleted workspace hidden from member", deleted: true, workspace: true},
		{name: "expired", deleted: true, expired: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			if tc.deleted {
				if err := f.s.Delete(ctx, f.scope, bridge.DeleteAppParams{Confirm: "CRM"}); err != nil {
					t.Fatal(err)
				}
			} else if _, err := f.s.Archive(ctx, f.scope); err != nil {
				t.Fatal(err)
			}
			if tc.workspace {
				app := f.base.apps["crm"]
				app.Space = "acme"
				f.base.apps["crm"] = app
			}
			if tc.expired {
				f.clock.advance(retention)
			}
			result, err := f.s.List(ctx, f.scope.Caller)
			if err != nil || len(result.Archived) != tc.archived || len(result.Deleted) != tc.visibleDeleted {
				t.Fatalf("lifecycle = %v, %v", result, err)
			}
			if tc.expired || tc.workspace {
				_, err := f.s.Restore(ctx, f.scope)
				code := bridge.CodeDenied
				if tc.expired {
					code = bridge.CodeNotFound
				}
				requireCode(t, err, code)
			}
		})
	}
}

func TestRepeatedActionsDoNotExtendRetentionOrActivity(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	for range 2 {
		if _, err := f.s.Archive(ctx, f.scope); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.log.lines) != 1 {
		t.Fatalf("archive repeated activity: %v", f.log.lines)
	}
	if err := f.s.Delete(ctx, f.scope, bridge.DeleteAppParams{Confirm: "CRM"}); err != nil {
		t.Fatal(err)
	}
	before, err := f.s.List(ctx, f.scope.Caller)
	if err != nil {
		t.Fatal(err)
	}
	f.clock.advance(time.Hour)
	if err := f.s.Delete(ctx, f.scope, bridge.DeleteAppParams{Confirm: "CRM"}); err != nil {
		t.Fatal(err)
	}
	after, err := f.s.List(ctx, f.scope.Caller)
	if err != nil {
		t.Fatal(err)
	}
	if before.Deleted[0].PurgeAt != after.Deleted[0].PurgeAt || len(f.log.lines) != 2 {
		t.Fatal("repeat delete extended retention or added activity")
	}
	_, err = f.s.Archive(ctx, f.scope)
	requireCode(t, err, bridge.CodeNotFound)
	_, err = f.s.Copy(ctx, f.scope, bridge.CopyAppParams{})
	requireCode(t, err, bridge.CodeNotFound)
}

func TestPurgeFailureRetainsRecoveryState(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if err := f.s.Delete(ctx, f.scope, bridge.DeleteAppParams{Confirm: "CRM"}); err != nil {
		t.Fatal(err)
	}
	f.clock.advance(retention)
	f.base.fail = "discard"
	if err := f.s.Purge(ctx); err == nil {
		t.Fatal("purge failure disappeared")
	}
	if _, err := os.Stat(filepath.Join(f.base.root, "crm", "lifecycle.json")); err != nil {
		t.Fatal(err)
	}
	f.base.fail = ""
	if err := f.s.Purge(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.base.root, "crm")); !os.IsNotExist(err) {
		t.Fatalf("purge left directory: %v", err)
	}
}

func TestInvalidLifecycleStateFailsClosed(t *testing.T) {
	for _, data := range []string{"{", `{"app":{"id":"another"},"state":"archived"}`, `{"app":{"id":"crm"},"state":"kept"}`, `{"app":{"id":"crm"},"state":"deleted","at":"2026-10-13T12:00:00Z","purge_at":"tomorrow"}`} {
		t.Run(data, func(t *testing.T) {
			f := setup(t)
			if err := os.WriteFile(filepath.Join(f.base.root, "crm", "lifecycle.json"), []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := f.all.Store.List(context.Background(), f.scope.Caller); err == nil {
				t.Fatal("invalid state exposed app")
			}
			if f.s.Allow("crm", "trigger", f.clock.Now()) {
				t.Fatal("invalid state allowed trigger")
			}
		})
	}
}

func TestStorePassthrough(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if Wrap(nil) != nil || Wrap(f.all.Store) != f.all.Store {
		t.Fatal("Wrap is not idempotent")
	}
	detail, err := f.all.Store.Get(ctx, "crm")
	if err != nil || !reflect.DeepEqual(detail, f.base.apps["crm"]) {
		t.Fatalf("live detail changed = %v, %v", detail, err)
	}
	def, err := f.all.Store.Definition(ctx, "crm")
	base, _ := f.base.Definition(ctx, "crm")
	if err != nil || !reflect.DeepEqual(def, base) {
		t.Fatalf("definition changed = %v", err)
	}
	created, err := f.all.Store.Create(ctx, f.scope.Caller, "Other", "description")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.all.Store.WriteFiles(ctx, created.ID, def.Files); err != nil {
		t.Fatal(err)
	}
	if err := f.all.Store.SetState(ctx, created.ID, bridge.StatePreview); err != nil {
		t.Fatal(err)
	}
	if _, err := f.all.Store.Keep(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.all.Store.Discard(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.base.calls, ","); got != "create,write,state,keep,discard" {
		t.Fatalf("passthrough calls = %s", got)
	}
}

type pausedGate struct {
	called bool
	allow  bool
}

func (g *pausedGate) Allow(string, string, time.Time) bool { g.called = true; return g.allow }
func TestGatePreservesSettingsPause(t *testing.T) {
	for _, archived := range []bool{false, true} {
		t.Run(fmt.Sprintf("archived=%v", archived), func(t *testing.T) {
			f := setup(t)
			if archived {
				if _, err := f.s.Archive(context.Background(), f.scope); err != nil {
					t.Fatal(err)
				}
			}
			settings := &pausedGate{}
			if Gate(f.all, settings).Allow("crm", "follow-up", f.clock.Now()) {
				t.Fatal("paused trigger allowed")
			}
			if settings.called == archived {
				t.Fatal("archived app recorded a fire, or live app skipped settings")
			}
		})
	}
}

type activityFunc func(context.Context, svc.Scope, string) error

func (f activityFunc) Write(ctx context.Context, scope svc.Scope, line string) error {
	return f(ctx, scope, line)
}

func TestActivityMayReadStore(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := setup(t)
		f.all.Crews.Activity = activityFunc(func(ctx context.Context, scope svc.Scope, _ string) error {
			_, err := f.all.Store.Get(ctx, scope.App)
			return err
		})
		if _, err := f.s.Archive(context.Background(), f.scope); err != nil {
			t.Fatal(err)
		}
	})
}

func TestAtomicStatePermissionsAndNoTemporaryFiles(t *testing.T) {
	f := setup(t)
	if _, err := f.s.Archive(context.Background(), f.scope); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.base.root, "crm", "lifecycle.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0077 != 0 {
		t.Fatalf("state permissions = %o", info.Mode().Perm())
	}
	var saved entry
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.State != bridge.StateArchived || saved.By != "mona" || saved.At != stamp(f.clock.Now()) {
		t.Fatalf("saved state = %+v", saved)
	}
	leftovers, err := filepath.Glob(filepath.Join(f.base.root, "crm", ".lifecycle-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("atomic write left files: %v, %v", leftovers, err)
	}
}

func TestArchiveRestoreKeepsRealRecordsAndDefinition(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	before, err := f.all.Store.Get(ctx, "crm")
	if err != nil {
		t.Fatal(err)
	}
	created, err := f.all.Records.Create(ctx, f.scope, bridge.CreateRecordParams{Type: "company", Set: map[string]any{"name": "Retained company"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Archive(ctx, f.scope); err != nil {
		t.Fatal(err)
	}
	archived, err := f.all.Store.Get(ctx, "crm")
	if err != nil || !reflect.DeepEqual(before, archived) {
		t.Fatalf("archived Get did not pass through: %v, %v", archived, err)
	}
	for _, restore := range []bool{false, true} {
		if restore {
			if _, err := f.s.Restore(ctx, f.scope); err != nil {
				t.Fatal(err)
			}
		}
		got, err := f.all.Records.Get(ctx, f.scope, bridge.GetRecordParams{Type: "company", ID: created.ID})
		if err != nil || !reflect.DeepEqual(created, got) {
			t.Fatalf("retained record = %v, %v", got, err)
		}
	}
}
