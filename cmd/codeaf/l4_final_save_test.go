package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncwords"
)

// L4-H4a, L4-H4d: the restored terminal gets immediate and three-second progress.
func TestL4QuitProgressClock(t *testing.T) {
	ticks := make(chan time.Time)
	waits := make(chan time.Duration)
	said := make(chan string)
	done := make(chan struct{})
	q := quitProgress{after: func(d time.Duration) <-chan time.Time { waits <- d; return ticks }, say: func(s string) { said <- s }, stop: done}
	finished := make(chan struct{})
	go func() { q.run(); close(finished) }()
	if d := <-waits; d <= 0 || d > time.Second {
		t.Fatal(d)
	}
	ticks <- time.Time{}
	if line := <-said; line != "saving this chat to your other devices…" {
		t.Fatal(line)
	}
	if d := <-waits; d != 3*time.Second {
		t.Fatal(d)
	}
	ticks <- time.Time{}
	if line := <-said; line != "still saving — ctrl+c leaves it saved here" {
		t.Fatal(line)
	}
	close(done)
	<-finished
}

// L4-H4b, L4-H4c: uploading failure is success locally; disk failure is not.
func TestL4QuitSaveWords(t *testing.T) {
	for _, tc := range []struct {
		err           error
		finishing, ok bool
		want          string
	}{
		{syncwords.UploadFailure(context.DeadlineExceeded), false, true, "saved here; it reaches your other devices when the sync service at 127.0.0.1:8901 answers"},
		{nil, true, false, "saving on this computer has not finished; its written turns stay here"},
		{errors.New("disk full"), false, false, "codeaf could not save this chat"},
	} {
		line, ok := finalSaveWords(tc.err, tc.finishing, "http://127.0.0.1:8901")
		if ok != tc.ok || !strings.HasPrefix(line, tc.want) || strings.Contains(line, "background") {
			t.Fatalf("%v %q", ok, line)
		}
	}
}

// L4-H4e: interruption ends the waiting function without waiting for a save reply.
func TestL4QuitWaitCanBeCanceled(t *testing.T) {
	finalSaving.Lock()
	finalSaving.pending = nil
	finalSaving.finishing = false
	finalSaving.Unlock()
	retainFinalClose(make(chan struct{}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	awaitFinalCloses(ctx)
	if !finalSavingFinishing() {
		t.Fatal("unanswered upload not retained locally")
	}
	line, ok := finalSaveWords(syncwords.UploadFailure(context.DeadlineExceeded), finalSavingFinishing(), "http://127.0.0.1:8901")
	if !ok || line != "saved here; it reaches your other devices when the sync service at 127.0.0.1:8901 answers" {
		t.Fatalf("%v %q", ok, line)
	}
	finalSaving.Lock()
	finalSaving.finishing = false
	finalSaving.Unlock()
}

// L4-H4b/H4c: an upload receipt never masks another chat's local failure.
func TestL4LocalSaveFailureWinsOverUpload(t *testing.T) {
	for _, pending := range []error{
		syncwords.UploadFailure(context.DeadlineExceeded),
		session.SavingError("/temporary/chat/transcript.jsonl", session.ErrSavingUnanswered),
	} {
		finalSaving.Lock()
		finalSaving.failure = nil
		finalSaving.finishing = false
		finalSaving.Unlock()
		recordFinalSaveFailure(pending)
		local := errors.New("local disk full")
		recordFinalSaveFailure(local)
		err, _, finishing := takeFinalSaveFailure()
		line, ok := finalSaveWords(err, finishing, "http://127.0.0.1:8901")
		if !errors.Is(err, local) || ok || !strings.HasPrefix(line, "codeaf could not save this chat") {
			t.Fatalf("local failure was hidden: %v %v %q", err, ok, line)
		}
	}
}

// L4-H4c: an unanswered close with a path cannot erase an earlier local write failure.
func TestL4UnansweredCloseKeepsLocalFailure(t *testing.T) {
	finalSaving.Lock()
	finalSaving.failure, finalSaving.finishing = nil, false
	finalSaving.Unlock()
	local := errors.New("local disk full")
	recordFinalSaveFailure(local)
	recordFinalSaveFailure(session.SavingError("/temporary/chat/transcript.jsonl", session.ErrSavingUnanswered))
	err, _, finishing := takeFinalSaveFailure()
	line, ok := finalSaveWords(err, finishing, "http://127.0.0.1:8901")
	if !errors.Is(err, local) || ok || !strings.HasPrefix(line, "codeaf could not save this chat") {
		t.Fatalf("local failure was replaced: %v %v %q", err, ok, line)
	}
}

// L4-H4c: concurrent final reporters share one local failure and one terminal receipt.
func TestL4ConcurrentFinalSavingKeepsFailure(t *testing.T) {
	t.Cleanup(func() {
		finalSaving.Lock()
		defer finalSaving.Unlock()
		finalSaving.failure = nil
		finalSaving.finishing = false
		finalSaving.surface = false
		finalSaving.reporting = false
		finalSaving.reportDone = nil
		finalSaving.durable = nil
	})

	oldFlush, oldErr := flushFinalSaves, os.Stderr
	out, err := os.CreateTemp(t.TempDir(), "receipt")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	os.Stderr = out
	defer func() { flushFinalSaves = oldFlush; os.Stderr = oldErr }()
	armFinalSaving()
	finalSaving.Lock()
	finalSaving.surface = false
	finalSaving.Unlock()
	flushFinalSaves = func(context.Context) error { return errors.New("local disk full") }
	results := make(chan bool, 2)
	go func() { results <- finishFinalSaving(context.Background()) }()
	go func() { results <- finishFinalSaving(context.Background()) }()
	first, second := <-results, <-results
	if first || second {
		t.Fatal("a concurrent local failure was reported successful")
	}
	raw, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), "codeaf could not save this chat") != 1 {
		t.Fatalf("receipts=%s", raw)
	}
}

// L4-H4c: one final reporter owns the flush as well as its failure receipt.
func TestL4FinalSavingFlushHasOneOwner(t *testing.T) {
	oldFlush, oldErr := flushFinalSaves, os.Stderr
	out, err := os.CreateTemp(t.TempDir(), "receipt")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	os.Stderr = out
	t.Cleanup(func() {
		flushFinalSaves, os.Stderr = oldFlush, oldErr
		finalSaving.Lock()
		defer finalSaving.Unlock()
		finalSaving.failure, finalSaving.surface, finalSaving.finishing = nil, false, false
		finalSaving.reporting, finalSaving.reportDone = false, nil
		finalSaving.durable = nil
	})
	armFinalSaving()
	finalSaving.Lock()
	finalSaving.surface = false
	finalSaving.Unlock()
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	flushFinalSaves = func(context.Context) error {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
			return errors.New("local disk full")
		}
		return nil
	}
	results := make(chan bool, 2)
	go func() { results <- finishFinalSaving(context.Background()) }()
	<-entered
	go func() { results <- finishFinalSaving(context.Background()) }()
	close(release)
	first, second := <-results, <-results
	if first || second || calls.Load() != 1 {
		t.Fatalf("results=%v,%v flushes=%d", first, second, calls.Load())
	}
}

// L4-H4e: an interrupt releases the sole reporter even when its flush is stalled.
func TestL4InterruptedFinalSavingUsesLocalReceipt(t *testing.T) {
	for _, safe := range []bool{false, true} {
		t.Run(map[bool]string{false: "local unconfirmed", true: "local confirmed"}[safe], func(t *testing.T) {
			oldFlush, oldOut := flushFinalSaves, os.Stdout
			out, err := os.CreateTemp(t.TempDir(), "receipt")
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			os.Stdout = out
			t.Cleanup(func() {
				flushFinalSaves, os.Stdout = oldFlush, oldOut
				finalSaving.Lock()
				defer finalSaving.Unlock()
				finalSaving.failure, finalSaving.surface, finalSaving.finishing = nil, false, false
				finalSaving.reporting, finalSaving.reportDone = false, nil
				finalSaving.durable = nil
			})
			armFinalSaving()
			finalSaving.Lock()
			finalSaving.address = "http://127.0.0.1:8901"
			finalSaving.Unlock()
			retainLocalDurability(func() bool { return safe })
			entered, release, workerDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
			defer func() { close(release); <-workerDone }()
			flushFinalSaves = func(context.Context) error {
				defer close(workerDone)
				close(entered)
				<-release
				return syncwords.UploadFailure(context.DeadlineExceeded)
			}
			result := make(chan bool, 1)
			go func() { result <- finishFinalSaving(context.Background()) }()
			<-entered
			interruptFinalSaving()
			select {
			case ok := <-result:
				if ok != safe {
					t.Fatalf("local=%v result=%v", safe, ok)
				}
			case <-time.After(time.Second):
				t.Fatal("interrupt still waits for the flush")
			}
			if got := finishFinalSaving(context.Background()); got != safe {
				t.Fatalf("follower result=%v local=%v", got, safe)
			}
			raw, err := os.ReadFile(out.Name())
			if err != nil {
				t.Fatal(err)
			}
			want := "saving on this computer has not finished; its written turns stay here\n"
			if safe {
				want = "saved here; it reaches your other devices when the sync service at 127.0.0.1:8901 answers\n"
			}
			if string(raw) != want {
				t.Fatalf("receipt=%q", raw)
			}
		})
	}
}
