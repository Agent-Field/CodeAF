package main

import (
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	lanes "github.com/Agent-Field/codeaf/internal/lane"
)

// holdLaneCompaction gates the real Persist writer before its first write.
func holdLaneCompaction(t *testing.T) (context.Context, context.CancelFunc, <-chan struct{}, func()) {
	t.Helper()
	t.Setenv("CODEAF_HOME", t.TempDir())
	lanes.Default().Reset()
	t.Cleanup(lanes.Default().Reset)
	reached, release := make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	allowWrite := func() { releaseOnce.Do(func() { close(release) }) }
	restore := lanes.BeforeCompactionInTests(func() {
		once.Do(func() { close(reached) })
		<-release
	})
	ctx, cancel := context.WithCancel(context.Background())
	lanes.Default().Ledger().Note(lanes.Sighting{
		ID:   lanes.ID{Model: "vendor/model", Lane: "provider"},
		TTFT: time.Second, Gen: time.Second, Tokens: 64, At: time.Now(),
	})
	done := startLaneSaving(ctx)
	t.Cleanup(func() {
		cancel()
		allowWrite()
		select {
		case <-done:
			restore()
		case <-time.After(5 * time.Second):
			t.Fatal("released lane writer did not stop")
		}
	})
	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		t.Fatal("writer did not reach compaction")
	}
	return ctx, cancel, done, allowWrite
}

// The exit path must join a compaction whose owed signal Flush cannot see.
func TestQuitWaitsForLaneCompaction(t *testing.T) {
	ctx, cancel, done, allowWrite := holdLaneCompaction(t)
	returned := make(chan struct{})
	go func() { finishLaneSaving(cancel, done); close(returned) }()
	<-ctx.Done()
	select {
	case <-returned:
		t.Fatal("exit returned while lane compaction could still write")
	case <-time.After(time.Millisecond):
	}
	allowWrite()
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("exit did not join the released writer")
	}
	select {
	case <-done:
	default:
		t.Fatal("exit returned before the writer stopped")
	}
	if _, err := os.Stat(lanes.StorePath()); err != nil {
		t.Fatalf("compaction did not save the lane notes: %v", err)
	}
}

func TestQuitBoundsLaneCompactionAndLogsOnce(t *testing.T) {
	_, cancel, done, _ := holdLaneCompaction(t)
	output, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { output.Close() })
	previous := os.Stderr
	os.Stderr = output
	t.Cleanup(func() { os.Stderr = previous })
	started := time.Now()
	finishLaneSaving(cancel, done)
	elapsed := time.Since(started)
	if elapsed < laneSaveWithin || elapsed > laneSaveWithin+500*time.Millisecond {
		t.Fatalf("quit took %s with writer held; deadline %s plus 500ms slack", elapsed, laneSaveWithin)
	}
	select {
	case <-done:
		t.Fatal("test released the writer before the deadline")
	default:
	}
	if _, err := output.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	printed, err := io.ReadAll(output)
	if err != nil {
		t.Fatal(err)
	}
	want := "codeaf: lane saving timed out; quitting without waiting further\n"
	if string(printed) != want || strings.Contains(string(printed), os.Getenv("CODEAF_HOME")) {
		t.Fatalf("deadline log = %q", printed)
	}
}
