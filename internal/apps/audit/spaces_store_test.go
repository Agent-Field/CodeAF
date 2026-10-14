package audit

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/realtime"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func testService(t *testing.T, all *svc.Services) *Service {
	t.Helper()
	s := New(svc.Config{Root: t.TempDir()}, all)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestConcurrentAppendsAndPublication(t *testing.T) {
	ctx := context.Background()
	hub := realtime.New(svc.Config{}, nil)
	s := testService(t, &svc.Services{Hub: hub})
	events, cancel := hub.Subscribe(bridge.TopicSpaceActivity)
	defer cancel()
	appEvents, cancelApp := hub.Subscribe("app/crm/activity")
	defer cancelApp()
	const count = 50
	var wg sync.WaitGroup
	seqs := make(chan int64, count)
	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e, err := s.Append(ctx, bridge.AuditEntry{Seq: 900, App: "crm", Member: "mona",
				Action: "app.apply", Decision: "allow", Activity: &bridge.ActivityLine{Text: "Mona shaped CRM · v12",
					View: "versions/v12", Undo: &bridge.UndoAction{Method: "app.restore", App: "crm", Params: json.RawMessage(`{"version":"v11"}`)}}})
			if err != nil {
				t.Error(err)
				return
			}
			seqs <- e.Seq
		}()
	}
	wg.Wait()
	close(seqs)
	got := make([]int64, 0, count)
	for seq := range seqs {
		got = append(got, seq)
	}
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	if len(got) != count {
		t.Fatalf("appended %d of %d", len(got), count)
	}
	for i, seq := range got {
		if seq != int64(i+1) {
			t.Fatalf("seqs = %v", got)
		}
		var spaceLine bridge.ActivityEntry
		for _, ch := range []<-chan svc.Event{events, appEvents} {
			select {
			case event := <-ch:
				line, ok := event.Data.(bridge.ActivityEntry)
				if !ok || line.Seq != seq || line.At.IsZero() || line.Text != "Mona shaped CRM · v12" || line.View != "versions/v12" || line.Undo == nil {
					t.Fatalf("event = %+v", event)
				}
				if spaceLine.Seq == 0 {
					spaceLine = line
				} else if !reflect.DeepEqual(spaceLine, line) {
					t.Fatalf("different topic payloads: %+v, %+v", spaceLine, line)
				}
			default:
				t.Fatal("missing activity event")
			}
		}
	}
	for _, e := range []bridge.AuditEntry{{Action: "records.update"}, {Action: "member.invite", Activity: &bridge.ActivityLine{Text: "@mona invited @raj to acme"}}} {
		if _, err := s.Append(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case event := <-events:
		if event.Data.(bridge.ActivityEntry).Seq != count+2 {
			t.Fatalf("space event = %+v", event)
		}
	default:
		t.Fatal("missing space-only activity")
	}
	for _, ch := range []<-chan svc.Event{events, appEvents} {
		select {
		case event := <-ch:
			t.Fatalf("unexpected publication = %+v", event)
		default:
		}
	}
}

func TestSQLiteGuardsAndWAL(t *testing.T) {
	s := testService(t, nil)
	if _, err := s.Append(context.Background(), bridge.AuditEntry{Action: "test"}); err != nil {
		t.Fatal(err)
	}
	var journal string
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil || journal != "wal" {
		t.Fatalf("journal=%s err=%v", journal, err)
	}
	for _, query := range []string{
		`UPDATE audit SET entry = '{}' WHERE seq = 1`,
		`DELETE FROM audit WHERE seq = 1`,
		`INSERT OR REPLACE INTO audit SELECT * FROM audit WHERE seq = 1`,
	} {
		t.Run(query, func(t *testing.T) {
			if _, err := s.db.Exec(query); err == nil {
				t.Fatal("SQL mutation bypassed append-only guard")
			}
		})
	}
	var tables int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`).Scan(&tables); err != nil || tables != 1 {
		t.Fatalf("user tables=%d err=%v", tables, err)
	}
}

type roleLookup struct {
	svc.AppRoles
	roles map[string]svc.Role
	calls int
	err   error
}

func (r *roleLookup) Role(_ context.Context, _, member string) (svc.Role, error) {
	r.calls++
	return r.roles[member], r.err
}

func TestActivityRoles(t *testing.T) {
	for _, tt := range []struct {
		name   string
		caller svc.Caller
		role   svc.Role
		want   []int64
	}{
		{"viewer", svc.Caller{Member: "jm", WS: svc.WorkspaceMember}, svc.RoleViewer, []int64{3, 1}},
		{"user", svc.Caller{Member: "raj", WS: svc.WorkspaceMember}, svc.RoleUser, []int64{3, 1}},
		{"editor", svc.Caller{Member: "mona", WS: svc.WorkspaceMember}, svc.RoleEditor, []int64{3, 1}},
		{"no app role", svc.Caller{Member: "raj", WS: svc.WorkspaceMember}, "", []int64{3}},
		{"admin has no app role", svc.Caller{Member: "admin", WS: svc.WorkspaceAdmin}, "", []int64{3}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			roles := &roleLookup{roles: map[string]svc.Role{tt.caller.Member: tt.role}}
			s := testService(t, &svc.Services{SpacesServices: svc.SpacesServices{AppRoles: roles}})
			for _, entry := range []bridge.AuditEntry{
				{App: "crm", Activity: &bridge.ActivityLine{Text: "app action"}},
				{App: "crm"}, {Activity: &bridge.ActivityLine{Text: "space action"}},
				{Space: "other", Activity: &bridge.ActivityLine{Text: "other space"}},
			} {
				if _, err := s.Append(context.Background(), entry); err != nil {
					t.Fatal(err)
				}
			}
			page, err := s.Activity(context.Background(), svc.Scope{Caller: tt.caller}, bridge.ActivityListParams{Limit: 1})
			if err != nil {
				t.Fatal(err)
			}
			seqs := []int64{page.Lines[0].Seq}
			if page.Next != 0 {
				next, err := s.Activity(context.Background(), svc.Scope{Caller: tt.caller}, bridge.ActivityListParams{Limit: 1, Before: page.Next})
				if err != nil || len(next.Lines) != 1 || next.Next != 0 {
					t.Fatalf("next=%+v err=%v", next, err)
				}
				seqs = append(seqs, next.Lines[0].Seq)
			}
			if !reflect.DeepEqual(seqs, tt.want) {
				t.Fatalf("seqs=%v want=%v", seqs, tt.want)
			}
		})
	}
}

func TestListAuthorizationAndLimits(t *testing.T) {
	ctx := context.Background()
	s := testService(t, nil)
	for range 502 {
		if _, err := s.Append(ctx, bridge.AuditEntry{Action: "test", Activity: &bridge.ActivityLine{Text: "space action"}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tt := range []struct {
		name         string
		role         svc.WorkspaceRole
		limit, count int
		code         bridge.ErrorCode
	}{
		{"owner default", svc.WorkspaceOwner, 0, 100, ""},
		{"admin", svc.WorkspaceAdmin, 1, 1, ""},
		{"maximum", svc.WorkspaceOwner, 501, 500, ""},
		{"member denied", svc.WorkspaceMember, 1, 0, bridge.CodeDenied},
		{"missing role denied", "", 1, 0, bridge.CodeDenied},
		{"invalid limit", svc.WorkspaceOwner, -1, 0, bridge.CodeInvalid},
	} {
		t.Run(tt.name, func(t *testing.T) {
			page, err := s.List(ctx, svc.Scope{Caller: svc.Caller{Member: "me", WS: tt.role}}, bridge.AuditListParams{Limit: tt.limit})
			if tt.code != "" {
				var typed *bridge.Error
				if !errors.As(err, &typed) || typed.Code != tt.code {
					t.Fatalf("error=%v want=%s", err, tt.code)
				}
				return
			}
			if err != nil || len(page.Entries) != tt.count || page.Next != int64(503-tt.count) {
				t.Fatalf("page=%+v err=%v", page, err)
			}
		})
	}
	for _, limit := range []int{0, 501} {
		page, err := s.Activity(ctx, svc.Scope{Caller: svc.PersonalCaller()}, bridge.ActivityListParams{Limit: limit})
		if err != nil || len(page.Lines) != pageLimit(limit) {
			t.Fatalf("activity lines=%d err=%v", len(page.Lines), err)
		}
	}
}

func TestActivityRoleFailureAndHiddenBatches(t *testing.T) {
	ctx := context.Background()
	roles := &roleLookup{roles: map[string]svc.Role{}}
	s := testService(t, &svc.Services{SpacesServices: svc.SpacesServices{AppRoles: roles}})
	if _, err := s.Append(ctx, bridge.AuditEntry{Activity: &bridge.ActivityLine{Text: "space action"}}); err != nil {
		t.Fatal(err)
	}
	for range 10 {
		if _, err := s.Append(ctx, bridge.AuditEntry{App: "crm", Activity: &bridge.ActivityLine{Text: "hidden"}}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.Activity(ctx, svc.Scope{Caller: svc.PersonalCaller()}, bridge.ActivityListParams{Limit: 1})
	if err != nil || len(page.Lines) != 1 || page.Lines[0].Seq != 1 || page.Next != 0 || roles.calls != 1 {
		t.Fatalf("page=%+v calls=%d err=%v", page, roles.calls, err)
	}
	roles.err = errors.New("roles unavailable")
	if _, err := s.Activity(ctx, svc.Scope{Caller: svc.PersonalCaller()}, bridge.ActivityListParams{}); !errors.Is(err, roles.err) {
		t.Fatalf("role error=%v", err)
	}
	if _, err := s.Activity(ctx, svc.Scope{}, bridge.ActivityListParams{}); err == nil {
		t.Fatal("nonmember read Activity")
	}
}
