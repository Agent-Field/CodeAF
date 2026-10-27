package main

import (
	"context"
	"fmt"
	"os"
	"sync"
)

const quitSaveFailedWord = "codeaf could not save this chat; unsaved turns stay on this computer and return on the next start."

var finalSaving struct {
	sync.Mutex
	failure error
	surface bool
}

// Close can fail while the alternate screen still owns the terminal. Retain
// that result until all surface and process cleanup has returned.
func recordFinalSaveFailure(err error) {
	if err == nil {
		return
	}
	finalSaving.Lock()
	defer finalSaving.Unlock()
	if finalSaving.failure == nil {
		finalSaving.failure = err
	}
}

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
	fmt.Fprintln(terminal, quitSaveFailedWord)
	return false
}
