package syncsetup

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
)

// The state audit: what a chat carries over the Stage 1 sync and what it leaves
// on the machine it ran on. A runs a chat that keeps one thing of every kind
// (a task working copy, a node journal, a memory, an artifact), B takes it, and
// each kind is asked for on B. The process has one CODEAF_HOME, so the test
// moves it to B's home before the take, as if B were another machine: B's
// graph.db, usage ledger and artifacts index are then B's own and start empty.
//
// A kind B lacks is a KNOWN GAP with a named follow-up, and the test skips it
// with that name. The day a fix lands the kind is present, the check passes,
// and the skip stops firing: nothing here needs editing to notice.

// gapOr passes when what is wanted is there and skips, naming the follow-up,
// when it is not.
func gapOr(t *testing.T, present bool, followUp string) {
	t.Helper()
	if !present {
		t.Skip("known gap, " + followUp)
	}
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }

// gitOut is the output of one git command in dir.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// git runs one git command in dir for the test's own setup.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// audited is what A kept and where, and what B holds after the take.
type audited struct {
	memoryID     string
	artifactPath string
	journalName  string
	placeA       session.Place
	placeB       session.Place
	workB        string
}

func TestTwoHomesStateAudit(t *testing.T) {
	h := newTwoHomes(t)
	seedTree(t, h.work)
	appendTo(t, filepath.Join(h.work, ".gitignore"), "build/\n")
	git(t, h.work, "init", "-q")
	git(t, h.work, "add", ".")
	git(t, h.work, "commit", "-q", "-m", "base")

	au := audited{placeA: session.Place{Dir: h.cell.Root}}
	au.keepOnA(t, h)

	a := h.openA()
	a.mustSay("ran a task, remembered a fact, made a report")
	h.durable(h.cell.ID, h.cell)
	if err := a.drive.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	// B is another machine: its own home for graph.db and the ledgers, and no
	// folder of A's. The rig's B shares A's disk, and a take whose project folder
	// is still there lands in it in place (take.go InPlace, projectOf), which
	// would make every check below compare A with itself.
	t.Setenv(home.EnvVar, h.b.Home)
	if err := os.Rename(h.work, filepath.Join(t.TempDir(), "a-project")); err != nil {
		t.Fatal(err)
	}
	// A's disk is not B's: what A's trees/ held is gone from where B looks.
	if err := os.RemoveAll(au.placeA.Trees()); err != nil {
		t.Fatal(err)
	}
	got, err := h.continuerB().Take(context.Background(), h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	au.placeB = session.Place{Dir: got.Taken.Cell.Root}
	au.workB = workspaceOf(got.Taken.Cell.Root)

	t.Run("node journal travels", au.journalTravels)
	t.Run("memory is rebuilt into B's graph", au.memoryRebuilt)
	t.Run("task working copy travels", au.worktreeTravels)
	t.Run("task branch is in B's repository", au.branchTravels)
	t.Run("task worktree registration is not stale on B", au.registrationClean)
	t.Run("artifact file travels", au.artifactFileTravels)
	t.Run("artifacts index row is on B", au.artifactRowOnB)
}

// keepOnA makes the four kinds of state on A, before the chat's turn is sealed.
func (au *audited) keepOnA(t *testing.T, h *twoHomes) {
	t.Helper()
	// a task working copy: a git worktree cut off the project, with a commit and
	// an unlanded file, under the session folder's trees/.
	tree := filepath.Join(au.placeA.Trees(), "1")
	git(t, h.work, "worktree", "add", "-q", "-b", "task/one", tree)
	if err := os.WriteFile(filepath.Join(tree, "done.txt"), []byte("committed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, tree, "add", ".")
	git(t, tree, "commit", "-q", "-m", "task work")
	if err := os.WriteFile(filepath.Join(tree, "wip.txt"), []byte("not committed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// the copy also deleted a tracked file and holds ignored build output.
	if err := os.Remove(filepath.Join(tree, "README.md")); err != nil {
		t.Fatal(err)
	}
	appendTo(t, filepath.Join(tree, "build", "out.bin"), "ignored\n")
	// the node's journal, which the checkpoint names and the cell carries.
	au.journalName = "20260929T100000_1.jsonl"
	journal := filepath.Join(au.placeA.NodeJournals(), au.journalName)
	appendTo(t, journal, `{"type":"message","role":"assistant","content":"working"}`+"\n")
	// a memory, written through the store the way the chat writes it.
	au.memoryID = au.remember(t, h)
	// an artifact: a file in the session's artifacts/ and its row in the index.
	au.artifactPath = filepath.Join(au.placeA.Artifacts(), "report.md")
	appendTo(t, au.artifactPath, "# report\n")
	row := session.Artifact{Path: au.artifactPath, Session: h.cell.ID, Title: "report", Kind: "document", Created: time.Now()}
	session.RecordSealedArtifact(au.placeA, row)
	session.RecordArtifact(home.Join("v3", session.ArtifactsIndexName), row)
}

func (au *audited) remember(t *testing.T, h *twoHomes) string {
	t.Helper()
	brain, err := store.Open(home.Join("graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer brain.Close()
	brain.SetMemoryLedger(session.CellMemories)
	session.CellMemories.Bind(h.cell.ID, h.cell.Root)
	m, err := brain.AddMemory(store.Memory{Type: store.MemoryDecision, Scope: store.MemoryScopeProject,
		Title: "parser order", Text: "migrate the parser before the printer", SourceSession: h.cell.ID})
	if err != nil {
		t.Fatal(err)
	}
	return m.ID
}

func (au *audited) journalTravels(t *testing.T) {
	if !exists(filepath.Join(au.placeB.NodeJournals(), au.journalName)) {
		t.Fatalf("B has no node journal at %s", au.placeB.NodeJournals())
	}
}

func (au *audited) memoryRebuilt(t *testing.T) {
	brain, err := store.Open(home.Join("graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer brain.Close()
	m, found, err := brain.MemoryRecord(au.memoryID)
	if err != nil || !found || m.Text != "migrate the parser before the printer" {
		t.Fatalf("B's graph.db memory = %+v, found %v, err %v", m, found, err)
	}
}

// worktreeTravels: the seal carries a task's working copy inside .cell/trees/,
// and the takeover cuts the worktree again on the task's branch at B's own path
// and lays the carried files over it (taskcopy).
func (au *audited) worktreeTravels(t *testing.T) {
	tree := filepath.Join(au.placeB.Trees(), "1")
	for path, want := range map[string]string{"wip.txt": "not committed\n", "done.txt": "committed\n"} {
		got, err := os.ReadFile(filepath.Join(tree, path))
		if err != nil || string(got) != want {
			t.Errorf("B's task copy %s = %q, %v; want %q", path, got, err, want)
		}
	}
	if exists(filepath.Join(tree, "README.md")) {
		t.Error("the file the task deleted is back in B's copy")
	}
	if exists(filepath.Join(tree, "build", "out.bin")) {
		t.Error("ignored build output travelled")
	}
	if out := gitOut(t, tree, "rev-parse", "--abbrev-ref", "HEAD"); out != "task/one" {
		t.Errorf("B's task copy is on %q, want task/one", out)
	}
}

// branchTravels: the task branch lives in the project's own .git, and the
// engine seals .git with the rest of the folder (it only refuses to merge or
// exclude it), so the branch and its commits arrive on B. Uncommitted files of
// the working copy do not (task working copy above).
func (au *audited) branchTravels(t *testing.T) {
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", "refs/heads/task/one")
	cmd.Dir = au.workB
	if err := cmd.Run(); err != nil {
		t.Fatalf("task/one is not in B's repository at %s: %v", au.workB, err)
	}
}

// registrationClean: the sealed .git carries .git/worktrees/1, whose gitdir
// names A's trees/1 by absolute path. On B that path is gone, so git lists the
// registration as prunable and holds task/one as checked out there.
func (au *audited) registrationClean(t *testing.T) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = au.workB
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git worktree list on B: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "prunable") {
		t.Fatalf("B's repository still registers a worktree at a path that is not here:\n%s", out)
	}
	if got := strings.Count(string(out), "worktree "); got != 2 {
		t.Fatalf("B's repository registers %d worktrees, want the project and the task copy:\n%s", got, out)
	}
}

// artifactFileTravels: a borrowed chat's artifacts/ is inside .cell/, so the
// sealed tree carries the file.
func (au *audited) artifactFileTravels(t *testing.T) {
	if !exists(filepath.Join(au.placeB.Artifacts(), "report.md")) {
		t.Fatalf("B has no artifact at %s", au.placeB.Artifacts())
	}
}

// artifactRowOnB: the machine's index is derived from the cell's ledger, so B's
// own index, which started empty, names the file at B's path.
func (au *audited) artifactRowOnB(t *testing.T) {
	rows := session.ReadArtifacts(home.Join("v3", session.ArtifactsIndexName))
	want := filepath.Join(au.placeB.Artifacts(), "report.md")
	if len(rows) != 1 || rows[0].Title != "report" || rows[0].Path != want {
		t.Fatalf("B's artifact index = %+v, want one row for %s", rows, want)
	}
}
