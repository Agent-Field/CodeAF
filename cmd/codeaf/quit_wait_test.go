package main

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// Contract E2 explains a long post-screen save and bounds an unavailable writer.
func TestQuitWaitExplainsSavingAndKeepsTheExistingCeiling(t *testing.T) {
	oldOut := os.Stdout
	out, err := os.CreateTemp(t.TempDir(), "screen")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	os.Stdout = out
	finalSaving.Lock()
	finalSaving.failure, finalSaving.pending, finalSaving.forced = nil, nil, nil
	finalSaving.Unlock()
	t.Cleanup(func() {
		os.Stdout = oldOut
		finalSaving.Lock()
		finalSaving.failure, finalSaving.pending, finalSaving.forced = nil, nil, nil
		finalSaving.Unlock()
	})
	retainFinalClose(make(chan struct{}))
	ctx, cancel := context.WithTimeout(t.Context(), 1200*time.Millisecond)
	defer cancel()
	awaitFinalCloses(ctx)
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), "saving this chat before quitting…") != 1 {
		t.Fatalf("long wait=%q", raw)
	}
	finalSaving.Lock()
	failure := finalSaving.failure
	finalSaving.Unlock()
	if failure == nil {
		t.Fatal("quit's exhausted save bound claimed success")
	}
}
