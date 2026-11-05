package syncsetup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
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

func TestSavePausesFailedCaptureWithoutNotedTurnsBecomesViewer(t *testing.T) {
	r := newDriveRig(t)
	ctx := t.Context()
	// Make this explicit ownership check the first displacement, without a
	// watch or heartbeat racing it and leaving a capture error at entry.
	r.a.Dir = struct{ directory.Client }{r.a.Dir}
	appendTo(t, transcriptOf(r.cell), `{"type":"message","role":"user","content":"start this chat"}`+"\n")
	first, err := r.engine.Seal(ctx, r.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	_, err = r.a.Dir.Create(ctx, r.cell.ID, directory.CellInit{Head: first.Turn.ID})
	mustNil(t, err)
	drive, err := r.a.Drive(ctx, r.engine, r.cell, DriveOptions{Sleep: func(ctx context.Context, _ time.Duration) error {
		<-ctx.Done()
		return ctx.Err()
	}})
	mustNil(t, err)
	fault := errors.New("held capture failed")
	rec, err := cellstore.NewRecorder(executor.Local{}, refusedCapture{fault}, r.cell, r.engine.WALPath(r.cell), cellstore.Options{Async: true, Now: func() time.Time { return time.Unix(100, 0) }})
	mustNil(t, err)
	t.Cleanup(func() {
		_ = rec.Drain(context.Background())
		_ = drive.Close(context.Background())
	})
	mustNil(t, rec.Around(ctx, executor.Call{Tool: "external", Deferred: true}, executor.EffectExternal, cellstore.AgentRun, func() ([]byte, bool) {
		return []byte("completed output"), false
	}))
	appendTo(t, transcriptOf(r.cell), `{"type":"message","role":"tool","content":"completed output"}`+"\n")
	executor.SettleStep(ctx, pauseStepSeat{rec: rec})
	if drive.batcher.Pending() != 0 || cellstore.Unsaved(r.cell) != 1 {
		t.Fatal("fixture must have a held call and zero noted turns")
	}
	view, err := r.b.Dir.Cell(ctx, r.cell.ID)
	mustNil(t, err)
	_, err = r.b.Dir.Acquire(ctx, r.cell.ID, directory.AcquireOpts{Force: true, Expect: &directory.LeaseExpectation{Device: view.Cell.Lease.Device, Fence: view.Cell.Lease.Fence}})
	mustNil(t, err)
	mustNil(t, drive.CheckLease(ctx))
	drive.mu.Lock()
	viewer := drive.viewer
	drive.mu.Unlock()
	if viewer.Line == "" || drive.Gate() == nil || drive.batcher.Pending() != 0 {
		t.Fatalf("failed capture left displacement pending: viewer=%+v pending=%d", viewer, drive.batcher.Pending())
	}
	branches, err := branchMapOf(r.homeA)
	mustNil(t, err)
	head, err := cellstore.Head(r.cell)
	mustNil(t, err)
	if branches.Resolve(r.cell.ID) != r.cell.ID || head == nil || head.Turn.ID != first.Turn.ID {
		t.Fatal("failed capture invented a kept head")
	}
	_, recovery, err := cellstore.OpenWAL(r.engine.WALPath(r.cell))
	mustNil(t, err)
	if len(recovery.Completed) != 1 || string(recovery.Completed[0].Stdout) != "completed output" {
		t.Fatalf("displacement lost the unsaved completion: %+v", recovery)
	}
	for _, boundary := range []struct {
		name  string
		check func(context.Context, cell.Cell) (bool, error)
	}{{"local work", r.engine.LocalWork}, {"dirty", r.engine.Dirty}} {
		work, err := boundary.check(ctx, r.cell)
		if err != nil || !work {
			t.Fatalf("%s lost the completed WAL call after capture failure: work=%v err=%v", boundary.name, work, err)
		}
	}
}

func TestSavePausesViewerRecoveryAllowsTakeback(t *testing.T) {
	for _, closeFirst := range []bool{false, true} {
		name := "open viewer"
		if closeFirst {
			name = "closed viewer"
		}
		t.Run(name, func(t *testing.T) {
			h := bContinued(t)
			ctx := t.Context()
			h.backToA()
			view, err := h.b.Dir.Cell(ctx, h.cell.ID)
			mustNil(t, err)
			_, err = h.b.Dir.Acquire(ctx, h.cell.ID, directory.AcquireOpts{Force: true, Expect: &directory.LeaseExpectation{Device: view.Cell.Lease.Device, Fence: view.Cell.Lease.Fence}})
			mustNil(t, err)
			old, err := cellstore.NewRecorder(nil, h.engine, h.cell, h.engine.WALPath(h.cell), cellstore.Options{})
			mustNil(t, err)
			ran := 0
			mustNil(t, old.Around(ctx, executor.Call{Tool: "external", Deferred: true}, executor.EffectExternal, cellstore.AgentRun, func() ([]byte, bool) {
				ran++
				return []byte("viewer recovered output"), false
			}))
			viewer, err := h.a.Drive(ctx, h.engine, h.cell, DriveOptions{DeviceName: nameA})
			mustNil(t, err)
			if viewer.Gate() == nil || viewer.batcher != nil {
				t.Fatal("reopened drive must be a viewer without a batcher")
			}
			rec, err := cellstore.NewRecorder(nil, viewer.Store(h.engine), h.cell, h.engine.WALPath(h.cell), cellstore.Options{Async: true})
			mustNil(t, err)
			closeViewer := func() {
				mustNil(t, rec.Drain(context.Background()))
				mustNil(t, viewer.Close(context.Background()))
				if cellstore.Unsaved(h.cell) != 0 {
					t.Fatal("closed viewer retained its held recorder")
				}
			}
			t.Cleanup(closeViewer)
			mustNil(t, cellstore.Await(ctx, h.cell))
			mustNil(t, cellstore.Captured(ctx, h.cell))
			_, recovery, err := cellstore.OpenWAL(h.engine.WALPath(h.cell))
			mustNil(t, err)
			if len(recovery.Completed) != 1 {
				t.Fatal("viewer save refused without retaining the completion")
			}
			if closeFirst {
				closeViewer()
			}
			back, err := h.continuerA().Take(ctx, h.cell.ID)
			mustNil(t, err)
			if back.Taken.Kept == "" || back.KeptTurns != 1 || ran != 1 {
				t.Fatalf("viewer take-back lost or reran the completed call: kept=%s turns=%d ran=%d", back.Taken.Kept, back.KeptTurns, ran)
			}
			_, recovery, err = cellstore.OpenWAL(h.engine.WALPath(h.cell))
			mustNil(t, err)
			if len(recovery.Completed) != 0 {
				t.Fatal("take-back did not seal the recovered completion")
			}
		})
	}
}
