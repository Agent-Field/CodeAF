package main

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCloseJoinsPlaceSweepAndNextProcessStillSweeps(t *testing.T) {
	type pass struct {
		started  chan struct{}
		canceled chan struct{}
		release  chan struct{}
	}
	passes := []pass{
		{make(chan struct{}), make(chan struct{}), make(chan struct{})},
		{make(chan struct{}), make(chan struct{}), make(chan struct{})},
	}
	var releases [2]sync.Once
	for i := range passes {
		i := i
		t.Cleanup(func() { releases[i].Do(func() { close(passes[i].release) }) })
	}

	previousSweep := sweepHome
	t.Cleanup(func() { sweepHome = previousSweep })
	var starts atomic.Int32
	var destructive atomic.Int32
	sweepHome = func(ctx context.Context, _ string, _ func(string)) {
		i := int(starts.Add(1)) - 1
		if i >= len(passes) {
			t.Errorf("unexpected sweep start %d", i+1)
			return
		}
		close(passes[i].started)
		<-ctx.Done()
		close(passes[i].canceled)
		<-passes[i].release
		if ctx.Err() == nil {
			destructive.Add(1) // models the next rename or remove in the walk
		}
	}

	wait := func(name string, ch <-chan struct{}) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out waiting for %s", name)
		}
	}
	closeProcess := func(name string, p *v3Process, pass int) {
		t.Helper()
		closed := make(chan struct{})
		go func() {
			p.closeAll()
			close(closed)
		}()
		wait(name+" cancellation", passes[pass].canceled)
		releases[pass].Do(func() { close(passes[pass].release) })
		wait(name+" close", closed)
	}

	proc1 := &v3Process{}
	proc1.startPlaceSweep()
	wait("first sweep start", passes[0].started)
	closeProcess("first process", proc1, 0)
	if got := destructive.Load(); got != 0 {
		t.Fatalf("first close allowed %d destructive sweep attempts after cancellation, want 0", got)
	}

	proc2 := &v3Process{}
	proc2.startPlaceSweep()
	wait("second sweep start", passes[1].started)
	if got := starts.Load(); got != 2 {
		t.Fatalf("sweep starts in one binary = %d, want 2", got)
	}
	closeProcess("second process", proc2, 1)
}
