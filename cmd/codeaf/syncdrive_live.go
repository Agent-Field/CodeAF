package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// liveDrive is the drive side of one chat, started when it can be. A chat opens
// before its computer is paired, and /pair inside the chat then makes the
// identity that sync needs; a drive side fixed at open would leave that chat
// unsynced until the next restart. So the chat's seat holds a liveDrive, which
// asks again at each seal, gate and idle for as long as the one thing missing
// is the identity, and goes on with the real drive from the call that finds it.
type liveDrive struct {
	start func() (*syncsetup.Drive, error)
	// transcript identifies the retained turns if this writer's final publish fails.
	transcript string

	mu        sync.Mutex
	drive     *syncsetup.Drive
	waiting   bool // sync is on and only an identity, or the end of solo, is missing
	ready     chan struct{}
	attempted bool
	closed    bool
	// changes coalesces local health updates for the host's cached surface state.
	changes chan struct{}
}

// newLiveDrive starts the drive side behind the first paint. Local startup
// seals do not wait for the relay; new tool work waits for its ownership check.
func newLiveDrive(start func(func()) (*syncsetup.Drive, error)) *liveDrive {
	l := &liveDrive{changes: make(chan struct{}, 1)}
	l.start = func() (*syncsetup.Drive, error) { return start(l.renewalChanged) }
	l.try()
	return l
}

// try starts the drive side if it is not running and an identity is there now.
// The identity is one file read and the relay is asked only once it exists, so a
// chat that never pairs costs nothing per call.
func (l *liveDrive) try() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || l.drive != nil || l.ready != nil || (l.attempted && !l.waiting) || (l.waiting && !syncsetup.MayTalk(home.Dir())) {
		return
	}
	l.ready = make(chan struct{})
	ready := l.ready
	guard.Go("chat sync startup", func() {
		var d *syncsetup.Drive
		var err error
		// A startup fault must release waiting tools as well as spare the
		// surface. Installing the result is owed even when start panics.
		defer func() {
			if closed := l.install(d, err, ready); closed && d != nil {
				ctx, cancel := context.WithTimeout(context.Background(), driveCloseWithin)
				defer cancel()
				_ = d.Close(ctx)
			}
		}()
		d, err = l.start()
	})
}

// install joins the startup seals under the same lock lateStore seals through,
// so a turn cannot fall between catch-up and installing the drive.
func (l *liveDrive) install(d *syncsetup.Drive, err error, ready chan struct{}) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	defer close(ready)
	l.ready = nil
	l.drive, l.waiting, l.attempted = d, errors.Is(err, syncsetup.ErrNoIdentity) || errors.Is(err, syncsetup.ErrQuiet), true
	if d != nil {
		d.CatchUp()
	}
	l.renewalChanged()
	return l.closed
}

// openDrive is the started drive, or nil when none has started or this writer
// has closed. It never starts one.
func (l *liveDrive) openDrive() *syncsetup.Drive {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	return l.drive
}

// RenewalStatus reads an already-started drive without starting one or waiting.
func (l *liveDrive) RenewalStatus() (time.Time, time.Duration, bool) {
	if l == nil {
		return time.Time{}, 0, false
	}
	d := l.openDrive()
	if d == nil {
		return time.Time{}, 0, false
	}
	return d.RenewalStatus()
}

// RenewalChanges lets the host hear health changes even when no turn is running.
func (l *liveDrive) RenewalChanges() <-chan struct{} {
	if l == nil {
		return nil
	}
	return l.changes
}

func (l *liveDrive) renewalChanged() {
	select {
	case l.changes <- struct{}{}:
	default:
	}
}

// pending reads the startup boundary without holding the mutex while waiting
// for its network work to finish.
func (l *liveDrive) pending() <-chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ready
}

// current is the running drive side, or nil while there is none.
func (l *liveDrive) current() *syncsetup.Drive {
	l.try()
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.drive
}

// Gate is the drive side's gate: nothing is refused until a drive side exists.
func (l *liveDrive) Gate() error {
	for {
		l.try()
		ready := l.pending()
		if ready == nil {
			break
		}
		<-ready
	}
	if d := l.started(); d != nil {
		ctx, cancel := context.WithTimeout(context.Background(), cellstore.SealWithin+holdsLeaseWithin)
		defer cancel()
		if err := d.CheckLease(ctx); err != nil {
			return err
		}
		return d.Gate()
	}
	return nil
}

// Idle uploads what is sealed, once a drive side exists.
func (l *liveDrive) Idle() {
	if d := l.current(); d != nil {
		d.Idle()
	}
}

// Store is the store a seal goes through: the engine itself until a drive side
// exists, then the drive side's. A turn sealed before it started is noted by the
// drive side's own catch-up, so nothing sealed in between is left behind.
func (l *liveDrive) Store(e cellstore.Engine) cellstore.Store {
	return lateStore{live: l, engine: e}
}

// Close ends the drive side if one ever started.
func (l *liveDrive) Close(ctx context.Context) error {
	ready := func() <-chan struct{} {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.closed = true
		return l.ready
	}()
	if ready != nil {
		select {
		case <-ready:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if d := l.started(); d != nil {
		return d.Close(ctx)
	}
	return nil
}

// holdsLeaseWithin bounds the look at the directory that a chat opened again makes.
const holdsLeaseWithin = 5 * time.Second

// superseded says a drive side was running and the chat has since been taken from it, by
// another machine or by a move back to this one, so that it no longer holds the lease it
// publishes under. A drive side that still waits for an identity, or that drives, is not.
func (l *liveDrive) superseded() bool {
	d := l.started()
	if d == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), holdsLeaseWithin)
	defer cancel()
	return !d.HoldsLease(ctx)
}

// started is the drive side if one ever started, without trying to start it.
func (l *liveDrive) started() *syncsetup.Drive {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.drive
}

// lateStore seals on whichever store the chat has now.
type lateStore struct {
	live   *liveDrive
	engine cellstore.Engine
}

var _ cellstore.Store = lateStore{}

// Seal implements cellstore.Store.
func (s lateStore) Seal(ctx context.Context, c cell.Cell, info cellstore.TurnInfo) (cellstore.Sealed, error) {
	s.live.try()
	used := s.live.started()
	var store cellstore.Store = s.engine
	if used != nil {
		store = used.Store(s.engine)
	}
	sealed, err := store.Seal(ctx, c, info)
	if err == nil {
		s.live.catchUpAfter(used)
	}
	return sealed, err
}

// catchUpAfter joins a seal made while the drive was being installed. The
// mutex covers only the catch-up decision, never the engine's disk work.
func (l *liveDrive) catchUpAfter(used *syncsetup.Drive) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.drive != nil && l.drive != used {
		l.drive.CatchUp()
	}
}
