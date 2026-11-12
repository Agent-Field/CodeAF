package cellindex

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	storetest "github.com/Agent-Field/codeaf/internal/cellstore/celltest"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestUsageRebuildRecognizesSeededJournalIdentity(t *testing.T) {
	bin := celltest.Engine(t)
	t.Setenv(home.EnvVar, t.TempDir())
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.Sandboxed})
	if err != nil {
		t.Fatal(err)
	}
	path := (session.Place{Dir: c.Root}).Transcript()
	if err := session.SeedConversation(path, "", "Job manager", "Manage this job."); err != nil {
		t.Fatal(err)
	}
	d, err := session.ReadDigest(path)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := cellstore.NewRecorder(nil, cellstore.Engine{Binary: bin, DataRoot: storetest.EngineDir(t)}, c, filepath.Join(t.TempDir(), "wal"), cellstore.Options{})
	if err != nil {
		t.Fatal(err)
	}
	cleanupRecorder(t, rec)
	rec.NoteModelCall(executor.ModelCall{Model: "m", TokensIn: 100, TokensOut: 7, CostMicroUSD: 1234})
	if err := rec.Around(context.Background(), executor.Call{Tool: "edit", Args: []byte(`{}`)}, executor.EffectLocal, cellstore.AgentRun, func() ([]byte, bool) { return nil, false }); err != nil {
		t.Fatal(err)
	}
	session.RecordUsage(session.UsageLedgerPath(), session.UsageLine{Model: "m", Session: d.ID, Calls: 1, Input: 100, Output: 7, USD: .001234})
	session.FlushUsage()
	if _, err := Rebuild(c, ""); err != nil {
		t.Fatal(err)
	}
	if got := totalOf(t); got != (total{in: 100, out: 7, micro: 1234}) {
		t.Fatalf("one provider call rebuilt twice: %+v", got)
	}
}
