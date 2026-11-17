package syncsetup

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/executor"
)

func TestUsageLostHoldModelCallSurvivesRecorderReopen(t *testing.T) {
	r := newDriveRig(t)
	ctx := t.Context()
	r.spokenFixture()
	first, err := r.engine.Seal(ctx, r.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	_, err = r.a.Dir.Create(ctx, r.cell.ID, directory.CellInit{Head: first.Turn.ID})
	mustNil(t, err)
	mustNil(t, cellstore.NotePublished(r.cell, first.Turn.ID))
	d, err := r.a.Drive(ctx, r.engine, r.cell, DriveOptions{})
	mustNil(t, err)
	t.Cleanup(func() { mustNil(t, d.Close(context.Background())) })
	d.cancel()
	<-d.done
	before, err := r.a.Dir.Cell(ctx, r.cell.ID)
	mustNil(t, err)
	_, err = r.b.Dir.Acquire(ctx, r.cell.ID, directory.AcquireOpts{Force: true, Expect: &directory.LeaseExpectation{Device: before.Cell.Lease.Device, Fence: before.Cell.Lease.Fence}})
	mustNil(t, err)
	mustNil(t, d.CheckLease(ctx))
	if d.Gate() == nil {
		t.Fatal("fixture did not lose the hold")
	}
	wal := filepath.Join(t.TempDir(), "model-wal")
	rec, err := cellstore.NewRecorder(nil, d.Store(r.engine), r.cell, wal, cellstore.Options{})
	mustNil(t, err)
	rec.NoteModelCall(executor.ModelCall{ID: "lost-hold-charge", Model: "fixture/model", TokensIn: 10, TokensOut: 1, CostMicroUSD: 9000})
	rec.Settle(ctx)
	calls, err := cellstore.ModelCalls(r.cell)
	mustNil(t, err)
	if len(calls) != 0 {
		t.Fatalf("fixture unexpectedly saved model calls after hold loss: %+v", calls)
	}
	t.Logf("calls after refused seal: %d", len(calls))
	// Discarding the first recorder models process memory being lost. The
	// reopened recorder is allowed to seal, so only durable recovery matters.
	reopened, err := cellstore.NewRecorder(nil, r.engine, r.cell, wal, cellstore.Options{})
	mustNil(t, err)
	reopened.Settle(ctx)
	calls, err = cellstore.ModelCalls(r.cell)
	mustNil(t, err)
	count := 0
	for _, call := range calls {
		if call.ID == "lost-hold-charge" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("reopened recorder recovered charge %d times, want once", count)
	}
}
