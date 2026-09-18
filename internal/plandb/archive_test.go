package plandb

// Tests for the archive: the sweep that moves old finished subtrees out of
// the live plan, the reads that stop seeing them, and the CLI verbs over it.

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Archive moves a finished subtree whose every task has been terminal past the
// window, leaves the live work where it was, and reads back through Archived
// — across a reopen, so the plan is proved still valid without the moved rows.
func TestPlandbCliArchiveMovesFinishedSubtreesOutOfTheLivePlan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	store := planOpen(t, path)
	clock := time.Now().UTC()
	store.now = func() time.Time { return clock }

	planAdd(t, store,
		TaskSpec{ID: "old", Title: "Deprecated parent"},
		TaskSpec{ID: "oldkid", Title: "Deprecated child", ParentID: "old"},
		planSpec("live", "Live work"),
	)
	planFinish(t, store, "oldkid", "w", "done")
	if task := store.Task("old"); task == nil || task.Status != StatusDone {
		t.Fatalf("old parent = %#v, want auto-completed done", task)
	}

	clock = clock.Add(73 * time.Hour)
	moved, err := store.Archive(72 * time.Hour)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if len(moved) != 2 || moved[0].ID != "old" || moved[1].ID != "oldkid" {
		t.Fatalf("archived %v, want [old oldkid]", planIDs(moved))
	}
	// The live plan no longer holds them.
	if store.Task("old") != nil || store.Task("oldkid") != nil {
		t.Fatal("an archived task is still in the live plan")
	}
	if ids := planIDs(store.Tasks()); len(ids) != 2 {
		t.Fatalf("live tasks = %v, want the root and the live task", ids)
	}
	if task := store.Task("live"); task == nil || task.Status != StatusReady {
		t.Fatalf("live task = %#v, want ready and untouched", task)
	}
	// Nothing that reads the plan sees the archived words any more.
	if got := store.Search("deprecated", 0); len(got) != 0 {
		t.Fatalf("search found archived work: %#v", got)
	}
	// Archived reads them back with the moment they were archived.
	archived, err := store.Archived()
	if err != nil {
		t.Fatalf("archived: %v", err)
	}
	if len(archived) != 2 || archived[0].ID != "old" || archived[0].ArchivedAt.IsZero() {
		t.Fatalf("archived = %#v, want [old oldkid] stamped", archived)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// The plan reopens valid and still hides the archived subtree.
	reopened := planReopen(t, path)
	defer reopened.Close()
	if reopened.Task("old") != nil || reopened.Task("oldkid") != nil {
		t.Fatal("the archive leaked back into the live plan on reopen")
	}
	back, err := reopened.Archived()
	if err != nil || len(back) != 2 || back[0].ID != "old" {
		t.Fatalf("archived after reopen = %#v (%v)", back, err)
	}
}

// The window holds: a recently finished subtree stays in the plan, and the
// root is never archived even when its last child leaves it.
func TestPlandbCliArchiveWaitsOutTheWindow(t *testing.T) {
	store := planOpen(t, "")
	clock := time.Now().UTC()
	store.now = func() time.Time { return clock }
	planAdd(t, store, planSpec("job", "Job"))
	planFinish(t, store, "job", "w", "done")

	clock = clock.Add(time.Hour)
	if moved, err := store.Archive(72 * time.Hour); err != nil || len(moved) != 0 {
		t.Fatalf("archive inside the window = %v (%v), want nothing", planIDs(moved), err)
	}
	clock = clock.Add(73 * time.Hour)
	moved, err := store.Archive(72 * time.Hour)
	if err != nil || len(moved) != 1 || moved[0].ID != "job" {
		t.Fatalf("archive past the window = %v (%v), want [job]", planIDs(moved), err)
	}
	// The root is the run and is never archived, even with every child gone.
	if store.Task("root") == nil {
		t.Fatal("the run root was archived")
	}
	if ids := planIDs(store.Tasks()); len(ids) != 1 || ids[0] != "root" {
		t.Fatalf("live tasks = %v, want only the root", ids)
	}
	// The plan still opens: a root whose last child left is no longer
	// composite.
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	_ = planReopen(t, store.path).Close()
}

// The CLI verbs: `archive --older-than` moves the old finished work and
// `list --archived` reads it back.
func TestPlandbCliArchiveAndListArchived(t *testing.T) {
	h := cliNewHarness(t)
	h.cliInitFresh()
	h.cliAdd("Finished", "done-one")
	h.cliAdd("Waiting", "waiting")

	st, err := Open(h.db, "", "", "", "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := st.Claim("done-one", "runtime"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := st.Done("done-one", "runtime", "finished", nil, nil); err != nil {
		t.Fatalf("done: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Nothing is 72h old, so the default window's work archives nothing.
	code := h.run("--db", h.db, "archive", "--older-than", "72h")
	cliWantCode(t, code, 0)
	if got := h.out.String(); got != "archived 0 tasks\n" {
		t.Fatalf("archive shape %q, want \"archived 0 tasks\\n\"", got)
	}
	// A zero window takes the finished task now.
	code = h.run("--db", h.db, "archive", "--older-than", "0s")
	cliWantCode(t, code, 0)
	if got := h.out.String(); got != "archived 1 tasks\n" {
		t.Fatalf("archive shape %q, want \"archived 1 tasks\\n\"", got)
	}
	code = h.run("--db", h.db, "list")
	cliWantCode(t, code, 0)
	if strings.Contains(h.out.String(), "t-done-one") {
		t.Fatalf("list still shows the archived task:\n%s", h.out.String())
	}
	code = h.run("--db", h.db, "list", "--archived")
	cliWantCode(t, code, 0)
	if !strings.Contains(h.out.String(), "t-done-one Finished [done]") {
		t.Fatalf("list --archived missed the row:\n%s", h.out.String())
	}
	// A bad window is refused with the cause.
	code = h.run("--db", h.db, "archive", "--older-than", "soon")
	cliWantError(t, h, code, "--older-than needs a duration")
}

func TestPlandbCliArchivePreservesChecks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	store := planOpen(t, path)
	clock := time.Now().UTC()
	store.now = func() time.Time { return clock }
	planAdd(t, store, TaskSpec{ID: "checked", Title: "Checked", Checks: []string{"go test ./internal/plandb"}})
	planFinish(t, store, "checked", "w", "done")
	clock = clock.Add(73 * time.Hour)
	if _, err := store.Archive(72 * time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := planReopen(t, path)
	defer reopened.Close()
	got, err := reopened.Archived()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Checks) != 1 || got[0].Checks[0] != "go test ./internal/plandb" {
		t.Fatalf("archived checks = %#v", got)
	}
}
