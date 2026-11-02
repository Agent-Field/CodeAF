package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/syncwords"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/session"
)

const quitSaveFailedWord = "codeaf could not save this chat; unsaved turns stay on this computer and return on the next start if this computer still holds the chat."
const quitSaveKeptWord = "codeaf could not save this chat; unsaved turns are kept in %s and return on the next start if this computer still holds the chat."
const quitSavingWord = "saving this chat to your other devices…"
const quitStillSavingWord = "still saving — ctrl+c leaves it saved here"
const quitStillLocalWord = "still saving on this computer — ctrl+c leaves the written turns here"
const quitLegacySavingWord = "codeaf is still saving this chat in the background; unsaved turns stay on this computer."
const quitSavedHereWord = "saved here; it reaches your other devices when the sync service at %s answers"

// finalSaving keeps one failure until cleanup has returned the terminal.
var finalSaving struct {
	sync.Mutex
	failure       error
	surface       bool
	finishing     bool
	address       string
	durable       []func() bool
	compatibility []func() bool
	reporting     bool
	reportDone    chan struct{}
	reportOK      bool
	interrupted   chan struct{}
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
	finalSaving.address = ""
	finalSaving.durable = nil
	finalSaving.compatibility = nil
	finalSaving.reporting = false
	finalSaving.reportDone = nil
	finalSaving.interrupted = make(chan struct{})
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
	for _, done := range pending {
	waiting:
		for {
			select {
			case <-done:
				break waiting
			case <-forced:
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

// interruptFinalSaving releases the reporter without waiting for a stalled flush.
func interruptFinalSaving() {
	finalSaving.Lock()
	defer finalSaving.Unlock()
	finalSaving.finishing = true
	if finalSaving.interrupted == nil {
		finalSaving.interrupted = make(chan struct{})
	}
	select {
	case <-finalSaving.interrupted:
	default:
		close(finalSaving.interrupted)
	}
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
	}
	if syncwords.UploadPending(err) {
		finalSaving.finishing = true
		if finalSaving.failure == nil {
			finalSaving.failure = err
		}
		return
	}
	previousUnanswered := errors.Is(finalSaving.failure, session.ErrSavingUnanswered)
	unanswered := errors.Is(err, session.ErrSavingUnanswered)
	if finalSaving.failure == nil || syncwords.UploadPending(finalSaving.failure) ||
		previousUnanswered && !unanswered ||
		!unanswered && unsavedTranscript(finalSaving.failure) == "" && unsavedTranscript(err) != "" {
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
func finishFinalSaving(ctx context.Context) (ok bool) {
	defer stopFinalSaveProgress()
	done, interrupted, alreadyFinishing, owner := beginFinalReport()
	if !owner {
		<-done
		return finalReportResult()
	}
	if !alreadyFinishing {
		flushCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		flushed := make(chan error, 1)
		flush := flushFinalSaves
		// Only this reporter owns the flush result. A late worker cannot change
		// a completed receipt or a later command's reporting generation.
		guard.Go("quit/flush", func() { flushed <- flush(flushCtx) })
		select {
		case err := <-flushed:
			recordFinalSaveFailure(err)
		case <-interrupted:
			select {
			case err := <-flushed:
				recordFinalSaveFailure(err)
			default:
			}
		case <-ctx.Done():
			markFinalSavingFinishing()
			select {
			case err := <-flushed:
				recordFinalSaveFailure(err)
			default:
			}
		}
	}
	err, surface, finishing, address, checks, legacy := takeFinalReport()
	defer func() { completeFinalReport(done, ok) }()
	if err == nil && !finishing {
		return true
	}
	safe := allLocalDurable(checks)
	if address == "" {
		address = syncsetup.Resolve(home.Dir()).URL
	}
	if syncwords.UploadPending(err) && surface && !safe {
		err = session.ErrSavingUnanswered
	}
	if finishing && (err == nil || errors.Is(err, session.ErrSavingUnanswered)) && safe {
		err = syncwords.UploadFailure(session.ErrSavingUnanswered)
	}
	stopFinalSaveProgress()
	line, ok := finalSaveWords(err, finishing, address)
	if legacy && (syncwords.UploadPending(err) || finishing && (err == nil || errors.Is(err, session.ErrSavingUnanswered))) {
		line, ok = quitLegacySavingWord, true
	}
	terminal := os.Stderr
	if surface {
		terminal = os.Stdout
	}
	if line != "" {
		fmt.Fprintln(terminal, line)
	}
	return ok
}

// finalSaveWords distinguishes durable local turns from a failed local write.
func finalSaveWords(err error, finishing bool, address string) (string, bool) {
	if syncwords.UploadPending(err) {
		return fmt.Sprintf(quitSavedHereWord, syncwords.Host(address)), true
	}
	if (err == nil && finishing) || errors.Is(err, session.ErrSavingUnanswered) {
		return "saving on this computer has not finished; its written turns stay here", false
	}
	if err == nil {
		return "", true
	}
	if transcript := unsavedTranscript(err); transcript != "" {
		return fmt.Sprintf(quitSaveKeptWord, transcript), false
	}
	return quitSaveFailedWord, false
}

// quitProgress has an injected clock because the three-second line is a UI contract.
type quitProgress struct {
	after func(time.Duration) <-chan time.Time
	say   func(string)
	stop  <-chan struct{}
	safe  func() bool
}

func (q quitProgress) run() {
	select {
	case <-q.stop:
		return
	case <-q.after(150 * time.Millisecond):
	}
	q.say(quitSavingWord)
	select {
	case <-q.stop:
		return
	case <-q.after(3 * time.Second):
		if q.safe != nil && !q.safe() {
			q.say(quitStillLocalWord)
		} else {
			q.say(quitStillSavingWord)
		}
	}
	<-q.stop
}

var finalProgress struct {
	sync.Mutex
	stop chan struct{}
	done chan struct{}
}

// startFinalSaveProgress runs only after the alternate screen has been restored.
// It covers all cleanup, including a closing link and the last local flush.
func startFinalSaveProgress() {
	address, checks, legacy := finalProgressState()
	if address == "" || strings.EqualFold(address, "off") {
		return
	}
	finalProgress.Lock()
	defer finalProgress.Unlock()
	if finalProgress.stop != nil {
		return
	}
	stop, done := make(chan struct{}), make(chan struct{})
	finalProgress.stop, finalProgress.done = stop, done
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	guard.Go("quit/interrupt", func() {
		defer signal.Stop(interrupt)
		select {
		case <-stop:
			return
		case <-interrupt:
			if legacy {
				surfaceForceExit(130)
				return
			}
			interruptFinalSaving()
			ok := finishFinalSaving(context.Background())
			if ok {
				surfaceForceExit(0)
			} else {
				surfaceForceExit(1)
			}
		}
	})

	guard.Go("quit/progress", func() {
		defer close(done)
		if legacy {
			return
		}
		q := quitProgress{safe: func() bool { return allLocalDurable(checks) }, after: time.After, say: func(line string) { fmt.Fprintln(os.Stdout, line) }, stop: stop}
		q.run()
	})
}

func stopFinalSaveProgress() {
	done := takeFinalProgress()
	if done != nil {
		<-done
	}
}

func retainLocalDurability(check func() bool) {
	finalSaving.Lock()
	defer finalSaving.Unlock()
	finalSaving.durable = append(finalSaving.durable, check)
}

// allLocalDurable requires every captured chat's positive local receipt.
func allLocalDurable(checks []func() bool) bool {
	if len(checks) == 0 {
		return false
	}
	for _, check := range checks {
		if !check() {
			return false
		}
	}
	return true
}

// prepareFinalReport starts a separate non-surface command without reusing a prior receipt.
func prepareFinalReport() {
	finalSaving.Lock()
	defer finalSaving.Unlock()
	if !finalSaving.surface {
		finalSaving.reporting = false
		finalSaving.reportDone = nil
		finalSaving.interrupted = make(chan struct{})
	}
}

func retainSavingCompatibility(check func() bool) {
	finalSaving.Lock()
	defer finalSaving.Unlock()
	finalSaving.compatibility = append(finalSaving.compatibility, check)
}

// legacySavingLocked preserves the baseline when any participating engine lacks receipts.
func legacySavingLocked() bool {
	for _, check := range finalSaving.compatibility {
		if !check() {
			return true
		}
	}
	return false
}

// beginFinalReport elects one reporter without holding the lock during I/O.
func beginFinalReport() (done, interrupted chan struct{}, finishing, owner bool) {
	finalSaving.Lock()
	defer finalSaving.Unlock()
	if finalSaving.reporting {
		return finalSaving.reportDone, nil, false, false
	}
	finalSaving.reporting = true
	finalSaving.reportDone = make(chan struct{})
	if finalSaving.interrupted == nil {
		finalSaving.interrupted = make(chan struct{})
	}
	return finalSaving.reportDone, finalSaving.interrupted, finalSaving.finishing, true
}
func finalReportResult() bool {
	finalSaving.Lock()
	defer finalSaving.Unlock()
	return finalSaving.reportOK
}
func completeFinalReport(done chan struct{}, ok bool) {
	finalSaving.Lock()
	defer finalSaving.Unlock()
	finalSaving.reportOK = ok
	close(done)
}
func takeFinalReport() (err error, surface, finishing bool, address string, checks []func() bool, legacy bool) {
	finalSaving.Lock()
	defer finalSaving.Unlock()
	err, surface, finishing, address = finalSaving.failure, finalSaving.surface, finalSaving.finishing, finalSaving.address
	checks = append([]func() bool(nil), finalSaving.durable...)
	legacy = legacySavingLocked()
	finalSaving.failure, finalSaving.surface, finalSaving.finishing = nil, false, false
	finalSaving.address, finalSaving.durable, finalSaving.compatibility = "", nil, nil
	return
}
func finalProgressState() (address string, checks []func() bool, legacy bool) {
	finalSaving.Lock()
	defer finalSaving.Unlock()
	return finalSaving.address, append([]func() bool(nil), finalSaving.durable...), legacySavingLocked()
}
func takeFinalProgress() chan struct{} {
	finalProgress.Lock()
	defer finalProgress.Unlock()
	stop, done := finalProgress.stop, finalProgress.done
	finalProgress.stop, finalProgress.done = nil, nil
	if stop != nil {
		close(stop)
	}
	return done
}
func setFinalSyncService(address string) {
	finalSaving.Lock()
	defer finalSaving.Unlock()
	finalSaving.address = address
}
