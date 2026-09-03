// Package leave is the one road out of a process somebody outside it asked to
// stop. It keeps the first signal on the ordinary leaving road and reserves a
// second signal for the person who cannot wait for that road to finish.
package leave

import (
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

// leavingSignals names the outside stops this process can turn into an orderly
// leave. os.Interrupt is SIGINT: `kill -INT`, or ^C at a terminal that is not
// in raw mode. SIGTERM is a `kill` with no flag and what a service manager sends
// to stop something. SIGHUP is a terminal window closing or the ssh session a
// chat was running in going away. SIGKILL and SIGSTOP are deliberately absent
// because they cannot be caught at all; that limit is also why a second caught
// signal forces the exit itself instead of being handed back to a disposition
// that would do it.
var leavingSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}

// terminalHandBackGrace bounds tidying the terminal because a person who asks
// twice to leave must not be trapped behind a hand-back that has itself hung.
const terminalHandBackGrace = 100 * time.Millisecond

// exit is private so the behaviour test can observe the forced status without
// ending its own test process.
var exit = os.Exit

// On routes every leaving signal to leaving, once, and answers a SECOND one by
// ending the process at once. handBack may be nil; it is where a door that owns
// the terminal puts it back before the process goes. The returned function
// stops listening and puts the signals' own disposition back.
func On(leaving func(), handBack func()) (stop func()) {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, leavingSignals...)

	done := make(chan struct{})
	var standing atomic.Bool
	standing.Store(true)
	standDown := func() bool {
		if !standing.CompareAndSwap(true, false) {
			return false
		}
		// Stop waits for signal's sends to finish before done lets either
		// watcher go. That order keeps a channel with no reader from remaining
		// registered during or after the hand-back.
		signal.Stop(signals)
		close(done)
		return true
	}

	go func() {
		select {
		case <-signals:
		case <-done:
			return
		}

		// THE SECOND WATCH IS ARMED BEFORE LEAVING STARTS. Leaving is allowed
		// to block while sessions and their work settle, and the second signal
		// is precisely the door out when that wait will not finish.
		go func() {
			select {
			case next := <-signals:
				if standDown() {
					force(next, handBack)
				}
			case <-done:
			}
		}()
		leaving()
	}()

	return func() {
		standDown()
	}
}

// force gives a terminal-owning door one bounded chance to put the screen
// back, then uses the shell's own signal status whether that chance returned
// or not.
func force(received os.Signal, handBack func()) {
	if handBack != nil {
		handedBack := make(chan struct{})
		go func() {
			handBack()
			close(handedBack)
		}()

		grace := time.NewTimer(terminalHandBackGrace)
		defer grace.Stop()
		select {
		case <-handedBack:
		case <-grace.C:
		}
	}
	exit(signalExitStatus(received))
}

// signalExitStatus is the single statement of the shell's rule for a process
// that was ended by a signal. When the signal's platform number cannot be
// read, bare 128 still says it ended on a signal: the shell's range starts
// there.
func signalExitStatus(received os.Signal) int {
	number, ok := received.(syscall.Signal)
	if !ok {
		return 128
	}
	return 128 + int(number)
}
