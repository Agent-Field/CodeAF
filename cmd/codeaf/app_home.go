package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/home"
)

// appHomeLock stays reachable until process death. Closing it when runApp
// returns would admit a new engine while main still finishes saving. The OS
// releases it on any exit, so a crash leaves nothing to clean up.
var appHomeLock *os.File

// appHomeWait is how long an engine waits for one that is closing the same home.
// It outlasts the longest careful wrap-up, so an update's replacement engine
// starts as soon as the old one ends.
var appHomeWait = appLeaveWait + 10*time.Second

const appHomePoll = 50 * time.Millisecond

// holdAppHome makes this the one engine on this home. Two engines would both
// spend on the same work. It runs before anything that could make a paid call.
func holdAppHome() error {
	if err := os.MkdirAll(home.Dir(), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(home.Join("app.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(appHomeWait)
	said := false
	for {
		err := filelock.Lock(file, true, true)
		if err == nil {
			appHomeLock = file
			// THE HOLDER WRITES ITS PID INTO THE LOCK, so an engine started
			// while this one is still closing can say whom it is waiting for.
			// The lock itself is the OS's and is never judged by this number;
			// a failed write only leaves the waiter unable to name the holder.
			if file.Truncate(0) == nil {
				_, _ = file.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
			}
			return nil
		}
		if !filelock.IsBusy(err) {
			_ = file.Close()
			return fmt.Errorf("hold codeaf app home: %w", err)
		}
		// A WAITING ENGINE SAYS SO, ONCE. The sixth live A+B run's restart
		// spent two silent fifteen-second starts behind an engine that was
		// still leaving; this line is what tells the log, and the app reading
		// it, that the start is waiting and not wedged.
		if !said {
			said = true
			fmt.Fprintln(os.Stderr, appHomeWaitingLine(file))
		}
		if time.Now().After(deadline) {
			_ = file.Close()
			return fmt.Errorf("another codeaf app is still closing this home after %s; nothing was started. Quit it, then open the app again", appHomeWait)
		}
		time.Sleep(appHomePoll)
	}
}

// appHomeWaitingLine is the one stderr line an engine writes while another
// engine still holds its home, naming that engine's pid when the lock says.
func appHomeWaitingLine(lock *os.File) string {
	raw := make([]byte, 32)
	n, _ := lock.ReadAt(raw, 0)
	if pid, err := strconv.Atoi(strings.TrimSpace(string(raw[:n]))); err == nil && pid > 0 {
		return fmt.Sprintf("codeaf app: waiting for the engine still closing this home (pid %d); it starts the moment that one has gone", pid)
	}
	return "codeaf app: waiting for the engine still closing this home; it starts the moment that one has gone"
}
