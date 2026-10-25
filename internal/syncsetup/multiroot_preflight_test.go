package syncsetup

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

// Labelled API fixture: native/public Take is real; task metadata is supplied
// explicitly to test rejection before task restoration mutates a shared repo.
func TestPublicTakeRejectsTwoUnboundRootsBeforeRepoOrRegistryMutation(t *testing.T) {
	ctx := context.Background()
	h := newTwoHomes(t)
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	if err := os.WriteFile(filepath.Join(h.work, "input.txt"), []byte("primary baseline\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, h.work, "init", "-q", "-b", "main")
	git(t, h.work, "add", ".")
	git(t, h.work, "commit", "-qm", "baseline")
	a := h.openA()
	a.mustSay("initial shared turn")
	if err := a.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := h.continuerB().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	at := first.Taken.Cell
	work := workspaceOf(at.Root)
	t.Setenv(home.EnvVar, h.homeB)
	secondHead := seedTwoRootProtocolFixture(t, at, work)
	b := h.openOn(h.b, h.engB, at, work, nameB)
	b.mustSay("two separate working roots")
	executor.Settle(ctx, b.seat)
	b.drive.Idle()
	h.durable(h.cell.ID, at)
	if err := b.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, h.homeA)
	registryPath := filepath.Join(h.homeA, "workspace-identities")
	if _, err := os.Stat(registryPath); !os.IsNotExist(err) {
		t.Fatalf("fixture receiver already has registry: %v", err)
	}
	var beforeRefs, beforeObjects string
	observed := false
	h.engine.Transport = afterRestoreSnapshot{under: h.engine.Transport, after: func(target cellstore.Target) {
		observed = true
		beforeRefs = gitOut(t, target.Tree, "show-ref")
		beforeObjects = gitOut(t, target.Tree, "count-objects", "-v")
		if exec.Command("git", "-C", target.Tree, "cat-file", "-e", secondHead+"^{commit}").Run() == nil {
			t.Fatal("fixture secondary commit already in primary snapshot")
		}
	}}
	_, err = h.continuerA().Take(ctx, h.cell.ID)
	if err == nil || !strings.Contains(err.Error(), "working root") {
		t.Fatalf("Take did not explain unbound roots: %v notices=%v", err, h.wall.all())
	}
	if !observed {
		t.Fatal("native restore observation did not run")
	}
	if got := gitOut(t, h.work, "show-ref"); got != beforeRefs {
		t.Fatal("task restoration changed borrowed primary refs before rejection")
	}
	if got := gitOut(t, h.work, "count-objects", "-v"); got != beforeObjects {
		t.Fatal("task unbundle changed borrowed primary objects before rejection")
	}
	if exec.Command("git", "-C", h.work, "cat-file", "-e", secondHead+"^{commit}").Run() == nil {
		t.Fatal("secondary commit entered borrowed primary repository")
	}
	if _, err := os.Stat(registryPath); !os.IsNotExist(err) {
		t.Fatal("rejected roots created or changed receiver identity registry")
	}
	for _, id := range []string{"1", "2"} {
		if _, err := os.Stat(filepath.Join(h.cell.Root, cell.TreesDir, id)); !os.IsNotExist(err) {
			t.Fatalf("rejected task %s was instantiated", id)
		}
		if _, err := os.Stat(filepath.Join(h.cell.Root, cell.CarriedTreesPath, id, "tree.json")); err != nil {
			t.Fatalf("incoming archive %s lost: %v", id, err)
		}
	}
}

type afterRestoreSnapshot struct {
	under cellstore.Transport
	after func(cellstore.Target)
}

func (s afterRestoreSnapshot) Do(ctx context.Context, target cellstore.Target, operation cellstore.Op) ([]byte, error) {
	raw, err := s.under.Do(ctx, target, operation)
	if err == nil && operation.Verb() == "materialize" {
		s.after(target)
	}
	return raw, err
}

func seedTwoRootProtocolFixture(t *testing.T, at cell.Cell, primary string) string {
	t.Helper()
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	chat, err := registry.Register(workspaceidentity.Chat, at.Root)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := session.LoadMeta(at.Root)
	if err != nil {
		t.Fatal(err)
	}
	meta.ChatURI = chat.URI
	if err := session.SaveMeta(at.Root, meta); err != nil {
		t.Fatal(err)
	}
	secondary := t.TempDir()
	git(t, filepath.Dir(primary), "clone", "-q", "--", primary, secondary)
	var rows []map[string]any
	var secondHead string
	for _, id := range []string{"1", "2"} {
		project := primary
		if id == "2" {
			project = secondary
		}
		projectRecord, err := registry.Register(workspaceidentity.Project, project)
		if err != nil {
			t.Fatal(err)
		}
		tree := filepath.Join(at.Root, cell.TreesDir, id)
		git(t, filepath.Dir(primary), "clone", "-q", "--", project, tree)
		git(t, tree, "checkout", "-qb", "task/fixture-"+id)
		if err := os.WriteFile(filepath.Join(tree, "output-"+id+".txt"), []byte("completed output "+id+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		git(t, tree, "add", ".")
		git(t, tree, "commit", "-qm", "worker output "+id)
		record, err := registry.Register(workspaceidentity.Run, tree, workspaceidentity.Links{OwnerURI: chat.URI, ProjectURI: projectRecord.URI})
		if err != nil {
			t.Fatal(err)
		}
		ground := "workspace:."
		if id == "2" {
			ground = secondary
			secondHead = gitOut(t, tree, "rev-parse", "HEAD")
		}
		copy := &session.TaskCopyRecord{URI: record.URI, ChatURI: chat.URI, ProjectURI: projectRecord.URI, GroundURI: projectRecord.URI, Dir: "session:trees/" + id, Ground: ground, Branch: "task/fixture-" + id, Mode: session.TaskModeWorktree}
		rows = append(rows, map[string]any{"id": int(id[0] - '0'), "title": "root fixture " + id, "state": "done", "report": "result retained", "changed": []string{"output-" + id + ".txt"}, "copy": copy})
	}
	raw, err := json.Marshal(map[string]any{"type": "tasks", "version": 1, "seq": 2, "runs": rows})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(session.Place{Dir: at.Root}.Tasks(), raw, 0600); err != nil {
		t.Fatal(err)
	}
	return secondHead
}

func TestPublicForkRejectsTwoUnboundRootsBeforeDetachingHistory(t *testing.T) {
	ctx := context.Background()
	h := newTwoHomes(t)
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	if err := os.WriteFile(filepath.Join(h.work, "input.txt"), []byte("baseline\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, h.work, "init", "-q", "-b", "main")
	git(t, h.work, "add", ".")
	git(t, h.work, "commit", "-qm", "baseline")
	a := h.openA()
	a.mustSay("shared initial")
	if err := a.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := h.continuerB().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	at := first.Taken.Cell
	work := workspaceOf(at.Root)
	t.Setenv(home.EnvVar, h.homeB)
	seedTwoRootProtocolFixture(t, at, work)
	original, err := os.ReadFile(session.Place{Dir: at.Root}.Tasks())
	if err != nil {
		t.Fatal(err)
	}
	appendTo(t, transcriptOf(at), "{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"unpublished two-root work\"}\n")
	engine := h.engB
	engine.Workspace = work
	if _, err := engine.Seal(ctx, at, cellstore.TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, h.homeA)
	if _, err := h.continuerA().Take(ctx, h.cell.ID); err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, h.homeB)
	again, err := h.continuerB().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Taken.Kept == "" {
		t.Fatal("fixture did not retain a branch")
	}
	t.Setenv(home.EnvVar, h.homeA)
	root := filepath.Join(t.TempDir(), again.Taken.Kept)
	var primary, beforeRefs, beforeObjects string
	h.engine.Transport = afterRestoreSnapshot{under: h.engine.Transport, after: func(target cellstore.Target) {
		primary = target.Tree
		beforeRefs = gitOut(t, primary, "show-ref")
		beforeObjects = gitOut(t, primary, "count-objects", "-v")
	}}
	opener := h.a.Continuer(h.engine, TakeOptions{RootFor: func(string) string { return root }})
	if _, err := opener.OpenBranch(ctx, again.Taken.Kept); err == nil || !strings.Contains(err.Error(), "working root") {
		t.Fatalf("fork did not explain unbound roots: %v", err)
	}
	if primary == "" {
		t.Fatal("native materialization was not observed")
	}
	primary = session.Place{Dir: root, Owned: true}.Work()
	if got := gitOut(t, primary, "show-ref"); got != beforeRefs {
		t.Fatal("rejected fork modified primary refs")
	}
	if got := gitOut(t, primary, "count-objects", "-v"); got != beforeObjects {
		t.Fatal("rejected fork modified primary objects")
	}
	got, err := os.ReadFile(session.Place{Dir: root}.Tasks())
	if err != nil || string(got) != string(original) {
		t.Fatalf("fork detached original frontier before root preflight: %v", err)
	}
	digest, err := session.ReadDigest(session.Place{Dir: root}.Transcript())
	if err != nil || digest.ID != h.cell.ID {
		t.Fatalf("fork rewrote journal identity before root preflight: %s %v", digest.ID, err)
	}
	if _, err := os.Stat(session.ForkHistoryDir(session.Place{Dir: root})); !os.IsNotExist(err) {
		t.Fatal("rejected fork created detached history")
	}
	if _, err := os.Stat(filepath.Join(h.homeA, "workspace-identities")); !os.IsNotExist(err) {
		t.Fatal("rejected fork created identity registry")
	}
	for _, id := range []string{"1", "2"} {
		if _, err := os.Stat(filepath.Join(root, cell.TreesDir, id)); !os.IsNotExist(err) {
			t.Fatalf("rejected fork instantiated copy %s", id)
		}
		if _, err := os.Stat(filepath.Join(root, cell.CarriedTreesPath, id, "tree.json")); err != nil {
			t.Fatalf("incoming archive %s lost: %v", id, err)
		}
	}
}
