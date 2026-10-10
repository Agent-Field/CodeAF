package automation

// process.go is a window's half of the clock: hold presence for as long as the
// window is open, and make sure a clock is running — now, and every so often
// after, so a clock that exited (a replaced build, a crash) is replaced while
// any window lives.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// keepEvery is how often an open window checks that a clock is running. A
// variable so a test need not wait; nothing in the product writes it.
var keepEvery = 30 * time.Second

// LogName is the clock's own log inside the store's root: what a clock that
// died at birth said on its way out, and what a running clock could not do.
const LogName = "clock.log"

// StartClock starts `binary args…` — the clock command — detached, unless a
// clock already holds the lock. Two windows racing both start one; the second
// finds the lock held and leaves, which is the lock doing its job.
//
// THE CLOCK INHERITS THIS WINDOW'S ENVIRONMENT — its keys, its PATH, its zone —
// because it is this person's work done while this window is open.
func StartClock(root, binary string, args ...string) error {
	if Held(root) {
		return nil
	}
	if !Startable(binary) {
		return errors.New("automations: not a codeaf that can run a clock")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer null.Close()
	stderr := any(null)
	if log, err := os.OpenFile(filepath.Join(root, LogName), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		defer log.Close()
		stderr = log
	}
	command := exec.Command(binary, args...)
	command.Stdin, command.Stdout = null, null
	command.Stderr = stderr.(*os.File)
	processgroup.ConfigureDetached(command)
	if err := command.Start(); err != nil {
		return err
	}
	// The clock is nobody's to wait for: it outlives the window that started it
	// whenever another window is still open.
	return command.Process.Release()
}

// Startable reports whether binary is a codeaf that can run a clock. A test
// binary is not: started with the clock's arguments it would run its tests,
// detached, in the background.
func Startable(binary string) bool {
	if strings.TrimSpace(os.Getenv("CODEAF_NO_AUTOMATIONS")) != "" {
		return false
	}
	base := filepath.Base(binary)
	return base != "" && !strings.HasSuffix(base, ".test") && !strings.HasSuffix(base, ".test.exe")
}

// Keep holds one open window until release is called, and keeps a clock
// running while it does: start is called now and every keepEvery after.
func Keep(root, label string, start func() error) (release func(), err error) {
	presence := NewPresence(root)
	letGo, err := presence.Hold(label)
	if err != nil {
		return func() {}, err
	}
	stop := make(chan struct{})
	var once sync.Once
	go func() {
		if start != nil {
			_ = start()
		}
		ticker := time.NewTicker(keepEvery)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if start != nil {
					_ = start()
				}
			}
		}
	}()
	return func() {
		once.Do(func() {
			close(stop)
			letGo()
		})
	}, nil
}
