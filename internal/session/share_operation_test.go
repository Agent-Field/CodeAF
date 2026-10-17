package session

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

func TestShareOperationAdmitWritesPendingReceiptWithPlannedIDs(t *testing.T) {
	root := t.TempDir()
	prov := ShareProvenance{Run: "run-a", Turn: "turn-a", Call: "call-a"}
	claim, err := AdmitShareOperation(root, "sess-1", "op-fresh-1", []string{"/w/report.md", "/w/chart.png"}, prov, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	defer claim.Release()
	if claim.Outcome != ShareClaimAdmitted {
		t.Fatalf("outcome=%q, want admitted", claim.Outcome)
	}
	if claim.Receipt.Status != ShareOpPending {
		t.Fatalf("status=%q, want pending", claim.Receipt.Status)
	}
	if len(claim.Receipt.Planned) != 2 {
		t.Fatalf("planned=%d, want 2", len(claim.Receipt.Planned))
	}
	for _, row := range claim.Receipt.Planned {
		if !ValidDeliverableID(row.ID) {
			t.Fatalf("planned id %q is not a deliverable id", row.ID)
		}
	}
	if claim.Receipt.Provenance.Call != "call-a" || claim.Receipt.Provenance.Run != "run-a" {
		t.Fatalf("provenance not stored: %+v", claim.Receipt.Provenance)
	}
	loaded, err := LoadShareReceipt(root, "sess-1", "op-fresh-1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.PayloadDigest != claim.Receipt.PayloadDigest {
		t.Fatalf("loaded digest %q != claimed %q", loaded.PayloadDigest, claim.Receipt.PayloadDigest)
	}
	if loaded.Planned[0].ID != claim.Receipt.Planned[0].ID {
		t.Fatalf("planned ids not durable: loaded %q claimed %q", loaded.Planned[0].ID, claim.Receipt.Planned[0].ID)
	}
}

func TestShareOperationCrashLeavesPendingAndSamePayloadReplaysOriginal(t *testing.T) {
	root := t.TempDir()
	first, err := AdmitShareOperation(root, "sess-1", "op-crash", []string{"/w/a.md"}, ShareProvenance{Run: "run-1", Call: "call-1"}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("first admit: %v", err)
	}
	plannedID := first.Receipt.Planned[0].ID
	digest := first.Receipt.PayloadDigest
	// Crash: drop the lock without settling. Pending receipt must remain.
	first.Release()

	loaded, err := LoadShareReceipt(root, "sess-1", "op-crash")
	if err != nil {
		t.Fatalf("load after crash: %v", err)
	}
	if loaded.Status != ShareOpPending {
		t.Fatalf("status after crash=%q, want pending", loaded.Status)
	}
	if loaded.Planned[0].ID != plannedID {
		t.Fatalf("planned id changed across crash")
	}

	// A later caller with a different provider call id must still see the
	// ORIGINAL provenance and planned ids — never re-attribute.
	second, err := AdmitShareOperation(root, "sess-1", "op-crash", []string{"/w/a.md"}, ShareProvenance{Run: "run-2", Call: "call-2"}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("replay admit: %v", err)
	}
	defer second.Release()
	if second.Outcome != ShareClaimReplay {
		t.Fatalf("outcome=%q, want replay", second.Outcome)
	}
	if second.Receipt.Provenance.Call != "call-1" || second.Receipt.Provenance.Run != "run-1" {
		t.Fatalf("replay rewrote provenance to latest caller: %+v", second.Receipt.Provenance)
	}
	if second.Receipt.Planned[0].ID != plannedID || second.Receipt.PayloadDigest != digest {
		t.Fatalf("replay changed planned id or digest")
	}
}

func TestShareOperationChangedPayloadConflictsWithoutRewrite(t *testing.T) {
	root := t.TempDir()
	first, err := AdmitShareOperation(root, "sess-1", "op-conflict", []string{"/w/a.md"}, ShareProvenance{Call: "call-1"}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("first admit: %v", err)
	}
	plannedID := first.Receipt.Planned[0].ID
	planShareForTest(t, first)
	if err := first.Settle(ShareOpAccepted, []string{plannedID}); err != nil {
		t.Fatalf("settle: %v", err)
	}

	second, err := AdmitShareOperation(root, "sess-1", "op-conflict", []string{"/w/b.md"}, ShareProvenance{Call: "call-2"}, time.Now().Add(time.Second))
	if !errors.Is(err, ErrShareOpConflict) {
		t.Fatalf("err=%v, want conflict", err)
	}
	if second == nil || second.Outcome != ShareClaimConflict {
		t.Fatalf("outcome=%v, want conflict", second)
	}
	loaded, err := LoadShareReceipt(root, "sess-1", "op-conflict")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Status != ShareOpAccepted || loaded.Planned[0].ID != plannedID || loaded.Planned[0].Path != "/w/a.md" {
		t.Fatalf("conflict rewrote the original receipt: %+v", loaded)
	}
	if loaded.Provenance.Call != "call-1" {
		t.Fatalf("conflict rewrote provenance: %+v", loaded.Provenance)
	}
}

func TestShareOperationHeldLockReportsBusy(t *testing.T) {
	root := t.TempDir()
	first, err := AdmitShareOperation(root, "sess-1", "op-busy", []string{"/w/a.md"}, ShareProvenance{}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("first admit: %v", err)
	}
	defer first.Release()

	second, err := AdmitShareOperation(root, "sess-1", "op-busy", []string{"/w/a.md"}, ShareProvenance{}, time.Now().Add(time.Second))
	if !errors.Is(err, ErrShareOpBusy) {
		t.Fatalf("err=%v, want busy", err)
	}
	if second == nil || second.Outcome != ShareClaimBusy {
		t.Fatalf("outcome=%v, want busy", second)
	}
}

func TestShareOperationProcessSlotsRefuseWithoutQueue(t *testing.T) {
	root := t.TempDir()
	held := make([]*ShareClaim, 0, shareOpSlotCap)
	for i := 0; i < shareOpSlotCap; i++ {
		op := "op-slot-" + string(rune('a'+i))
		claim, err := AdmitShareOperation(root, "sess-1", op, []string{"/w/f.md"}, ShareProvenance{}, time.Now().Add(time.Second))
		if err != nil {
			t.Fatalf("admit %d: %v", i, err)
		}
		held = append(held, claim)
	}
	defer func() {
		for _, c := range held {
			c.Release()
		}
	}()

	extra, err := AdmitShareOperation(root, "sess-1", "op-slot-extra", []string{"/w/f.md"}, ShareProvenance{}, time.Now().Add(time.Second))
	if !errors.Is(err, ErrShareOpBusy) {
		t.Fatalf("err=%v, want busy (no queue)", err)
	}
	if extra == nil || extra.Outcome != ShareClaimBusy {
		t.Fatalf("outcome=%v, want busy", extra)
	}
}

func TestShareOperationLockFileIsSeparateFromReceipt(t *testing.T) {
	root := t.TempDir()
	op := "op-paths"
	lockPath, receiptPath := ShareOpPaths(root, "sess-1", op)
	if lockPath == receiptPath {
		t.Fatal("lock and receipt must be distinct paths")
	}
	if filepath.Base(lockPath) != shareOpLockName || filepath.Base(receiptPath) != shareOpReceipt {
		t.Fatalf("unexpected names: lock=%q receipt=%q", lockPath, receiptPath)
	}
	claim, err := AdmitShareOperation(root, "sess-1", op, []string{"/w/a.md"}, ShareProvenance{}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	defer claim.Release()
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock missing: %v", err)
	}
	if _, err := os.Stat(receiptPath); err != nil {
		t.Fatalf("receipt missing: %v", err)
	}
	// The lock inode must still be flockable as busy while the claim holds it,
	// proving the receipt rename did not replace the locked file.
	probe, err := os.OpenFile(lockPath, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open lock: %v", err)
	}
	defer probe.Close()
	if err := filelock.Lock(probe, true, true); !filelock.IsBusy(err) {
		t.Fatalf("expected busy on stable lock file, got %v", err)
	}
}

func TestShareOperationInvalidOperationID(t *testing.T) {
	cases := []string{"", " leading", "trailing ", "a\nb", string(make([]byte, shareOpIDMax+1))}
	for _, id := range cases {
		if err := ValidateShareOperationID(id); !errors.Is(err, ErrShareOpInvalid) {
			t.Fatalf("id %q: err=%v, want invalid", id, err)
		}
	}
	if err := ValidateShareOperationID("ok-op-1"); err != nil {
		t.Fatalf("valid id refused: %v", err)
	}
}

func TestShareOperationSettleAcceptedPreservesPlannedIDs(t *testing.T) {
	root := t.TempDir()
	claim, err := AdmitShareOperation(root, "sess-1", "op-settle", []string{"/w/a.md", "/w/b.md"}, ShareProvenance{Call: "c1"}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	ids := []string{claim.Receipt.Planned[0].ID, claim.Receipt.Planned[1].ID}
	planShareForTest(t, claim)
	if err := claim.Settle(ShareOpAccepted, ids); err != nil {
		t.Fatalf("settle: %v", err)
	}
	loaded, err := LoadShareReceipt(root, "sess-1", "op-settle")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Status != ShareOpAccepted {
		t.Fatalf("status=%q", loaded.Status)
	}
	if loaded.Planned[0].ID != ids[0] || loaded.Planned[1].ID != ids[1] {
		t.Fatalf("settle rewrote planned ids")
	}
	if len(loaded.RecordedIDs) != 2 || loaded.RecordedIDs[0] != ids[0] {
		t.Fatalf("recorded ids=%v", loaded.RecordedIDs)
	}

	replay, err := AdmitShareOperation(root, "sess-1", "op-settle", []string{"/w/a.md", "/w/b.md"}, ShareProvenance{Call: "later"}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	defer replay.Release()
	if replay.Outcome != ShareClaimReplay || replay.Receipt.Provenance.Call != "c1" {
		t.Fatalf("settled replay lost original: outcome=%q prov=%+v", replay.Outcome, replay.Receipt.Provenance)
	}
	if err := replay.Settle(ShareOpAccepted, []string{"should-not-write"}); err != nil {
		t.Fatalf("replay settle: %v", err)
	}
	again, err := LoadShareReceipt(root, "sess-1", "op-settle")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(again.RecordedIDs) != 2 || again.RecordedIDs[0] != ids[0] {
		t.Fatalf("replay settle rewrote recorded ids: %v", again.RecordedIDs)
	}
}
