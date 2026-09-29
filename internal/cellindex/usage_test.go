package cellindex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/furrow"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

type total struct {
	in, out int
	micro   int64
}

func totalOf(t *testing.T) (sum total) {
	t.Helper()
	session.FlushUsage()
	lines, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range lines {
		sum.in, sum.out, sum.micro = sum.in+l.Input, sum.out+l.Output, sum.micro+int64(l.USD*1e6+0.5)
	}
	return sum
}

// Deleting the ledger and rebuilding it from the receipts gives back exactly
// what the live rows said, and a rewind afterwards changes nothing.
func TestUsageRebuildsFromReceiptsAndSurvivesARewind(t *testing.T) {
	bin, err := furrow.ResolveOwned()
	if err != nil {
		t.Skipf("no engine binary: %v", err)
	}
	t.Setenv(home.EnvVar, t.TempDir())
	e := cellstore.Engine{Binary: bin, DataRoot: t.TempDir()}
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.Sandboxed})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := cellstore.NewRecorder(nil, e, c, filepath.Join(t.TempDir(), "wal"), cellstore.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for i, micro := range []int64{1234, 5678, 91011} {
		call := executor.ModelCall{Model: "m", Role: "turn", TokensIn: 100 * (i + 1), TokensOut: 7 * (i + 1), CostMicroUSD: micro}
		session.RecordUsage(session.UsageLedgerPath(), session.UsageLine{Model: "m", Session: c.ID,
			Input: call.TokensIn, Output: call.TokensOut, USD: float64(micro) / 1e6})
		rec.NoteModelCall(call)
		if err := os.WriteFile(filepath.Join(c.Root, "f.txt"), []byte{byte('a' + i)}, 0o644); err != nil {
			t.Fatal(err)
		}
		around(t, rec)
	}
	live := totalOf(t)
	rebuilt := rebuildFresh(t, c)
	if rebuilt != live {
		t.Fatalf("rebuilt %+v, live %+v", rebuilt, live)
	}
	turns, _ := cellstore.Turns(c)
	if _, err := e.Rewind(context.Background(), c, turns[0].ID); err != nil {
		t.Fatal(err)
	}
	if again := rebuildFresh(t, c); again != live {
		t.Fatalf("after rewind %+v, live %+v", again, live)
	}
}

func around(t *testing.T, rec *cellstore.Recorder) {
	t.Helper()
	call := executor.Call{Tool: "edit", Args: []byte(`{}`)}
	if err := rec.Around(context.Background(), call, executor.EffectLocal, cellstore.AgentRun, func() ([]byte, bool) { return nil, false }); err != nil {
		t.Fatal(err)
	}
}

// rebuildFresh deletes the ledger, rebuilds it and reads it back.
func rebuildFresh(t *testing.T, c cell.Cell) total {
	t.Helper()
	session.StopUsageWriter(session.UsageLedgerPath()) // a writer holds the old file open
	if err := os.Remove(session.UsageLedgerPath()); err != nil {
		t.Fatal(err)
	}
	built, err := Rebuild(c, "")
	if err != nil {
		t.Fatal(err)
	}
	if !contains(built, "usage.jsonl") {
		t.Fatalf("built %v", built)
	}
	return totalOf(t)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
