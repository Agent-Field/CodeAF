package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/session"
)

const quitSaveFailedWord = "codeaf could not save this chat; unsaved turns stay on this computer and return on the next start if this computer still holds the chat."
const quitSaveKeptWord = "codeaf could not save this chat; unsaved turns are kept in %s and return on the next start if this computer still holds the chat."
const quitStillSavingWord = "codeaf is still saving this chat in the background; unsaved turns stay on this computer."

// finalSaving keeps one failure until cleanup has returned the terminal.
var finalSaving struct {
	sync.Mutex
	failure   error
	surface   bool
	finishing bool
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
	finalSaving.finishing = false
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
func takeFinalSaveFailure() (error, bool, bool) {
	finalSaving.Lock()
	defer finalSaving.Unlock()
	err, surface, finishing := finalSaving.failure, finalSaving.surface, finalSaving.finishing
	finalSaving.failure, finalSaving.surface = nil, false
	finalSaving.finishing = false
	return err, surface, finishing
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
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	defer signal.Stop(interrupt)
	for _, done := range pending {
	waiting:
		for {
			select {
			case <-done:
				break waiting
			case <-forced:
				return
			case <-interrupt:
				surfaceForceExit(130)
				return
			case <-ctx.Done():
				markFinalSavingFinishing()
				return
			}
		}
	}
}

// markFinalSavingFinishing records that the wait ended while a save was still
// under way, so the quit reports it as finishing rather than as failed.
func markFinalSavingFinishing() {
	finalSaving.Lock()
	defer finalSaving.Unlock()
	finalSaving.finishing = true
}

// finalSavingFinishing reports whether a save was left finishing in the
// background, in which case no second, competing flush is started.
func finalSavingFinishing() bool {
	finalSaving.Lock()
	defer finalSaving.Unlock()
	return finalSaving.finishing
}

// Close can fail while the alternate screen still owns the terminal. Retain
// that result until all surface and process cleanup has returned.
func recordFinalSaveFailure(err error) {
	if err == nil {
		return
	}
	finalSaving.Lock()
	defer finalSaving.Unlock()
	if errors.Is(err, session.ErrSavingUnanswered) {
		finalSaving.finishing = true
		return
	}
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
	if !finalSavingFinishing() {
		recordFinalSaveFailure(flushFinalSaves(ctx))
	}
	err, surface, finishing := takeFinalSaveFailure()
	if err == nil && !finishing {
		return true
	}
	terminal := os.Stderr
	if surface {
		terminal = os.Stdout
	}
	if err == nil {
		fmt.Fprintln(terminal, quitStillSavingWord)
		return true
	}
	line := quitSaveFailedWord
	if transcript := unsavedTranscript(err); transcript != "" {
		line = fmt.Sprintf(quitSaveKeptWord, transcript)
	}
	fmt.Fprintln(terminal, line)
	return false
}
