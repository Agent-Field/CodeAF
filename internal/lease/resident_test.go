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

// The stamp is what makes a handover possible at all, and its ordering has one
// rule that matters more than being right about which build is newer: it must
// never claim to be newer than a holder that said nothing. Every binary from
// before this existed is exactly that holder.
func TestABuildNeverClaimsToBeNewerThanSilence(t *testing.T) {
	now := time.Now().UTC()
	newer := Build{ModTime: now, Size: 20, Revision: "b"}
	older := Build{ModTime: now.Add(-time.Hour), Size: 10, Revision: "a"}

	if !newer.NewerThan(older) {
		t.Fatal("a later mtime did not order as newer")
	}
	if older.NewerThan(newer) {
		t.Fatal("an earlier mtime ordered as newer")
	}
	if newer.NewerThan(Build{}) {
		t.Fatal("a stamped build claimed to outrank a holder that never said")
	}
	if (Build{}).NewerThan(newer) {
		t.Fatal("an unstamped build claimed to outrank a stamped one")
	}
	if newer.NewerThan(newer) {
		t.Fatal("a build outranked itself")
	}
	// A same-second rebuild is evidence of difference only when the size and
	// the revision both disagree; a mere size difference is not a direction.
	sameSecond := Build{ModTime: now, Size: 21, Revision: "b"}
	if sameSecond.NewerThan(newer) {
		t.Fatal("a same-second build of the same revision claimed to be newer")
	}
	rebuilt := Build{ModTime: now, Size: 21, Revision: "c"}
	if !rebuilt.NewerThan(newer) {
		t.Fatal("a same-second build of a different revision did not order as newer")
	}
}

// The lock carries the stamp, so a visitor can compare without asking the
// holder anything.
func TestTheLockCarriesTheHoldersBuild(t *testing.T) {
	dir := t.TempDir()
	release, _, err := AcquireResident(dir, "chat")
	if err != nil || release == nil {
		t.Fatalf("acquire: %v", err)
	}
	defer release()

	holder, err := ProbeResident(dir)
	if err != nil || holder == nil {
		t.Fatalf("probe: %v %v", holder, err)
	}
	if holder.Build != LocalBuild() {
		t.Fatalf("the lock does not name this binary: %+v", holder.Build)
	}
	// A heartbeat rewrites the payload and must not lose it.
	if err := NoteResidentTick(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	stamped, err := ProbeResident(dir)
	if err != nil || stamped == nil {
		t.Fatalf("probe after tick: %v %v", stamped, err)
	}
	if stamped.Build != holder.Build {
		t.Fatalf("a heartbeat dropped the build stamp: %+v", stamped.Build)
	}
}
