//go:build !windows

// K6 keeps Unix process, lock and mode assertions on the systems that provide them.
package executor

import (
	"context"
	"os/exec"
	"syscall"
	"testing"
)

func TestAShellCallTellsTheObserverWhatItLeftRunning(t *testing.T) {
	var got []ExecResult
	seat := Watching(plainSeat{}, observerFunc(func(_ ExecRequest, res ExecResult) { got = append(got, res) }))
	ctx, spawns := WithSpawns(context.Background())
	call := shellCall("sleep 30 &")
	call.Spawns = spawns
	err := seat.Around(ctx, call, func() ([]byte, bool) {
		cmd := exec.Command("sleep", "30")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() })
		Spawned(ctx, cmd.Process.Pid)
		return nil, false
	})
	if err != nil {
		t.Fatal(err)
	}
	last := got[len(got)-1]
	if len(last.Services) != 1 || last.Services[0].Name != "sleep" {
		t.Fatalf("the left-running sleep was not observed: %+v", got)
	}
}
