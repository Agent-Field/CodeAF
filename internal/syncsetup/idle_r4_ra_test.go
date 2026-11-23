package syncsetup

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
)

// Home waits for installation before reading ownership for its first send.
// A taken-back writer may idle-release before that send; its installed status
// must survive release without granting any authority at the released fence.
func TestIdleRelease_R4_RA_TakenBackReleasedWriterRemainsInstalled(t *testing.T) {
	h := newTwoHomes(t)
	a := idleContractChat(t, h)
	first := idleContractReleased(t, h, a.drive)
	expect := func(v directory.CellView) *directory.LeaseExpectation {
		return &directory.LeaseExpectation{Device: v.Cell.Lease.Device, Fence: v.Cell.Lease.Fence}
	}
	got, err := h.continuerB().OpenAvailable(t.Context(), h.cell.ID, expect(first))
	mustNil(t, err)
	b := h.openOn(h.b, h.engB, got.Taken.Cell, workspaceOf(got.Taken.Cell.Root), nameB)
	release := func(d *Drive) directory.CellView {
		t.Helper()
		d.Idle()
		var view directory.CellView
		waitFor(t, "this writer's explicit idle release", func() bool {
			var err error
			view, err = h.a.Dir.Cell(t.Context(), h.cell.ID)
			return err == nil && d.IdleReleased() && view.Cell.Lease.Expires == 0 && view.Cell.Lease.Fence == d.batcher.Fence()
		})
		return view
	}
	second := release(b.drive)
	back, err := h.continuerA().OpenAvailable(t.Context(), h.cell.ID, expect(second))
	mustNil(t, err)
	current := h.openOn(h.a, h.engine, back.Taken.Cell, workspaceOf(back.Taken.Cell.Root), nameA)
	seen := release(current.drive)
	before, err := os.ReadFile(transcriptOf(back.Taken.Cell))
	mustNil(t, err)
	_, period, holding := current.drive.RenewalStatus()
	if period != directory.HeartbeatEvery || holding {
		t.Fatalf("taken-back writer looks uninstalled after idle release: period=%s holding=%v", period, holding)
	}
	if current.drive.Gate() == nil {
		t.Fatal("installed status revived work permission at the released fence")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	confirmed, err := current.drive.ConfirmWork(ctx)
	if err != nil || !confirmed || current.drive.Gate() != nil {
		t.Fatalf("first send could not take back the installed writer: confirmed=%v error=%v gate=%v", confirmed, err, current.drive.Gate())
	}
	view, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	if view.Cell.Lease.Fence <= seen.Cell.Lease.Fence {
		t.Fatal("taking back the installed writer revived its released fence")
	}
	after, err := os.ReadFile(transcriptOf(back.Taken.Cell))
	mustNil(t, err)
	if string(after) != string(before) || !strings.Contains(string(after), "save this chat") {
		t.Fatal("writer readiness or take-back changed the saved conversation")
	}
	mustNil(t, b.drive.CheckLease(ctx))
	_, period, holding = b.drive.RenewalStatus()
	if period != 0 || holding {
		t.Fatal("the displaced other computer reported itself installed or holding again")
	}
	_, _, holding = a.drive.RenewalStatus()
	if holding || a.drive.Gate() == nil {
		t.Fatal("the original window revived authority at its released fence")
	}
}
