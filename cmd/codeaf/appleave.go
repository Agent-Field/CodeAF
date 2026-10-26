package main

// ── the engine leaves when its app has gone, and leaves in bounded time ─────
//
// THE APP IS THE PROCESS THAT STARTED THIS ONE. Its pipe closing is how a quit
// or a crash is normally heard, but a pipe can outlive the app: another
// process that inherited its end keeps it open, and then this engine would
// serve this machine's factory, hold its election and its port, and keep its
// conversations for as long as that stranger lives. So the parent is watched
// too ([watchAppParent]): when the process that started this one has ended,
// the engine leaves exactly as it does when the pipe closes.
//
// LEAVING IS THE QUIT'S CAREFUL WRAP-UP, AND IT IS BOUNDED. Whatever starts it
// (the pipe, a shutdown request, a signal, the parent ending) the
// engine's Close runs: this machine's factory is let go first, its runner
// stopped as a quit stops it, then replies are written down and stopped and
// processes are ended. Nothing in that may hold the process forever:
// [leaveWithin] ends it [appLeaveWait] after the leaving began, and a second
// SIGTERM, SIGINT or SIGHUP ends it at once ([leave.On], the one road out every
// other door takes). An engine that swallowed every SIGTERM behind a wrap-up
// that never finished could only be ended by SIGKILL, and kept the next
// engine from serving the factory.
//
// No reply is carried on by an engine whose app has gone: the next engine
// carries it on (appquit.go, appclose.go). There is nothing to wait for here.

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	// appParentPoll is how often the engine looks whether the process that
	// started it is still there. It is one system call.
	appParentPoll = time.Second
	// appLeaveWait is how long the careful wrap-up may take before the process
	// ends regardless: the factory's runner and its service each get their own
	// stop wait, the conversations theirs, and a running process its grace.
	appLeaveWait = 2*appLocalStopWait + appReleaseWait + appQuitGraceS*time.Second
)

// watchAppParent calls gone once the process that started this one has ended,
// which shows as a different parent (the orphan's adopter). An engine started
// by the system itself has no app to outlive, and is not watched.
func watchAppParent(ctx context.Context, parent func() int, every time.Duration, gone func()) {
	first := parent()
	if first <= 1 {
		return
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if parent() != first {
				gone()
				return
			}
		}
	}
}

// leaveWithin waits for the engine to start leaving, by a signal or the
// app's going (ctx) or by its Close beginning (left), and calls end once wait
// has passed after that. end is reached only by a wrap-up that outran its
// bound.
//
// A WRAP-UP THAT FINISHED IS NOT ENDED. returned closes when the door that
// started this watch has done its wrap-up and returned; after that the process
// is on its way out through main, and there is nothing left to bound. Without
// it, the door's own deferred cancel started the clock as it returned, and a
// process that went on living after the door, as a test binary does, was
// ended thirty seconds later in the middle of whatever it was doing.
func leaveWithin(ctx context.Context, left, returned <-chan struct{}, wait time.Duration, end func()) {
	select {
	case <-ctx.Done():
	case <-left:
	case <-returned:
		return
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		end()
	case <-returned:
	}
}

// appLeaveNow is how the engine ends when its wrap-up outran [appLeaveWait].
func appLeaveNow() {
	fmt.Fprintf(os.Stderr, "codeaf app: still leaving after %s; ending now\n", appLeaveWait)
	os.Exit(1)
}

// ignoreBrokenPipes makes a write to a pipe whose reader has gone fail with
// EPIPE instead of ending the process. Go ends a program that writes to a
// broken stdout or stderr unless SIGPIPE is ignored; the app's pipes are both,
// and the engine outlives the app on every quit (appleave.go).
func ignoreBrokenPipes() { signal.Ignore(syscall.SIGPIPE) }
