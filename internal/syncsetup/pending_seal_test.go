package syncsetup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/executor"
)

type refusedCapture struct{ err error }

func (s refusedCapture) Seal(context.Context, cell.Cell, cellstore.TurnInfo) (cellstore.Sealed, error) {
	return cellstore.Sealed{}, s.err
}

// TestD4PendingSaveCannotBypassOwnership proves D4: failing to finish a save is
// a refusal at the next ownership check, never permission to skip the check and
// run a command after another computer could have continued the chat.
func TestD4PendingSaveCannotBypassOwnership(t *testing.T) {
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	fault := errors.New("capture refused")
	rec, err := cellstore.NewRecorder(nil, refusedCapture{fault}, c, filepath.Join(t.TempDir(), "wal"), cellstore.Options{Async: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Around(t.Context(), executor.Call{Tool: "bash"}, executor.EffectExternal, cellstore.AgentRun, func() ([]byte, bool) { return []byte("done"), false }); err != nil {
		t.Fatal(err)
	}
	if err := rec.Drain(t.Context()); !errors.Is(err, fault) {
		t.Fatalf("save failure not retained: %v", err)
	}
	drive := &Drive{cell: c}
	if err := drive.CheckLease(t.Context()); !errors.Is(err, fault) {
		t.Fatalf("failed save bypassed ownership: %v", err)
	}
}

// D4/L9 requires take-back to branch a dead process's completed external call
// even when its only effect was output in the private WAL, with no file edit.
func TestD4TakebackKeepsDeadCallsWithoutWorkspaceEdits(t *testing.T) {
	h := bContinued(t)
	rec, err := cellstore.NewRecorder(nil, h.engine, h.cell, h.engine.WALPath(h.cell), cellstore.Options{})
	if err != nil {
		t.Fatal(err)
	}
	const output = "D4_EXTERNAL_CALL_COMPLETED_ONCE"
	ran := 0
	err = rec.Around(t.Context(), executor.Call{Tool: "external", Deferred: true}, executor.EffectExternal, cellstore.AgentRun, func() ([]byte, bool) {
		ran++
		return []byte(output), false
	})
	if err != nil {
		t.Fatal(err)
	}
	// Dropping the recorder before Settle models death after durable completion.
	back, err := h.continuerA().Take(t.Context(), h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.Taken.Kept == "" || back.KeptTurns != 1 || ran != 1 {
		t.Fatalf("dead completion lost or rerun: kept=%s turns=%d ran=%d", back.Taken.Kept, back.KeptTurns, ran)
	}
	branch, err := h.a.Dir.Cell(t.Context(), back.Taken.Kept)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte(output))
	var receipt, bytes bool
	// Fetch the whole cell: the existing fetchInto helper intentionally returns
	// only workspace files, while receipts and output live in the composed state.
	h.awayFromA()
	eng := h.engB
	eng.DataRoot = t.TempDir()
	root := filepath.Join(t.TempDir(), h.cell.ID)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	fetcher := h.b.Continuer(eng, TakeOptions{RootFor: func(string) string { return root }})
	if err := fetcher.taker(t.Context(), h.b.scope(h.cell.ID)).Fetch.Fetch(t.Context(), cell.Cell{ID: h.cell.ID, Root: root}, branch.Cell.Head); err != nil {
		t.Fatal(err)
	}
	for path, file := range readTree(t, root) {
		bytes = bytes || file.body == output
		if strings.HasPrefix(filepath.ToSlash(path), cellstore.ReceiptsDir+"/") {
			var saved cellstore.Receipt
			if json.Unmarshal([]byte(file.body), &saved) == nil {
				for _, call := range saved.Calls {
					receipt = receipt || (call.Tool == "external" && call.SideEffect == string(executor.EffectExternal) && call.StdoutHash == hex.EncodeToString(want[:]))
				}
			}
		}
	}
	if !receipt || !bytes {
		t.Fatalf("branch omitted external receipt/output: receipt=%v output=%v", receipt, bytes)
	}
	_, recovery, err := cellstore.OpenWAL(h.engine.WALPath(h.cell))
	if err != nil || len(recovery.Completed) != 0 {
		t.Fatalf("branched completion still pending: %+v %v", recovery, err)
	}
}
