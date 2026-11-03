package workstream

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

// Part A (f): optimistic saves refuse stale versions without changing history;
// restoration copies an old title and document into a new immutable version.
func TestPartAPageSaveHistoryAndRestore(t *testing.T) {
	s, _, now := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Pages")
	nodes, err := s.Pages(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Page(ctx, nodes[0].ID, false)
	if err != nil {
		t.Fatal(err)
	}
	doc := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Saved"}]}]}`)
	*now = now.Add(time.Hour)
	saved, _, err := s.SavePage(ctx, first.ID, first.Version, doc, Attribution{By: "agent", From: &Source{Kind: "chat", ID: "source"}})
	if err != nil || saved.Version != 2 {
		t.Fatalf("save: %+v %v", saved, err)
	}
	_, c, err := s.SavePage(ctx, first.ID, 1, json.RawMessage(emptyDoc), Attribution{})
	var stale *VersionConflict
	if !errors.As(err, &stale) || stale.ID != first.ID || stale.Version != 2 || stale.UpdatedBy != "agent" || !stale.UpdatedAt.Equal(*now) || len(c.Parts) != 0 {
		t.Fatalf("stale: %+v %v", stale, err)
	}
	current, _ := s.Page(ctx, first.ID, false)
	history, _ := s.PageHistory(ctx, first.ID, 50)
	if !reflect.DeepEqual(current, saved) || len(history) != 2 || history[0].Version != 2 || history[1].Version != 1 {
		t.Fatalf("stale changed page or history: %+v %+v", current, history)
	}
	title := "Renamed"
	_, _, err = s.UpdatePage(ctx, first.ID, PagePatch{Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	restored, _, err := s.RestorePage(ctx, first.ID, 1, 2, Attribution{})
	if err != nil || restored.Version != 3 || restored.Title != "Overview" || string(restored.Doc) != emptyDoc {
		t.Fatalf("restore: %+v %v", restored, err)
	}
	history, _ = s.PageHistory(ctx, first.ID, 50)
	if len(history) != 3 || history[0].Version != 3 || history[1].SavedBy != "agent" {
		t.Fatalf("history: %+v", history)
	}
	_, _, err = s.SavePage(ctx, first.ID, 3, json.RawMessage(`{"x":"`+strings.Repeat("x", DocMax)+`"}`), Attribution{})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("size: %v", err)
	}
}

// Part A (f): placement defaults differ for create and move; Overview is fixed,
// parent cycles and cross-workstream moves refuse, and delete removes descendants.
func TestPartAPageTreePlacementCyclesAndDelete(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Tree")
	nodes, _ := s.Pages(ctx, w.ID)
	overview := nodes[0]
	a, _, err := s.CreatePage(ctx, PageCreate{WorkstreamID: w.ID, Title: "A"})
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := s.CreatePage(ctx, PageCreate{WorkstreamID: w.ID, Title: "B"})
	if err != nil {
		t.Fatal(err)
	}
	child, _, err := s.CreatePage(ctx, PageCreate{WorkstreamID: w.ID, ParentID: a.ID, Title: "Child"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Position != 1 || b.Position != 2 || child.Position != 0 {
		t.Fatalf("positions: %+v %+v %+v", a, b, child)
	}
	if _, _, err := s.MovePage(ctx, a.ID, child.ID, ""); !errors.Is(err, ErrCycle) {
		t.Fatalf("descendant: %v", err)
	}
	if _, _, err := s.MovePage(ctx, a.ID, a.ID, ""); !errors.Is(err, ErrCycle) {
		t.Fatalf("self: %v", err)
	}
	if _, _, err := s.MovePage(ctx, overview.ID, a.ID, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Overview move: %v", err)
	}
	moved, _, err := s.MovePage(ctx, b.ID, "", "")
	if err != nil || moved.Position != 1 {
		t.Fatalf("root move: %+v %v", moved, err)
	}
	nodes, _ = s.Pages(ctx, w.ID)
	ids := []string{}
	for _, n := range nodes {
		ids = append(ids, n.ID)
	}
	if !reflect.DeepEqual(ids, []string{overview.ID, b.ID, a.ID, child.ID}) {
		t.Fatalf("depth-first: %v", ids)
	}
	other := createStream(t, s, "OtherTree")
	otherPages, _ := s.Pages(ctx, other.ID)
	if _, _, err := s.MovePage(ctx, a.ID, otherPages[0].ID, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("cross-workstream: %v", err)
	}
	if _, _, err := s.DeletePage(ctx, overview.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Overview delete: %v", err)
	}
	deleted, _, err := s.DeletePage(ctx, a.ID)
	if err != nil || !reflect.DeepEqual(deleted, []string{a.ID, child.ID}) {
		t.Fatalf("delete: %v %v", deleted, err)
	}
	if _, err := s.Page(ctx, child.ID, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("child remains: %v", err)
	}
}

func TestPartAPlanMembershipVersionsAndDone(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Plan", workspace.Ref{Kind: workspace.FactoryJobKind, ID: "8", Factory: "f"})
	p, err := s.Plan(ctx, w.ID)
	if err != nil || p.Version != 0 || p.Milestones == nil || p.Waves == nil {
		t.Fatalf("initial: %+v %v", p, err)
	}
	waves := []Wave{{N: 1, Items: []PlanItem{{Kind: "job", Factory: "f", Job: 8}, {Kind: "draft", DraftID: "draft", Title: "Next"}}}}
	p, _, err = s.SetPlan(ctx, w.ID, 0, []Milestone{{Title: "Ship"}}, waves, Attribution{By: "agent"})
	if err != nil || p.Version != 1 || p.Milestones[0].ID == "" {
		t.Fatalf("set: %+v %v", p, err)
	}
	_, _, err = s.SetPlan(ctx, w.ID, 0, nil, nil, Attribution{})
	var stale *VersionConflict
	if !errors.As(err, &stale) || stale.Version != 1 || stale.UpdatedBy != "agent" {
		t.Fatalf("stale: %v", err)
	}
	for _, bad := range [][]Wave{
		{{N: 0}}, {{N: 2}, {N: 1}}, {{N: 1}, {N: 1}}, {{N: 1, Items: []PlanItem{{Kind: "job", Factory: "f", Job: 99}}}},
	} {
		if _, _, err := s.SetPlan(ctx, w.ID, 1, nil, bad, Attribution{}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid waves %+v: %v", bad, err)
		}
	}
	_, _, err = s.MarkDone(ctx, w.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SetPlan(ctx, w.ID, 1, nil, nil, Attribution{}); !errors.Is(err, ErrConflict) {
		t.Fatalf("done: %v", err)
	}
}

func TestPartAMarkdownExportSupportsEditorNodes(t *testing.T) {
	doc := json.RawMessage(`{"type":"doc","content":[{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Title","marks":[{"type":"bold"}]}]},{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Item"}]}]}]},{"type":"codeBlock","attrs":{"language":"go"},"content":[{"type":"text","text":"code"}]},{"type":"blockquote","content":[{"type":"paragraph","content":[{"type":"text","text":"Quote"}]}]},{"type":"unknown","content":[{"type":"text","text":"Retained"}]}]}`)
	got, err := Markdown(doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## Title", "- Item", "```go\ncode\n```", "> Quote", "Retained"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestPartAOverviewIdentitySurvivesRenaming(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Identity")
	pages, err := s.Pages(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	title := "Introduction"
	overview, _, err := s.UpdatePage(ctx, pages[0].ID, PagePatch{Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := s.CreatePage(ctx, PageCreate{WorkstreamID: w.ID, Title: "Overview"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.MovePage(ctx, overview.ID, other.ID, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("renamed Overview move: %v", err)
	}
	if _, _, err := s.DeletePage(ctx, overview.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("renamed Overview delete: %v", err)
	}
	if _, _, err := s.MovePage(ctx, other.ID, "", ""); err != nil {
		t.Fatalf("ordinary page named Overview: %v", err)
	}
	pages, err = s.Pages(ctx, w.ID)
	if err != nil || pages[0].ID != overview.ID || pages[0].Position != 0 {
		t.Fatalf("Overview order: %+v %v", pages, err)
	}
}
