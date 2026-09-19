package main

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// A PLACE SWEEP STARTED BY THE PRODUCT MUST NOT WRITE AFTER ITS PROCESS CLOSES.
//
// This forces the real late-write ordering without load. The sweep seam blocks
// after openV3Process starts it. The product door then returns and closeAll
// completes. Only then does the sweep report an error through noteSweep, whose
// write resolves CODEAF_HOME at that late moment. An unjoined sweep therefore
// recreates v3/sweep.log after the owner has closed, the same ordering that can
// race a TempDir RemoveAll and produce "directory not empty".
func TestThePlaceSweepStopsBeforeTheProcessCloses(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_HOME", root)
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	t.Setenv("OPENROUTER_API_KEY", "test-key")

	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	var releaseOnce sync.Once
	free := func() { releaseOnce.Do(func() { close(release) }) }
	previousSweep := sweepHome
	sweepOnce = sync.Once{}
	sweepHome = func(_ string, note func(string)) {
		close(started)
		<-release
		note("forced sweep failure after close")
		close(finished)
	}
	t.Cleanup(func() {
		free()
		<-finished
		sweepHome = previousSweep
		sweepOnce = sync.Once{}
	})

	proc, err := openV3Process("resume")
	if err != nil {
		t.Fatalf("the resume door did not open: %v", err)
	}
	<-started
	proc.closeAll()

	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	free()
	<-finished

	late := filepath.Join(root, "v3", sweepLogName)
	if _, err := os.Stat(late); err == nil {
		t.Fatalf("the product-owned place sweep wrote %s after closeAll returned; the door must join it before returning", late)
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
