package cell

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestR5bCrashedArrivalOwnerDoesNotStallWaiters(t *testing.T) {
	if cellRoot := os.Getenv("CODEAF_TEST_ARRIVAL_CRASH_ROOT"); cellRoot != "" {
		workDir := filepath.Join(cellRoot, "workspace")
		_ = BeginWorkspaceArrival(cellRoot, workDir, nil, nil)
		os.Exit(0)
	}

	tmp := t.TempDir()
	cellRoot := filepath.Join(tmp, "v3", "cells", "cell-r5b-crash")
	stateDir := filepath.Join(cellRoot, StateDir)
	workDir := filepath.Join(cellRoot, "workspace")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	marker := ArrivalMarkerPath(cellRoot)
	if marker == "" {
		t.Fatal("expected non-empty ArrivalMarkerPath")
	}
	if strings.HasPrefix(marker, cellRoot+string(filepath.Separator)) {
		t.Fatalf("arrival marker %q must live outside cellRoot %q", marker, cellRoot)
	}

	// Spawn a separate process that begins an arrival and exits without Complete().
	cmd := exec.Command(os.Args[0], "-test.run=^TestR5bCrashedArrivalOwnerDoesNotStallWaiters$")
	cmd.Env = append(os.Environ(), "CODEAF_TEST_ARRIVAL_CRASH_ROOT="+cellRoot)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child process failed: %v\n%s", err, out)
	}

	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected crashed child to leave arrival marker at %s: %v", marker, err)
	}

	// A waiter in another process must detect the dead owner, remove the marker,
	// and return immediately instead of waiting out DefaultArrivalStallBound (2m).
	start := time.Now()
	if err := WaitWorkspace(t.Context(), cellRoot, workDir); err != nil {
		t.Fatalf("WaitWorkspace after owner crash: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("WaitWorkspace stalled for %s after owner crash; expected immediate return", elapsed)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("expected stale marker %s to be removed, stat err = %v", marker, err)
	}
	if WorkspaceArriving(cellRoot, workDir) {
		t.Fatal("expected WorkspaceArriving to be false after crashed owner cleanup")
	}

	// Also verify PID recycling protection when start time is available:
	// write a marker with our own live PID but a bogus process start time token.
	if ownStart, alive := processStatus(os.Getpid()); alive && ownStart != "" {
		if err := writeArrivalMarkerRecord(marker, arrivalMarkerRecord{
			PID:   os.Getpid(),
			Start: ownStart + "-recycled",
		}); err != nil {
			t.Fatalf("write recycled-pid marker: %v", err)
		}
		start = time.Now()
		if err := WaitWorkspace(t.Context(), cellRoot, workDir); err != nil {
			t.Fatalf("WaitWorkspace with recycled PID start mismatch: %v", err)
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("WaitWorkspace stalled for %s on recycled PID; expected immediate return", elapsed)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("expected recycled-PID marker %s to be removed, stat err = %v", marker, err)
		}
	}
}
