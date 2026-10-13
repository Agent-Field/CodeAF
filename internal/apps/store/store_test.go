package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

var (
	testCtx  = context.Background()
	owner    = svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}
	testTime = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
)

type eventLog struct{ events []svc.Event }

func (h *eventLog) Publish(topic string, data any) {
	h.events = append(h.events, svc.Event{Topic: topic, Data: data})
}
func (h *eventLog) Subscribe(string) (<-chan svc.Event, func()) { return nil, func() {} }

func newStore(t *testing.T) (*Service, *eventLog) {
	t.Helper()
	hub := &eventLog{}
	s := NewWithOptions(svc.Config{Root: filepath.Join(t.TempDir(), "apps")}, &svc.Services{Hub: hub}, Options{WithoutDemo: true})
	s.now = func() time.Time { return testTime }
	return s, hub
}

func create(t *testing.T, s *Service) bridge.AppSummary {
	t.Helper()
	m, err := s.Create(testCtx, owner, "CRM", "A CRM for the sales pipeline")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func preview(t *testing.T, s *Service) bridge.AppSummary {
	t.Helper()
	m := create(t, s)
	files, _ := spec.GoldenCRM()
	if err := s.WriteFiles(testCtx, m.ID, files); err != nil {
		t.Fatal(err)
	}
	for _, state := range []bridge.AppState{bridge.StateBuilding, bridge.StatePreview} {
		if err := s.SetState(testCtx, m.ID, state); err != nil {
			t.Fatal(err)
		}
	}
	return m
}

func kept(t *testing.T, s *Service) bridge.AppSummary {
	t.Helper()
	m := preview(t, s)
	m, err := s.Keep(testCtx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func expectCode(t *testing.T, err error, code bridge.ErrorCode) {
	t.Helper()
	var typed *bridge.Error
	if !errors.As(err, &typed) || typed.Code != code {
		t.Fatalf("error = %v, want %s", err, code)
	}
}

func TestCreateRegistry(t *testing.T) {
	s, hub := newStore(t)
	apps, err := s.List(testCtx, owner)
	if err != nil || len(apps) != 0 {
		t.Fatalf("fresh registry = %v, %v", apps, err)
	}
	m := create(t, s)
	if m.ID != "crm" || m.State != bridge.StatePlanning || m.Version != 0 || m.Space != "personal" || m.UpdatedAt != testTime.Format(time.RFC3339) {
		t.Fatalf("created = %+v", m)
	}
	second := create(t, s)
	if second.ID != "crm-2" {
		t.Fatalf("second id = %q", second.ID)
	}
	for _, name := range []string{"records.db", "sample.db"} {
		if _, err := os.Stat(filepath.Join(s.appDir(m.ID), name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("store created %s: %v", name, err)
		}
	}
	repo, err := git.PlainOpen(s.definitionDir(m.ID))
	if err != nil {
		t.Fatal(err)
	}
	head, err := repo.Reference(plumbing.HEAD, false)
	if err != nil || head.Target() != draftBranch {
		t.Fatalf("draft HEAD = %v, %v", head, err)
	}
	if _, err := repo.Head(); !errors.Is(err, plumbing.ErrReferenceNotFound) {
		t.Fatalf("draft already committed: %v", err)
	}
	detail, err := s.Get(testCtx, m.ID)
	if err != nil || len(detail.Files) != 0 {
		t.Fatalf("new draft = %+v, %v", detail, err)
	}
	reopened := New(svc.Config{Root: s.root}, nil)
	apps, err = reopened.List(testCtx, owner)
	if err != nil || len(apps) != 2 {
		t.Fatalf("reopened = %+v, %v", apps, err)
	}
	apps, err = reopened.List(testCtx, svc.Caller{Member: "someone-else"})
	if err != nil || len(apps) != 0 {
		t.Fatalf("private apps visible to another member: %+v, %v", apps, err)
	}
	if len(hub.events) != 2 || hub.events[0].Topic != bridge.TopicState(m.ID) || hub.events[0].Data.(bridge.AppSummary) != m {
		t.Fatalf("create events = %+v", hub.events)
	}
}

func TestSlugAllocation(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"Sales CRM!", "sales-crm"}, {"123 invoices", "app-123-invoices"}, {"日本語", "app"}, {"***", "app"}, {strings.Repeat("a", 90), strings.Repeat("a", 48)}, {"Already__here", "already-here"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newStore(t)
			m, err := s.Create(testCtx, owner, tc.name, "paragraph")
			if err != nil || m.ID != tc.want || !validID(m.ID) {
				t.Fatalf("id = %q, %v, want %q", m.ID, err, tc.want)
			}
			second, err := s.Create(testCtx, owner, tc.name, "paragraph")
			if err != nil || second.ID == m.ID || !validID(second.ID) {
				t.Fatalf("second id = %q, %v", second.ID, err)
			}
		})
	}
}

func TestCreateRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		caller svc.Caller
		code   bridge.ErrorCode
	}{
		{" ", owner, bridge.CodeInvalid}, {"CRM", svc.Caller{Member: "other"}, bridge.CodeDenied}, {"CRM", svc.Caller{Member: svc.Owner, Agent: true}, bridge.CodeDenied},
	} {
		t.Run(tc.name+string(tc.code), func(t *testing.T) {
			s, hub := newStore(t)
			_, err := s.Create(testCtx, tc.caller, tc.name, "paragraph")
			expectCode(t, err, tc.code)
			if len(hub.events) != 0 {
				t.Fatal("failed create published an event")
			}
		})
	}
}

func TestWriteDraftReplacement(t *testing.T) {
	s, _ := newStore(t)
	m := create(t, s)
	planOnly := spec.Files{spec.PlanFile: []byte(`{"ask":"a paragraph","name":"CRM","needs":"computed"}`)}
	if err := s.WriteFiles(testCtx, m.ID, planOnly); err != nil {
		t.Fatal(err)
	}
	detail, err := s.Get(testCtx, m.ID)
	if err != nil || len(detail.Files) != 1 || detail.Files[spec.PlanFile] != string(planOnly[spec.PlanFile]) {
		t.Fatalf("plan draft = %+v, %v", detail, err)
	}
	files, _ := spec.GoldenCRM()
	if err := s.WriteFiles(testCtx, m.ID, files); err != nil {
		t.Fatal(err)
	}
	delete(files, "screens/deal.json")
	if err := s.WriteFiles(testCtx, m.ID, files); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.definitionDir(m.ID), "screens/deal.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed screen remains: %v", err)
	}
	second := create(t, s)
	if err := s.WriteFiles(testCtx, second.ID, files); err != nil {
		t.Fatal(err)
	}
	def, err := s.Definition(testCtx, second.ID)
	if err != nil || def.App.ID != second.ID || def.App.Space != "personal" || def.App.Version != 0 {
		t.Fatalf("draft identity = %+v, %v", def, err)
	}
	files[spec.PlanFile][0] = '!'
	def.Plan.Ask = "caller mutation"
	again, err := s.Definition(testCtx, second.ID)
	if err != nil || again.Plan.Ask == def.Plan.Ask {
		t.Fatalf("caller changed stored definition: %+v, %v", again, err)
	}
}

func TestUnsafePathsLeaveDraftIntact(t *testing.T) {
	for _, name := range []string{"../outside", "/absolute", "screens/../../outside.json", ".git/config", "records.db", "sample.json", "custom/foo.jsx", "screens/foo/bar.json", "screens/.json", "screens/hello\\there.json", "runs/name.yml"} {
		t.Run(name, func(t *testing.T) {
			s, hub := newStore(t)
			m := create(t, s)
			files := spec.Files{spec.PlanFile: []byte("original")}
			if err := s.WriteFiles(testCtx, m.ID, files); err != nil {
				t.Fatal(err)
			}
			files[name] = []byte("unsafe")
			expectCode(t, s.WriteFiles(testCtx, m.ID, files), bridge.CodeInvalid)
			detail, err := s.Get(testCtx, m.ID)
			if err != nil || len(detail.Files) != 1 || detail.Files[spec.PlanFile] != "original" {
				t.Fatalf("failed write changed files: %+v, %v", detail, err)
			}
			if len(hub.events) != 1 {
				t.Fatal("file writes changed state events")
			}
		})
	}
}

func TestInvalidBuildCanReachCheck(t *testing.T) {
	for _, data := range []string{`{`, `{"budget":"invalid type"}`, `{"name":7,"version":"invalid type"}`} {
		t.Run(data, func(t *testing.T) {
			s, _ := newStore(t)
			m := create(t, s)
			if err := s.WriteFiles(testCtx, m.ID, spec.Files{spec.AppFile: []byte(data)}); err != nil {
				t.Fatalf("invalid build could not be saved for Check: %v", err)
			}
			got, err := s.Get(testCtx, m.ID)
			if err != nil || got.Name != m.Name || got.Files[spec.AppFile] == "" {
				t.Fatalf("invalid draft = %+v, %v", got, err)
			}
			for _, state := range []bridge.AppState{bridge.StateBuilding, bridge.StatePreview} {
				if err := s.SetState(testCtx, m.ID, state); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Keep(testCtx, m.ID); err == nil {
				t.Fatal("invalid build became v1")
			}
			got, err = s.Get(testCtx, m.ID)
			if err != nil || got.State != bridge.StatePreview || got.Version != 0 {
				t.Fatalf("invalid Keep changed state: %+v, %v", got, err)
			}
		})
	}
}

func TestStateTransitionsAndEvents(t *testing.T) {
	for _, tc := range []struct {
		from, to bridge.AppState
		allowed  bool
	}{
		{bridge.StatePlanning, bridge.StateBuilding, true}, {bridge.StatePlanning, bridge.StatePreview, false},
		{bridge.StatePlanning, bridge.StateKept, false}, {bridge.StateBuilding, bridge.StatePreview, true},
		{bridge.StateBuilding, bridge.StateFailed, true}, {bridge.StateBuilding, bridge.StatePlanning, true},
		{bridge.StateFailed, bridge.StateBuilding, true}, {bridge.StateFailed, bridge.StatePlanning, true},
		{bridge.StateFailed, bridge.StatePreview, false}, {bridge.StatePreview, bridge.StatePlanning, true},
		{bridge.StatePreview, bridge.StateBuilding, true}, {bridge.StatePreview, bridge.StateKept, false},
		{bridge.StateKept, StateArchived, true}, {StateArchived, bridge.StateKept, true},
		{bridge.StateKept, bridge.StateBuilding, false}, {StateArchived, bridge.StatePlanning, false},
		{bridge.StatePlanning, StateDiscarded, false}, {bridge.StateBuilding, "unknown", false},
	} {
		t.Run(string(tc.from)+"/"+string(tc.to), func(t *testing.T) {
			s, hub := newStore(t)
			app := create(t, s)
			m, err := s.load(app.ID)
			if err != nil {
				t.Fatal(err)
			}
			m.State = tc.from
			if err := s.save(&m); err != nil {
				t.Fatal(err)
			}
			s.now = func() time.Time { return testTime.Add(time.Minute) }
			err = s.SetState(testCtx, m.ID, tc.to)
			if !tc.allowed {
				expectCode(t, err, bridge.CodeConflict)
				if len(hub.events) != 1 {
					t.Fatal("refused transition published")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			moved, err := s.load(m.ID)
			if err != nil || moved.State != tc.to || moved.UpdatedAt == m.UpdatedAt {
				t.Fatalf("state = %+v, %v", moved, err)
			}
			if len(hub.events) != 2 || hub.events[1].Topic != bridge.TopicState(m.ID) || hub.events[1].Data.(bridge.AppSummary) != moved.AppSummary {
				t.Fatalf("events = %+v", hub.events)
			}
			if err := s.SetState(testCtx, m.ID, tc.to); err != nil || len(hub.events) != 2 {
				t.Fatalf("repeated state = %v, events = %+v", err, hub.events)
			}
		})
	}
}

func TestKeepCreatesImmutableV1(t *testing.T) {
	s, hub := newStore(t)
	m := kept(t, s)
	if m.State != bridge.StateKept || m.Version != 1 {
		t.Fatalf("kept = %+v", m)
	}
	def, err := s.Definition(testCtx, m.ID)
	if err != nil || def.App.Version != 1 || def.App.ID != m.ID || def.App.Space != "personal" {
		t.Fatalf("kept definition = %+v, %v", def, err)
	}
	versions, err := s.Versions(testCtx, m.ID)
	if err != nil || len(versions) != 1 || versions[0].Version != 1 || versions[0].Tag != "v1" || versions[0].By != svc.Owner || versions[0].Request != def.Plan.Ask || versions[0].At != testTime.Format(time.RFC3339) {
		t.Fatalf("versions = %+v, %v", versions, err)
	}
	if err := os.WriteFile(filepath.Join(s.definitionDir(m.ID), spec.AppFile), []byte("corrupted working tree"), 0600); err != nil {
		t.Fatal(err)
	}
	detail, err := s.Get(testCtx, m.ID)
	if err != nil || !strings.Contains(detail.Files[spec.AppFile], `"version": 1`) {
		t.Fatalf("kept read used mutable files: %+v, %v", detail, err)
	}
	reopened := New(svc.Config{Root: s.root}, nil)
	if got, err := reopened.Get(testCtx, m.ID); err != nil || !reflect.DeepEqual(got, detail) {
		t.Fatalf("reopened = %+v, %v", got, err)
	}
	if len(hub.events) != 4 || hub.events[3].Data.(bridge.AppSummary) != m {
		t.Fatalf("keep events = %+v", hub.events)
	}
	expectCode(t, s.WriteFiles(testCtx, m.ID, spec.Files{}), bridge.CodeConflict)
	_, err = s.Keep(testCtx, m.ID)
	expectCode(t, err, bridge.CodeConflict)
	expectCode(t, s.Discard(testCtx, m.ID), bridge.CodeConflict)
}

func TestKeepRefusalsDoNotCreateVersion(t *testing.T) {
	for _, state := range []bridge.AppState{bridge.StatePlanning, bridge.StateBuilding, bridge.StateFailed, bridge.StatePreview} {
		t.Run(string(state), func(t *testing.T) {
			s, hub := newStore(t)
			app := create(t, s)
			m, _ := s.load(app.ID)
			m.State = state
			if err := s.save(&m); err != nil {
				t.Fatal(err)
			}
			_, err := s.Keep(testCtx, m.ID)
			if state == bridge.StatePreview {
				expectCode(t, err, bridge.CodeInvalid)
			} else {
				expectCode(t, err, bridge.CodeConflict)
			}
			versions, err := s.Versions(testCtx, m.ID)
			if err != nil || len(versions) != 0 || len(hub.events) != 1 {
				t.Fatalf("refused keep = %+v, %v, events=%+v", versions, err, hub.events)
			}
		})
	}
}

func TestRestoreAppendsAndKeepsRecords(t *testing.T) {
	s, hub := newStore(t)
	m := kept(t, s)
	v1, err := s.GetVersion(testCtx, m.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	recordsPath := filepath.Join(s.appDir(m.ID), "records.db")
	if err := os.WriteFile(recordsPath, []byte("records owned by another lane"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, version := range []int{2, 3} {
		s.now = func() time.Time { return testTime.Add(time.Duration(version) * time.Minute) }
		got, err := s.Restore(testCtx, m.ID, 1)
		if err != nil || got.Version != version {
			t.Fatalf("restore = %+v, %v", got, err)
		}
	}
	versions, err := s.Versions(testCtx, m.ID)
	if err != nil || len(versions) != 3 || versions[0].Version != 3 || versions[1].Request != "restore of v1" {
		t.Fatalf("versions = %+v, %v", versions, err)
	}
	v1Again, err := s.GetVersion(testCtx, m.ID, 1)
	if err != nil || !reflect.DeepEqual(v1Again.Files, v1.Files) {
		t.Fatalf("restore rewrote v1: %+v, %v", v1Again, err)
	}
	current, err := s.Definition(testCtx, m.ID)
	if err != nil || current.App.Version != 3 {
		t.Fatalf("restored definition = %+v, %v", current, err)
	}
	if data, err := os.ReadFile(recordsPath); err != nil || string(data) != "records owned by another lane" {
		t.Fatalf("restore changed records: %q, %v", data, err)
	}
	repo, err := git.PlainOpen(s.definitionDir(m.ID))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{2, 3} {
		commit, err := taggedCommit(repo, n)
		if err != nil || len(commit.ParentHashes) != 1 || commit.ParentHashes[0].String() != versions[4-n].Commit {
			t.Fatalf("v%d did not append history: %v, %v", n, commit, err)
		}
	}
	if len(hub.events) != 6 || hub.events[5].Data.(bridge.AppSummary).Version != 3 {
		t.Fatalf("restore events = %+v", hub.events)
	}
	_, err = s.Restore(testCtx, m.ID, 999)
	expectCode(t, err, bridge.CodeNotFound)
	_, err = s.GetVersion(testCtx, m.ID, 0)
	expectCode(t, err, bridge.CodeInvalid)
}

func TestArchiveKeepsDefinitionAndRecords(t *testing.T) {
	s, hub := newStore(t)
	m := kept(t, s)
	before, err := s.Get(testCtx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.appDir(m.ID), "records.db")
	if err := os.WriteFile(path, []byte("real records"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Archive(testCtx, m.ID); err != nil {
		t.Fatal(err)
	}
	archived, err := s.Get(testCtx, m.ID)
	if err != nil || archived.State != StateArchived || !reflect.DeepEqual(before.Files, archived.Files) {
		t.Fatalf("archived = %+v, %v", archived, err)
	}
	apps, err := s.List(testCtx, owner)
	if err != nil || len(apps) != 1 || apps[0].State != StateArchived {
		t.Fatalf("archived listing = %+v, %v", apps, err)
	}
	_, err = s.Restore(testCtx, m.ID, 1)
	expectCode(t, err, bridge.CodeConflict)
	if err := s.Unarchive(testCtx, m.ID); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "real records" {
		t.Fatalf("archive changed records: %q, %v", data, err)
	}
	if len(hub.events) != 6 || hub.events[4].Data.(bridge.AppSummary).State != StateArchived || hub.events[5].Data.(bridge.AppSummary).State != bridge.StateKept {
		t.Fatalf("archive events = %+v", hub.events)
	}
}

func TestDeleteRetentionAndRecovery(t *testing.T) {
	for _, tc := range []struct {
		name    string
		elapsed time.Duration
		recover bool
	}{
		{"just deleted", 0, true}, {"before cutoff", deletedRetention - time.Nanosecond, true}, {"at cutoff", deletedRetention, false}, {"after cutoff", deletedRetention + time.Hour, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, hub := newStore(t)
			m := kept(t, s)
			path := filepath.Join(s.appDir(m.ID), "records.db")
			if err := os.WriteFile(path, []byte("real records"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := s.Delete(testCtx, owner, m.ID, m.Name); err != nil {
				t.Fatal(err)
			}
			_, err := s.Get(testCtx, m.ID)
			expectCode(t, err, bridge.CodeNotFound)
			apps, err := s.List(testCtx, owner)
			if err != nil || len(apps) != 0 {
				t.Fatalf("deleted app still listed: %+v, %v", apps, err)
			}
			if len(hub.events) != 5 || hub.events[4].Data.(bridge.AppSummary).State != StateDeleted {
				t.Fatalf("delete events = %+v", hub.events)
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != "real records" {
				t.Fatalf("delete removed records early: %q, %v", data, err)
			}
			// Reopening proves recovery is driven by persisted deletion time.
			s = New(svc.Config{Root: s.root}, &svc.Services{Hub: hub})
			s.now = func() time.Time { return testTime.Add(tc.elapsed) }
			got, err := s.RestoreDeleted(testCtx, owner, m.ID)
			if tc.recover {
				if err != nil || got.State != bridge.StateKept || got.Version != 1 {
					t.Fatalf("recovery = %+v, %v", got, err)
				}
			} else {
				expectCode(t, err, bridge.CodeConflict)
			}
			if err := s.PurgeDeleted(testCtx); err != nil {
				t.Fatal(err)
			}
			_, err = os.Stat(path)
			if tc.recover && err != nil {
				t.Fatalf("purged recovered records: %v", err)
			}
			if !tc.recover && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("expired records remain: %v", err)
			}
		})
	}
}

func TestDeleteRefusals(t *testing.T) {
	for _, tc := range []struct {
		name         string
		caller       svc.Caller
		confirmation string
		code         bridge.ErrorCode
	}{
		{"wrong name", owner, "crm", bridge.CodeInvalid}, {"wrong member", svc.Caller{Member: "other"}, "CRM", bridge.CodeDenied}, {"agent", svc.Caller{Member: svc.Owner, Agent: true}, "CRM", bridge.CodeDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, hub := newStore(t)
			m := kept(t, s)
			expectCode(t, s.Delete(testCtx, tc.caller, m.ID, tc.confirmation), tc.code)
			if got, err := s.Get(testCtx, m.ID); err != nil || got.State != bridge.StateKept {
				t.Fatalf("refused deletion changed app: %+v, %v", got, err)
			}
			if len(hub.events) != 4 {
				t.Fatal("refused delete published")
			}
		})
	}
}

func TestDefinitionRollbackOnMetadataFailure(t *testing.T) {
	s, hub := newStore(t)
	m := preview(t, s)
	before, err := s.Get(testCtx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(s.appDir(m.ID), metadataFile)
	state, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var injected bool
	s.now = func() time.Time {
		if !injected {
			injected = true
			if err := os.Remove(statePath); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(statePath, 0700); err != nil {
				t.Fatal(err)
			}
		}
		return testTime
	}
	if _, err := s.Keep(testCtx, m.ID); err == nil {
		t.Fatal("Keep succeeded without durable metadata")
	}
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, state, 0600); err != nil {
		t.Fatal(err)
	}
	after, err := s.Get(testCtx, m.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("failed Keep changed app: %+v, %v", after, err)
	}
	versions, err := s.Versions(testCtx, m.ID)
	if err != nil || len(versions) != 0 {
		t.Fatalf("failed Keep left tags: %+v, %v", versions, err)
	}
	if len(hub.events) != 3 {
		t.Fatal("failed Keep published")
	}
}

func TestSymlinksCannotRedirectDefinitionWrites(t *testing.T) {
	for _, tc := range []struct{ name, relative string }{
		{"file", "plan.json"}, {"screens directory", "screens"}, {"git directory", ".git"}, {"definition root", "."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newStore(t)
			m := create(t, s)
			outside := t.TempDir()
			marker := filepath.Join(outside, "marker")
			if err := os.WriteFile(marker, []byte("untouched"), 0600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(s.definitionDir(m.ID), tc.relative)
			if err := os.RemoveAll(link); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, link); err != nil {
				t.Fatal(err)
			}
			expectCode(t, s.WriteFiles(testCtx, m.ID, spec.Files{spec.PlanFile: []byte("new")}), bridge.CodeInvalid)
			data, err := os.ReadFile(marker)
			if err != nil || string(data) != "untouched" {
				t.Fatalf("outside changed: %q, %v", data, err)
			}
		})
	}
}

func TestStoreErrors(t *testing.T) {
	s, _ := newStore(t)
	for _, id := range []string{"../escape", "/root", "", "UPPER"} {
		_, err := s.Get(testCtx, id)
		expectCode(t, err, bridge.CodeInvalid)
	}
	_, err := s.Get(testCtx, "missing")
	expectCode(t, err, bridge.CodeNotFound)
	ctx, cancel := context.WithCancel(testCtx)
	cancel()
	if _, err := s.Create(ctx, owner, "CRM", "paragraph"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled create = %v", err)
	}
	if _, err := s.List(ctx, owner); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled list = %v", err)
	}
	_, err = New(svc.Config{}, nil).List(testCtx, owner)
	expectCode(t, err, bridge.CodeInvalid)
}

func TestDiscardRemovesEverything(t *testing.T) {
	for _, state := range []bridge.AppState{bridge.StatePlanning, bridge.StateBuilding, bridge.StatePreview, bridge.StateFailed} {
		t.Run(string(state), func(t *testing.T) {
			s, hub := newStore(t)
			app := create(t, s)
			m, _ := s.load(app.ID)
			m.State = state
			if err := s.save(&m); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(s.appDir(m.ID), "sample.db"), []byte("sample"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := s.Discard(testCtx, m.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(s.appDir(m.ID)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("discarded folder remains: %v", err)
			}
			_, err := s.Get(testCtx, m.ID)
			expectCode(t, err, bridge.CodeNotFound)
			if len(hub.events) != 2 || hub.events[1].Data.(bridge.AppSummary).State != StateDiscarded {
				t.Fatalf("discard events = %+v", hub.events)
			}
		})
	}
}

func TestCreateConcurrentIDs(t *testing.T) {
	s, _ := newStore(t)
	const count = 12
	results := make(chan bridge.AppSummary, count)
	errs := make(chan error, count)
	var workers sync.WaitGroup
	for range count {
		workers.Go(func() { m, err := s.Create(testCtx, owner, "CRM", "paragraph"); results <- m; errs <- err })
	}
	workers.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	ids := map[string]bool{}
	for m := range results {
		if ids[m.ID] {
			t.Fatalf("duplicate id %q", m.ID)
		}
		ids[m.ID] = true
	}
	if len(ids) != count {
		t.Fatalf("got %d ids", len(ids))
	}
}
