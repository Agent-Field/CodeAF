package main

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/lease"
	"github.com/Agent-Field/codeaf/internal/store"
)

// An errand that cannot be the brain says who has the role and stops.
//
// The silent version of this cost two full benchmark grids: `do` runs whose
// store was already spoken for posted their command, watched a journal nobody
// was serving, and burned 25-40 minutes each at nodes:0, spend $0.00, with one
// repeated progress line and nothing anywhere naming the lock. The wait is now
// bounded and the diagnostic names the holder, the store, and the way out.
func TestDoFailsFastWhenAnotherProcessHoldsTheResidentLock(t *testing.T) {
	// The wait under test is the default behavior; a shell that exports
	// CODEAF_NO_RESIDENT would turn it into a refusal, so pin it off.
	t.Setenv("CODEAF_NO_RESIDENT", "")
	dir := t.TempDir()
	path := filepath.Join(dir, "graph.db")
	release, heldBy, err := lease.AcquireResident(path, "chat")
	if err != nil || release == nil || heldBy != nil {
		t.Fatalf("could not stand in for the resident: release %v, held %+v, err %v", release != nil, heldBy, err)
	}
	defer release()

	var stdout, stderr strings.Builder
	started := time.Now()
	err = doErrand(doRequest{
		task:     "write the release note",
		database: path,
		// Every seat is named, so the crew needs no router to reach the lock.
		model: "test/model", planModel: "test/model", checkModel: "test/model",
		// The wall is long and the bound is short on purpose: the thing under
		// test is that the run leaves on the bound rather than on the wall.
		timeout:      5 * time.Minute,
		residentWait: 300 * time.Millisecond,
		stdout:       &stdout,
		stderr:       &stderr,
	})
	elapsed := time.Since(started)
	if err == nil {
		t.Fatalf("the errand deferred to a resident that was never coming and said it worked\nstderr:\n%s", stderr.String())
	}
	if elapsed > 30*time.Second {
		t.Fatalf("the errand waited %s — it burned the wall instead of the bound", elapsed)
	}
	// The diagnostic has to carry the three things a person needs: who holds the
	// role, which store it is held for, and what to do about it.
	for _, want := range []string{strconv.Itoa(lockedPID(t, path)), path, "--db"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the diagnostic does not name %q: %v", want, err)
		}
	}
	if !strings.Contains(stderr.String(), "waiting for the resident (pid") {
		t.Fatalf("the wait was silent:\n%s", stderr.String())
	}
}

// lockedPID reads the pid off the lock the test itself is holding, so the
// assertion above quotes the journal's own answer rather than assuming it.
func lockedPID(t *testing.T, path string) int {
	t.Helper()
	holder, err := lease.ProbeResident(path)
	if err != nil || holder == nil {
		t.Fatalf("probe the lock this test holds = %+v, %v", holder, err)
	}
	return holder.PID
}

// --no-resident is the hard isolation a script asks for: with a live resident
// holding the store, the run refuses at the door instead of handing the brief
// over. The refusal is the point — a resident that picks the command up
// compiles it conversationally, which is the one thing a harness cannot
// measure — so the run must stop fast, name the holder and the switch, and
// leave the journal exactly as it found it: no command for the resident to
// apply, because a run this process refused is not a run anybody may do.
func TestDoNoResidentRefusesWhileAResidentHoldsTheLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "graph.db")
	release, _, err := lease.AcquireResident(path, "chat")
	if err != nil || release == nil {
		t.Fatalf("could not stand in for the resident: release %v, err %v", release != nil, err)
	}
	defer release()

	var stdout, stderr strings.Builder
	started := time.Now()
	err = doErrand(doRequest{
		task:       "write the release note",
		database:   path,
		noResident: true,
		// Every seat is named, so the crew needs no router to reach the lock.
		model: "test/model", planModel: "test/model", checkModel: "test/model",
		timeout: 5 * time.Minute,
		stdout:  &stdout,
		stderr:  &stderr,
	})
	if err == nil {
		t.Fatalf("--no-resident deferred to the live resident instead of refusing\nstderr:\n%s", stderr.String())
	}
	if elapsed := time.Since(started); elapsed > 30*time.Second {
		t.Fatalf("the refusal waited %s — it must stop at the door, not at the bound", elapsed)
	}
	// The diagnostic carries what the refusal is about: who holds the role,
	// which store it is held for, and the switch that refused it.
	for _, want := range []string{strconv.Itoa(lockedPID(t, path)), path, "--no-resident"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not name %q: %v", want, err)
		}
	}
	// NOTHING WAS HANDED OVER: the journal holds no command, so no resident can
	// apply a run this process refused. This is what makes the refusal a
	// refusal and not a slower hand-off.
	graph, openErr := store.Open(path)
	if openErr != nil {
		t.Fatalf("open the store to read the journal: %v", openErr)
	}
	defer graph.Close()
	if pending, err := graph.PendingCommands(10); err != nil || len(pending) != 0 {
		t.Fatalf("the refused run left %d command(s) in the journal (err %v) — nothing may be handed over", len(pending), err)
	}
}

// CODEAF_NO_RESIDENT=1 is the same switch from the environment, for a wrapper
// that cannot change a call site.
func TestCodeafNoResidentEnvRefusesWhileAResidentHoldsTheLock(t *testing.T) {
	t.Setenv("CODEAF_NO_RESIDENT", "1")
	dir := t.TempDir()
	path := filepath.Join(dir, "graph.db")
	release, _, err := lease.AcquireResident(path, "chat")
	if err != nil || release == nil {
		t.Fatalf("could not stand in for the resident: release %v, err %v", release != nil, err)
	}
	defer release()

	var stdout, stderr strings.Builder
	err = doErrand(doRequest{
		task:     "write the release note",
		database: path,
		model:    "test/model", planModel: "test/model", checkModel: "test/model",
		timeout: 5 * time.Minute,
		stdout:  &stdout,
		stderr:  &stderr,
	})
	if err == nil {
		t.Fatalf("CODEAF_NO_RESIDENT=1 deferred to the live resident instead of refusing\nstderr:\n%s", stderr.String())
	}
	if !strings.Contains(err.Error(), "--no-resident") {
		t.Fatalf("the refusal does not name the switch: %v", err)
	}
}

// A --json caller gets the same refusal through the envelope it reads every
// other ending through: one object, ok false, stop "error", and the sentence
// in the error field — never empty stdout with the reason buried on stderr.
func TestDoNoResidentRefusalReachesTheJSONEnvelope(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "graph.db")
	release, _, err := lease.AcquireResident(path, "chat")
	if err != nil || release == nil {
		t.Fatalf("could not stand in for the resident: release %v, err %v", release != nil, err)
	}
	defer release()

	var stdout, stderr strings.Builder
	err = doErrand(doRequest{
		task:       "write the release note",
		database:   path,
		noResident: true,
		asJSON:     true,
		model:      "test/model", planModel: "test/model", checkModel: "test/model",
		timeout: 5 * time.Minute,
		stdout:  &stdout,
		stderr:  &stderr,
	})
	if err == nil {
		t.Fatalf("--json --no-resident reported success instead of refusing")
	}
	fields := errandJSONFields(t, stdout.String())
	if got, _ := fields["ok"].(bool); got {
		t.Fatalf("the refusal envelope says ok: true\n%s", stdout.String())
	}
	if got, _ := fields["stop"].(string); got != "error" {
		t.Fatalf("stop = %q, want %q\n%s", got, "error", stdout.String())
	}
	if got, _ := fields["error"].(string); !strings.Contains(got, "--no-resident") {
		t.Fatalf("the envelope's error does not name the switch: %q\n%s", got, stdout.String())
	}
}

// The whole door, as the issue names it: `do --no-resident` on a box with a
// live resident. Driven through runDo so the argv-to-request wiring is under
// test — the flag string has to reach the refusal, not just the request field
// a test fills in by hand.
func TestDoNoResidentFlagFromTheCommandLineRefuses(t *testing.T) {
	// The switch under test is the flag, so a shell that exports the env
	// spelling must not be the thing refusing.
	t.Setenv("CODEAF_NO_RESIDENT", "")
	dir := t.TempDir()
	path := filepath.Join(dir, "graph.db")
	release, _, err := lease.AcquireResident(path, "chat")
	if err != nil || release == nil {
		t.Fatalf("could not stand in for the resident: release %v, err %v", release != nil, err)
	}
	defer release()

	stdout, _, runErr := captureDoRefusal(t, []string{
		"--no-resident", "--db", path,
		"--model", "test/model", "--plan-model", "test/model", "--check-model", "test/model",
		"write the release note",
	})
	if exitCodeOf(runErr) != 1 {
		t.Fatalf("exit = %d, want 1 (%v)", exitCodeOf(runErr), runErr)
	}
	// main is the one that prints the `error:` line for a plain door error;
	// what runDo hands back is the sentence itself, and it has to name the
	// holder and the switch.
	if !strings.Contains(runErr.Error(), "the resident lock") || !strings.Contains(runErr.Error(), "--no-resident") {
		t.Fatalf("the refusal does not name the switch and the holder: %v", runErr)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty — a refused run writes nothing to the machine stream", stdout)
	}
}

// The other half of the same failure: a holder that is serving a different
// database is not slow, it is absent. There is nothing to wait for, and the run
// says so immediately rather than spending its bound finding out.
func TestAResidentServingAnotherStoreIsRefusedImmediately(t *testing.T) {
	dir := t.TempDir()
	mine := filepath.Join(dir, "mine.db")
	theirs := filepath.Join(dir, "theirs.db")
	holder := &lease.Resident{PID: 4242, Host: "box", Store: theirs}

	err := residentServesThisStore(holder, mine)
	if err == nil {
		t.Fatal("a resident serving another store was accepted as this errand's brain")
	}
	for _, want := range []string{"4242", "box", theirs, "--db"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the diagnostic does not name %q: %v", want, err)
		}
	}
	// The same store, however named, is the legitimate case and stays legitimate.
	if err := residentServesThisStore(&lease.Resident{PID: 7, Store: mine}, mine); err != nil {
		t.Fatalf("a resident serving this very store was refused: %v", err)
	}
	if err := residentServesThisStore(&lease.Resident{PID: 7}, mine); err != nil {
		t.Fatalf("an older build's lock, which names no store, was refused outright: %v", err)
	}
}
