package main

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/syncwords"
)

// syncProgress says one delayed sentence only while a terminal is still waiting.
// The caller joins it before writing a result, keeping piped output unchanged.
func syncProgress(address string, terminal bool, out io.Writer, after func(time.Duration) <-chan time.Time) func() {
	if !terminal {
		return func() {}
	}
	stopped, done := make(chan struct{}), make(chan struct{})
	var once sync.Once
	guard.Go("sync/progress", func() {
		defer close(done)
		select {
		case <-stopped:
			return
		case <-after(2 * time.Second):
			select {
			case <-stopped:
				return
			default:
				fmt.Fprintln(out, syncwords.Asking(address))
			}
		}
	})
	return func() { once.Do(func() { close(stopped) }); <-done }
}

func terminalSyncProgress(address string) func() {
	return syncProgress(address, stdinIsTerminal(os.Stderr), os.Stderr, time.After)
}
