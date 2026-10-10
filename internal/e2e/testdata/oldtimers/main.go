// Command oldtimers is the first thing every codeaf does on start, and nothing
// else: it takes the old feature's timers off the login it runs as
// (internal/automation's legacy.go).
//
// IT IS A PROGRAM OF ITS OWN FOR ONE TEST. The removal refuses to act inside a
// test binary, so internal/e2e's host guard cannot prove what it does by
// calling it; it builds this, runs it behind the guard with a throwaway HOME
// and stand-ins for launchctl and systemctl first on PATH, and reads what the
// stand-ins were asked (hostguard_test.go's
// TestTheOldTimersComeOffOnlyTheThrowawayLogin). It lives under testdata so
// that `go build ./...` and `go vet ./...` never meet it.
package main

import "github.com/Agent-Field/codeaf/internal/automation"

func main() { automation.RemoveOldTimers() }
