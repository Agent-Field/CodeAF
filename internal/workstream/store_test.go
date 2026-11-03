package workstream

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

func testStore(t *testing.T) (*Store, *workspace.Store, *time.Time) {
	t.Helper()
	w, err := workspace.Open(filepath.Join(t.TempDir(), "collections.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	})
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	n := 0
	s := New(w, Options{Now: func() time.Time { return now }, NewID: func() (string, error) { n++; return fmt.Sprintf("id-%d", n), nil }})
	return s, w, &now
}

func createStream(t *testing.T, s *Store, name string, refs ...workspace.Ref) Workstream {
	t.Helper()
	row, _, err := s.Create(context.Background(), Create{Name: name, AgentID: "agent-" + name, Refs: refs})
	if err != nil {
		t.Fatal(err)
	}
	return row
}

// Part A create: storage creates exactly one collection, row and Overview;
// client retries and refused job ownership leave no partial creation.
func TestPartACreateIsAtomicAndIdempotent(t *testing.T) {
	s, w, _ := testStore(t)
	ctx := context.Background()
	args := Create{Name: "Billing", AgentID: "agent", OriginChatID: "origin", ClientID: "retry", Refs: []workspace.Ref{{Kind: workspace.FactoryJobKind, ID: "7", Factory: "f"}}}
	first, c, err := s.Create(ctx, args)
	if err != nil || len(c.Parts) == 0 {
		t.Fatalf("create: %+v %v", c, err)
	}
	indexed, err := s.ByClientID(ctx, "retry")
	if err != nil || indexed.ID != first.ID {
		t.Fatalf("client lookup: %+v %v", indexed, err)
	}
	if _, err := s.ByClientID(ctx, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty client id: %v", err)
	}
	args.Name = "Other"
	args.AgentID = "other"
	again, c, err := s.Create(ctx, args)
	if err != nil || again.ID != first.ID || len(c.Parts) != 0 {
		t.Fatalf("retry: %+v %+v %v", again, c, err)
	}
	all, err := s.List(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("rows: %v %v", all, err)
	}
	collections, err := w.Collections(ctx)
	if err != nil || len(collections) != 1 || !collections[0].Workstream {
		t.Fatalf("collections: %v %v", collections, err)
	}
	pages, err := s.Pages(ctx, first.ID)
	if err != nil || len(pages) != 1 || pages[0].Title != "Overview" || pages[0].Owner != "you" || pages[0].Version != 1 || pages[0].Position != 0 {
		t.Fatalf("pages: %v %v", pages, err)
	}
	p, err := s.Page(ctx, pages[0].ID, true)
	if err != nil || string(p.Doc) != `{"type":"doc","content":[]}` || p.Markdown == nil || *p.Markdown != "" {
		t.Fatalf("overview: %+v %v", p, err)
	}
	members, err := w.Members(ctx, first.ID)
	if err != nil || len(members) != 3 {
		t.Fatalf("members: %v %v", members, err)
	}
	_, _, err = s.Create(ctx, Create{Name: "Conflict", AgentID: "third", Refs: args.Refs})
	var taken *workspace.WorkstreamTaken
	if !errors.As(err, &taken) || taken.WorkstreamID != first.ID || taken.WorkstreamName != "Billing" {
		t.Fatalf("taken: %v", err)
	}
	all, _ = s.List(ctx)
	collections, _ = w.Collections(ctx)
	if len(all) != 1 || len(collections) != 1 {
		t.Fatal("failed create left state")
	}
}

// Part A (b): a batch add rolls back every ref and both workstream timestamps.
func TestPartAJobOwnershipRefusalAndPlainDuplicate(t *testing.T) {
	s, w, now := testStore(t)
	ctx := context.Background()
	job := workspace.Ref{Kind: workspace.FactoryJobKind, ID: "17", Factory: "factory"}
	a := createStream(t, s, "First", job)
	b := createStream(t, s, "Second")
	before, _ := w.Members(ctx, b.ID)
	*now = now.Add(time.Hour)
	_, _, err := w.AddRefs(ctx, b.ID, []workspace.Addition{{Ref: workspace.Ref{Kind: workspace.ConversationKind, ID: "extra"}}, {Ref: job}})
	var taken *workspace.WorkstreamTaken
	if !errors.As(err, &taken) || taken.Factory != "factory" || taken.Job != 17 || taken.WorkstreamID != a.ID || taken.WorkstreamName != "First" {
		t.Fatalf("taken: %+v %v", taken, err)
	}
	after, _ := w.Members(ctx, b.ID)
	a2, _ := s.Get(ctx, a.ID)
	b2, _ := s.Get(ctx, b.ID)
	if !reflect.DeepEqual(before, after) || !a.UpdatedAt.Equal(a2.UpdatedAt) || !b.UpdatedAt.Equal(b2.UpdatedAt) {
		t.Fatal("refused add changed state")
	}
	plain, err := w.Create(ctx, "Plain")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Add(ctx, plain.ID, job); err != nil {
		t.Fatal(err)
	}
	duplicate, err := w.Duplicate(ctx, a.ID, "Copy")
	if err != nil || duplicate.Workstream {
		t.Fatalf("duplicate: %+v %v", duplicate, err)
	}
	if _, err := s.Get(ctx, duplicate.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("duplicate became workstream: %v", err)
	}
	if _, err := w.Delete(ctx, a.ID); !errors.Is(err, workspace.ErrWorkstreamDelete) {
		t.Fatalf("delete: %v", err)
	}
	if err := w.Remove(ctx, a.ID, job); err != nil {
		t.Fatal(err)
	}
	if err := w.Add(ctx, b.ID, job); err != nil {
		t.Fatal(err)
	}
	b2, _ = s.Get(ctx, b.ID)
	if !b2.UpdatedAt.After(b.UpdatedAt) {
		t.Fatal("membership did not update timestamp")
	}
}

// Part A (d): stage persists the first departure, all merged is not Done,
// and reopening an unstarted workstream gives Building even with Backlog.
func TestPartAStageAndObservation(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	job := workspace.Ref{Kind: workspace.FactoryJobKind, ID: "1", Factory: "f"}
	row := createStream(t, s, "Stage", job)
	for _, state := range []string{"", "backlog", "dropped", "unknown"} {
		if got := Stage(time.Time{}, time.Time{}, []string{state}); got != "shaping" {
			t.Fatalf("%s: %s", state, got)
		}
	}
	for _, state := range []string{"queued", "running", "needs_you", "done"} {
		if got := Stage(time.Time{}, time.Time{}, []string{state}); got != "building" {
			t.Fatalf("%s: %s", state, got)
		}
	}
	c, err := s.ObserveJobStates(ctx, row.ID, map[Job]string{{Factory: "f", Number: 1}: "queued"})
	if err != nil || len(c.Parts) == 0 {
		t.Fatalf("observe: %+v %v", c, err)
	}
	got, _ := s.Get(ctx, row.ID)
	if got.StartedAt.IsZero() || got.Stage != "building" {
		t.Fatalf("started: %+v", got)
	}
	c, err = s.ObserveJobStates(ctx, row.ID, map[Job]string{{Factory: "f", Number: 1}: "backlog"})
	if err != nil || len(c.Parts) != 0 {
		t.Fatal("observation reset start")
	}
	done, _, err := s.MarkDone(ctx, row.ID, "")
	if err != nil || done.Stage != "done" {
		t.Fatalf("done: %+v %v", done, err)
	}
	open, _, err := s.Reopen(ctx, row.ID)
	if err != nil || open.Stage != "building" {
		t.Fatalf("reopen: %+v %v", open, err)
	}
	fresh := createStream(t, s, "Unstarted")
	_, _, err = s.MarkDone(ctx, fresh.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	open, _, err = s.Reopen(ctx, fresh.ID)
	if err != nil || open.StartedAt.IsZero() || open.Stage != "building" {
		t.Fatalf("unstarted reopen: %+v %v", open, err)
	}
}

func TestPartAUpdateClearsSettingsAndKeepsNameRules(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	row := createStream(t, s, "Settings")
	why := "Because"
	budget := 20.0
	small := "small"
	row, _, err := s.Update(ctx, row.ID, Patch{Why: &why, BudgetUSD: &budget, Autonomy: &small, Wake: &Wake{Morning: true}})
	if err != nil || row.BudgetUSD == nil || *row.BudgetUSD != 20 || !row.Wake.Morning {
		t.Fatalf("update: %+v %v", row, err)
	}
	why = ""
	budget = 0
	row, _, err = s.Update(ctx, row.ID, Patch{Why: &why, BudgetUSD: &budget})
	if err != nil || row.Why != "" || row.BudgetUSD != nil {
		t.Fatalf("clear: %+v %v", row, err)
	}
	createStream(t, s, "Taken")
	name := "taken"
	if _, _, err := s.Update(ctx, row.ID, Patch{Name: &name}); !errors.Is(err, workspace.ErrNameTaken) {
		t.Fatalf("name: %v", err)
	}
}
