package command

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The reader follows the recorder, which is the third time this seam has had to
// be repaired and the first time it has been pinned.
//
// The recorder moved out of `.obs` into `.aforge/trace/` and this function went
// on spelling the old path by hand: it opened nothing, rendered empty, and
// looked exactly like a worker that was thinking rather than writing — for every
// run on every profile, silently, until somebody measured it. The recorder has
// now moved again, out of the leaf's working directory entirely, and the same
// failure was one line away for the same reason.
//
// Both homes stay readable. A trace is written once and read for as long as
// anybody is still asking what a node did, and a relocation that emptied every
// finished run's view would be worse than what the move was fixing.
func TestTheWindowFindsARecorderInEitherHomeItWasEverWrittenTo(t *testing.T) {
	database := filepath.Join(t.TempDir(), "chat.db")
	graph, err := store.Open(database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = graph.Close() })
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-1", Brief: "the job", Stage: 2},
		{ID: "task-1-leaf", Parent: "task-1", Brief: "a part of it", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "room", Intent: "the job"}); err != nil {
		t.Fatal(err)
	}

	workspaceRoot, scratchRoot := t.TempDir(), t.TempDir()
	commander := New(Options{
		Store: graph, Database: database,
		WorkspaceRoot: workspaceRoot, ScratchRoot: scratchRoot,
		JobID: func(store.Node) string { return "task-1" },
	})

	// Where a worker files it today: the scratch home, outside the directory the
	// leaf was told to work in.
	write(t, exec.TraceFile(scratchRoot, "task-1-leaf"), "── turn 1  finish=stop in=10 out=2 cached=0 ──\n")
	text, _, _, _ := commander.NodeTraceTail("task-1-leaf", 1<<10, 0, time.Time{})
	if text == "" {
		t.Fatal("the window cannot see a recorder in the home every current run writes to")
	}

	// And where every run recorded before the move still is: under the job's own
	// directory. A second commander, so the first read cannot be what found it.
	older := New(Options{
		Store: graph, Database: database,
		WorkspaceRoot: workspaceRoot, ScratchRoot: t.TempDir(),
		JobID: func(store.Node) string { return "task-1" },
	})
	write(t, exec.TraceFile(filepath.Join(workspaceRoot, "task-1"), "task-1-leaf"),
		"── turn 1  finish=stop in=99 out=9 cached=0 ──\n")
	legacy, _, _, _ := older.NodeTraceTail("task-1-leaf", 1<<10, 0, time.Time{})
	if legacy == "" {
		t.Fatal("a run recorded before the move became unreadable")
	}

	// A node with no recorder anywhere is silence rather than an error, which is
	// what a queued worker looks like.
	if quiet, _, _, _ := commander.NodeTraceTail("task-1", 1<<10, 0, time.Time{}); quiet != "" {
		t.Fatalf("a node that has written nothing returned %q", quiet)
	}
}

// A commander built with a database and nothing else derives the same scratch
// home the surface does, so a visitor window and the resident read one directory
// rather than two spellings of one intention.
func TestTheScratchHomeIsDerivedFromTheStoreWhenNobodyNamesOne(t *testing.T) {
	database := filepath.Join(t.TempDir(), "chat.db")
	if got := scratchRootFor("", database); got == "" || got == filepath.Dir(database) {
		t.Fatalf("scratch home for %s = %q", database, got)
	}
	if got := scratchRootFor("/named", database); got != "/named" {
		t.Fatalf("a named scratch home was overridden: %q", got)
	}
	if got := scratchRootFor("", ""); got != "" {
		t.Fatalf("a commander with no store invented a scratch home: %q", got)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
