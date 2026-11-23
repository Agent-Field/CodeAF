package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/syncwords"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// L4-R4: An interrupted close with a legacy welcome keeps the baseline background-saving success.
func TestL4FixLegacyInterruptWords(t *testing.T) {
	oldFlush, oldOut := flushFinalSaves, os.Stdout
	out, err := os.CreateTemp(t.TempDir(), "receipt")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	os.Stdout = out
	armFinalSaving()
	t.Cleanup(func() { flushFinalSaves, os.Stdout = oldFlush, oldOut; takeFinalSaveFailure() })
	// The remote client has an old engine's welcome, with no optional capability.
	client := &remote.Client{}
	agent := client.Agent()
	checks := []func() bool{agent.LocalDurable}
	for _, check := range checks {
		retainLocalDurability(check)
	}
	// Surface assembly must capture compatibility alongside durability.
	// The optional seam is discovered here so this test compiles on the pre-fix tree.
	legacy, ok := any(agent).(interface{ SavingReceipts() bool })
	if !ok {
		t.Fatal("no negotiated legacy saving mode")
	}
	if legacy.SavingReceipts() {
		t.Fatal("legacy engine advertised receipts")
	}
	// The launcher captures the engine's negotiated mode through its surface options.
	retainSavingCompatibility(legacy.SavingReceipts)
	interruptFinalSaving()
	if !finishFinalSaving(context.Background()) {
		t.Fatal("legacy interrupt changed the baseline exit status")
	}
	raw, _ := os.ReadFile(out.Name())
	if strings.TrimSpace(string(raw)) != "codeaf is still saving this chat in the background; unsaved turns stay on this computer." {
		t.Fatalf("receipt=%s", raw)
	}
}

// L4-R4: A legacy forced interrupt keeps the baseline exit code after restoration.
func TestL4FixLegacyForcedInterrupt(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Cleanup(cell.UseAvailabilityInTests(false))
	oldRun, oldExit := runSurfaceProgram, surfaceForceExit
	defer func() {
		runSurfaceProgram, surfaceForceExit = oldRun, oldExit
		stopFinalSaveProgress()
		takeFinalSaveFailure()
	}()
	restored := false
	code := 0
	surfaceForceExit = func(got int) {
		if !restored {
			t.Error("exit before terminal restoration")
		}
		code = got
	}
	runSurfaceProgram = func(_ context.Context, o tui3.Options) error {
		o.SavingCompatibility(func() bool { return false })
		o.ForceQuit()
		restored = true
		return nil
	}
	if err := runSurface(context.Background(), tui3.Options{ProfileDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if code != 130 {
		t.Fatalf("legacy interrupt exit=%d", code)
	}
}

// L4-R5: The terminal reports the idle chat's pending upload after the other chat detached successfully.
func TestL4FixTwoChatQuitReceipt(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Cleanup(cell.UseAvailabilityInTests(false))
	oldRun, oldFlush, oldOut := runSurfaceProgram, flushFinalSaves, os.Stdout
	out, err := os.CreateTemp(t.TempDir(), "receipt")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	os.Stdout = out
	defer func() {
		runSurfaceProgram, flushFinalSaves, os.Stdout = oldRun, oldFlush, oldOut
		stopFinalSaveProgress()
		takeFinalSaveFailure()
	}()
	flushFinalSaves = func(context.Context) error { return nil }
	runSurfaceProgram = func(_ context.Context, o tui3.Options) error {
		// These are the two per-chat outcomes exercised by tui3's two-chat departure regression.
		o.SavingDurability(func() bool { return true })
		o.SavingDurability(func() bool { return true })
		o.SaveFailure(syncwords.UploadFailure(context.DeadlineExceeded))
		return nil
	}
	if err := runSurface(context.Background(), tui3.Options{ProfileDir: t.TempDir(), SyncService: "http://127.0.0.1:8901"}); err != nil {
		t.Fatal(err)
	}
	if !finishFinalSaving(context.Background()) {
		t.Fatal("a successful detach poisoned the idle chat's upload receipt")
	}
	raw, _ := os.ReadFile(out.Name())
	if strings.TrimSpace(string(raw)) != "saved here; it reaches your other devices when the sync service at 127.0.0.1:8901 answers" {
		t.Fatalf("receipt=%s", raw)
	}
}
