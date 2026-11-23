//go:build !windows

package bare

import (
	"context"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// A call that has already returned is not killed by the cancellation that
// follows it. Both of the watcher's cases are ready when the call's own context
// is cancelled just behind its return; the group it left running must survive
// whichever the watcher is handed.
func TestACallThatReturnedLeavesItsBackgroundProcessAlone(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pgid := cmd.Process.Pid
	// The process is reaped as soon as it dies, so a killed group reads as gone.
	reaped := make(chan struct{})
	go func() { _ = cmd.Wait(); close(reaped) }()
	t.Cleanup(func() { _ = syscall.Kill(-pgid, syscall.SIGKILL); <-reaped })

	for i := 0; i < 200; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		over := make(chan struct{})
		cancel()
		close(over)
		watchCancel(ctx, newBashCall("sleep 30", cmd, nil, ctx), over)
	}
	time.Sleep(50 * time.Millisecond)
	if !processgroup.Alive(pgid) {
		t.Fatal("the cancellation after a finished call killed the group it left running")
	}
}
