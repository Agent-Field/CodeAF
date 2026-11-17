package main

import (
	"net"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// AN OFFLINE TURN IS PAUSED, NOT LOST. A turn that never reached the model's
// service ends as offline with a person's sentence — never the transport's
// "after 0 retries: after 8 attempts: … connection refused" — and its words are
// kept beside its error entry, so after the app restarts the conversation still
// offers Retry message and a retry sends the same words.
func TestAppOfflineTurnIsSaidPlainlyAndItsRetryOutlivesARestart(t *testing.T) {
	r := newCloseRig(t, session.PresenceIdle, time.Minute)
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
	words := "Check new Toronto rental listings under $2,600"
	r.conv.mu.Lock()
	r.conv.script = []session.Event{{Kind: session.EventError, Err: refused}}
	r.conv.mu.Unlock()
	if _, err := r.b.Send(r.id, words, ""); err != nil {
		t.Fatal(err)
	}
	i := r.rec.waitFor(t, 10*time.Second, appTurnDone)
	r.rec.mu.Lock()
	done := r.rec.data[i].(appwire.TurnDone)
	r.rec.mu.Unlock()
	if done.OK || done.ErrorKind != appwire.ErrorOffline || done.Error != session.TurnOfflineWords {
		t.Fatalf("turn.done = %+v, want offline with %q", done, session.TurnOfflineWords)
	}
	var kept *appKeptFooter
	for _, f := range appRigFooters(t, r) {
		if f.Entry.Kind == appwire.EntryError && f.Entry.ErrorKind == appwire.ErrorOffline {
			f := f
			kept = &f
		}
	}
	if kept == nil || kept.Retry != words {
		t.Fatalf("the offline error was not kept with its words: %+v", appRigFooters(t, r))
	}
	// The session writes the person's message to the record; this rig's fake
	// conversation does not, so write it as the real one would have.
	n, _ := appErrorEntryTurn(kept.Entry.ID)
	for appUserCount(r.file) <= n {
		appendRigUser(t, r.file, words)
	}

	// The app restarts: a fresh backend over the same folder.
	r.backend(t)
	a, err := r.b.find(r.id)
	if err != nil {
		t.Fatal(err)
	}
	var retryable bool
	r.b.locked(func() { retryable = a.view.Retryable })
	if !retryable {
		t.Fatal("after a restart the offline message is no longer offered again")
	}
	r.conv.mu.Lock()
	r.conv.script = []session.Event{{Kind: session.EventTurnDone}}
	r.conv.texts = nil
	r.conv.mu.Unlock()
	if _, err := r.b.Retry(r.id); err != nil {
		t.Fatalf("retry after restart = %v", err)
	}
	r.rec.waitFor(t, 10*time.Second, appTurnDone)
	r.conv.mu.Lock()
	sent := append([]string(nil), r.conv.texts...)
	r.conv.mu.Unlock()
	if len(sent) != 1 || sent[0] != words {
		t.Fatalf("the retry sent %q, want the kept words", sent)
	}
}

// A FAILURE A NEWER MESSAGE ANSWERED IS NOT OFFERED AGAIN. The kept words
// count only while their error is under the person's newest message.
func TestAppKeptFailureIsNotOfferedOnceANewerMessageWasSent(t *testing.T) {
	r := newCloseRig(t, session.PresenceIdle, time.Minute)
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
	appRigSend(t, r, "first try", []session.Event{{Kind: session.EventError, Err: refused}})
	appendRigUser(t, r.file, "first try")
	appendRigUser(t, r.file, "something newer")
	appendRigUser(t, r.file, "and newer still")
	if words, _ := appRecoverFailed(r.file); words != "" {
		t.Fatalf("a failure under an older message is offered again: %q", words)
	}
}

func appendRigUser(t *testing.T, file, words string) {
	t.Helper()
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(`{"type":"message","role":"user","content":"` + words + `","timestamp":"2026-10-19T16:31:00Z"}` + "\n"); err != nil {
		t.Fatal(err)
	}
}
