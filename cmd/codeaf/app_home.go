package main

import (
	"fmt"
	"os"
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
	for {
		err := filelock.Lock(file, true, true)
		if err == nil {
			appHomeLock = file
			return nil
		}
		if !filelock.IsBusy(err) {
			_ = file.Close()
			return fmt.Errorf("hold codeaf app home: %w", err)
		}
		if time.Now().After(deadline) {
			_ = file.Close()
			return fmt.Errorf("another codeaf app is still closing this home after %s; nothing was started. Quit it, then open the app again", appHomeWait)
		}
		time.Sleep(appHomePoll)
	}
}
