package syncsetup

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Fictional checkpoint metadata is seeded at the API seam. Public OpenBranch
// must preserve ordinary profiles' frontier and task-copy restoration policy.
func TestPublicOpenBranchWithoutPrototypePreservesLegacyTaskFrontier(t *testing.T) {
	ctx := context.Background()
	h := newTwoHomes(t)
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "0")
	if err := os.WriteFile(filepath.Join(h.work, "input.txt"), []byte("baseline\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, h.work, "init", "-q", "-b", "main")
	git(t, h.work, "add", ".")
	git(t, h.work, "commit", "-qm", "baseline")
	a := h.openA()
	a.mustSay("shared initial turn")
	if err := a.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := h.continuerB().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	at := first.Taken.Cell
	work := workspaceOf(at.Root)
	tree := filepath.Join(at.Root, cell.TreesDir, "1")
	git(t, work, "worktree", "add", "-qb", "task/kept", tree)
	if err := os.WriteFile(filepath.Join(tree, "result.txt"), []byte("keep this work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"type":"tasks","version":1,"seq":1,"runs":[{"id":1,"title":"legacy active work","state":"running","pendingRun":{},"copy":{"dir":"session:trees/1","branch":"task/kept"}}]}`)
	if err := os.WriteFile(session.Place{Dir: at.Root}.Tasks(), original, 0600); err != nil {
		t.Fatal(err)
	}
	appendTo(t, transcriptOf(at), "{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"unpublished work\"}\n")
	engine := h.engB
	engine.Workspace = work
	if _, err := engine.Seal(ctx, at, cellstore.TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.continuerA().Take(ctx, h.cell.ID); err != nil {
		t.Fatal(err)
	}
	again, err := h.continuerB().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Taken.Kept == "" {
		t.Fatal("fixture did not retain unpublished branch")
	}
	root := filepath.Join(t.TempDir(), again.Taken.Kept)
	opener := h.a.Continuer(h.engine, TakeOptions{RootFor: func(string) string { return root }})
	received, err := opener.OpenBranch(ctx, again.Taken.Kept)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(session.Place{Dir: received.Taken.Cell.Root}.Tasks())
	if err != nil || !bytes.Equal(got, original) {
		t.Fatalf("disabled prototype rewrote legacy frontier: %s %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, cell.CarriedTreesPath, "1", "tree.json")); err != nil {
		t.Fatalf("legacy carrier was moved: %v", err)
	}
	if _, err := os.Stat(session.ForkHistoryDir(session.Place{Dir: root})); !os.IsNotExist(err) {
		t.Fatal("disabled prototype created independent history")
	}
	if raw, err := os.ReadFile(filepath.Join(root, cell.TreesDir, "1", "result.txt")); err != nil || string(raw) != "keep this work\n" {
		t.Fatalf("legacy task copy did not restore normally: %q %v", raw, err)
	}
	meta, err := session.LoadMeta(root)
	if err != nil || meta.ID != again.Taken.Kept || !meta.Owned {
		t.Fatalf("legacy branch identity behavior changed: %+v %v", meta, err)
	}
}

func TestLegacyBranchIdentityLeavesExistingPlanAndCheckpointUntouched(t *testing.T) {
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "0")
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	place := session.Place{Dir: c.Root}
	if err := os.WriteFile(place.Transcript(), []byte("{\"type\":\"session\",\"id\":\"parent\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(c.Root, session.Meta{ID: "parent", Workspace: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	frontier := []byte(`{"type":"tasks","version":1,"seq":1,"runs":[{"id":1,"title":"legacy active","state":"running","pendingRun":{}}]}`)
	if err := os.WriteFile(place.Tasks(), frontier, 0600); err != nil {
		t.Fatal(err)
	}
	plan := []byte("existing plan bytes")
	if err := os.WriteFile(filepath.Join(c.Root, "plandb.db"), plan, 0600); err != nil {
		t.Fatal(err)
	}
	if err := branchIdentity(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string][]byte{place.Tasks(): frontier, filepath.Join(c.Root, "plandb.db"): plan} {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("disabled prototype modified %s: %q %v", path, got, err)
		}
	}
}
