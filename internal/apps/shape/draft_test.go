package shape

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/caps"
	"github.com/Agent-Field/codeaf/internal/apps/realtime"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func setupDraft(t *testing.T) (*Service, svc.Config, *svc.Services, svc.Scope) {
	t.Helper()
	cfg := svc.Config{Root: t.TempDir()}
	all := &svc.Services{}
	all.Store = store.New(cfg, all)
	all.Records = records.New(cfg, all)
	all.Hub = realtime.New(cfg, all)
	all.Caps = caps.New(cfg, all)
	if err := all.Store.(*store.Service).SeedGoldenCRM(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := New(cfg, all)
	all.Shape = s
	scope := svc.Scope{App: "crm", Caller: svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}, Data: svc.DataReal}
	return s, cfg, all, scope
}

func openDraft(t *testing.T, s *Service, scope svc.Scope) *draft {
	t.Helper()
	if _, err := s.Open(context.Background(), scope, bridge.Empty{}); err != nil {
		t.Fatal(err)
	}
	d, err := s.draftFor(scope)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func needCode(t *testing.T, err error, code bridge.ErrorCode) {
	t.Helper()
	var typed *bridge.Error
	if !errors.As(err, &typed) || typed.Code != code {
		t.Fatalf("error = %v, want %s", err, code)
	}
}

func needEvent(t *testing.T, events <-chan svc.Event, member, state string) {
	t.Helper()
	select {
	case event := <-events:
		value, ok := event.Data.(bridge.ShapeEvent)
		if !ok || value.Kind != "draft" || value.Draft == nil || value.Draft.Member != member || value.Draft.State != state || value.At == "" {
			t.Fatalf("event = %+v", event)
		}
	default:
		t.Fatal("missing draft event")
	}
}

func TestOpenGetRestartAndDiscard(t *testing.T) {
	s, cfg, all, scope := setupDraft(t)
	ctx := context.Background()
	got, err := s.Get(ctx, scope, bridge.Empty{})
	if err != nil || got.Draft != nil {
		t.Fatalf("get before open = %+v, %v", got, err)
	}
	realBefore, err := all.Records.Query(ctx, scope, bridge.QueryParams{Type: "company"})
	if err != nil {
		t.Fatal(err)
	}
	events, cancel := all.Hub.Subscribe(bridge.TopicShape(scope.App, scope.Caller.Member))
	defer cancel()
	opened, err := s.Open(ctx, scope, bridge.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	if opened.Base != 1 || opened.State != "open" || opened.Changes == nil || len(opened.Changes) != 0 {
		t.Fatalf("draft = %+v", opened)
	}
	needEvent(t, events, svc.Owner, "open")
	dir := filepath.Join(cfg.Root, "crm", "drafts", svc.Owner)
	for _, name := range []string{"draft.json", "files/app.json", "overlay.db"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	base, err := all.Store.Definition(ctx, "crm")
	if err != nil {
		t.Fatal(err)
	}
	files, err := spec.ReadDir(filepath.Join(dir, "files"))
	if err != nil || !reflect.DeepEqual(files, base.Files) {
		t.Fatalf("files differ from definition: %v", err)
	}
	opened.Changes = append(opened.Changes, bridge.Change{ID: "caller-edit"})
	reopened, err := s.Open(ctx, scope, bridge.Empty{})
	if err != nil || len(reopened.Changes) != 0 {
		t.Fatalf("reopen = %+v, %v", reopened, err)
	}
	needEvent(t, events, svc.Owner, "open")
	for _, member := range []string{"raj", "mona"} {
		other := scope
		other.Caller.Member = member
		if result, err := s.Open(ctx, other, bridge.Empty{}); err != nil || result.Member != member {
			t.Fatalf("member %s: %+v, %v", member, result, err)
		}
	}
	restarted := New(cfg, all)
	for _, member := range []string{svc.Owner, "raj", "mona"} {
		other := scope
		other.Caller.Member = member
		result, err := restarted.Get(ctx, other, bridge.Empty{})
		if err != nil || result.Draft == nil || result.Draft.Member != member {
			t.Fatalf("restart %s: %+v, %v", member, result, err)
		}
	}
	if _, err := restarted.Discard(ctx, scope, bridge.Empty{}); err != nil {
		t.Fatal(err)
	}
	needEvent(t, events, svc.Owner, "discarded")
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("draft folder remains: %v", err)
	}
	realAfter, err := all.Records.Query(ctx, scope, bridge.QueryParams{Type: "company"})
	if err != nil || !reflect.DeepEqual(realBefore, realAfter) {
		t.Fatalf("discard changed real records: %v", err)
	}
	got, err = New(cfg, all).Get(ctx, scope, bridge.Empty{})
	if err != nil || got.Draft != nil {
		t.Fatalf("discard restored: %+v, %v", got, err)
	}
}

func TestDraftInvalidScopeAndCorruptRestart(t *testing.T) {
	s, cfg, all, scope := setupDraft(t)
	for _, member := range []string{"", ".", "..", "../raj", "raj/other", "raj\\other", "raj\x00"} {
		t.Run(member, func(t *testing.T) {
			invalidScope := scope
			invalidScope.Caller.Member = member
			_, err := s.Open(context.Background(), invalidScope, bridge.Empty{})
			needCode(t, err, bridge.CodeInvalid)
		})
	}
	openDraft(t, s, scope)
	name := filepath.Join(cfg.Root, "crm", "drafts", svc.Owner, "draft.json")
	if err := os.WriteFile(name, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(cfg, all).Get(context.Background(), scope, bridge.Empty{}); err == nil {
		t.Fatal("corrupt draft was silently lost")
	}
}

func TestRestartIgnoresUncommittedOpeningDirectory(t *testing.T) {
	s, cfg, all, scope := setupDraft(t)
	d := openDraft(t, s, scope)
	stage := filepath.Join(cfg.Root, scope.App, "drafts", ".opening-interrupted")
	if err := os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(d.dir, "draft.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "draft.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := New(cfg, all).Get(context.Background(), scope, bridge.Empty{})
	if err != nil || got.Draft == nil {
		t.Fatalf("uncommitted folder blocked recovery: %+v, %v", got, err)
	}
}
