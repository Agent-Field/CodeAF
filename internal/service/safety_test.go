package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestAStopDuringTheGraceIsTheSameStop is the double stop: a second Stop while
// the first is still waiting out SIGTERM must not orphan the first's cleanup,
// so the row ends not running and Start again starts a new process.
func TestAStopDuringTheGraceIsTheSameStop(t *testing.T) {
	h := newHarness(t)
	row := h.keep("stubborn", PolicyOnFailure)
	h.rt.proc(row.PID).ignoreTerm = true
	if _, err := h.s.Stop("agent-a", row.ID); err != nil {
		t.Fatal(err)
	}
	h.clock.fireDue(2 * time.Second)
	second, err := h.s.Stop("agent-a", row.ID)
	if err != nil || !second.Stopping || second.State != StateStopped {
		t.Fatalf("the second stop is the first one, still in its grace: %+v %v", second, err)
	}
	for i := 0; i < 50 && h.get(row.ID).Stopping; i++ {
		h.clock.fireDue(time.Second)
		time.Sleep(time.Millisecond)
	}
	settle(t, h.s)
	got := h.get(row.ID)
	if got.Running || got.Stopping || got.PID != 0 {
		t.Fatalf("a dead process is still on the row: %+v", got)
	}
	if signals := h.rt.proc(row.PID).signals; len(signals) != 2 || signals[0] || !signals[1] {
		t.Fatalf("one TERM and one KILL, whatever the second stop did: %v", signals)
	}
	if _, err := h.s.Start("agent-a", row.ID); err != nil {
		t.Fatal(err)
	}
	settle(t, h.s)
	if again := h.get(row.ID); !again.Running || again.PID == row.PID {
		t.Fatalf("start again did not start: %+v", again)
	}
}

// failingWrites makes the store's writer fail from the nth write on.
func failingWrites(h *harness, from int) *int {
	count := new(int)
	var mu sync.Mutex
	h.setWrite(func(dir string, raw []byte) error {
		mu.Lock()
		*count++
		n := *count
		mu.Unlock()
		if n >= from {
			return errors.New("disk full")
		}
		return writeStore(dir, raw)
	})
	return count
}

// TestAYesTheFileCannotKeepStartsNothing: when the record cannot be written
// before the launch, nothing runs, nothing is banked and nothing is listed.
func TestAYesTheFileCannotKeepStartsNothing(t *testing.T) {
	h := newHarness(t)
	failingWrites(h, 1)
	spec := webSpec("web", h.dir)
	if _, err := h.s.Keep("agent-a", spec, PolicyOnFailure, Provenance{}); !errors.Is(err, ErrNotSaved) {
		t.Fatalf("keep reported %v", err)
	}
	if h.rt.starts != 0 || len(h.rt.running()) != 0 {
		t.Fatal("a process started that the file does not name")
	}
	if _, ok := h.s.Approved(spec); ok {
		t.Fatal("a yes the file could not keep is banked")
	}
	if rows := h.s.List("agent-a"); len(rows) != 0 {
		t.Fatalf("a record the file could not keep is listed: %+v", rows)
	}
	h.setWrite(writeStore)
	h.reopen()
	if rows := h.s.List("agent-a"); len(rows) != 0 {
		t.Fatal("the next codeaf found a record")
	}
}

// TestALaunchTheFileCannotRecordRunsNothing: the record was written and the
// gate made, and the gate's identity could not be written. The gate is let go,
// so the approved command never runs; the row keeps the gate until its exit is
// seen, says why, and the next codeaf still says why.
func TestALaunchTheFileCannotRecordRunsNothing(t *testing.T) {
	h := newHarness(t)
	failingWrites(h, 2)
	row, err := h.s.Keep("agent-a", webSpec("web", h.dir), PolicyOnFailure, Provenance{})
	if !errors.Is(err, ErrNotSaved) {
		t.Fatalf("keep reported %v", err)
	}
	if row.State != StateFailed || !strings.Contains(row.Reason, "could not write") {
		t.Fatalf("the row says why it is not running: %+v", row)
	}
	if h.rt.launches != 1 || h.rt.starts != 0 {
		t.Fatalf("the approved command ran %d times from %d gates", h.rt.starts, h.rt.launches)
	}
	deadline := time.Now().Add(5 * time.Second)
	for h.get(row.ID).Running {
		if time.Now().After(deadline) {
			t.Fatal("the let-go gate never left the row")
		}
		time.Sleep(time.Millisecond)
	}
	if len(h.rt.running()) != 0 {
		t.Fatalf("still running: %v", h.rt.running())
	}
	h.setWrite(writeStore)
	h.reopen()
	if got := h.get(row.ID); got.Running || got.State != StateFailed || len(h.rt.running()) != 0 {
		t.Fatalf("the next codeaf: %+v", got)
	}
}

// TestAPolicyChangeTheFileCannotKeepIsUndone, and a removal likewise.
func TestAChangeTheFileCannotKeepIsUndone(t *testing.T) {
	h := newHarness(t)
	row := h.keep("web", PolicyOnFailure)
	if _, err := h.s.Stop("agent-a", row.ID); err != nil {
		t.Fatal(err)
	}
	settle(t, h.s)
	failingWrites(h, 1)
	got, err := h.s.SetPolicy("agent-a", row.ID, PolicyAlways)
	if !errors.Is(err, ErrNotSaved) || got.Policy != PolicyOnFailure {
		t.Fatalf("a policy the file could not keep was reported: %+v %v", got, err)
	}
	if policy, _ := h.s.Approved(webSpec("web", h.dir)); policy != PolicyOnFailure {
		t.Fatal("the banked yes moved with an unsaved policy")
	}
	if err := os.WriteFile(row.LogPath, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.s.Remove("agent-a", row.ID); !errors.Is(err, ErrNotSaved) {
		t.Fatalf("remove reported %v", err)
	}
	if _, err := h.s.Get("agent-a", row.ID); err != nil {
		t.Fatal("a removal the file could not keep removed the record")
	}
	if body, _ := os.ReadFile(row.LogPath); string(body) != "kept" {
		t.Fatal("a removal the file could not keep removed the output")
	}
}

// TestAListenerThatClosesOrBlocksDoesNotHoldClose.
func TestAListenerThatClosesOrBlocksDoesNotHoldClose(t *testing.T) {
	h := newHarness(t)
	closed := make(chan struct{})
	h.s.Subscribe(func(Event) {
		h.s.Close()
		select {
		case <-closed:
		default:
			close(closed)
		}
	})
	h.keep("web", PolicyOnFailure)
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close from inside a listener never returned")
	}

	other := newHarness(t)
	block := make(chan struct{})
	defer close(block)
	other.s.Subscribe(func(Event) { <-block })
	other.keep("web", PolicyOnFailure)
	done := make(chan struct{})
	go func() { other.s.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a blocked listener held Close")
	}
}

// TestARowNeverAliasesTheRecord.
func TestARowNeverAliasesTheRecord(t *testing.T) {
	h := newHarness(t)
	row := h.keep("web", PolicyNever)
	h.rt.exit(t, h.s, row.PID, 3)
	got := h.get(row.ID)
	*got.ExitCode = 0
	got.Check = nil
	if again := h.get(row.ID); again.ExitCode == nil || *again.ExitCode != 3 {
		t.Fatal("writing into a row changed the record")
	}
}

// TestAnUnreadableBootProvesNoReboot: a boot that cannot be read is neither
// the same boot nor another one, so a saved service is not started beside a
// copy that may still be running.
func TestAnUnreadableBootProvesNoReboot(t *testing.T) {
	for name, boots := range map[string][2]string{
		"unreadable now":    {"boot-1", ""},
		"unreadable before": {"", "boot-2"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.rt.boot = boots[0]
			row := h.keep("web", PolicyAlways)
			h.s.Close()
			h.rt.mu.Lock()
			h.rt.boot = boots[1]
			h.rt.procs = map[int]*fakeProc{}
			h.rt.mu.Unlock()
			h.open()
			if got := h.get(row.ID); got.Running || got.State != StateNotRunning || h.rt.starts != 1 {
				t.Fatalf("an unproven reboot started it again: %+v", got)
			}
		})
	}
}

// TestAFileCodeafDidNotWriteIsRefused: corrupt, oversized, newer or holding a
// spec that was never validated — Open says so, starts nothing, and lets go of
// the lock so the person can repair it.
func TestAFileCodeafDidNotWriteIsRefused(t *testing.T) {
	for name, body := range map[string]string{
		"corrupt":       "{",
		"newer":         `{"version":2}`,
		"unvalidated":   `{"version":1,"seq":1,"services":[{"id":"s1","owner":"a","spec":{"name":"x","command":"","dir":"/tmp","health":{"kind":"port","port":1}},"policy":"always","state":"healthy"}]}`,
		"bad identity":  `{"version":1,"seq":1,"services":[{"id":"s1","owner":"a","spec":{"name":"x","command":"x","dir":"/tmp","health":{"kind":"port","port":1}},"policy":"always","state":"healthy","identity":{"pid":1,"start":0}}]}`,
		"bad consent":   `{"version":1,"consent":{"zz":{"policy":"always","health":{"kind":"port","port":1}}}}`,
		"unknown state": `{"version":1,"seq":1,"services":[{"id":"s1","owner":"a","spec":{"name":"x","command":"x","dir":"/tmp","health":{"kind":"port","port":1}},"policy":"always","state":"zombie"}]}`,
		"oversized":     `{"version":1,"boot":"` + strings.Repeat("x", maxStoreBytes) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, storeFile), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			rt := newFakeRuntime()
			if s, err := Open(Options{Dir: dir, Runtime: rt, Clock: newFakeClock()}); err == nil || errors.Is(err, ErrBusy) {
				if s != nil {
					s.Close()
				}
				t.Fatalf("opened: %v", err)
			}
			if rt.starts != 0 {
				t.Fatal("a refused file started something")
			}
			if err := os.Remove(filepath.Join(dir, storeFile)); err != nil {
				t.Fatal(err)
			}
			s, err := Open(Options{Dir: dir, Runtime: rt, Clock: newFakeClock()})
			if err != nil {
				t.Fatalf("the refusal kept the lock: %v", err)
			}
			s.Close()
		})
	}
}

// TestAFolderIsKeptExactlyAsItIsOnDisk, trailing space included.
func TestAFolderIsKeptExactlyAsItIsOnDisk(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "web ")
	spec, err := Spec{Command: "npm run dev", Dir: dir, Health: Health{Port: 5173}}.Check()
	if err != nil || spec.Dir != dir {
		t.Fatalf("the folder %q became %q: %v", dir, spec.Dir, err)
	}
	if other := (Spec{Command: "npm run dev", Dir: strings.TrimSpace(dir), Health: Health{Port: 5173}}); other.ConsentKey() == spec.ConsentKey() {
		t.Fatal("two folders shared one yes")
	}
}

// TestAFoldedEpisodeStillSaysItHappened: a failure that recovered before
// anybody read it leaves its episode on the healthy row.
func TestAFoldedEpisodeStillSaysItHappened(t *testing.T) {
	h := newHarness(t)
	row := h.keep("web", PolicyOnFailure)
	h.rt.exit(t, h.s, row.PID, 1)
	h.advance(2 * time.Second)
	h.advance(10 * time.Second)
	got := h.get(row.ID)
	if got.State != StateHealthy || got.Episode != row.ID+".1" || got.EpisodeOpen ||
		got.EpisodeReason != "exited 1" || got.EpisodeStartedAt == nil || got.Restarts != 1 {
		t.Fatalf("the recovered row lost its episode: %+v", got)
	}
	h.reopen()
	if again := h.get(row.ID); again.Episode != got.Episode || again.EpisodeReason != got.EpisodeReason {
		t.Fatal("the episode did not survive a restart of codeaf")
	}
}

// TestAChangedCheckUnderAYesIsNotSlippedIn: the same command with a new check
// is not approved, and while the first is running it is refused rather than
// swapped in.
func TestAChangedCheckUnderAYesIsNotSlippedIn(t *testing.T) {
	h := newHarness(t)
	h.keep("web", PolicyOnFailure)
	changed := webSpec("web", h.dir)
	changed.Health = Health{Cmd: "curl -fsS localhost:5173 && rm -rf ~"}
	if _, ok := h.s.Approved(changed); ok {
		t.Fatal("a new check command rode an old yes")
	}
	if _, err := h.s.Keep("agent-a", changed, PolicyOnFailure, Provenance{}); !errors.Is(err, ErrRunning) {
		t.Fatalf("a new check was swapped into a running service: %v", err)
	}
}
