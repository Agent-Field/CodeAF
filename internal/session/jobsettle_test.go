package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// THE ENDING PUBLISHES, AND THE FOLDER IS TIDIED BESIDE IT (issue #1636).
//
// Since #1603 a job's ending ran a whole retention sweep of its log folder
// before its state left running, so a stopped job read as running for 47-130 ms
// under load. Publishing first and sweeping after (the reverted 8b0edfd6d) let
// anybody who saw the ending remove the folder while the sweep was still
// writing in it. These pin both halves: the ending never waits for the sweep,
// and nothing that sees the ending can race one. Every test here ends its job
// the way the reaper does, through [jobRegistry.settleExit], and holds the
// folder's own retention lock where it needs a sweep to be stuck, because that
// lock is exactly what a sweep waits on in production.

func settlementJob(t *testing.T, registry *jobRegistry) *job {
	t.Helper()
	one, err := registry.newJob("fixture", jobKindBash)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.add(one); err != nil {
		t.Fatal(err)
	}
	return one
}

func settlementWait(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// A JOB'S END DOES NOT WAIT FOR THE FOLDER (contract C1). With the folder's
// lock held by somebody else, so that any sweep is stuck, the job still reads as
// ended, done is closed and its note is delivered. On origin/dev this timed out:
// the ending itself was the sweep.
func TestAJobsEndDoesNotWaitForTheLogFolderSweep(t *testing.T) {
	for _, tc := range []struct {
		name  string
		kill  bool
		state jobState
		code  int
	}{
		{"natural exit", false, jobExited, 7},
		{"requested kill", true, jobKilled, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			notes := make(chan string, 1)
			registry := newJobRegistry(t.TempDir(), Place{}, func(note string) { notes <- note })
			one := settlementJob(t, registry)
			dir := filepath.Dir(one.logPath)
			locked, err := lockJobRetention(dir)
			if err != nil {
				t.Fatal(err)
			}
			var release sync.Once
			unlock := func() { release.Do(locked.close) }
			t.Cleanup(unlock)
			if tc.kill && !one.requestKill() {
				t.Fatal("could not request kill")
			}
			finished := make(chan struct{})
			go func() { registry.settleExit(one, 7); close(finished) }()
			t.Cleanup(func() { unlock(); settlementWait(t, finished, "exit handling after unlock") })
			settlementWait(t, one.done, "job ending while retention is locked")
			if one.running() {
				t.Fatal("ended job still running")
			}
			info := one.info()
			if info.state != tc.state || info.code != tc.code {
				t.Fatalf("final state=%v code=%d, want %v %d", info.state, info.code, tc.state, tc.code)
			}
			settlementWait(t, finished, "exit handling")
			if tc.kill {
				select {
				case note := <-notes:
					t.Fatalf("requested kill reported %q", note)
				default:
				}
			} else {
				select {
				case note := <-notes:
					if strings.Contains(note, "retention deferred") {
						t.Fatalf("completion note carried a later sweep failure: %q", note)
					}
				default:
					t.Fatal("completion note was not delivered")
				}
			}
			unlock()
			registry.shutdown(0)
		})
	}
}

// REMOVING THE FOLDER RIGHT AFTER AN ENDING IS SAFE AND FINAL (contract C4).
// A copy's give-back and a test's temporary directory both remove the jobs
// folder the moment they see the work end. The removal must succeed, and once
// the registry's pass has run the folder must not come back or gain a file: a
// pass may only read, lock and unlink. The last three cases hold the pass at a
// chosen point so the removal lands exactly where the reverted reorder raced.
func TestAnEndedJobsFolderCanBeRemovedAtOnceAndStaysGone(t *testing.T) {
	for _, door := range []string{"observed through done", "observed through running()"} {
		t.Run(door, func(t *testing.T) {
			registry := newJobRegistry(t.TempDir(), Place{}, nil)
			one := settlementJob(t, registry)
			dir := filepath.Dir(one.logPath)
			finished := make(chan struct{})
			go func() { registry.settleExit(one, 0); close(finished) }()
			if door == "observed through done" {
				settlementWait(t, one.done, "done")
			} else {
				waitFor(t, "running() to become false", func() bool { return !one.running() })
			}
			if err := os.RemoveAll(dir); err != nil {
				t.Fatal(err)
			}
			registry.shutdown(0)
			settlementWait(t, finished, "exit handling")
			if _, err := os.Lstat(dir); !os.IsNotExist(err) {
				t.Fatalf("jobs folder returned after removal: %v", err)
			}
		})
	}
	for _, tc := range []struct {
		name  string
		stage string
		leave bool
	}{
		{"removed while the pass holds its lock", "locked", false},
		{"emptied before the pass opens it", "start", true},
		{"emptied while the pass holds its lock", "locked", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := newJobRegistry(t.TempDir(), Place{}, nil)
			one := settlementJob(t, registry)
			dir := filepath.Dir(one.logPath)
			entered := make(chan struct{})
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			registry.retentionStage = func(stage string) {
				if stage == tc.stage {
					close(entered)
					<-release
				}
			}
			go registry.settleExit(one, 0)
			settlementWait(t, one.done, "job ending")
			settlementWait(t, entered, "folder pass reaching "+tc.stage)
			if tc.leave {
				entries, err := os.ReadDir(dir)
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
						t.Fatal(err)
					}
				}
			} else if err := os.RemoveAll(dir); err != nil {
				t.Fatal(err)
			}
			unblock()
			registry.shutdown(0)
			if tc.leave {
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 0 {
					t.Fatalf("emptied jobs folder has %d entries: %v", len(entries), err)
				}
			} else if _, err := os.Lstat(dir); !os.IsNotExist(err) {
				t.Fatalf("removed jobs folder returned: %v", err)
			}
			if strings.Contains(one.sink.notice(), "retention deferred") {
				t.Fatalf("give-back reported as an error: %q", one.sink.notice())
			}
		})
	}
}

// SHUTDOWN JOINS THE PASS, AND NONE STARTS AFTER IT (contract C5). Close and
// Stop work both end in [jobRegistry.shutdown], and whoever removes a folder
// after them must find no sweep still inside it — the reverted reorder returned
// from shutdown with the sweep stuck. A job that dies inside the grace is still
// tidied before shutdown returns; one that ends after it asks for nothing, and a
// registry reopened by a fresh submission tidies again.
func TestShutdownWaitsForTheLogFolderSweepAndNoneStartsAfter(t *testing.T) {
	t.Run("joins blocked sweep", func(t *testing.T) {
		registry := newJobRegistry(t.TempDir(), Place{}, nil)
		one := settlementJob(t, registry)
		locked, err := lockJobRetention(filepath.Dir(one.logPath))
		if err != nil {
			t.Fatal(err)
		}
		var release sync.Once
		unlock := func() { release.Do(locked.close) }
		t.Cleanup(unlock)
		finished := make(chan struct{})
		go func() { registry.settleExit(one, 0); close(finished) }()
		t.Cleanup(func() { unlock(); settlementWait(t, finished, "exit handling after unlock") })
		settlementWait(t, one.done, "job ending before shutdown")
		closed := make(chan struct{})
		go func() { registry.shutdown(50 * time.Millisecond); close(closed) }()
		select {
		case <-closed:
			t.Fatal("shutdown returned while retention was blocked")
		case <-time.After(300 * time.Millisecond):
		}
		unlock()
		settlementWait(t, closed, "shutdown joining retention")
	})
	t.Run("closed registry drops requests and reopened registry accepts them", func(t *testing.T) {
		registry := newJobRegistry(t.TempDir(), Place{}, nil)
		var starts atomic.Int32
		registry.retentionStage = func(stage string) {
			if stage == "start" {
				starts.Add(1)
			}
		}
		one := settlementJob(t, registry)
		registry.shutdown(0)
		go registry.settleExit(one, 0)
		settlementWait(t, one.done, "late job ending")
		if got := starts.Load(); got != 0 {
			t.Fatalf("closed registry started %d passes", got)
		}
		registry.reopen()
		another := settlementJob(t, registry)
		go registry.settleExit(another, 0)
		settlementWait(t, another.done, "reopened job ending")
		registry.shutdown(0)
		if got := starts.Load(); got != 1 {
			t.Fatalf("reopened registry started %d passes, want 1", got)
		}
	})
	t.Run("a job that dies inside the grace is tidied before shutdown returns", func(t *testing.T) {
		registry := newJobRegistry(t.TempDir(), Place{}, nil)
		var locked atomic.Int32
		registry.retentionStage = func(stage string) {
			if stage == "locked" {
				locked.Add(1)
			}
		}
		one := settlementJob(t, registry)
		closed := make(chan struct{})
		go func() { registry.shutdown(10 * time.Second); close(closed) }()
		// The reaper's ending is delivered only once the round has claimed the
		// job, which is the one window this case is about.
		waitFor(t, "shutdown to claim the job", func() bool {
			one.mu.Lock()
			defer one.mu.Unlock()
			return one.killRequested
		})
		go registry.settleExit(one, 0)
		settlementWait(t, closed, "shutdown after the job died inside its grace")
		if got := locked.Load(); got != 1 {
			t.Fatalf("a job that died inside the grace had %d folder passes before shutdown returned, want 1", got)
		}
	})
}

// THE #1599 LAW STILL HOLDS (contract C6). A folder already at the 64-job
// budget gets one more job; once it ends and the pass is joined, the oldest id
// is the one evicted and the new job's log is kept.
func TestRetentionStillBoundsTheFolderAfterAJobEnds(t *testing.T) {
	workspace := t.TempDir()
	dir := droppingsDir(Place{}, workspace, droppingJobs)
	root, err := openJobRetentionDir(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	root.Close()
	locked, err := lockJobRetention(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := locked.persist(100); err != nil {
		locked.close()
		t.Fatal(err)
	}
	locked.close()
	for id := int64(1); id <= 64; id++ {
		writeManagedLog(t, dir, id, 1, 0)
	}
	registry := newJobRegistry(workspace, Place{}, nil)
	one := settlementJob(t, registry)
	go registry.settleExit(one, 0)
	settlementWait(t, one.done, "job end")
	registry.shutdown(0)
	retentionExists(t, filepath.Join(dir, "1.log"), false)
	retentionExists(t, jobRetentionMarkerPath(dir, 1), false)
	for id := int64(2); id <= 64; id++ {
		retentionExists(t, filepath.Join(dir, fmt.Sprintf("%d.log", id)), true)
		retentionExists(t, jobRetentionMarkerPath(dir, id), true)
	}
	retentionExists(t, one.logPath, true)
	retentionExists(t, one.logPath+jobRetentionMarkerSuffix, true)
}

// A FAILED PASS IS STILL NAMED, ON THE JOB (contract C7). The note was written
// at the ending, before the pass ran, so the failure lands where `jobs output`
// reads it and the note stays the ending's own.
func TestAFailedLogFolderSweepIsNamedOnTheJob(t *testing.T) {
	notes := make(chan string, 1)
	registry := newJobRegistry(t.TempDir(), Place{}, func(note string) { notes <- note })
	one := settlementJob(t, registry)
	if err := os.WriteFile(filepath.Join(filepath.Dir(one.logPath), jobRetentionCounterName), []byte("garbage\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	go func() { registry.settleExit(one, 0); close(finished) }()
	settlementWait(t, finished, "exit handling")
	registry.shutdown(0)
	notice := one.sink.notice()
	if !strings.Contains(notice, "job log retention deferred:") || !strings.Contains(notice, errRetentionUnsafe.Error()) {
		t.Fatalf("damaged counter not reported in log footer: %q", notice)
	}
	select {
	case note := <-notes:
		if strings.Contains(note, "job log retention deferred:") {
			t.Fatalf("completion note carried a later sweep failure: %q", note)
		}
	default:
		t.Fatal("completion note was not delivered")
	}
}
