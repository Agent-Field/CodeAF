package main

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// CLOSE SEALS THE PLACE SWEEP WITHOUT WAITING FOR THE WALK.
//
// The seam blocks the sweep as if it were walking a machine that has held a
// thousand conversations, so its length is unbounded. closeAll must still return
// promptly (it seals the note rather than joining the walk), and the sweep's
// late error note, reported after the seal, must write nothing beneath the home
// the process just left. Without the seal that late note recreates v3/sweep.log
// after the owner has closed, the ordering that races a TempDir RemoveAll and
// produces "directory not empty".
func TestClosingSealsThePlaceSweepWithoutWaitingForTheWalk(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_HOME", root)
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	t.Setenv("OPENROUTER_API_KEY", "test-key")

	started := make(chan struct{})
	release := make(chan struct{})
	noted := make(chan struct{})
	var releaseOnce sync.Once
	free := func() { releaseOnce.Do(func() { close(release) }) }
	previousSweep := sweepHome
	sweepOnce = sync.Once{}
	sweepMu.Lock()
	sweepSealed = false
	sweepMu.Unlock()
	sweepHome = func(_ string, note func(string)) {
		close(started)
		<-release
		note("forced sweep failure after close")
		close(noted)
	}
	t.Cleanup(func() {
		free()
		<-noted
		sweepHome = previousSweep
		sweepOnce = sync.Once{}
		sweepMu.Lock()
		sweepSealed = false
		sweepMu.Unlock()
	})

	proc, err := openV3Process("resume")
	if err != nil {
		t.Fatalf("the resume door did not open: %v", err)
	}
	<-started

	// closeAll returns while the walk is still blocked: quit does not wait for it.
	closed := make(chan struct{})
	go func() {
		proc.closeAll()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("closeAll blocked on the place sweep walk; quit must not wait for a walk that grows with the profile")
	}

	// The sweep is sealed, so its late error note writes nothing.
	free()
	<-noted

	late := filepath.Join(root, "v3", sweepLogName)
	if _, err := os.Stat(late); err == nil {
		t.Fatalf("a sealed place sweep wrote %s after closeAll returned", late)
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
