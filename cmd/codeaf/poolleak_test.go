package main

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// A LAUNCH'S START-UP ERRANDS BELONG TO THE PROCESS THAT STARTED THEM.
//
// [wirePoolIndex] starts two fire-and-forget errands on every door: the index
// refresh, which writes doc.json and doc.json.sig under the profile's pool
// directory, and the outbox push, which opens the outbox and writes the
// install nonce there. Both run through [guard.Go], which joins nothing at
// shutdown, so a process that closed left them writing into a profile nobody
// was going to wait for. In a test whose profile is a TempDir that is the
// cleanup's own `unlinkat …/pool: directory not empty`; on a door that reopens
// on another profile it is a write into a directory the process no longer
// owns. Neither errand is on a run's path — both are start-up errands — so
// joining them at close costs the person nothing.
//
// THIS IS THE RACE, MADE CERTAIN. The endpoint below holds a fetch open until
// the test releases it, so a refresh that is not stopped is certainly still in
// flight when the close path returns — for the whole of the pull's own budget,
// which is seconds, not the microseconds a scheduling accident needs. The seam
// is read through the same [poolRefreshGo] every pool goroutine starts through,
// so the assertion is about the real errand and not a stand-in: the moment the
// close path has returned, no pool writer may still be running. The wait below
// is a fraction of that budget, so a process that closed without joining them
// fails here rather than passing a second later and racing the profile's own
// clean-up.
func TestTheLaunchesPoolWritersStopWhenTheProcessCloses(t *testing.T) {
	released := make(chan struct{})
	var release sync.Once
	free := func() { release.Do(func() { close(released) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-released:
		case <-r.Context().Done():
		}
		http.Error(w, "no index for a test", http.StatusServiceUnavailable)
	}))
	t.Cleanup(func() { free(); server.Close() })
	// The refresh fetches the index here and, when the primary does not answer,
	// falls through to the mirror here as well; the push sends here too. So
	// nothing reaches the network and an unstopped refresh is blocked for the
	// pull's own budget on each of the two addresses.
	t.Setenv("CODEAF_MODEL_POOL_URL", server.URL+"/index.json")
	t.Setenv("CODEAF_MODEL_POOL_MIRROR_URL", server.URL+"/mirror.json")
	t.Setenv("CODEAF_MODEL_POOL_SUBMIT_URL", server.URL+"/rows")

	var running sync.WaitGroup
	prev := poolRefreshGo
	poolRefreshGo = func(scope string, fn func()) {
		running.Add(1)
		go func() {
			defer running.Done()
			fn()
		}()
	}
	t.Cleanup(func() { poolRefreshGo = prev })

	proc := v3TestProcess(t)
	if _, err := openV3Launch(proc, v3Options{Model: "test/model", Workspace: t.TempDir()}); err != nil {
		t.Fatalf("the launch every v3 door assembles through did not open: %v", err)
	}

	proc.closeAll()

	done := make(chan struct{})
	go func() { running.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("a pool writer was still running after the process closed.\n" +
			"The refresh and the push write under the profile's pool directory and nothing joins them at\n" +
			"shutdown, so a process that closed left them writing into a profile nobody waits for — the\n" +
			"TempDir cleanup's own \"directory not empty\". They must stop when the process closes (poolindex.go).")
	}
}
