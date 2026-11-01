package main

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// Contract 1b keeps an unanswered save distinct from a failed one at the saving ceiling.
// L4-H4b requires a new quit generation and a positive local receipt at that ceiling.
func TestQuitWaitReportsStillFinishingAtTheSavingCeiling(t *testing.T) {
	oldOut, oldFlush := os.Stdout, flushFinalSaves
	flushFinalSaves = func(context.Context) error { return nil }
	defer func() { flushFinalSaves = oldFlush }()
	out, err := os.CreateTemp(t.TempDir(), "screen")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	os.Stdout = out
	armFinalSaving()
	finalSaving.Lock()
	finalSaving.failure, finalSaving.pending = nil, nil
	finalSaving.Unlock()
	t.Cleanup(func() {
		os.Stdout = oldOut
		finalSaving.Lock()
		finalSaving.failure, finalSaving.pending, finalSaving.forced = nil, nil, nil
		finalSaving.surface = false
		finalSaving.reporting, finalSaving.reportDone, finalSaving.durable = false, nil, nil
		finalSaving.Unlock()
	})
	retainFinalClose(make(chan struct{}))
	retainLocalDurability(func() bool { return true })
	ctx, cancel := context.WithDeadline(t.Context(), time.Unix(0, 0))
	defer cancel()
	awaitFinalCloses(ctx)
	finishFinalSaving(context.Background())
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), "saved here; it reaches your other devices when the sync service at") != 1 || strings.Contains(string(raw), "could not save") {
		t.Fatalf("long wait=%q", raw)
	}
	finalSaving.Lock()
	failure := finalSaving.failure
	finalSaving.Unlock()
	if failure != nil {
		t.Fatal("quit called an unanswered save a failure")
	}
}
