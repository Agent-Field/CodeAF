package main

// chatwall.go is `--max-hours` ending the PROCESS and not only the work.
//
// ── WHAT WAS TRUE ────────────────────────────────────────────────────────────
//
// The flag's help said "how many hours an unattended --yolo session may carry
// its own work on", and that is all it did: the session's wall reader
// (internal/session's wallclock.go) stops the work at the wall and writes the
// ending — and then the window sits on its composer, waiting for a person. In a
// rig, nobody is ever coming. On 2026-09-23 three `codeaf chat --no-host ...
// --max-hours 0.15` processes were found alive forty-three hours later: every
// one had stopped its work at nine minutes, and every one had then waited for a
// keystroke for two days, holding its journals, its model catalogue and its
// terminal.
//
// ── WHAT IS TRUE NOW ─────────────────────────────────────────────────────────
//
// A grace after the wall — long enough for the reader's settle tick, the stop,
// and the ending line to be on the screen and in the journal — this process asks
// itself to leave, exactly as a person's `kill` would: SIGTERM, which every
// door answers on its ordinary leaving road (the draft written, every
// conversation closed, every journal flushed; internal/leave). If that road has
// not finished a short while later, the second signal is sent, which the leave
// road answers by exiting at once. So a capped session cannot outlive its cap by
// more than [launchWallGrace] plus [launchWallForce], whoever is or is not
// watching.

import (
	"os"
	"syscall"
	"time"
)

// launchWallGrace is how long after the wall the window is left open: the
// session's reader needs a settle tick past the wall to stop the work and say
// so, and a person watching deserves to read the line.
const launchWallGrace = 2 * time.Minute

// launchWallForce is how long the ordinary leaving road is given before the
// second signal.
const launchWallForce = 30 * time.Second

// armLaunchWall schedules the two leaves and answers the function that cancels
// both — the door's own defer, so a window that closed first leaves nothing
// armed. after is time.AfterFunc in the product and a recorder in the test.
func armLaunchWall(wall time.Duration, after func(time.Duration, func()) *time.Timer, leave func()) func() {
	if wall <= 0 || after == nil || leave == nil {
		return func() {}
	}
	first := after(wall+launchWallGrace, leave)
	second := after(wall+launchWallGrace+launchWallForce, leave)
	return func() {
		if first != nil {
			first.Stop()
		}
		if second != nil {
			second.Stop()
		}
	}
}

// leaveThisProcess is a person's `kill` sent from inside: SIGTERM to this
// process, answered by whichever leaving road the door installed. A platform
// with no SIGTERM to send ends the process instead, because the one thing the
// cap may not do is nothing.
func leaveThisProcess() {
	self, err := os.FindProcess(os.Getpid())
	if err == nil && self.Signal(syscall.SIGTERM) == nil {
		return
	}
	os.Exit(1)
}
