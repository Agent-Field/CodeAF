package main

import (
	"context"
	"errors"
	"sync"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
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

	mu      sync.Mutex
	drive   *syncsetup.Drive
	waiting bool // sync is on and only an identity is missing
}

// newLiveDrive starts the drive side now when it can. It answers nil when sync
// is off or cannot start for any reason a pairing would not change, so the chat
// runs unsynced exactly as it did before sync existed.
func newLiveDrive(start func() (*syncsetup.Drive, error)) *liveDrive {
	l := &liveDrive{start: start}
	l.try()
	if l.drive == nil && !l.waiting {
		return nil
	}
	return l
}

// try starts the drive side if it is not running and an identity is there now.
// The identity is one file read and the relay is asked only once it exists, so a
// chat that never pairs costs nothing per call.
func (l *liveDrive) try() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.drive != nil || (l.waiting && !identityMade()) {
		return
	}
	d, err := l.start()
	l.drive, l.waiting = d, errors.Is(err, syncsetup.ErrNoIdentity) || errors.Is(err, syncsetup.ErrQuiet)
}

// identityMade is whether this computer has an identity to sign with now.
func identityMade() bool {
	_, err := identity.Load(home.Dir())
	return !errors.Is(err, identity.ErrNone)
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
	if d := l.current(); d != nil {
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
	if d := l.started(); d != nil {
		return d.Close(ctx)
	}
	return nil
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
	var store cellstore.Store = s.engine
	if d := s.live.current(); d != nil {
		store = d.Store(s.engine)
	}
	return store.Seal(ctx, c, info)
}
