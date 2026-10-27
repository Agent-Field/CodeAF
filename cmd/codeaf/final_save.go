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
	finalSaving.Lock()
	pending, forced := finalSaving.pending, finalSaving.forced
	finalSaving.pending = nil
	finalSaving.Unlock()
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
	finalSaving.Lock()
	err, surface := finalSaving.failure, finalSaving.surface
	finalSaving.failure, finalSaving.surface = nil, false
	finalSaving.Unlock()
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
