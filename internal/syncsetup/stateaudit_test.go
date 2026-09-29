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
	got, err := h.continuerB().Take(context.Background(), h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	au.placeB = session.Place{Dir: got.Taken.Cell.Root}
	au.workB = workspaceOf(got.Taken.Cell.Root)
	// A's disk is not B's: what A's trees/ held is gone from where B looks.
	if err := os.RemoveAll(au.placeA.Trees()); err != nil {
		t.Fatal(err)
	}

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
	// the node's journal, which the checkpoint names and the cell carries.
	au.journalName = "20260929T100000_1.jsonl"
	journal := filepath.Join(au.placeA.NodeJournals(), au.journalName)
	appendTo(t, journal, `{"type":"message","role":"assistant","content":"working"}`+"\n")
	// a memory, written through the store the way the chat writes it.
	au.memoryID = au.remember(t, h)
	// an artifact: a file in the session's artifacts/ and its row in the index.
	au.artifactPath = filepath.Join(au.placeA.Artifacts(), "report.md")
	appendTo(t, au.artifactPath, "# report\n")
	session.RecordArtifact(home.Join("v3", session.ArtifactsIndexName), session.Artifact{
		Path: au.artifactPath, Session: h.cell.ID, Title: "report", Kind: "document", Created: time.Now(),
	})
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

// worktreeTravels: trees/ sits in the session folder outside the sealed tree
// (session/place.go Trees, cell/migrate.go: "trees/ stay where they are").
func (au *audited) worktreeTravels(t *testing.T) {
	gapOr(t, exists(filepath.Join(au.placeB.Trees(), "1", "wip.txt")),
		"a task's working copy (trees/<id>) is outside the sealed tree: its uncommitted files are lost on move; follow-up: seal trees/ as child cells (ARCHITECTURE section 14)")
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
	gapOr(t, !strings.Contains(string(out), "prunable"),
		"the sealed .git registers a worktree at A's absolute trees/1 path, dangling on B; follow-up: prune on take (cell/migrate.go keeps trees/ machine-local)")
}

// artifactFileTravels: a borrowed chat's artifacts/ is beside the sealed tree,
// not in it (session/place.go Artifacts; landing.go deliverablesDir).
func (au *audited) artifactFileTravels(t *testing.T) {
	gapOr(t, exists(filepath.Join(au.placeB.Artifacts(), "report.md")),
		"a borrowed chat's artifacts/ is outside the sealed tree; follow-up: land deliverables in the workspace or seal artifacts/")
}

// artifactRowOnB: the global index is neither sealed nor rebuilt by
// cellindex.Indexes (meta, usage, tasks, memories only).
func (au *audited) artifactRowOnB(t *testing.T) {
	rows := session.ReadArtifacts(home.Join("v3", session.ArtifactsIndexName))
	gapOr(t, len(rows) == 1 && rows[0].Title == "report",
		"artifacts.jsonl has no cellindex.Index: the moved chat's deliverables are missing from B's artifact picker; follow-up: artifactIndex from sealed artifact receipts")
}
