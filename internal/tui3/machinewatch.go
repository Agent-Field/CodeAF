package tui3

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/dirwatch"
)

// ── HOME FOLLOWS THE DIRECTORY'S CHANGE SOCKET ──────────────────────────────
//
// Asking the directory on a timer is the price of not knowing when something
// changed, and on a hosted relay every ask is billed. The relay can say so
// itself: it keeps a socket open and sends the directory's version whenever it
// moves. The socket carries no records, so what home does with a frame is ask
// once, through the same read as always (machinepoll.go).
//
// THE SOCKET IS ONLY AS OPEN AS THE LIST IS LOOKED AT. It is joined when home
// is showing and the window is attended, the same test that lets the list ask
// at all, and let go the moment that stops being true.
//
// A FRAME IS NEWS ONLY IF IT IS NEWER THAN THE LAST READING. Every read carries
// the version it was read at, so a frame that names a version home has already
// read past causes nothing. Frames that arrive while a read is in flight cause
// at most one more read, however many there are.
//
// WHEN THE SOCKET IS DOWN nothing here is in the way: the list asks on its own
// schedule exactly as it did before there was a socket, and the schedule's
// backstop is the only thing that changes while the socket is up.

// dirWatchMsg is the feed's change signal, coming BACK as a message. The
// follower it came from rides along so a signal from a follower already let go
// is told apart from one that is current.
type dirWatchMsg struct{ from dirwatch.Follower }

// followable is a chat list source that can follow the directory's change feed.
type followable interface{ Follow() dirwatch.Follower }

// socketUp reports whether the change socket is up, which is what lets the
// list's schedule fall back to its backstop.
func (a *app) socketUp() bool { return a.dirFeed != nil && a.dirFeed.State().Up }

// tendWatch joins the feed when the list is wanted and leaves it when it is
// not, and returns the command that waits for the feed's first signal.
func (a *app) tendWatch(wanted bool) tea.Cmd {
	switch {
	case wanted && a.dirFeed == nil:
		return a.joinWatch()
	case !wanted && a.dirFeed != nil:
		a.dirFeed.Close()
		a.dirFeed, a.readOwed = nil, false
	}
	return nil
}

// joinWatch follows the feed if the source has one, and starts waiting on it.
func (a *app) joinWatch() tea.Cmd {
	src, ok := a.machines.(followable)
	if !ok {
		return nil
	}
	a.dirFeed = src.Follow()
	return a.awaitWatch()
}

// awaitWatch waits, off the update loop, for the followed feed to signal. A
// feed let go closes its channel, and the wait ends with nothing to say.
func (a *app) awaitWatch() tea.Cmd {
	f := a.dirFeed
	if f == nil {
		return nil
	}
	return func() tea.Msg {
		if _, open := <-f.Changes(); !open {
			return nil
		}
		return dirWatchMsg{from: f}
	}
}

// tookWatch files a signal: it asks for the next one, and reads the list if the
// feed has announced a version newer than the one last read. A feed that has
// just gone down sends the schedule back to its fast pace, so what was missed
// while it was down is caught within a beat.
func (a *app) tookWatch(msg dirWatchMsg) tea.Cmd {
	if msg.from != a.dirFeed {
		return nil
	}
	if !a.socketUp() {
		a.machinePoll.hurry()
	}
	// A presence change moves no version but redraws the devices row.
	a.touch()
	a.considerResume()
	a.announceJoined()
	return tea.Batch(a.awaitWatch(), a.readForFrame())
}

// readForFrame reads the list if the feed is ahead of the last reading, or owes
// the read until the one in flight lands.
func (a *app) readForFrame() tea.Cmd {
	if !a.feedAhead() {
		return nil
	}
	if a.machinesAsking {
		a.readOwed = true
		return nil
	}
	return a.askMachines()
}

// readOwedByFrames is the read a frame asked for while another was in flight,
// made once that one has landed, and only if it landed.
func (a *app) readOwedByFrames(landed bool) tea.Cmd {
	owed := a.readOwed
	a.readOwed = false
	if !owed || !landed || !a.feedAhead() {
		return nil
	}
	return a.askMachines()
}

// feedAhead reports whether the feed has announced a version past the reading.
func (a *app) feedAhead() bool {
	return a.dirFeed != nil && a.dirFeed.State().Version > a.machineRead.version
}

// probeWatch asks the feed whether its socket is alive, for a window that has
// just been returned to: a laptop that slept has a socket that looks open.
func (a *app) probeWatch() {
	if a.dirFeed != nil {
		a.dirFeed.Probe()
	}
}
