//go:build !windows

package main

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// A FIFO in the workspace is stat'ed, never opened: opening it would wait for
// a writer that never comes. The alias spelling reaches it the same way.
func TestAppFileStatNeverOpensAFIFO(t *testing.T) {
	backend, agent, real, _ := aliasBackend(t, true)
	if err := syscall.Mkfifo(filepath.Join(real, "pipe"), 0o600); err != nil {
		t.Skipf("this filesystem cannot make a FIFO: %v", err)
	}
	done := make(chan appwire.FileStat, 1)
	go func() { done <- backend.statFile(agent.Workspace, nil, filepath.Join(real, "pipe")) }()
	select {
	case stat := <-done:
		if stat.State != appwire.FileExists || stat.Binary {
			t.Fatalf("the FIFO's stat = %+v", stat)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stat'ing a FIFO in the workspace waited for a writer")
	}
}
