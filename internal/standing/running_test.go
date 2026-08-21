package standing

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// peekRunner is the session lane again, with one addition: it asks the store
// what it says about the item WHILE the item is being checked and while it is
// being fired. That is the only place the question can be asked honestly — a
// marker that is up for the length of a pass cannot be observed after the pass.
type peekRunner struct {
	store   *Store
	inProbe RunningMark
	probed  bool
	inSay   RunningMark
	said    bool
}

func (p *peekRunner) Probe(_ context.Context, item Item) (string, error) {
	p.inProbe, p.probed = p.store.Running(item.ID)
	return "the last run on main failed", nil
}

func (p *peekRunner) Say(_ context.Context, item Item, _ string) (Outcome, error) {
	p.inSay, p.said = p.store.Running(item.ID)
	return Outcome{Kind: "said"}, nil
}

func (p *peekRunner) Run(_ context.Context, item Item, _, _ string) (Outcome, error) {
	p.inSay, p.said = p.store.Running(item.ID)
	return Outcome{Kind: "landed"}, nil
}

// THE MARKER IS THE WHOLE OF "FIRING NOW" ACROSS PROCESSES. It says checking
// while the look is happening, firing while the work is, and it is gone the
// moment that item's pass is over — so a second window that reads it is reading
// what is true and never what was true five minutes ago.
func TestTheMarkerSaysCheckingThenFiringAndIsGoneAfterwards(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &peekRunner{store: store}

	watch := reminder("tell me when CI goes red", time.Time{})
	watch.When = When{Kind: WhenProbe, Words: "when CI goes red", Probe: Probe{Command: "gh run list"}}
	made, err := store.Create(watch)
	if err != nil {
		t.Fatal(err)
	}

	ticker := newTicker(store, runner, now)
	sentinel, _ := answers(true)
	ticker.Sentinel = sentinel
	mustTick(t, ticker)

	if !runner.probed {
		t.Fatal("nothing was marked while the look was happening")
	}
	if runner.inProbe.What != RunningChecking {
		t.Fatalf("the look's marker says %q, wanted %q", runner.inProbe.What, RunningChecking)
	}
	if runner.inProbe.PID != os.Getpid() {
		t.Fatalf("the marker names process %d, not this one", runner.inProbe.PID)
	}
	if !runner.inProbe.Since.Equal(now) {
		t.Fatalf("the marker started at %s, wanted the pass's own moment", runner.inProbe.Since)
	}
	if !runner.said {
		t.Fatal("nothing was marked while the firing was happening")
	}
	if runner.inSay.What != RunningFiring {
		t.Fatalf("the firing's marker says %q, wanted %q", runner.inSay.What, RunningFiring)
	}

	// AND THE PASS FOR THAT ITEM ENDING TAKES IT DOWN. Not the process exiting,
	// not the next pass overwriting it: a row that kept `●` after the work was
	// over would be the screen asserting what it cannot derive.
	if mark, running := store.Running(made.ID); running {
		t.Fatalf("the marker outlived the pass: %+v", mark)
	}
	if _, err := os.Stat(store.RunningPath(made.ID)); !os.IsNotExist(err) {
		t.Fatalf("the marker file is still on disk (%v)", err)
	}
}

// A RAIL IS A DECISION NOT TO LOOK, so an item the day's spending stopped is an
// item nobody ever had in their hands, and nothing is marked for it.
func TestAnItemARailSkippedIsNeverMarked(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &peekRunner{store: store}

	watch := reminder("tell me when CI goes red", time.Time{})
	watch.When = When{Kind: WhenProbe, Words: "when CI goes red", Probe: Probe{Command: "gh run list"}}
	watch.Rails.MaxPerDay = 1
	made, err := store.Create(watch)
	if err != nil {
		t.Fatal(err)
	}
	// One firing already spent today's whole allowance.
	if err := store.Append(Entry{At: now, ItemID: made.ID, Kind: string(ActionSay), USD: 0.01}); err != nil {
		t.Fatal(err)
	}

	ticker := newTicker(store, runner, now)
	sentinel, _ := answers(true)
	ticker.Sentinel = sentinel
	pass := mustTick(t, ticker)

	if pass.Skipped != 1 {
		t.Fatalf("the pass is %+v, wanted the rail to skip it", pass)
	}
	if runner.probed {
		t.Fatal("a rail-skipped item was looked at anyway")
	}
	if _, err := os.Stat(store.RunningPath(made.ID)); !os.IsNotExist(err) {
		t.Fatalf("a rail-skipped item was marked as running (%v)", err)
	}
}

// THE MARKER IS ON DISK IN ONE PLACE AND SAYS THREE THINGS. Its shape is a
// contract between two processes that never meet, so it is asserted here rather
// than left to whatever json.Marshal happened to do.
func TestTheMarkerOnDiskNamesTheProcessTheMomentAndTheHalf(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	store.markRunning("abc123", RunningFiring)

	want := filepath.Join(store.Root(), "abc123", RunningFile)
	if got := store.RunningPath("abc123"); got != want {
		t.Fatalf("the marker lives at %q, wanted %q", got, want)
	}
	raw, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("the marker was not written: %v", err)
	}
	var mark RunningMark
	if err := json.Unmarshal(raw, &mark); err != nil {
		t.Fatalf("the marker is not readable: %v (%s)", err, raw)
	}
	if mark.PID != os.Getpid() || mark.What != RunningFiring || !mark.Since.Equal(now) {
		t.Fatalf("the marker says %+v", mark)
	}
	// AND IT IS NOT A DOCUMENT. List reads the root by `.json` suffix, and a
	// marker that landed there would be one more thing every reader must skip.
	items, err := store.List()
	if err != nil || len(items) != 0 {
		t.Fatalf("the marker was read as an item: %d items, %v", len(items), err)
	}

	store.clearRunning("abc123")
	if _, running := store.Running("abc123"); running {
		t.Fatal("a cleared marker still answers yes")
	}
}

// NO `●` FOREVER AFTER A CRASH. A process killed mid-firing cannot take its own
// marker down, so every reader doubts one: a dead process is a leftover, and so
// is a marker older than the longest a pass may last, whatever process id it
// happens to be wearing.
func TestAMarkerFromADeadProcessOrAnOldPassIsNoMarkerAtAll(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)

	// A live marker from this very process is believed.
	store.markRunning("abc123", RunningChecking)
	if _, running := store.Running("abc123"); !running {
		t.Fatal("a marker written a moment ago by a live process was not believed")
	}

	// Past one pass's own ceiling it is a leftover, pid or no pid.
	store.clock = held(now.Add(TickWindow + time.Second))
	if mark, running := store.Running("abc123"); running {
		t.Fatalf("a marker older than one pass is still believed: %+v", mark)
	}
	store.clock = held(now)

	// And a marker naming a process that is gone is a leftover at any age.
	dead := reapedPID(t)
	writeMark(t, store, "abc123", RunningMark{PID: dead, Since: now, What: RunningFiring})
	if mark, running := store.Running("abc123"); running {
		t.Fatalf("a marker from a dead process is still believed: %+v", mark)
	}

	// So is one that says nothing, one that is not JSON at all, and one for an
	// item nobody ever wrote a marker for.
	writeMark(t, store, "abc123", RunningMark{PID: os.Getpid(), Since: now})
	if _, running := store.Running("abc123"); running {
		t.Fatal("a marker with nothing to say is believed")
	}
	if err := os.WriteFile(store.RunningPath("abc123"), []byte("half a fi"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, running := store.Running("abc123"); running {
		t.Fatal("an unreadable marker is believed")
	}
	if _, running := store.Running("nobody"); running {
		t.Fatal("an item with no marker is believed to be running")
	}
}

// writeMark puts one marker on disk exactly as it is given, so a test can write
// the ones a healthy pass never would.
func writeMark(t *testing.T, store *Store, id string, mark RunningMark) {
	t.Helper()
	data, err := json.Marshal(mark)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(store.ItemDir(id), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.RunningPath(id), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

// reapedPID is a process id that certainly names nothing: a child this test
// started, waited for and reaped. Inventing a large number instead would be a
// test that passes because a machine happened to be quiet.
func reapedPID(t *testing.T) int {
	t.Helper()
	child := exec.Command("/bin/sh", "-c", "exit 0")
	if err := child.Run(); err != nil {
		t.Skipf("this machine has no shell to make a dead process with: %v", err)
	}
	return child.Process.Pid
}
