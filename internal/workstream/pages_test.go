package workstream

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/workspace"
	"github.com/Agent-Field/codeaf/internal/workstream/pagedoc"
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
	owner := "agent"
	if _, _, err := s.UpdatePage(ctx, first.ID, PagePatch{Owner: &owner}); err != nil {
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
	if page, err := s.Page(ctx, child.ID, false); err != nil || page.DeletedAt.IsZero() {
		t.Fatalf("deleted child must remain readable: %+v %v", page, err)
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

// page.get's markdown is the shared page renderer's output (pagedoc), so the
// engine has one renderer and it matches the desktop app's.
func TestPartAMarkdownExportIsThePageRenderer(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Markdown")
	pages, err := s.Pages(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	doc := json.RawMessage(`{"type":"doc","content":[{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Title"}]},{"type":"paragraph","content":[{"type":"text","text":"Body"}]}]}`)
	if _, _, err := s.SavePage(ctx, pages[0].ID, 1, doc, Attribution{}); err != nil {
		t.Fatal(err)
	}
	p, err := s.Page(ctx, pages[0].ID, true)
	if err != nil {
		t.Fatal(err)
	}
	want, err := pagedoc.Markdown(doc)
	if err != nil {
		t.Fatal(err)
	}
	if p.Markdown == nil || *p.Markdown != want || !strings.Contains(want, "Title") {
		t.Fatalf("markdown %v, want %q", p.Markdown, want)
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

// WP-F1 (a): saved content and attribution combine with current page metadata;
// selecting the current version is identical to reading without a version.
func TestWorkstreamPageGetSavedVersion(t *testing.T) {
	s, _, now := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Version reads")
	firstDoc := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"First"}]}]}`)
	first, _, err := s.CreatePage(ctx, PageCreate{WorkstreamID: w.ID, Title: "First title", Owner: "agent", Doc: firstDoc,
		Attribution: Attribution{By: "agent", From: &Source{Kind: "chat", ID: "first-source"}}})
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Hour)
	if _, _, err := s.SavePage(ctx, first.ID, 1, json.RawMessage(emptyDoc), Attribution{}); err != nil {
		t.Fatal(err)
	}
	parent, _, err := s.CreatePage(ctx, PageCreate{WorkstreamID: w.ID, Title: "Parent"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.MovePage(ctx, first.ID, parent.ID, ""); err != nil {
		t.Fatal(err)
	}
	title, owner, read := "Current title", "agent", true
	if _, _, err := s.UpdatePage(ctx, first.ID, PagePatch{Title: &title, Owner: &owner, ReadByJobs: &read}); err != nil {
		t.Fatal(err)
	}
	current, err := s.Page(ctx, first.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := s.PageAtVersion(ctx, first.ID, current.Version, false)
	if err != nil || !reflect.DeepEqual(selected, current) {
		t.Fatalf("current version: %+v, want %+v: %v", selected, current, err)
	}
	check := func(deleted bool) {
		t.Helper()
		old, err := s.PageAtVersion(ctx, first.ID, 1, true)
		if err != nil {
			t.Fatal(err)
		}
		want := current
		want.Title, want.Doc, want.Version = first.Title, first.Doc, first.Version
		want.UpdatedAt, want.UpdatedBy, want.UpdatedFrom = first.UpdatedAt, first.UpdatedBy, first.UpdatedFrom
		md, err := pagedoc.Markdown(firstDoc)
		if err != nil {
			t.Fatal(err)
		}
		want.Markdown = &md
		if deleted {
			want.DeletedAt = *now
		}
		if !reflect.DeepEqual(old, want) || old.Markdown == nil || !strings.Contains(*old.Markdown, "First") {
			t.Fatalf("saved version: %+v, want %+v", old, want)
		}
	}
	check(false)
	if _, err := s.PageAtVersion(ctx, first.ID, 99, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown version: %v", err)
	}
	for _, version := range []int{0, -1} {
		if _, err := s.PageAtVersion(ctx, first.ID, version, false); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid version %d: %v", version, err)
		}
	}
	if _, _, err := s.DeletePage(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	check(true)
}

func createWorkstreamPage(t *testing.T, s *Store, workstreamID, parent, title string) Page {
	t.Helper()
	p, _, err := s.CreatePage(context.Background(), PageCreate{WorkstreamID: workstreamID, ParentID: parent, Title: title})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// WP-F1 (b): delete groups preserve separate deletions, history, nesting and order;
// undelete appends under a live parent and changes attribution without a save.
func TestWorkstreamDeletedPageTreeRestore(t *testing.T) {
	s, _, now := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Restorable tree")
	deleted, err := s.DeletedPages(ctx, w.ID)
	if err != nil || deleted == nil || len(deleted) != 0 {
		t.Fatalf("initial deleted list: %+v %v", deleted, err)
	}
	parent := createWorkstreamPage(t, s, w.ID, "", "Parent")
	root := createWorkstreamPage(t, s, w.ID, parent.ID, "Root")
	earlier := createWorkstreamPage(t, s, w.ID, root.ID, "Earlier deletion")
	first := createWorkstreamPage(t, s, w.ID, root.ID, "First")
	grandchild := createWorkstreamPage(t, s, w.ID, first.ID, "Grandchild")
	second := createWorkstreamPage(t, s, w.ID, root.ID, "Second")
	sibling := createWorkstreamPage(t, s, w.ID, parent.ID, "Sibling")
	doc := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Keep me"}]}]}`)
	// The agent writes only its own pages, so the page is the agent's for this save.
	agentOwner := "agent"
	if _, _, err := s.UpdatePage(ctx, root.ID, PagePatch{Owner: &agentOwner}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SavePage(ctx, root.ID, 1, doc, Attribution{By: "agent", From: &Source{Kind: "chat", ID: "source"}}); err != nil {
		t.Fatal(err)
	}
	lastTitle := "Last title"
	if _, _, err := s.UpdatePage(ctx, root.ID, PagePatch{Title: &lastTitle, Attribution: Attribution{By: "agent", From: &Source{Kind: "chat", ID: "source"}}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.DeletePage(ctx, earlier.ID); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Hour)
	ids, change, err := s.DeletePage(ctx, root.ID)
	if err != nil || !reflect.DeepEqual(ids, []string{root.ID, first.ID, grandchild.ID, second.ID}) || !reflect.DeepEqual(change.Parts, []ChangedPart{PartWorkstream, PartPages}) {
		t.Fatalf("delete receipt: %v %+v %v", ids, change, err)
	}
	deleted, err = s.DeletedPages(ctx, w.ID)
	if err != nil || len(deleted) != 2 || deleted[0].ID != root.ID || deleted[1].ID != earlier.ID || !deleted[0].DeletedAt.Equal(*now) || !deleted[0].DeletedAt.After(deleted[1].DeletedAt) {
		t.Fatalf("delete roots: %+v %v", deleted, err)
	}
	nodes, err := s.Pages(ctx, w.ID)
	if err != nil || len(nodes) != 3 || nodes[1].ID != parent.ID || nodes[2].ID != sibling.ID || nodes[2].Position != 0 {
		t.Fatalf("live tree: %+v %v", nodes, err)
	}
	for _, id := range ids {
		p, err := s.Page(ctx, id, true)
		if err != nil || !p.DeletedAt.Equal(*now) {
			t.Fatalf("deleted page %s: %+v %v", id, p, err)
		}
	}
	for name, write := range map[string]func() error{
		"save": func() error {
			_, _, err := s.SavePage(ctx, root.ID, 2, json.RawMessage(emptyDoc), Attribution{})
			return err
		},
		"update":  func() error { _, _, err := s.UpdatePage(ctx, root.ID, PagePatch{}); return err },
		"move":    func() error { _, _, err := s.MovePage(ctx, root.ID, "", ""); return err },
		"delete":  func() error { _, _, err := s.DeletePage(ctx, root.ID); return err },
		"history": func() error { _, err := s.PageHistory(ctx, root.ID, 50); return err },
		"create parent": func() error {
			_, _, err := s.CreatePage(ctx, PageCreate{WorkstreamID: w.ID, ParentID: root.ID, Title: "No"})
			return err
		},
		"create after": func() error {
			_, _, err := s.CreatePage(ctx, PageCreate{WorkstreamID: w.ID, AfterID: root.ID, Title: "No"})
			return err
		},
		"move parent": func() error { _, _, err := s.MovePage(ctx, sibling.ID, root.ID, ""); return err },
		"move after":  func() error { _, _, err := s.MovePage(ctx, sibling.ID, "", root.ID); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if err := write(); !errors.Is(err, ErrNotFound) {
				t.Fatalf("deleted target: %v", err)
			}
		})
	}
	*now = now.Add(time.Hour)
	restored, change, err := s.UndeletePage(ctx, root.ID)
	if err != nil || restored.ParentID != parent.ID || restored.Position != 1 || restored.Title != lastTitle || restored.Version != 2 || string(restored.Doc) != string(doc) || !restored.DeletedAt.IsZero() || !restored.UpdatedAt.Equal(*now) || restored.UpdatedBy != "you" || restored.UpdatedFrom != nil || change.ID != w.ID || !reflect.DeepEqual(change.Parts, []ChangedPart{PartWorkstream, PartPages}) {
		t.Fatalf("restore: %+v %+v %v", restored, change, err)
	}
	nodes, err = s.Pages(ctx, w.ID)
	var liveIDs []string
	for _, p := range nodes {
		liveIDs = append(liveIDs, p.ID)
	}
	if err != nil || len(liveIDs) != 7 || !reflect.DeepEqual(liveIDs[1:], []string{parent.ID, sibling.ID, root.ID, first.ID, grandchild.ID, second.ID}) {
		t.Fatalf("restored order: %+v %v", nodes, err)
	}
	for _, want := range []Page{first, grandchild, second} {
		p, err := s.Page(ctx, want.ID, false)
		if err != nil || p.ParentID != want.ParentID || p.Version != want.Version || !p.DeletedAt.IsZero() {
			t.Fatalf("restored nesting: %+v %v", p, err)
		}
	}
	deleted, err = s.DeletedPages(ctx, w.ID)
	if err != nil || len(deleted) != 1 || deleted[0].ID != earlier.ID {
		t.Fatalf("earlier deletion revived: %+v %v", deleted, err)
	}
	history, err := s.PageHistory(ctx, root.ID, 50)
	if err != nil || len(history) != 2 || history[0].Title != root.Title || history[0].SavedBy != "agent" || history[0].From == nil || history[0].From.ID != "source" {
		t.Fatalf("restore changed saved history: %+v %v", history, err)
	}
}

// WP-F1 (b): restoring a descendant of a deleted parent restores only its own
// group subtree, appends at the root, and remains permitted on a done workstream.
func TestWorkstreamDeletedDescendantRestoresAtRoot(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Detached restore")
	root := createWorkstreamPage(t, s, w.ID, "", "Root")
	child := createWorkstreamPage(t, s, w.ID, root.ID, "Child")
	grandchild := createWorkstreamPage(t, s, w.ID, child.ID, "Grandchild")
	other := createWorkstreamPage(t, s, w.ID, root.ID, "Other")
	last := createWorkstreamPage(t, s, w.ID, "", "Last")
	if _, _, err := s.MarkDone(ctx, w.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.DeletePage(ctx, root.ID); err != nil {
		t.Fatal(err)
	}
	restored, _, err := s.UndeletePage(ctx, child.ID)
	if err != nil || restored.ParentID != "" || restored.Position != 2 || restored.Version != 1 {
		t.Fatalf("descendant restore: %+v %v", restored, err)
	}
	nodes, err := s.Pages(ctx, w.ID)
	if err != nil || len(nodes) != 4 || nodes[1].ID != last.ID || nodes[2].ID != child.ID || nodes[3].ID != grandchild.ID {
		t.Fatalf("detached tree: %+v %v", nodes, err)
	}
	for _, id := range []string{root.ID, other.ID} {
		p, err := s.Page(ctx, id, false)
		if err != nil || p.DeletedAt.IsZero() {
			t.Fatalf("unrelated deleted page: %+v %v", p, err)
		}
	}
	rootRestored, _, err := s.UndeletePage(ctx, root.ID)
	if err != nil || rootRestored.ParentID != "" || rootRestored.Position != 3 {
		t.Fatalf("root append: %+v %v", rootRestored, err)
	}
	p, err := s.Page(ctx, child.ID, false)
	if err != nil || p.ParentID != "" || p.Position != 2 {
		t.Fatalf("later root restore moved detached child: %+v %v", p, err)
	}
	stream, err := s.Get(ctx, w.ID)
	if err != nil || stream.DoneAt.IsZero() {
		t.Fatalf("restore changed done state: %+v %v", stream, err)
	}
}

// WP-F1 (b): explicitly purging a workstream and its collection removes deleted
// pages and their saved versions through the cascade, while page.delete retains both.
func TestWorkstreamDeletedPagesCollectionCascade(t *testing.T) {
	s, storage, _ := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Page retention")
	root := createWorkstreamPage(t, s, w.ID, "", "Root")
	child := createWorkstreamPage(t, s, w.ID, root.ID, "Child")
	if _, _, err := s.DeletePage(ctx, root.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{root.ID, child.ID} {
		if p, err := s.PageAtVersion(ctx, id, 1, false); err != nil || p.DeletedAt.IsZero() {
			t.Fatalf("retained version: %+v %v", p, err)
		}
	}
	if err := storage.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM workstreams WHERE id=?", w.ID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM collections WHERE id=?", w.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := storage.Read(ctx, func(tx *sql.Tx, _ int) error {
		for _, table := range []string{"workstream_pages", "workstream_page_versions"} {
			var count int
			if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				t.Errorf("%s retained %d rows after collection purge", table, count)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// WP-F1 (c): the exported creation default trims Unicode space and uses case
// folding, while the store's existing explicit boolean remains authoritative.
func TestWorkstreamReadByJobsDefault(t *testing.T) {
	for _, tc := range []struct {
		title string
		want  bool
	}{
		{"Spec", true}, {" decisions ", true}, {"DECISIONS", true}, {"\u2003sPeC\u2003", true},
		{"Spec notes", false}, {"Overview", false}, {"Notes", false}, {"", false},
	} {
		if got := ReadByJobsDefault(tc.title); got != tc.want {
			t.Errorf("%q: %v, want %v", tc.title, got, tc.want)
		}
	}
	s, _, _ := testStore(t)
	w := createStream(t, s, "Explicit store default")
	p := createWorkstreamPage(t, s, w.ID, "", "Spec")
	if p.ReadByJobs {
		t.Fatal("store changed an explicit false to the title default")
	}
}
