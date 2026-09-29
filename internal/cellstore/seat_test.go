package cellstore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/executor"
)

func TestSeatIsPlainWhenCellsAreOff(t *testing.T) {
	t.Setenv("CODEAF_CELLS", "")
	seat, err := SeatFor(executor.Sandboxed, newCell(t), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := seat.(executor.Stance); !ok {
		t.Fatalf("flag off built %T, want a plain stance with no recorder", seat)
	}
}

func TestComposedEngineSealsTheWorkspaceAndKeepsTheCellOutOfIt(t *testing.T) {
	c := newCell(t)
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "a.txt"), []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := realEngine(t)
	engine.Workspace = workspace
	sealed, err := engine.Seal(context.Background(), c, TurnInfo{Trigger: AgentRun})
	if err != nil {
		t.Fatal(err)
	}
	if sealed.Turn.ID == "" {
		t.Fatal("no turn sealed")
	}
	if _, err := os.Stat(filepath.Join(c.Root, TurnsPath)); err != nil {
		t.Fatalf("the chain is not in the cell: %v", err)
	}
	// The engine still leaves its own .furrow/ identity marker in the tree it
	// attaches (repository.rs WORKSPACE_FILE); that is the engine's to move.
	for _, litter := range []string{cell.StateDir, ".git"} {
		if _, err := os.Stat(filepath.Join(workspace, litter)); err == nil {
			t.Fatalf("the seal wrote %s into the user's workspace", litter)
		}
	}
	if inside(engine.LocalDir(c), workspace) {
		t.Fatal("the engine store must live outside the workspace")
	}
}

func TestComposedArgsNameTheCellDirOnlyWhenTheTreeIsAWorkspace(t *testing.T) {
	c := newCell(t)
	if got := (Engine{}).cellDirArgs(c); got != nil {
		t.Fatalf("a cell's own folder composes nothing, got %v", got)
	}
	got := Engine{Workspace: "/w"}.cellDirArgs(c)
	if len(got) != 2 || got[0] != "--cell-dir" || got[1] != filepath.Join(c.Root, cell.StateDir) {
		t.Fatalf("args = %v", got)
	}
}

// Whatever a tool call does inside, it leaves one receipt row, and each seal is
// chained to the last by parent: a call that runs a process, one that only
// writes a file, and one that fails.
func TestSealedSeatRecordsEachToolCallAsOneReceipt(t *testing.T) {
	c := newCell(t)
	rec, fake := newRecorder(t, c, &stubExec{}, filepath.Join(t.TempDir(), "wal"))
	seat := sealed{Stance: executor.Stance{Class: executor.HostBound}, rec: rec}
	for _, failed := range []bool{false, true} {
		err := seat.Around(context.Background(), executor.Call{Tool: "bash", Args: []byte(`{"command":"x"}`)}, func() ([]byte, bool) { return []byte("out"), failed })
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := fake.count("turn-end"); got != 2 {
		t.Fatalf("%d seals for two calls", got)
	}
	head, _ := Head(c)
	call := head.Receipt.Calls[0]
	if len(head.Receipt.Calls) != 1 || call.Tool != "bash" || call.Exit != 1 || call.SideEffect != "external" || head.Turn.Parent == "" {
		t.Fatalf("head %+v", head)
	}
}

func TestACallWhoseIntentCannotBeLoggedDoesNotRun(t *testing.T) {
	c := newCell(t)
	dir := filepath.Join(t.TempDir(), "log")
	rec, _ := newRecorder(t, c, &stubExec{}, filepath.Join(dir, "wal"))
	// The log's folder becomes a file, so nothing more can be appended to it.
	if err := os.WriteFile(dir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ran := false
	err := rec.Around(context.Background(), executor.Call{Tool: "bash"}, executor.EffectLocal, func() ([]byte, bool) { ran = true; return nil, false })
	if err == nil || ran {
		t.Fatalf("err = %v, ran = %v; an unlogged call must not run", err, ran)
	}
}

func inside(parent, dir string) bool {
	rel, err := filepath.Rel(parent, dir)
	return err == nil && !strings.HasPrefix(rel, "..")
}
