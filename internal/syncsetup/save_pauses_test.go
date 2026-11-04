package syncsetup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/executor"
)

type pauseStepSeat struct {
	executor.Stance
	rec *cellstore.Recorder
}

func (s pauseStepSeat) Settle(ctx context.Context) { s.rec.Settle(ctx) }

func TestSavePausesOwnershipLossKeepsHeldCallsInTheBranch(t *testing.T) {
	r := newDriveRig(t)
	ctx := t.Context()
	appendTo(t, transcriptOf(r.cell), `{"type":"message","role":"user","content":"start this chat"}`+"\n")
	first, err := r.engine.Seal(ctx, r.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	_, err = r.a.Dir.Create(ctx, r.cell.ID, directory.CellInit{Head: first.Turn.ID})
	mustNil(t, err)
	drive, err := r.a.Drive(ctx, r.engine, r.cell, DriveOptions{})
	mustNil(t, err)
	t.Cleanup(func() { mustNil(t, drive.Close(context.Background())) })
	rec, err := cellstore.NewRecorder(executor.Local{}, drive.Store(r.engine), r.cell, r.engine.WALPath(r.cell), cellstore.Options{Async: true, Now: func() time.Time { return time.Unix(100, 0) }})
	mustNil(t, err)
	mustNil(t, rec.Around(ctx, executor.Call{Tool: "bash", Args: []byte("held call"), Deferred: true}, executor.EffectExternal, cellstore.AgentRun, func() ([]byte, bool) {
		mustNil(t, os.WriteFile(filepath.Join(r.work, "held.txt"), []byte("held work"), 0600))
		return []byte("held output"), false
	}))
	appendTo(t, transcriptOf(r.cell), `{"type":"message","role":"tool","content":"held output"}`+"\n")
	executor.SettleStep(ctx, pauseStepSeat{rec: rec})
	before, err := cellstore.Head(r.cell)
	mustNil(t, err)
	if before == nil || before.Turn.ID != first.Turn.ID {
		t.Fatal("held call was captured before ownership loss")
	}
	view, err := r.b.Dir.Cell(ctx, r.cell.ID)
	mustNil(t, err)
	_, err = r.b.Dir.Acquire(ctx, r.cell.ID, directory.AcquireOpts{Force: true, Expect: &directory.LeaseExpectation{Device: view.Cell.Lease.Device, Fence: view.Cell.Lease.Fence}})
	mustNil(t, err)
	mustNil(t, drive.CheckLease(ctx))
	branches, err := branchMapOf(r.homeA)
	mustNil(t, err)
	id := branches.Resolve(r.cell.ID)
	if id == r.cell.ID {
		t.Fatal("ownership loss did not preserve the held work as a branch")
	}
	kept, err := r.a.Dir.Cell(ctx, id)
	mustNil(t, err)
	head, err := cellstore.Head(r.cell)
	mustNil(t, err)
	if head == nil || kept.Cell.Head != head.Turn.ID || len(head.Receipt.Calls) != 1 || head.Receipt.Calls[0].Tool != "bash" || drive.Gate() == nil {
		t.Fatalf("branch did not carry the held call or retire its writer: head=%+v branch=%+v", head, kept.Cell)
	}
	_, recovery, err := cellstore.OpenWAL(r.engine.WALPath(r.cell))
	mustNil(t, err)
	if len(recovery.Completed) != 0 {
		t.Fatal("preserved call remained unsealed in the WAL")
	}
	mustNil(t, os.Remove(filepath.Join(r.work, "held.txt")))
	mustNil(t, r.engine.Restore(ctx, r.cell, kept.Cell.Head, []string{"held.txt"}))
	content, err := os.ReadFile(filepath.Join(r.work, "held.txt"))
	mustNil(t, err)
	if string(content) != "held work" {
		t.Fatalf("kept head did not restore the held call's file: %q", content)
	}
}
