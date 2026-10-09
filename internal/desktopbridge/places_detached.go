package desktopbridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// ── ASKING WITH NO WINDOW OPEN ──────────────────────────────────────────────
//
// The desktop opens on Home, and Home is where offers to organize are shown —
// so the moment that most wants a model is the one with no conversation tab
// open at all. Rules alone cannot name a group of chats, so without a door the
// first Home a person sees would have nothing to offer but folder matches.
//
// THE DOOR IS AN EXISTING SAVED CONVERSATION, READ AND NEVER DRIVEN. The
// model, its key and its bill belong to an engine, and an engine is always
// holding a conversation; so the bridge attaches to ONE conversation that
// already exists on disk — the one the engine host is already holding when
// there is one, otherwise the newest one somebody spoke in — as a WATCHER
// ([remote.Hello.Watch]). A watcher is never given the keyboard, is refused any
// door that puts words into the conversation, and starts no turn; the only
// thing asked of it is [remote.MethodPlacesAsk], a getter beside the turn.
//
// IT NEVER MINTS. The hello names the transcript and is never [remote.Hello.New];
// a welcome naming any other transcript is closed and refused, so a background
// job can never leave an untitled conversation behind. No saved conversation
// means no door and rules alone, which is also exactly right for an empty
// library: there is nothing to organize.
//
// ONE CONNECTION, CACHED AND BOUNDED. It is opened on the first ask a job
// actually makes (a job the rules answered opens nothing), reused while it is
// warm, closed after detachedIdle without an ask, on any failed ask, and when
// the bridge closes. An open that fails is not retried for detachedRetry, so a
// broken engine costs one attempt per window rather than one per job.
//
// THE CALL IS BILLED TO THAT CONVERSATION'S ERRANDS, as every places ask is
// billed to the conversation whose engine answered it.

const (
	// detachedOpenWithin bounds attaching: spawning an engine and, the first
	// time, the host it joins (internal/enginehost waits up to ten seconds).
	detachedOpenWithin = 30 * time.Second
	// detachedIdle is how long the background connection stays open unused.
	detachedIdle = 2 * time.Minute
	// detachedRetry is how long a failed attach is not tried again.
	detachedRetry = 5 * time.Minute
)

var errAdviceClosed = errors.New("the desktop is closing")

// detachedDoor is the one cached background connection.
type detachedDoor struct {
	file string
	conn Connection
	door placeAskDoor
	idle *time.Timer
}

// backgroundFile is the saved conversation a background ask would ride, or ""
// when there is none: one the engine host is holding right now if any (so
// nothing is booted), else the newest one somebody spoke in. Its transcript
// must exist; a row whose folder holds no transcript is nobody's conversation.
func (a *PlaceAdvice) backgroundFile() string {
	a.b.mu.Lock()
	places := a.b.places
	a.b.mu.Unlock()
	if places == nil {
		return ""
	}
	var spoken string
	for _, row := range places.world(false).Sessions() {
		if row.DeletionPending || row.At.IsZero() || row.Transcript == "" {
			continue
		}
		// The detached launcher joins this row's own workspace host. A saved
		// remote or deleted project cannot provide a local reader; skip it
		// instead of retrying that broken newest row while valid chats wait.
		if a.SharedWorkspace == "" || filepath.Clean(row.Workspace) != filepath.Clean(a.SharedWorkspace) {
			workspace, err := os.Stat(row.Workspace)
			if err != nil || !workspace.IsDir() {
				continue
			}
		}
		info, err := os.Stat(row.Transcript)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			continue
		}
		if row.Open || row.Live {
			return row.Transcript
		}
		if spoken == "" && !row.Archived {
			spoken = row.Transcript
		}
	}
	return spoken
}

// detachedAsker is the Asker for a job with no open conversation to ride, or
// nil — rules only — when there is no door, no saved conversation, or the last
// attach to it failed recently.
func (a *PlaceAdvice) detachedAsker() askDoor {
	if a.Detached == nil {
		return nil
	}
	file := a.backgroundFile()
	if file == "" || a.recentlyFailed(file) {
		return nil
	}
	return func(ctx context.Context) (placeAskDoor, error) { return a.detached(ctx, file) }
}

func (a *PlaceAdvice) recentlyFailed(file string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.bgFailedFile == file && a.now().Sub(a.bgFailedAt) < detachedRetry
}

// detached is the cached door onto file, attaching when there is none.
func (a *PlaceAdvice) detached(ctx context.Context, file string) (placeAskDoor, error) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil, errAdviceClosed
	}
	if bg := a.bg; bg != nil && bg.file == file {
		a.touchLocked(bg)
		a.mu.Unlock()
		return bg.door, nil
	}
	old := a.bg
	a.bg = nil
	a.mu.Unlock()
	if old != nil {
		old.idle.Stop()
		old.conn.Close()
	}
	conn, door, err := a.attach(ctx, file)
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		a.bgFailedFile, a.bgFailedAt = file, a.now()
		return nil, err
	}
	if a.closed {
		conn.Close()
		return nil, errAdviceClosed
	}
	bg := &detachedDoor{file: file, conn: conn, door: door}
	a.bg = bg
	a.touchLocked(bg)
	a.opened++
	return door, nil
}

// attach opens the watcher connection within detachedOpenWithin and checks
// it is the conversation asked for and that its engine answers the question.
// A connection that arrives after the deadline is closed when it does.
func (a *PlaceAdvice) attach(ctx context.Context, file string) (Connection, placeAskDoor, error) {
	result := make(chan openedConn, 1)
	go func() {
		conn, err := a.Detached(file)
		result <- openedConn{conn, err}
	}()
	timer := time.NewTimer(detachedOpenWithin)
	defer timer.Stop()
	var got openedConn
	select {
	case got = <-result:
	case <-ctx.Done():
		go closeLate(result)
		return Connection{}, nil, ctx.Err()
	case <-timer.C:
		go closeLate(result)
		return Connection{}, nil, errors.New("a saved conversation could not be opened in time to ask about places")
	}
	if got.err != nil {
		return Connection{}, nil, got.err
	}
	conn := got.conn
	if filepath.Clean(conn.Welcome.SessionFile) != filepath.Clean(file) {
		closeConn(conn)
		return Connection{}, nil, errors.New("the engine opened a different conversation than the one asked for")
	}
	door, ok := conn.Agent.(placeAskDoor)
	if !ok || !conn.Welcome.PlaceAsk {
		closeConn(conn)
		return Connection{}, nil, errors.New("this engine cannot ask about places")
	}
	return conn, door, nil
}

// openedConn is one attach's outcome.
type openedConn struct {
	conn Connection
	err  error
}

// closeLate closes a connection that arrived after nobody was waiting for it.
func closeLate(ch <-chan openedConn) {
	if got := <-ch; got.err == nil {
		closeConn(got.conn)
	}
}

func closeConn(c Connection) {
	if c.Close != nil {
		c.Close()
	}
}

// touchLocked restarts bg's idle clock.
func (a *PlaceAdvice) touchLocked(bg *detachedDoor) {
	if bg.idle != nil {
		bg.idle.Stop()
	}
	wait := a.DetachedIdle
	if wait <= 0 {
		wait = detachedIdle
	}
	bg.idle = time.AfterFunc(wait, func() { a.dropDetached(bg) })
}

// dropDetached closes bg if it is still the cached connection.
func (a *PlaceAdvice) dropDetached(bg *detachedDoor) {
	a.mu.Lock()
	if a.bg != bg {
		a.mu.Unlock()
		return
	}
	a.bg = nil
	a.mu.Unlock()
	bg.idle.Stop()
	closeConn(bg.conn)
}
