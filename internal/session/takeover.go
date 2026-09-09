package session

// takeover.go is how a conversation open in ANOTHER WINDOW is moved to this one.
//
// A local conversation lives inside the process of the terminal that opened it,
// and its journal is held under a flock nothing else can take (sessionfile.go).
// A second terminal cannot join it; it can only ask the holder to let go. This
// file is that ask, and it is the shape answers.go already is: ONE FILE in the
// session's own folder, written by whoever asked, picked up by the holding
// session on the heartbeat it already runs.
//
// ── THE FOUR LAWS ──
//
//   - THE HOLDER DECIDES NOTHING. A request found on the tick is announced to
//     the surface drawing that session and nothing else happens here: the
//     surface detaches and closes the conversation the way /new does, its
//     tasks land "paused — it resumes", and the window that asked resumes them
//     from the checkpoint. The engine never closes itself from inside a tick.
//
//   - A REQUEST IS ANSWERED AT ONCE, MID-REPLY OR NOT. It used to be held back
//     until the running turn ended, on the reasoning that a reply is never cut
//     — and the cost of that reasoning was the defect this road was reported
//     for: a person pressed enter, confirmed, and watched `coming here · 4m50s`
//     while a long reply finished somewhere they could not see. Nothing is lost
//     by answering now. The holder interrupts and closes the way /new does
//     (internal/tui3's takeOver), which lands its running work as
//     `paused — it resumes`, and the window that asked resumes it from the
//     checkpoint with the partial reply already in the journal.
//
//   - A REQUEST IS TAKEN OFF DISK BEFORE IT IS ANNOUNCED, so a holder that is
//     slow to let go is asked once and never a second time on the next beat,
//     and a request left by a window that gave up ([CancelTakeover] removes it,
//     and a stale one is ignored by age) is not found by a session opened a
//     week later.
//
//   - IT RIDES THE TASK LANE. The standing lane is the one subscription that
//     outlives every turn on every surface — the front conversation pumps it
//     and a kept one drains it (internal/tui3's keeper.go) — and standing news
//     already rides it for exactly that reason ([Agent.emitStandingNews]). A
//     lane of its own would be a third subscription for one event a session
//     sees at most once.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// takeoverName is the request, inside one session's folder, beside presence.json.
const takeoverName = "takeover.json"

// takeoverDoorstep is how often a live session looks for a request, and it is
// deliberately NOT [presenceHeartbeat] (taskpresence.go's [presenceDesk.beat]
// says why at length).
//
// THE COST IS ONE OPEN OF A PATH THAT IS NOT THERE. [takeoverAsked] reads one
// file in the session's own folder, and on every session on the machine except
// the one being asked for, that read fails at the first syscall — which is
// cheaper per second than the two-hundred-byte temp-write-and-rename the
// heartbeat already does every five seconds. What it buys is the difference
// between a conversation that arrives when you press enter and one that arrives
// a few seconds later for no reason a person can see.
//
// IT IS THE ASKING WINDOW'S BEAT, TO WITHIN A GLANCE. The window waiting looks
// at the flock five times a second (internal/tui3's takeover.go); a doorstep
// slower than that would make the flock beat pointlessly fine, and a doorstep
// faster would be this side polling harder than anybody is watching.
const takeoverDoorstep = 250 * time.Millisecond

// TakeoverStale is how old a request may be and still be answered. A window that
// asked and was closed removes its request ([CancelTakeover]); one that was
// killed cannot, and this is what stops its ask outliving it by a week. It is
// generous because a holder mid-reply waits for the turn to end before it looks
// again, and a long reply is minutes, not seconds.
//
// IT IS EXPORTED BECAUSE THE ASKING WINDOW HAS TO KNOW IT. Past this age no
// holder will ever answer — [takeoverAsked] deletes the request unread — so a
// surface still waiting on the flock past it is waiting for something that
// cannot happen, and the one number that says so has to be the same number on
// both sides of the exchange (ONE SOURCE OF TRUTH).
const TakeoverStale = 10 * time.Minute

// TakeoverWord is the one sentence a surface says about a conversation another
// window took, and the engine spells it so the event and the surface agree.
const TakeoverWord = "moved to another window"

// MovedWord is that sentence with THE WAY BACK on it, and it belongs to the
// engine road: a conversation the engine holds is opened in another terminal by
// one keystroke and comes back by the same one, so the window it left names the
// key rather than reporting a loss ([EventMoved]).
//
// IT IS BUILT ON [TakeoverWord] AND NOT WRITTEN A SECOND TIME. A person meets
// one of these two sentences on the day their conversation walks to another
// terminal, and two spellings of that would be two programs.
const MovedWord = TakeoverWord + " · enter on home brings it back"

// ErrNoSessionDir is [AskTakeover] on a conversation with no folder — a
// memory-only one, or the legacy flat layout — which nothing could ever read.
var ErrNoSessionDir = errors.New("this conversation has no folder to leave a request in")

// TakeoverPath is where a request for the session in dir is written.
func TakeoverPath(sessionDir string) string {
	return filepath.Join(strings.TrimSpace(sessionDir), takeoverName)
}

// takeoverRequest is the file's whole content: when it was asked. Who asked is
// not recorded because the holder cannot act on it — every window on this
// machine looks the same from inside a process — and a fact nobody can act on
// is bookkeeping.
type takeoverRequest struct {
	At time.Time `json:"at"`
}

// AskTakeover asks the window holding the session in dir to let go of it.
//
// TEMP-AND-RENAME, like presence.json beside it, so the holder's tick never
// reads half a request. Whether anybody is there to read it is not this
// function's to say: the caller watches the journal's flock ([InUse]) and
// decides by that.
func AskTakeover(sessionDir string) error {
	dir := strings.TrimSpace(sessionDir)
	if dir == "" {
		return ErrNoSessionDir
	}
	raw, err := json.Marshal(takeoverRequest{At: time.Now()})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".takeover-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, TakeoverPath(dir)); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// CancelTakeover withdraws a request the asking window no longer wants
// answered — it stopped waiting. A request already taken is nothing to remove,
// and that is not an error.
func CancelTakeover(sessionDir string) {
	if strings.TrimSpace(sessionDir) == "" {
		return
	}
	_ = os.Remove(TakeoverPath(sessionDir))
}

// takeoverAsked reports whether a live request is waiting for the session in
// dir, WITHOUT taking it. A request older than [TakeoverStale] is a window that
// died asking, and it is removed here so it is never answered.
func takeoverAsked(sessionDir string, now time.Time) bool {
	if strings.TrimSpace(sessionDir) == "" {
		return false
	}
	path := TakeoverPath(sessionDir)
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var request takeoverRequest
	if err := json.Unmarshal(raw, &request); err != nil || request.At.IsZero() || now.Sub(request.At) > TakeoverStale {
		_ = os.Remove(path)
		return false
	}
	return true
}

// takeTakeover removes the request for the session in dir and reports whether
// there was one to remove.
func takeTakeover(sessionDir string) bool {
	if strings.TrimSpace(sessionDir) == "" {
		return false
	}
	return os.Remove(TakeoverPath(sessionDir)) == nil
}

// drainTakeover is the beat's look for a request (taskpresence.go). It runs on
// the TICK and never on a nudge, for the reason answers are drained there: a
// nudge fires under the agent's own lock.
//
// A TURN IN FLIGHT NO LONGER HOLDS THE REQUEST BACK (the second law above). The
// flags are read under the lock and released before anything touches the disk,
// which is this package's standing rule about holding a.mu across a call that
// may take a while.
//
// THE THREE GUARDS THAT REMAIN ARE ABOUT WHETHER THERE IS ANYBODY TO TELL.
// A conversation running INSIDE a task has no window of its own and no surface
// to hear this; a closed one has nothing left to let go of; and one already
// told is told once, because the announcement is taken off the lane and a
// second copy would ask a surface to leave a conversation it has already left.
func (a *Agent) drainTakeover() {
	if a.config.InTask {
		return
	}
	dir := a.config.Place.Dir
	if !takeoverAsked(dir, time.Now()) {
		return
	}
	a.mu.Lock()
	closed, told := a.closed, a.takenOver
	a.mu.Unlock()
	if closed || told {
		return
	}
	if !takeTakeover(dir) {
		return
	}
	a.announceTakeover()
}

// announceTakeover tells every surface on this session that another window has
// asked for it, once, and remembers that it did — so a surface that looks later
// (a kept conversation waking on its stir) still finds the fact.
func (a *Agent) announceTakeover() {
	a.mu.Lock()
	if a.closed || a.takenOver {
		a.mu.Unlock()
		return
	}
	a.takenOver = true
	watchers := make([]*eventStream, len(a.taskWatchers))
	copy(watchers, a.taskWatchers)
	a.mu.Unlock()
	event := Event{Kind: EventTakeover, Text: TakeoverWord}
	for _, watcher := range watchers {
		watcher.send(event)
	}
}

// TakeoverAsked reports that another window has asked for this session and it
// has not been let go of yet. It is what a surface that missed the event asks.
func (a *Agent) TakeoverAsked() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.takenOver
}
