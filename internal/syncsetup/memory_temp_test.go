package syncsetup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// THE RELAY RIGS' DURABLE STORES LIVE ON MEMORY-BACKED STORAGE WHEN THE MACHINE
// HAS SOME, because what these tests time is the protocol and not the disk.
//
// The native relay and the personal learner commit every Raft entry through
// SQLite with synchronous=FULL (internal/raftstore refuses anything weaker), so
// one cold-router round in TestR9R23PersonalLearnerRefreshesColdRouterGeneration
// makes 188 fsyncs. On a disk that a parallel full-suite run is also flushing,
// one fsync can take a quarter of a second, and the test took anywhere from one
// second to forty-nine. Its client requests and its wait both give up at the
// package's 30-second request timeout, so whenever the flushes added up past
// that, it failed (nightly 2026-11-07; Full check on dev 8a0106d11). On tmpfs the
// same round takes about a tenth of a second.
//
// tmpfs keeps every write and every ordering the stores ask for while the
// process lives; all it removes is the wait for a shared disk. None of these
// tests claims to survive a power cut, which is the one guarantee it gives up.
// Where there is no tmpfs with room (macOS, Windows, a small container
// /dev/shm), the package runs in the ordinary temporary directory as before.
func TestMain(m *testing.M) {
	os.Exit(runInMemoryTemp(m))
}

// memoryTempPrefix names this package's temporary roots, so a run that was
// killed before it could clean up is recognisable to the next one.
const memoryTempPrefix = "codeaf-syncsetup-"

// staleMemoryTemp is how old a leftover root must be before a later run removes
// it. A live run of this package lasts minutes, so six hours cannot be one.
const staleMemoryTemp = 6 * time.Hour

// runInMemoryTemp says on stderr, once, when the run moved to a tmpfs root, so
// a CI log shows whether the runner had the tmpfs this relies on: a log without
// that line is a run on the shared disk. It says nothing otherwise, because a
// helper process's output is read by the test that started it.
func runInMemoryTemp(m *testing.M) int {
	shm, ok := memoryTempBase()
	// A HELPER PROCESS RUNS INSIDE ITS PARENT'S ROOT. Several tests here run
	// this test binary again as a worker, and the worker comes through TestMain
	// too. If it made a root of its own, a worker the parent stops (most of them
	// are stopped, not finished) would leave that root in memory for good. It
	// inherits TMPDIR instead, and the parent's cleanup removes both.
	if !ok || ownMemoryTemp(shm, os.Getenv("TMPDIR")) {
		return m.Run()
	}
	// A KILLED RUN LEAVES ITS ROOT IN MEMORY, not on a disk, so the next run
	// clears what an earlier one could not, before it adds its own.
	removeStaleMemoryTemp(shm, time.Now())
	root, err := os.MkdirTemp(shm, memoryTempPrefix)
	if err != nil {
		return m.Run()
	}
	defer os.RemoveAll(root)
	previous, had := os.LookupEnv("TMPDIR")
	if err := os.Setenv("TMPDIR", root); err != nil {
		return m.Run()
	}
	fmt.Fprintf(os.Stderr, "syncsetup: temporary files on tmpfs at %s\n", root)
	defer func() {
		if had {
			os.Setenv("TMPDIR", previous)
		} else {
			os.Unsetenv("TMPDIR")
		}
	}()
	return m.Run()
}

// ownMemoryTemp reports whether dir is one of this package's roots directly
// under base, which is what a helper process inherits from the run that
// started it.
func ownMemoryTemp(base, dir string) bool {
	return dir != "" && filepath.Dir(filepath.Clean(dir)) == base && strings.HasPrefix(filepath.Base(dir), memoryTempPrefix)
}

// removeStaleMemoryTemp removes this package's roots that no live run can own.
func removeStaleMemoryTemp(base string, now time.Time) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), memoryTempPrefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil || now.Sub(info.ModTime()) < staleMemoryTemp {
			continue
		}
		os.RemoveAll(filepath.Join(base, entry.Name()))
	}
}
