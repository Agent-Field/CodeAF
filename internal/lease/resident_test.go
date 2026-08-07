package lease

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireResidentAndRelease(t *testing.T) {
	dir := t.TempDir()
	release, heldBy, err := AcquireResident(dir, "chat")
	if err != nil || release == nil || heldBy != nil {
		t.Fatalf("acquire = release %v, held %+v, err %v", release != nil, heldBy, err)
	}
	holder, err := ProbeResident(dir)
	if err != nil || holder == nil {
		t.Fatalf("probe held lease = %+v, %v", holder, err)
	}
	if holder.PID != os.Getpid() || holder.Surface != "chat" || holder.AcquiredAt.IsZero() {
		t.Fatalf("holder = %+v", holder)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if holder, err := ProbeResident(dir); err != nil || holder != nil {
		t.Fatalf("probe released lease = %+v, %v", holder, err)
	}
}

func TestAcquireResidentReportsConflict(t *testing.T) {
	dir := t.TempDir()
	release, heldBy, err := AcquireResident(dir, "chat")
	if err != nil || release == nil || heldBy != nil {
		t.Fatalf("first acquire = release %v, held %+v, err %v", release != nil, heldBy, err)
	}
	defer release()

	secondRelease, holder, err := AcquireResident(dir, "wake")
	if err != nil {
		t.Fatal(err)
	}
	if secondRelease != nil || holder == nil || holder.PID != os.Getpid() || holder.Surface != "chat" {
		t.Fatalf("conflict = release %v, holder %+v", secondRelease != nil, holder)
	}
}

func TestProbeResidentIgnoresStalePayload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, residentLockName)
	stale := []byte(`{"pid":999999,"host":"gone","surface":"chat","acquired_at":"2020-01-01T00:00:00Z"}`)
	if err := os.WriteFile(path, stale, 0o600); err != nil {
		t.Fatal(err)
	}
	if holder, err := ProbeResident(dir); err != nil || holder != nil {
		t.Fatalf("stale probe = %+v, %v", holder, err)
	}
	release, holder, err := AcquireResident(dir, "wake")
	if err != nil || release == nil || holder != nil {
		t.Fatalf("acquire over stale payload = release %v, held %+v, err %v", release != nil, holder, err)
	}
	defer release()
	live, err := ProbeResident(dir)
	if err != nil || live == nil || live.PID != os.Getpid() || live.Surface != "wake" {
		t.Fatalf("rewritten holder = %+v, %v", live, err)
	}
}

func TestProbeReportsAHolderThatStoppedTicking(t *testing.T) {
	dir := t.TempDir()
	release, heldBy, err := AcquireResident(dir, "chat")
	if err != nil || release == nil || heldBy != nil {
		t.Fatalf("acquire = release %v, held %+v, err %v", release != nil, heldBy, err)
	}
	defer release()

	// Silence is not evidence of death: a holder that has never stamped a pass
	// is unknown, not stuck, and must never be taken from.
	holder, err := ProbeResident(dir)
	if err != nil || holder == nil || holder.Stuck || !holder.LastTick.IsZero() {
		t.Fatalf("holder before any tick = %+v, %v", holder, err)
	}

	if err := NoteResidentTick(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	holder, err = ProbeResident(dir)
	if err != nil || holder == nil || holder.Stuck || holder.LastTick.IsZero() {
		t.Fatalf("holder after a fresh tick = %+v, %v", holder, err)
	}
	if holder.PID != os.Getpid() || holder.Surface != "chat" || holder.AcquiredAt.IsZero() {
		t.Fatalf("stamping a tick lost the holder's identity: %+v", holder)
	}

	if err := NoteResidentTick(dir, time.Now().Add(-2*StuckAfter)); err != nil {
		t.Fatal(err)
	}
	holder, err = ProbeResident(dir)
	if err != nil || holder == nil || !holder.Stuck {
		t.Fatalf("holder that stopped ticking = %+v, %v", holder, err)
	}
	if _, conflict, err := AcquireResident(dir, "wake"); err != nil || conflict == nil || !conflict.Stuck {
		t.Fatalf("conflict report = %+v, %v", conflict, err)
	}
}
