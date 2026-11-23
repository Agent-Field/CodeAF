package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

// C3.2: when a save at quit really fails, the underlying error text is in the
// chat log, with the transcript it names, once, and not only in the sentence
// the terminal prints.
func TestAFinalSaveFailureIsWrittenToTheChatLogOnce(t *testing.T) {
	profile := t.TempDir()
	t.Setenv(config.ProfileDirEnv, profile)
	finalSaving.Lock()
	finalSaving.failure, finalSaving.finishing = nil, false
	finalSaving.Unlock()
	loggedFinalSaves.Lock()
	loggedFinalSaves.seen = nil
	loggedFinalSaves.Unlock()
	t.Cleanup(func() {
		finalSaving.Lock()
		finalSaving.failure, finalSaving.finishing = nil, false
		finalSaving.Unlock()
	})

	cause := errors.New("the engine said: disk exploded while sealing")
	failure := session.SavingError("/chats/one/.cell/transcript.jsonl", cause)
	recordFinalSaveFailure(failure)
	recordFinalSaveFailure(failure) // the same failure reaching the quit by a second road

	raw, err := os.ReadFile(chatLogPath(profile))
	if err != nil {
		t.Fatalf("the failure left no chat log: %v", err)
	}
	log := string(raw)
	if got := strings.Count(log, cause.Error()); got != 1 {
		t.Fatalf("the cause is in the chat log %d times, want once:\n%s", got, log)
	}
	if !strings.Contains(log, "/chats/one/.cell/transcript.jsonl") {
		t.Fatalf("the chat log does not name the transcript:\n%s", log)
	}
	// And the sentence is the one the terminal always printed.
	err, _, finishing := takeFinalSaveFailure()
	line, ok := finalSaveWords(err, finishing, "")
	if ok || !strings.HasPrefix(line, "codeaf could not save this chat; unsaved turns are kept in /chats/one/.cell/transcript.jsonl") {
		t.Fatalf("the quit sentence changed: %v %q", ok, line)
	}
}

// C3.2: a failure with no transcript location is logged too, and logging never
// touches the terminal the sentence goes to.
func TestAFinalSaveFailureWithNoLocationIsLoggedWithoutTouchingTheTerminal(t *testing.T) {
	profile := t.TempDir()
	t.Setenv(config.ProfileDirEnv, profile)
	loggedFinalSaves.Lock()
	loggedFinalSaves.seen = nil
	loggedFinalSaves.Unlock()
	oldErr := os.Stderr
	out, err := os.CreateTemp(t.TempDir(), "terminal")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	os.Stderr = out
	t.Cleanup(func() {
		os.Stderr = oldErr
		finalSaving.Lock()
		defer finalSaving.Unlock()
		finalSaving.failure, finalSaving.surface, finalSaving.finishing = nil, false, false
		finalSaving.reporting, finalSaving.reportDone = false, nil
		finalSaving.durable = nil
	})
	oldFlush := flushFinalSaves
	t.Cleanup(func() { flushFinalSaves = oldFlush })
	flushFinalSaves = func(context.Context) error { return errors.New("local disk full at flush") }
	armFinalSaving()
	finalSaving.Lock()
	finalSaving.surface = false
	finalSaving.Unlock()
	if finishFinalSaving(context.Background()) {
		t.Fatal("a failed flush was reported saved")
	}
	raw, err := os.ReadFile(chatLogPath(profile))
	if err != nil || !strings.Contains(string(raw), "local disk full at flush") {
		t.Fatalf("the flush failure is not in the chat log: %v %q", err, raw)
	}
	terminal, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(terminal), "local disk full at flush") || strings.Count(string(terminal), "codeaf could not save this chat") != 1 {
		t.Fatalf("the terminal got the cause or lost the sentence: %q", terminal)
	}
}

// C3.1: a window whose drive side never started knows it only shows a chat when
// its local gate saw another computer hold it; a window that was never refused
// does not.
func TestALateStoreViewsOnlyWhenTheLocalGateSawAnotherHolder(t *testing.T) {
	seen := &liveDrive{localGate: &localToolGate{line: "live-pc continued this chat. This copy is read-only and may be behind"}}
	if !(lateStore{live: seen}).Viewing() {
		t.Fatal("a window whose gate saw another holder does not know it only shows the chat")
	}
	free := &liveDrive{localGate: &localToolGate{}}
	if (lateStore{live: free}).Viewing() {
		t.Fatal("a window nothing refused thinks it only shows the chat")
	}
	if (lateStore{live: &liveDrive{}}).Viewing() {
		t.Fatal("a window with no gate thinks it only shows the chat")
	}
}
