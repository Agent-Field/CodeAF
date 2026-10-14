package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type testRoles struct{ svc.AppRoles }

func (testRoles) Role(_ context.Context, app, _ string) (svc.Role, error) {
	if app == "hidden" {
		return "", nil
	}
	return svc.RoleViewer, nil
}

func TestAuditStore(t *testing.T) {
	ctx := context.Background()
	cfg := svc.Config{Root: t.TempDir()}
	all := &svc.Services{SpacesServices: svc.SpacesServices{AppRoles: testRoles{}}}
	s := New(cfg, all)
	t.Cleanup(func() { _ = s.Close() })
	at := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return at }
	input := []bridge.AuditEntry{
		{App: "crm", Member: "me", Action: "records.update", Detail: json.RawMessage(`{"field":"name"}`), Activity: &bridge.ActivityLine{Text: "Updated CRM", Undo: &bridge.UndoAction{Method: "records.update", App: "crm", Params: json.RawMessage(`{"id":"d1"}`)}, View: "records/d1"}},
		{App: "crm", Member: "raj", Action: "records.read"},
		{App: "other", Member: "me", Action: "apps.keep", Activity: &bridge.ActivityLine{Text: "Kept other"}},
		{Space: "acme", App: "crm", Member: "me", Action: "records.update"},
		{App: "hidden", Member: "me", Action: "apps.keep", Activity: &bridge.ActivityLine{Text: "Hidden activity"}},
	}
	for i, e := range input {
		e.Seq, e.At = 999, at.Add(-time.Hour)
		e.Decision = "allow"
		got, err := s.Append(ctx, e)
		if err != nil {
			t.Fatal(err)
		}
		if got.Seq != int64(i+1) || !got.At.Equal(at) {
			t.Fatalf("Append = %+v", got)
		}
	}
	scope := svc.Scope{Caller: svc.PersonalCaller()}
	for _, tt := range []struct {
		name string
		p    bridge.AuditListParams
		seqs []int64
		next int64
	}{
		{"newest first", bridge.AuditListParams{}, []int64{5, 3, 2, 1}, 0},
		{"first page", bridge.AuditListParams{Limit: 2}, []int64{5, 3}, 3},
		{"second page", bridge.AuditListParams{Before: 3, Limit: 2}, []int64{2, 1}, 0},
		{"filters", bridge.AuditListParams{App: "crm", Member: "me", Action: "records.update"}, []int64{1}, 0},
		{"no match", bridge.AuditListParams{Member: "absent"}, []int64{}, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.List(ctx, scope, tt.p)
			if err != nil {
				t.Fatal(err)
			}
			seqs := make([]int64, 0, len(got.Entries))
			for _, e := range got.Entries {
				seqs = append(seqs, e.Seq)
				if !e.At.Equal(at) || e.Space != svc.PersonalSpace {
					t.Fatalf("entry = %+v", e)
				}
			}
			if !reflect.DeepEqual(seqs, tt.seqs) || got.Next != tt.next {
				t.Fatalf("seqs=%v next=%d, want %v %d", seqs, got.Next, tt.seqs, tt.next)
			}
		})
	}
	for _, tt := range []struct {
		name string
		p    bridge.ActivityListParams
		seqs []int64
		next int64
	}{
		{"projection", bridge.ActivityListParams{}, []int64{3, 1}, 0},
		{"filtered first page", bridge.ActivityListParams{Limit: 1}, []int64{3}, 3},
		{"filtered next page", bridge.ActivityListParams{Before: 3, Limit: 1}, []int64{1}, 0},
		{"app", bridge.ActivityListParams{App: "crm"}, []int64{1}, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.Activity(ctx, scope, tt.p)
			if err != nil {
				t.Fatal(err)
			}
			seqs := make([]int64, 0, len(got.Lines))
			for _, line := range got.Lines {
				seqs = append(seqs, line.Seq)
				if line.Seq == 1 && (line.Text != "Updated CRM" || line.View != "records/d1" || !reflect.DeepEqual(line.Undo, input[0].Activity.Undo)) {
					t.Fatalf("projection = %+v", line)
				}
			}
			if !reflect.DeepEqual(seqs, tt.seqs) || got.Next != tt.next {
				t.Fatalf("seqs=%v next=%d, want %v %d", seqs, got.Next, tt.seqs, tt.next)
			}
		})
	}

	db, err := sql.Open("sqlite", filepath.Join(cfg.Root, "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, query := range []string{`UPDATE audit SET member = 'changed' WHERE seq = 1`, `DELETE FROM audit WHERE seq = 1`} {
		t.Run(query, func(t *testing.T) {
			if _, err := db.Exec(query); err == nil {
				t.Fatal("append-only trigger allowed mutation")
			}
		})
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := New(cfg, all)
	defer reopened.Close()
	page, err := reopened.List(ctx, scope, bridge.AuditListParams{App: "crm", Member: "me"})
	if err != nil || len(page.Entries) != 1 || string(page.Entries[0].Detail) != string(input[0].Detail) {
		t.Fatalf("reopened = %+v, %v", page, err)
	}
	e, err := reopened.Append(ctx, bridge.AuditEntry{Member: "me", Action: "test"})
	if err != nil || e.Seq != 6 {
		t.Fatalf("reopened Append = %+v, %v", e, err)
	}
}
