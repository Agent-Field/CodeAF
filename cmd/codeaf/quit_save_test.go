package main

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

type failedSaveAgent struct{}

func (failedSaveAgent) Close() error {
	return errors.New("snapshot: dial unix /missing.sock: connection refused")
}

// B7 puts the failure on the restored screen even when stderr is a log.
func TestQuitSaveFailureUsesTheRestoredSurfaceOutput(t *testing.T) {
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
	want := "codeaf could not save this chat; unsaved turns stay on this computer and return on the next start."
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
func TestQuitSaveFailureIsOnePlainSentence(t *testing.T) {
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
	want := "codeaf could not save this chat; unsaved turns stay on this computer and return on the next start.\n"
	if code == 0 || string(printed) != want {
		t.Fatalf("exit=%d terminal=%q", code, printed)
	}
}
