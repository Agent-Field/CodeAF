//go:build unix

package session

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// A FIFO WHERE THE PROGRAM RECORD GOES NEVER HOLDS THE APP'S READ: the task
// read and a page of older actions both answer at once, with no record — not
// once the FIFO is opened for writing.
func TestAFIFOProgramRecordDoesNotHoldTheAppsRead(t *testing.T) {
	agent, dir := programAppFixture(t)
	view := appTaskOf(t, agent)
	path := filepath.Join(plandb.TaskDir(dir, "alpha"), delegate.ProgramFile)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan *AppProgramView, 1)
	go func() {
		got, _, _ := agent.AppTask(view.ID)
		_, _, _ = agent.AppProgramActions(view.ID, fixtureLaunch+".1")
		done <- got.Program
	}()
	select {
	case program := <-done:
		if program != nil && program.Read != AppProgramReadUnknown {
			t.Fatalf("a FIFO record was read as %+v", program)
		}
	case <-time.After(20 * time.Second):
		// Free the blocked reader before failing, so the test binary can leave.
		if fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			_ = syscall.Close(fd)
		}
		t.Fatal("the app's read waited on a FIFO program record")
	}
}
