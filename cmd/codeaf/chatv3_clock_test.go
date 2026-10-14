package main

import (
	"os"
	osexec "os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/home"
)

// THE REAL BINARY IS THE CLOCK. A reminder due a moment ago, one open window,
// and `codeaf clock` started the way a window starts it: the run is taken,
// said and recorded, and the clock leaves when asked to — with this package's
// HOME and CODEAF_HOME already moved to throwaway folders, so the old-timer
// removal every start does finds nothing of the developer's to touch.
func TestTheClockProcessSaysADueReminder(t *testing.T) {
	binary := buildCodeafStamped(t, "")
	state := t.TempDir()
	t.Setenv(home.EnvVar, state)
	root := automationsRoot()
	store, err := automation.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	reminder, err := store.Create(automation.Automation{
		Title: "leave", Workspace: t.TempDir(),
		Schedule: automation.Schedule{At: time.Now().Add(-time.Second)},
		Action:   automation.Action{Say: "time to leave"},
	})
	if err != nil {
		t.Fatal(err)
	}
	release, err := automation.NewPresence(root).Hold("test window")
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	clock := osexec.Command(binary, "clock")
	clock.Env = append(os.Environ(), home.EnvVar+"="+state, "HOME="+filepath.Join(state, "home"))
	if err := clock.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- clock.Wait() }()
	defer func() {
		_ = clock.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(20 * time.Second):
			_ = clock.Process.Kill()
			t.Error("the clock did not leave when asked")
		}
	}()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := store.Runs(reminder.ID, 5)
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) == 1 && runs[0].Phase == automation.PhaseOver {
			if runs[0].Outcome != automation.OutcomeDone || runs[0].Line != "time to leave" {
				t.Fatalf("the reminder's run reads %+v", runs[0])
			}
			if !automation.Held(root) {
				t.Fatal("the clock that said it does not hold the lock")
			}
			return
		}
		select {
		case err := <-exited:
			log, _ := os.ReadFile(filepath.Join(root, automation.LogName))
			t.Fatalf("the clock left before saying it: %v\n%s", err, log)
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("the reminder was never said")
}
