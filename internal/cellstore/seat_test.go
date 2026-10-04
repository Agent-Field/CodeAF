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
	t.Setenv("CODEAF_CELLS", "0")
	seat, err := SeatFor(executor.Sandboxed, newCell(t), t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := seat.(executor.Stance); !ok {
		t.Fatalf("flag off built %T, want a plain stance with no recorder", seat)
	}
}

func TestComposedEngineSealsTheWorkspaceAndKeepsTheCellOutOfIt(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
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
	})
}

func TestComposedDirIsNamedOnlyWhenTheTreeIsAWorkspace(t *testing.T) {
	c := newCell(t)
	if got := (Engine{}).cellDir(c); got != "" {
		t.Fatalf("a cell's own folder composes nothing, got %q", got)
	}
	if got := (Engine{Workspace: "/w"}).cellDir(c); got != filepath.Join(c.Root, cell.StateDir) {
		t.Fatalf("cell dir = %q", got)
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
	err := rec.Around(context.Background(), executor.Call{Tool: "bash"}, executor.EffectLocal, AgentRun, func() ([]byte, bool) { ran = true; return nil, false })
	if err == nil || ran {
		t.Fatalf("err = %v, ran = %v; an unlogged call must not run", err, ran)
	}
}

func inside(parent, dir string) bool {
	rel, err := filepath.Rel(parent, dir)
	return err == nil && !strings.HasPrefix(rel, "..")
}

// A setup turn's call is external whatever the class, and its seal says Setup;
// an ordinary call on the same recorder stays local and AgentRun.
func TestSetupSeatSealsExternalCallsAsASetupTurn(t *testing.T) {
	c := newCell(t)
	rec, _ := newRecorder(t, c, &stubExec{}, filepath.Join(t.TempDir(), "wal"))
	seat := sealed{Stance: executor.Stance{Class: executor.Sandboxed}, rec: rec}
	for _, tc := range []struct {
		name    string
		seat    executor.Seat
		trigger Trigger
		effect  string
	}{
		{"ordinary", seat, AgentRun, "local"},
		{"setup", executor.ForSetup(seat), Setup, "external"},
	} {
		err := tc.seat.Around(context.Background(), executor.Call{Tool: "bash", Args: []byte(tc.name)}, func() ([]byte, bool) { return []byte("out"), false })
		if err != nil {
			t.Fatal(err)
		}
		head, _ := Head(c)
		if head.Turn.Trigger != tc.trigger || head.Receipt.Calls[0].SideEffect != tc.effect {
			t.Errorf("%s: trigger %s effect %s, want %s %s", tc.name, head.Turn.Trigger, head.Receipt.Calls[0].SideEffect, tc.trigger, tc.effect)
		}
	}
}

// A call that asked for its seal to wait leaves the tree unsealed until the
// caller settles, and the settle takes the whole batch in one seal that ends
// where the transcript then ends: the result line written between the call and
// the settle is inside it.
func TestDeferredCallsAreSealedOnceAtSettleWithTheResultLine(t *testing.T) {
	c := newCell(t)
	rec, fake := newRecorder(t, c, &stubExec{}, filepath.Join(t.TempDir(), "wal"))
	seat := sealed{Stance: executor.Stance{Class: executor.HostBound}, rec: rec}
	transcript := filepath.Join(c.Root, cell.TranscriptPath)
	if err := os.MkdirAll(filepath.Dir(transcript), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		call := executor.Call{Tool: "bash", Args: []byte(name), Deferred: true}
		if err := seat.Around(context.Background(), call, func() ([]byte, bool) { return nil, false }); err != nil {
			t.Fatal(err)
		}
	}
	if got := fake.count("turn-end"); got != 0 {
		t.Fatalf("%d seals before the results were written", got)
	}
	if err := os.WriteFile(transcript, []byte("assistant call\ntool result\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	seat.Settle(context.Background())
	head, _ := Head(c)
	if fake.count("turn-end") != 1 || len(head.Receipt.Calls) != 2 || head.Receipt.Transcript.End != int64(len("assistant call\ntool result\n")) {
		t.Fatalf("seals %d, head %+v", fake.count("turn-end"), head)
	}
}

// The line a turn ends on is sealed by a settle that has no call to carry, and
// a settle with nothing new seals nothing, so an idle chat adds no turns.
func TestSettleSealsTheClosingAnswerOnceAndOnlyWhenTheTranscriptGrew(t *testing.T) {
	c := newCell(t)
	rec, fake := newRecorder(t, c, &stubExec{}, filepath.Join(t.TempDir(), "wal"))
	transcript := filepath.Join(c.Root, cell.TranscriptPath)
	if err := os.MkdirAll(filepath.Dir(transcript), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(transcript, []byte("closing answer\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		rec.Settle(context.Background())
	}
	if got := fake.count("turn-end"); got != 1 {
		t.Fatalf("%d seals for one closing answer, want 1", got)
	}
	head, _ := Head(c)
	if len(head.Receipt.Calls) != 0 || head.Receipt.Transcript.End != int64(len("closing answer\n")) {
		t.Fatalf("head %+v", head)
	}
}
