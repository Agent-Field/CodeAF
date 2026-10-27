package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/session"
)

const quitSaveFailedWord = "codeaf could not save this chat; unsaved turns stay on this computer and return on the next start."
const quitSaveKeptWord = "codeaf could not save this chat; unsaved turns are kept in %s and return on the next start."
const quitSavingWord = "saving this chat before quitting… (up to %s)"

// finalSaving keeps one failure until cleanup has returned the terminal.
var finalSaving struct {
	sync.Mutex
	failure error
	surface bool
	// pending keeps closing replies alive until the launcher can safely shut their links.
	pending []<-chan struct{}
	// forced bypasses cleanup after a second interrupt has restored the screen.
	forced chan struct{}
}

// armFinalSaving marks this process as one whose surface reports the final save
// on the restored terminal, and returns the channel a forced exit closes.
func armFinalSaving() chan struct{} {
	finalSaving.Lock()
	defer finalSaving.Unlock()
	finalSaving.surface = true
	finalSaving.forced = make(chan struct{})
	return finalSaving.forced
}

// takeFinalCloses hands the waiter every closing reply still owed, once.
func takeFinalCloses() ([]<-chan struct{}, chan struct{}) {
	finalSaving.Lock()
	defer finalSaving.Unlock()
	pending := finalSaving.pending
	finalSaving.pending = nil
	return pending, finalSaving.forced
}

// takeFinalSaveFailure hands back the retained failure, once, and whether a
// surface was running, which decides the terminal stream the sentence goes to.
func takeFinalSaveFailure() (error, bool) {
	finalSaving.Lock()
	defer finalSaving.Unlock()
	err, surface := finalSaving.failure, finalSaving.surface
	finalSaving.failure, finalSaving.surface = nil, false
	return err, surface
}

func retainFinalClose(done <-chan struct{}) {
	finalSaving.Lock()
	defer finalSaving.Unlock()
	kept := finalSaving.pending[:0]
	for _, previous := range finalSaving.pending {
		select {
		case <-previous:
		default:
			kept = append(kept, previous)
		}
	}
	finalSaving.pending = append(kept, done)
}

func forceFinalClose() {
	finalSaving.Lock()
	defer finalSaving.Unlock()
	if finalSaving.forced != nil {
		select {
		case <-finalSaving.forced:
		default:
			close(finalSaving.forced)
		}
	}
}

// awaitFinalCloses keeps the links open for saving replies after the screen closes.
func awaitFinalCloses(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, cellstore.SealWithin)
	defer cancel()
	pending, forced := takeFinalCloses()
	if len(pending) == 0 {
		return
	}
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	defer signal.Stop(interrupt)
	for _, done := range pending {
	waiting:
		for {
			select {
			case <-done:
				break waiting
			case <-timer.C:
				fmt.Fprintln(os.Stdout, fmt.Sprintf(quitSavingWord, cellstore.SealWithin))
			case <-forced:
				return
			case <-interrupt:
				surfaceForceExit(130)
				return
			case <-ctx.Done():
				recordFinalSaveFailure(ctx.Err())
				return
			}
		}
	}
}

// Close can fail while the alternate screen still owns the terminal. Retain
// that result until all surface and process cleanup has returned.
func recordFinalSaveFailure(err error) {
	if err == nil {
		return
	}
	finalSaving.Lock()
	defer finalSaving.Unlock()
	if finalSaving.failure == nil || (unsavedTranscript(finalSaving.failure) == "" && unsavedTranscript(err) != "") {
		finalSaving.failure = err
	}
}

// unsavedTranscript reads only the typed location supplied by the writer.
func unsavedTranscript(err error) string {
	var unsaved *session.UnsavedChatError
	if errors.As(err, &unsaved) {
		return unsaved.Transcript
	}
	return ""
}

// finishFinalSaving reports one plain sentence after the screen is restored.
func finishFinalSaving(ctx context.Context) bool {
	recordFinalSaveFailure(flushFinalSaves(ctx))
	err, surface := takeFinalSaveFailure()
	if err == nil {
		return true
	}
	terminal := os.Stderr
	if surface {
		terminal = os.Stdout
	}
	line := quitSaveFailedWord
	if transcript := unsavedTranscript(err); transcript != "" {
		line = fmt.Sprintf(quitSaveKeptWord, transcript)
	}
	fmt.Fprintln(terminal, line)
	return false
}
