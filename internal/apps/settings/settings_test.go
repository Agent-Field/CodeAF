package settings

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

var testNow = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

type testStore struct {
	svc.Store
	bundle *spec.Bundle
	err    error
}

func (s *testStore) Get(ctx context.Context, app string) (bridge.AppDetail, error) {
	def, err := s.Definition(ctx, app)
	if err != nil {
		return bridge.AppDetail{}, err
	}
	return bridge.AppDetail{AppSummary: bridge.AppSummary{ID: app, Name: def.App.Name, Space: def.App.Space, State: bridge.StateKept}}, nil
}

func (s *testStore) List(context.Context, svc.Caller) ([]bridge.AppSummary, error) {
	return []bridge.AppSummary{{ID: "crm", Name: "CRM", State: bridge.StateKept}}, nil
}

func (s *testStore) Definition(_ context.Context, app string) (*spec.Bundle, error) {
	if s.err != nil {
		return nil, s.err
	}
	if app != "crm" {
		return nil, bridge.Errorf(bridge.CodeNotFound, "no app "+app)
	}
	return s.bundle, nil
}

type testRuns struct {
	svc.Runs
	rows []bridge.RunState
	err  error
}

func (r *testRuns) List(context.Context, svc.Scope, bridge.ListRunsParams) (bridge.ListRunsResult, error) {
	return bridge.ListRunsResult{Runs: r.rows}, r.err
}

type testLifecycle struct {
	svc.Lifecycle
	states bridge.LifecycleResult
	err    error
}

func (l testLifecycle) List(context.Context, svc.Caller) (bridge.LifecycleResult, error) {
	return l.states, l.err
}

type testActivity struct{ lines []string }

func (a *testActivity) Write(_ context.Context, _ svc.Scope, line string) error {
	a.lines = append(a.lines, line)
	return nil
}

func fixture(t *testing.T) (*Service, *testStore, svc.Scope, *testActivity) {
	t.Helper()
	store := &testStore{bundle: &spec.Bundle{
		App: &spec.App{ID: "crm", Name: "CRM", Space: "personal",
			Budget:  spec.Budget{Month: 20, AtCap: "ask"},
			Records: map[string]*spec.RecordType{"deal": {Label: "Deal"}},
			Triggers: []spec.Trigger{
				{ID: "new-deal", On: spec.TriggerRecordCreated, Type: "deal", Run: "log"},
				{ID: "deal-quiet-7-days", On: spec.TriggerRecordChanged, Type: "deal", When: "quiet", Run: "follow-up"},
				{ID: "monday", On: spec.TriggerSchedule, Cron: "0 9 * * 1", Run: "digest"},
				{ID: "email", On: spec.TriggerConnector, Connector: "gmail", Event: "new-email", Run: "log"},
			},
		},
		Runs: map[string]*spec.Run{
			"log":       {Run: "log", Title: "log on deal"},
			"follow-up": {Run: "follow-up", Title: "draft a follow-up", Steps: []*spec.Step{{Steps: []*spec.Step{{Agent: "CRM agent"}}}}},
			"digest":    {Run: "digest", Title: "stale deals digest", Steps: []*spec.Step{{Agent: "CRM agent"}}},
		},
	}}
	activity := &testActivity{}
	all := &svc.Services{Store: store, Runs: &testRuns{}}
	all.Crews.Activity = activity
	s := New(svc.Config{Root: t.TempDir()}, all)
	s.SetClock(func() time.Time { return testNow })
	return s, store, svc.Scope{App: "crm", Caller: svc.Caller{Member: "mona", Role: svc.RoleEditor}}, activity
}

func requireCode(t *testing.T, err error, code bridge.ErrorCode) {
	t.Helper()
	if code == "" {
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	var typed *bridge.Error
	if !errors.As(err, &typed) || typed.Code != code {
		t.Fatalf("error = %v, want code %s", err, code)
	}
}

func seedSettings(t *testing.T, s *Service, raw string) {
	t.Helper()
	dir := filepath.Join(s.root, "crm")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestListTriggers(t *testing.T) {
	for _, tt := range []struct {
		name, raw string
		cap       bool
		code      bridge.ErrorCode
	}{
		{name: "definition"},
		{name: "persisted pause and fire", raw: `{"triggers":{"monday":{"paused":true,"paused_by":"mona","paused_at":"2026-10-13T08:15:00Z","last_fired":"2026-10-12T09:00:00Z"}}}`},
		{name: "AI at cap", cap: true},
		{name: "damaged settings", raw: "{", code: bridge.CodeInternal},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, _, scope, _ := fixture(t)
			if tt.raw != "" {
				seedSettings(t, s, tt.raw)
			}
			if tt.cap {
				s.all.Runs = &testRuns{rows: []bridge.RunState{{StartedAt: testNow.Format(time.RFC3339), Cost: bridge.Cost{Spent: 20}}}}
			}
			out, err := s.ListTriggers(context.Background(), scope)
			requireCode(t, err, tt.code)
			if err != nil {
				return
			}
			if len(out.Triggers) != 4 {
				t.Fatalf("triggers = %+v", out.Triggers)
			}
			labels := []string{"Deal created → log on deal", "Deal quiet 7 days → draft a follow-up", "Every Monday 09:00 → stale deals digest", "New email · Gmail → log on deal"}
			sources := []bridge.TriggerSource{bridge.TriggerSourceRecord, bridge.TriggerSourceRecord, bridge.TriggerSourceSchedule, bridge.TriggerSourceConnector}
			for i, info := range out.Triggers {
				if info.Label != labels[i] || info.Source != sources[i] || info.AI != (i == 1 || i == 2) {
					t.Fatalf("trigger %d = %+v", i, info)
				}
			}
			schedule := out.Triggers[2]
			if tt.raw != "" {
				if !schedule.Paused || schedule.PausedBy != "mona" || schedule.PausedAt != "2026-10-13T08:15:00Z" || schedule.LastFired != "2026-10-12T09:00:00Z" || schedule.NextFire != "" {
					t.Fatalf("paused schedule = %+v", schedule)
				}
			} else if tt.cap {
				if schedule.NextFire != "" || !schedule.Paused || out.Triggers[0].Paused || out.Triggers[3].Paused {
					t.Fatalf("at cap = %+v", out)
				}
			} else if schedule.NextFire != "2026-10-19T09:00:00Z" {
				t.Fatalf("next fire = %s", schedule.NextFire)
			}
		})
	}
}

func TestPauseTrigger(t *testing.T) {
	for _, tt := range []struct {
		name          string
		role          svc.Role
		id            string
		agent, resume bool
		code          bridge.ErrorCode
	}{
		{name: "editor pauses", role: svc.RoleEditor, id: "deal-quiet-7-days"},
		{name: "editor resumes", role: svc.RoleEditor, id: "deal-quiet-7-days", resume: true},
		{name: "user", role: svc.RoleUser, id: "new-deal", code: bridge.CodeDenied},
		{name: "viewer", role: svc.RoleViewer, id: "new-deal", code: bridge.CodeDenied},
		{name: "agent", role: svc.RoleEditor, id: "new-deal", agent: true, code: bridge.CodeDenied},
		{name: "unknown", role: svc.RoleEditor, id: "missing", code: bridge.CodeNotFound},
		{name: "empty", role: svc.RoleEditor, code: bridge.CodeNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, _, scope, activity := fixture(t)
			scope.Caller.Role, scope.Caller.Agent = tt.role, tt.agent
			if tt.resume {
				seedSettings(t, s, `{"triggers":{"deal-quiet-7-days":{"paused":true,"paused_by":"raj","paused_at":"2026-10-12T08:00:00Z","last_fired":"2026-10-11T09:00:00Z"}}}`)
			}
			params := bridge.PauseTriggerParams{ID: tt.id, Paused: !tt.resume}
			info, err := s.PauseTrigger(context.Background(), scope, params)
			requireCode(t, err, tt.code)
			if err != nil {
				if len(activity.lines) != 0 {
					t.Fatal("denied call wrote Activity")
				}
				return
			}
			verb := "paused"
			if tt.resume {
				verb = "resumed"
				if info.Paused || info.PausedAt != "" || info.PausedBy != "" || info.LastFired != "2026-10-11T09:00:00Z" {
					t.Fatalf("resumed = %+v", info)
				}
			} else if !info.Paused || info.PausedBy != "mona" || info.PausedAt != testNow.Format(time.RFC3339) {
				t.Fatalf("paused = %+v", info)
			}
			want := "@mona " + verb + " Deal quiet 7 days · Undo"
			if !reflect.DeepEqual(activity.lines, []string{want}) {
				t.Fatalf("Activity = %q", activity.lines)
			}
			_, err = s.PauseTrigger(context.Background(), scope, params)
			requireCode(t, err, "")
			if len(activity.lines) != 1 {
				t.Fatal("unchanged pause wrote Activity")
			}
			reopened := New(svc.Config{Root: s.root}, s.all)
			out, err := reopened.ListTriggers(context.Background(), scope)
			requireCode(t, err, "")
			if out.Triggers[1].Paused != !tt.resume {
				t.Fatal("pause did not survive restart")
			}
		})
	}
}

func TestGetBudget(t *testing.T) {
	for _, tt := range []struct {
		name, space               string
		role                      svc.Role
		spent, month              float64
		override, canEdit, paused bool
	}{
		{"personal", "personal", svc.RoleEditor, 6.4, 20, false, true, false},
		{"at cap", "personal", svc.RoleEditor, 20, 20, false, true, true},
		{"over cap", "personal", svc.RoleEditor, 21, 20, false, true, true},
		{"override", "personal", svc.RoleEditor, 6.4, 5, true, true, true},
		{"workspace editor is not Admin", "acme", svc.RoleEditor, 6.4, 20, false, false, false},
		{"viewer", "personal", svc.RoleViewer, 6.4, 20, false, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, store, scope, _ := fixture(t)
			store.bundle.App.Space, scope.Caller.Role = tt.space, tt.role
			s.all.Runs = &testRuns{rows: []bridge.RunState{
				{StartedAt: "2026-10-01T00:00:00Z", Cost: bridge.Cost{Spent: tt.spent}},
				{StartedAt: "2026-09-30T23:00:00Z", Cost: bridge.Cost{Spent: 100}},
				{StartedAt: "2026-11-01T00:00:00Z", Cost: bridge.Cost{Spent: 100}},
				{StartedAt: "2026-10-01T00:00:00Z", Status: bridge.RunRunning, Cost: bridge.Cost{Cap: 10}},
			}}
			if tt.override {
				seedSettings(t, s, `{"month":5}`)
			}
			info, err := s.GetBudget(context.Background(), scope)
			requireCode(t, err, "")
			if info.Month != tt.month || info.Spent != tt.spent || info.CanEdit != tt.canEdit || info.Paused != tt.paused || info.Currency != "USD" || info.AtCap != "ask" || info.ResetsAt != "2026-11-01T00:00:00Z" || !reflect.DeepEqual(info.Presets, []float64{5, 10, 20, 50}) {
				t.Fatalf("budget = %+v", info)
			}
		})
	}
}

func TestSetBudget(t *testing.T) {
	for _, tt := range []struct {
		name, space string
		role        svc.Role
		month       float64
		agent       bool
		code        bridge.ErrorCode
	}{
		{"preset", "personal", svc.RoleEditor, 10, false, ""},
		{"custom", "personal", svc.RoleEditor, 12.5, false, ""},
		{"zero", "personal", svc.RoleEditor, 0, false, bridge.CodeInvalid},
		{"negative", "personal", svc.RoleEditor, -1, false, bridge.CodeInvalid},
		{"NaN", "personal", svc.RoleEditor, math.NaN(), false, bridge.CodeInvalid},
		{"infinite", "personal", svc.RoleEditor, math.Inf(1), false, bridge.CodeInvalid},
		{"user", "personal", svc.RoleUser, 10, false, bridge.CodeDenied},
		{"agent", "personal", svc.RoleEditor, 10, true, bridge.CodeDenied},
		{"workspace editor", "acme", svc.RoleEditor, 10, false, bridge.CodeDenied},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, store, scope, activity := fixture(t)
			store.bundle.App.Space, scope.Caller.Role, scope.Caller.Agent = tt.space, tt.role, tt.agent
			seedSettings(t, s, `{"triggers":{"new-deal":{"paused":true}}}`)
			info, err := s.SetBudget(context.Background(), scope, bridge.SetBudgetParams{Month: tt.month})
			requireCode(t, err, tt.code)
			if err != nil {
				if _, ok := s.Month("crm"); ok || len(activity.lines) != 0 {
					t.Fatal("rejected budget changed settings")
				}
				return
			}
			if info.Month != tt.month || !info.CanEdit || store.bundle.App.Budget.Month != 20 {
				t.Fatalf("budget = %+v", info)
			}
			if len(activity.lines) != 1 {
				t.Fatalf("Activity = %q", activity.lines)
			}
			_, err = s.SetBudget(context.Background(), scope, bridge.SetBudgetParams{Month: tt.month})
			requireCode(t, err, "")
			if len(activity.lines) != 1 {
				t.Fatal("unchanged budget wrote Activity")
			}
			reopened := New(svc.Config{Root: s.root}, s.all)
			if month, ok := reopened.Month("crm"); !ok || month != tt.month {
				t.Fatal("override did not survive restart")
			}
			p, err := reopened.read("crm")
			if err != nil || !p.Triggers["new-deal"].Paused {
				t.Fatal("budget lost trigger settings")
			}
			stat, err := os.Stat(filepath.Join(s.root, "crm", "settings.json"))
			if err != nil || stat.Mode().Perm() != 0o600 {
				t.Fatalf("settings permissions = %v, error = %v", stat, err)
			}
		})
	}
}

func TestAllow(t *testing.T) {
	for _, tt := range []struct {
		name, id, raw string
		states        bridge.LifecycleResult
		lifeErr       error
		atCap, want   bool
	}{
		{name: "active", id: "new-deal", want: true},
		{name: "AI active", id: "deal-quiet-7-days", want: true},
		{name: "schedule", id: "monday", want: true},
		{name: "unknown", id: "missing"},
		{name: "paused", id: "new-deal", raw: `{"triggers":{"new-deal":{"paused":true}}}`},
		{name: "archived", id: "new-deal", states: bridge.LifecycleResult{Archived: []bridge.LifecycleApp{{App: bridge.AppSummary{ID: "crm"}}}}},
		{name: "deleted", id: "new-deal", states: bridge.LifecycleResult{Deleted: []bridge.LifecycleApp{{App: bridge.AppSummary{ID: "crm"}}}}},
		{name: "lifecycle fails closed", id: "new-deal", lifeErr: errors.New("offline")},
		{name: "lifecycle stub", id: "new-deal", lifeErr: bridge.NotBuilt("lifecycle"), want: true},
		{name: "AI cap", id: "deal-quiet-7-days", atCap: true},
		{name: "non AI cap", id: "new-deal", atCap: true, want: true},
		{name: "damaged settings", id: "new-deal", raw: "{"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, _, _, _ := fixture(t)
			s.all.Crews.Lifecycle = testLifecycle{states: tt.states, err: tt.lifeErr}
			if tt.raw != "" {
				seedSettings(t, s, tt.raw)
			}
			if tt.atCap {
				s.all.Runs = &testRuns{rows: []bridge.RunState{{StartedAt: testNow.Format(time.RFC3339), Cost: bridge.Cost{Spent: 20}}}}
			}
			if got := s.Allow("crm", tt.id, testNow); got != tt.want {
				t.Fatalf("Allow = %v, want %v", got, tt.want)
			}
			p, err := s.read("crm")
			if tt.want && (err != nil || p.Triggers[tt.id].LastFired != testNow.Format(time.RFC3339)) {
				t.Fatalf("fire not recorded: %+v, %v", p, err)
			}
			if !tt.want && p.Triggers[tt.id].LastFired != "" {
				t.Fatal("skipped fire was recorded")
			}
		})
	}
}

func TestMonth(t *testing.T) {
	for _, tt := range []struct {
		name, app, raw string
		want           float64
		ok             bool
	}{
		{"absent", "crm", "", 0, false},
		{"override", "crm", `{"month":10}`, 10, true},
		{"damaged", "crm", "{", 0, false},
		{"invalid persisted cap", "crm", `{"month":-1}`, 0, false},
		{"path traversal", "../crm", `{"month":10}`, 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, _, _, _ := fixture(t)
			if tt.raw != "" {
				seedSettings(t, s, tt.raw)
			}
			if month, ok := s.Month(tt.app); month != tt.want || ok != tt.ok {
				t.Fatalf("Month = %g, %v", month, ok)
			}
		})
	}
}

func TestConcurrentFiresKeepAllSettings(t *testing.T) {
	s, store, scope, _ := fixture(t)
	s.all.Crews.Activity = nil
	store.bundle.App.Triggers = nil
	for i := 0; i < 20; i++ {
		store.bundle.App.Triggers = append(store.bundle.App.Triggers, spec.Trigger{ID: string(rune('a' + i)), On: spec.TriggerRecordCreated, Run: "log"})
	}
	var wg sync.WaitGroup
	for _, trigger := range store.bundle.App.Triggers {
		wg.Go(func() {
			if !s.Allow("crm", trigger.ID, testNow) {
				t.Errorf("fire denied: %s", trigger.ID)
			}
		})
	}
	wg.Go(func() {
		_, err := s.SetBudget(context.Background(), scope, bridge.SetBudgetParams{Month: 10})
		if err != nil {
			t.Error(err)
		}
	})
	wg.Wait()
	p, err := s.read("crm")
	if err != nil || len(p.Triggers) != 20 || p.Month == nil || *p.Month != 10 {
		t.Fatalf("settings lost a concurrent write: %+v, %v", p, err)
	}
}

func TestMethodsRejectUnreadableState(t *testing.T) {
	methods := []struct {
		name string
		call func(*Service, svc.Scope) error
	}{
		{"triggers.list", func(s *Service, scope svc.Scope) error {
			_, err := s.ListTriggers(context.Background(), scope)
			return err
		}},
		{"triggers.pause", func(s *Service, scope svc.Scope) error {
			_, err := s.PauseTrigger(context.Background(), scope, bridge.PauseTriggerParams{ID: "monday", Paused: true})
			return err
		}},
		{"budget.get", func(s *Service, scope svc.Scope) error {
			_, err := s.GetBudget(context.Background(), scope)
			return err
		}},
		{"budget.set", func(s *Service, scope svc.Scope) error {
			_, err := s.SetBudget(context.Background(), scope, bridge.SetBudgetParams{Month: 10})
			return err
		}},
	}
	for _, method := range methods {
		for _, problem := range []struct {
			name  string
			code  bridge.ErrorCode
			setup func(*testing.T, *Service, *testStore, *svc.Scope)
		}{
			{"missing app", bridge.CodeNotFound, func(_ *testing.T, _ *Service, _ *testStore, scope *svc.Scope) { scope.App = "missing" }},
			{"invalid id", bridge.CodeInvalid, func(_ *testing.T, _ *Service, _ *testStore, scope *svc.Scope) { scope.App = "../crm" }},
			{"store unavailable", bridge.CodeInternal, func(_ *testing.T, s *Service, _ *testStore, _ *svc.Scope) { s.all.Store = nil }},
			{"bad JSON", bridge.CodeInternal, func(t *testing.T, s *Service, _ *testStore, _ *svc.Scope) { seedSettings(t, s, "{") }},
			{"runs unavailable", bridge.CodeInternal, func(_ *testing.T, s *Service, _ *testStore, _ *svc.Scope) { s.all.Runs = nil }},
			{"runs read fails", bridge.CodeInternal, func(_ *testing.T, s *Service, _ *testStore, _ *svc.Scope) {
				s.all.Runs = &testRuns{err: bridge.Errorf(bridge.CodeInternal, "journal unavailable")}
			}},
		} {
			t.Run(method.name+"/"+problem.name, func(t *testing.T) {
				s, store, scope, activity := fixture(t)
				problem.setup(t, s, store, &scope)
				requireCode(t, method.call(s, scope), problem.code)
				if len(activity.lines) != 0 {
					t.Fatal("failed method wrote Activity")
				}
			})
		}
	}
}

func TestBudgetMonthUsesUTCAndRollsOver(t *testing.T) {
	for _, tt := range []struct {
		at, reset string
		spent     float64
	}{
		{"2026-10-31T23:59:59Z", "2026-11-01T00:00:00Z", 3},
		{"2026-11-01T00:00:00Z", "2026-12-01T00:00:00Z", 2},
		{"2026-12-31T23:59:59Z", "2027-01-01T00:00:00Z", 1},
	} {
		t.Run(tt.at, func(t *testing.T) {
			s, store, scope, _ := fixture(t)
			at, _ := time.Parse(time.RFC3339, tt.at)
			s.SetClock(func() time.Time { return at })
			store.bundle.App.Budget.AtCap = "stop"
			s.all.Runs = &testRuns{rows: []bridge.RunState{
				{StartedAt: "2026-10-31T23:59:59Z", Cost: bridge.Cost{Spent: 2}},
				{StartedAt: "2026-10-31T20:00:00-04:00", Cost: bridge.Cost{Spent: 1}},
				{StartedAt: "invalid", Cost: bridge.Cost{Spent: 1}},
			}}
			info, err := s.GetBudget(context.Background(), scope)
			requireCode(t, err, "")
			if info.Spent != tt.spent || info.ResetsAt != tt.reset || info.AtCap != "stop" {
				t.Fatalf("budget = %+v", info)
			}
		})
	}
}

func TestSkippedAndOutOfOrderEventsKeepLastFire(t *testing.T) {
	s, _, scope, _ := fixture(t)
	if !s.Allow("crm", "new-deal", testNow) || !s.Allow("crm", "new-deal", testNow.Add(-time.Hour)) {
		t.Fatal("active fire denied")
	}
	_, err := s.PauseTrigger(context.Background(), scope, bridge.PauseTriggerParams{ID: "new-deal", Paused: true})
	if err != nil || s.Allow("crm", "new-deal", testNow.Add(time.Hour)) {
		t.Fatalf("paused fire allowed, pause error = %v", err)
	}
	p, err := s.read("crm")
	if err != nil || p.Triggers["new-deal"].LastFired != testNow.Format(time.RFC3339) {
		t.Fatalf("last fire = %+v, %v", p, err)
	}
}

func TestAtomicWriteFailureKeepsPreviousSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	previous := filepath.Join(path, "previous")
	if err := os.WriteFile(previous, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(path, []byte(`{"month":10}`)); err == nil {
		t.Fatal("replacement of a directory succeeded")
	}
	raw, err := os.ReadFile(previous)
	if err != nil || string(raw) != "kept" {
		t.Fatal("failed replacement changed previous state")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary file left behind: %v, %v", entries, err)
	}
}
