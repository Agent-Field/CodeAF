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
// has passed after that. A process that finished leaving sooner has already
// exited, so end is reached only by a wrap-up that outran its bound.
func leaveWithin(ctx context.Context, left <-chan struct{}, wait time.Duration, end func()) {
	select {
	case <-ctx.Done():
	case <-left:
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	<-timer.C
	end()
}

// appLeaveNow is how the engine ends when its wrap-up outran [appLeaveWait].
func appLeaveNow() {
	fmt.Fprintf(os.Stderr, "codeaf app: still leaving after %s; ending now\n", appLeaveWait)
	os.Exit(1)
}
