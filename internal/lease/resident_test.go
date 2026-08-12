package lease

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestAcquireResidentAndRelease(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "graph.db")
	release, heldBy, err := AcquireResident(store, "chat")
	if err != nil || release == nil || heldBy != nil {
		t.Fatalf("acquire = release %v, held %+v, err %v", release != nil, heldBy, err)
	}
	holder, err := ProbeResident(store)
	if err != nil || holder == nil {
		t.Fatalf("probe held lease = %+v, %v", holder, err)
	}
	if holder.PID != os.Getpid() || holder.Surface != "chat" || holder.AcquiredAt.IsZero() {
		t.Fatalf("holder = %+v", holder)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if holder, err := ProbeResident(store); err != nil || holder != nil {
		t.Fatalf("probe released lease = %+v, %v", holder, err)
	}
}

func TestAcquireResidentReportsConflict(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "graph.db")
	release, heldBy, err := AcquireResident(store, "chat")
	if err != nil || release == nil || heldBy != nil {
		t.Fatalf("first acquire = release %v, held %+v, err %v", release != nil, heldBy, err)
	}
	defer release()

	secondRelease, holder, err := AcquireResident(store, "wake")
	if err != nil {
		t.Fatal(err)
	}
	if secondRelease != nil || holder == nil || holder.PID != os.Getpid() || holder.Surface != "chat" {
		t.Fatalf("conflict = release %v, holder %+v", secondRelease != nil, holder)
	}
}

func TestProbeResidentIgnoresStalePayload(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "graph.db")
	path, err := LockPath(store)
	if err != nil {
		t.Fatal(err)
	}
	stale := []byte(`{"pid":999999,"host":"gone","surface":"chat","acquired_at":"2020-01-01T00:00:00Z"}`)
	if err := os.WriteFile(path, stale, 0o600); err != nil {
		t.Fatal(err)
	}
	if holder, err := ProbeResident(store); err != nil || holder != nil {
		t.Fatalf("stale probe = %+v, %v", holder, err)
	}
	release, holder, err := AcquireResident(store, "wake")
	if err != nil || release == nil || holder != nil {
		t.Fatalf("acquire over stale payload = release %v, held %+v, err %v", release != nil, holder, err)
	}
	defer release()
	live, err := ProbeResident(store)
	if err != nil || live == nil || live.PID != os.Getpid() || live.Surface != "wake" {
		t.Fatalf("rewritten holder = %+v, %v", live, err)
	}
}

func TestProbeReportsAHolderThatStoppedTicking(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "graph.db")
	release, heldBy, err := AcquireResident(store, "chat")
	if err != nil || release == nil || heldBy != nil {
		t.Fatalf("acquire = release %v, held %+v, err %v", release != nil, heldBy, err)
	}
	defer release()

	// Silence is not evidence of death: a holder that has never stamped a pass
	// is unknown, not stuck, and must never be taken from.
	holder, err := ProbeResident(store)
	if err != nil || holder == nil || holder.Stuck || !holder.LastTick.IsZero() {
		t.Fatalf("holder before any tick = %+v, %v", holder, err)
	}

	if err := NoteResidentTick(store, time.Now()); err != nil {
		t.Fatal(err)
	}
	holder, err = ProbeResident(store)
	if err != nil || holder == nil || holder.Stuck || holder.LastTick.IsZero() {
		t.Fatalf("holder after a fresh tick = %+v, %v", holder, err)
	}
	if holder.PID != os.Getpid() || holder.Surface != "chat" || holder.AcquiredAt.IsZero() {
		t.Fatalf("stamping a tick lost the holder's identity: %+v", holder)
	}

	if err := NoteResidentTick(store, time.Now().Add(-2*StuckAfter)); err != nil {
		t.Fatal(err)
	}
	holder, err = ProbeResident(store)
	if err != nil || holder == nil || !holder.Stuck {
		t.Fatalf("holder that stopped ticking = %+v, %v", holder, err)
	}
	if _, conflict, err := AcquireResident(store, "wake"); err != nil || conflict == nil || !conflict.Stuck {
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
	store := filepath.Join(dir, "graph.db")
	release, _, err := AcquireResident(store, "chat")
	if err != nil || release == nil {
		t.Fatalf("acquire: %v", err)
	}
	defer release()

	holder, err := ProbeResident(store)
	if err != nil || holder == nil {
		t.Fatalf("probe: %v %v", holder, err)
	}
	if holder.Build != LocalBuild() {
		t.Fatalf("the lock does not name this binary: %+v", holder.Build)
	}
	// A heartbeat rewrites the payload and must not lose it.
	if err := NoteResidentTick(store, time.Now()); err != nil {
		t.Fatal(err)
	}
	stamped, err := ProbeResident(store)
	if err != nil || stamped == nil {
		t.Fatalf("probe after tick: %v %v", stamped, err)
	}
	if stamped.Build != holder.Build {
		t.Fatalf("a heartbeat dropped the build stamp: %+v", stamped.Build)
	}
}

// A holder rewrites the lock's payload without a lock of its own — it is the
// holder, so nothing else may write — and the moments after a role changes
// hands are exactly when several processes are reading it. A reader that landed
// inside a write used to come back with "unexpected end of JSON input", which a
// window waiting to promote read as a broken lock and gave up on.
func TestReadingALockThatIsBeingRewrittenNeverReportsItBroken(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "graph.db")
	release, _, err := AcquireResident(store, "chat")
	if err != nil || release == nil {
		t.Fatalf("acquire: %v", err)
	}
	defer release()

	stop := make(chan struct{})
	writing := make(chan struct{})
	go func() {
		defer close(writing)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			// The heartbeat is the rewrite, and it is the one the resident makes
			// on every completed pass.
			if err := NoteResidentTick(store, time.Now().Add(time.Duration(i)*time.Second)); err != nil {
				t.Errorf("tick: %v", err)
				return
			}
		}
	}()

	deadline := time.Now().Add(2 * time.Second)
	reads := 0
	for time.Now().Before(deadline) {
		holder, err := ProbeResident(store)
		if err != nil {
			close(stop)
			<-writing
			t.Fatalf("a lock being rewritten read as broken: %v", err)
		}
		if holder == nil || holder.PID <= 0 {
			close(stop)
			<-writing
			t.Fatalf("a held lock read as free: %+v", holder)
		}
		reads++
	}
	close(stop)
	<-writing
	if reads < 100 {
		t.Fatalf("only %d reads landed; the race was never exercised", reads)
	}
}

// The bug this key change exists for: two stores that share a directory are two
// stores. A directory-wide lock elected one resident for both, so the second
// process sat watching a journal nobody was serving — 25-40 minutes of wall at
// nodes:0 and $0.00 on a benchmark grid, with nothing on any stream naming the
// lock it was losing to.
func TestTwoStoresInOneDirectoryEachGetTheirOwnResident(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "one.db")
	second := filepath.Join(dir, "two.db")

	releaseFirst, heldBy, err := AcquireResident(first, "do")
	if err != nil || releaseFirst == nil || heldBy != nil {
		t.Fatalf("first acquire = release %v, held %+v, err %v", releaseFirst != nil, heldBy, err)
	}
	defer releaseFirst()

	releaseSecond, heldBy, err := AcquireResident(second, "do")
	if err != nil || releaseSecond == nil || heldBy != nil {
		t.Fatalf("a sibling store could not get its own resident: release %v, held %+v, err %v",
			releaseSecond != nil, heldBy, err)
	}
	defer releaseSecond()

	// And the same store is still one resident, which is the whole point of the
	// lock: the key moved, the exclusion did not.
	again, holder, err := AcquireResident(first, "wake")
	if err != nil || again != nil || holder == nil {
		t.Fatalf("the same store elected two residents: release %v, holder %+v, err %v", again != nil, holder, err)
	}
	if holder.Store != absoluteStore(first) {
		t.Fatalf("the lock does not name the store it guards: %q", holder.Store)
	}
}

// A binary from before the key changed holds the directory-wide lock. It may be
// serving this very store, and two brains over one journal is the one outcome
// this lease exists to prevent — so the old process keeps the role until it
// exits, exactly as it did before.
func TestALiveLegacyHolderStillOwnsTheRole(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "graph.db")
	legacy, err := os.OpenFile(filepath.Join(dir, legacyLockName), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	if err := syscall.Flock(int(legacy.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if err := writeResident(legacy, Resident{PID: os.Getpid(), Host: "here", Surface: "chat"}); err != nil {
		t.Fatal(err)
	}

	release, holder, err := AcquireResident(store, "do")
	if err != nil || release != nil || holder == nil {
		t.Fatalf("acquire beside a live legacy holder = release %v, holder %+v, err %v", release != nil, holder, err)
	}
	probed, err := ProbeResident(store)
	if err != nil || probed == nil || probed.PID != os.Getpid() {
		t.Fatalf("probe beside a live legacy holder = %+v, %v", probed, err)
	}

	// Once it lets go, the role is free and store-keyed like everything else.
	if err := syscall.Flock(int(legacy.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	release, holder, err = AcquireResident(store, "do")
	if err != nil || release == nil || holder != nil {
		t.Fatalf("acquire after the legacy holder exited = release %v, holder %+v, err %v", release != nil, holder, err)
	}
	_ = release()
}
