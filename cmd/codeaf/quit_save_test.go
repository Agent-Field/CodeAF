package main

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

type failedSaveAgent struct{}

func (failedSaveAgent) Close() error {
	return session.SavingError("/temporary/chat/transcript.jsonl", errors.New("snapshot: dial unix /missing.sock: connection refused"))
}

// B7 puts the failure on the restored screen even when stderr is a log.
// L4-H4c preserves the nonzero receipt for a real local saving failure.
func TestL4QuitSaveFailureUsesTheRestoredSurfaceOutput(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Cleanup(cell.UseAvailabilityInTests(false))
	oldRun, oldFlush := runSurfaceProgram, flushFinalSaves
	oldOut, oldErr := os.Stdout, os.Stderr
	out, err := os.CreateTemp(t.TempDir(), "screen")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	errs, err := os.CreateTemp(t.TempDir(), "errors")
	if err != nil {
		t.Fatal(err)
	}
	defer errs.Close()
	os.Stdout, os.Stderr = out, errs
	t.Cleanup(func() {
		runSurfaceProgram, flushFinalSaves = oldRun, oldFlush
		os.Stdout, os.Stderr = oldOut, oldErr
	})
	runSurfaceProgram = func(context.Context, tui3.Options) error {
		closeV3Agent(failedSaveAgent{})
		closeV3Agent(failedSaveAgent{})
		_, err := io.WriteString(os.Stdout, "screen restored\n")
		return err
	}
	if err := runSurface(context.Background(), tui3.Options{ProfileDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	flushFinalSaves = func(context.Context) error { return failedSaveAgent{}.Close() }
	defer telemetryArgs("--version")()
	if code := execute(); code == 0 {
		t.Fatal("failed save exited successfully")
	}
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	printed, err := io.ReadAll(out)
	if err != nil {
		t.Fatal(err)
	}
	want := "codeaf could not save this chat; unsaved turns are kept in /temporary/chat/transcript.jsonl and return on the next start if this computer still holds the chat."
	if strings.Count(string(printed), want) != 1 || !strings.HasSuffix(string(printed), want+"\n") || !strings.HasPrefix(string(printed), "screen restored\n") {
		t.Fatalf("restored terminal: %q", printed)
	}
	if _, err := errs.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(errs)
	if err != nil || len(raw) != 0 {
		t.Fatalf("error log=%q read=%v", raw, err)
	}
}

// B7 reports a final save failure once without exposing the engine's error.
// L4-H4c keeps local failure words distinct from an upload-only receipt.
func TestL4QuitSaveFailureIsOnePlainSentence(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	finalSaving.Lock()
	finalSaving.surface = false
	finalSaving.Unlock()
	oldFlush := flushFinalSaves
	defer func() { flushFinalSaves = oldFlush }()
	flushFinalSaves = func(context.Context) error {
		return errors.New("seal: snapshot: engine unavailable: dial unix /missing.sock: no such file")
	}
	defer telemetryArgs("--version")()
	oldErr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = oldErr; r.Close() }()
	code := execute()
	w.Close()
	printed, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	want := "codeaf could not save this chat; unsaved turns stay on this computer and return on the next start if this computer still holds the chat.\n"
	if code == 0 || string(printed) != want {
		t.Fatalf("exit=%d terminal=%q", code, printed)
	}
}

// Contract D2 waits for the actual saving reply after restoring the screen.
func TestQuitKeepsALateSaveFailureAfterTheScreenIsRestored(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Cleanup(cell.UseAvailabilityInTests(false))
	oldRun, oldFlush, oldOut := runSurfaceProgram, flushFinalSaves, os.Stdout
	out, err := os.CreateTemp(t.TempDir(), "screen")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	os.Stdout = out
	t.Cleanup(func() { runSurfaceProgram, flushFinalSaves, os.Stdout = oldRun, oldFlush, oldOut })
	release, saved, restored := make(chan struct{}), make(chan struct{}), make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
		<-saved
	}()
	runSurfaceProgram = func(_ context.Context, options tui3.Options) error {
		if options.ClosingStarted != nil {
			options.ClosingStarted(saved)
		}
		go func() {
			defer close(saved)
			<-release
			options.SaveFailure(failedSaveAgent{}.Close())
		}()
		_, err := io.WriteString(os.Stdout, "screen restored\n")
		close(restored)
		return err
	}
	profile := t.TempDir()
	finished := make(chan error, 1)
	go func() { finished <- runSurface(context.Background(), tui3.Options{ProfileDir: profile}) }()
	<-restored
	select {
	case err := <-finished:
		t.Fatalf("quit returned %v before the host's saving reply", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	released = true
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	flushFinalSaves = func(context.Context) error { return nil }
	defer telemetryArgs("--version")()
	if code := execute(); code == 0 {
		t.Fatal("the late failed save exited successfully")
	}
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	printed, err := io.ReadAll(out)
	if err != nil {
		t.Fatal(err)
	}
	want := "codeaf could not save this chat; unsaved turns are kept in /temporary/chat/transcript.jsonl and return on the next start if this computer still holds the chat.\n"
	if !strings.HasPrefix(string(printed), "screen restored\n") || !strings.HasSuffix(string(printed), want) || strings.Count(string(printed), want) != 1 {
		t.Fatalf("late saving report = %q", printed)
	}
}

// Contract D3 lets a second interrupt end the process after restoring its screen.
func TestSecondQuitInterruptSkipsStuckCleanupAfterRestoringScreen(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Cleanup(cell.UseAvailabilityInTests(false))
	oldRun, oldExit := runSurfaceProgram, surfaceForceExit
	t.Cleanup(func() {
		runSurfaceProgram, surfaceForceExit = oldRun, oldExit
		finalSaving.Lock()
		finalSaving.surface, finalSaving.forced = false, nil
		finalSaving.Unlock()
	})
	restored, exitCode := false, 0
	surfaceForceExit = func(code int) {
		if !restored {
			t.Fatal("the forced exit ran before restoring the terminal")
		}
		exitCode = code
	}
	runSurfaceProgram = func(_ context.Context, options tui3.Options) error {
		options.ClosingStarted(make(chan struct{}))
		options.ForceQuit()
		restored = true
		return nil
	}
	if err := runSurface(context.Background(), tui3.Options{ProfileDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if exitCode != 130 {
		t.Fatalf("the second interrupt did not end the process: exit=%d", exitCode)
	}
}
