package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/syncwords"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// liveDrive is the drive side of one chat, started when it can be. A chat opens
// before its computer is paired, and /pair inside the chat then makes the
// identity that sync needs; a drive side fixed at open would leave that chat
// unsynced until the next restart. So the chat's seat holds a liveDrive, which
// asks again at each seal, gate and idle while local setup is missing. A refusal
// that time or capacity can cure also arms a timer, so pending turns upload even
// when the person leaves the chat open without saving another turn.
type liveDrive struct {
	start   func() (*syncsetup.Drive, error)
	now     func() time.Time
	after   func(time.Duration, func()) func() bool
	say     func(string)
	notices *driveNotices
	// transcript identifies the retained turns if this writer's final publish fails.
	transcript string
	localGate  *localToolGate

	// published is visible before idle release is enabled. Settings must not
	// wait for installation or catch-up disk work under mu.
	published atomic.Pointer[syncsetup.Drive]
	mu             sync.Mutex
	drive          *syncsetup.Drive
	working        bool   // A turn may start before asynchronous startup finishes.
	turn           uint64 // A newer turn retires the previous capture waiter.
	parked         bool   // Startup awaits a journal owner.
	storageWaiting bool
	waiting        bool // local setup or a retryable failure still permits another attempt
	ready          chan struct{}
	attempted      bool
	closed         bool
	retry          *time.Timer
	retryAt        time.Time
	retryWait      time.Duration
	stopRetry      func() bool
	refusedSince   time.Time
	told           error
	// changes coalesces local health updates for the host's cached surface state.
	changes     chan struct{}
	idleCheck   func() bool
	surfaceIdle func() bool
}

// newLiveDrive starts the drive side behind the first paint. Local startup
// screening stays off the surface loop; saving and new tools wait for ownership.
func newLiveDrive(start func(func()) (*syncsetup.Drive, error), notices ...*driveNotices) *liveDrive {
	l := newPendingLiveDrive(start, notices...)
	l.begin()
	return l
}

// newPendingLiveDrive reserves the startup boundary before journal ownership is known.
func newPendingLiveDrive(start func(func()) (*syncsetup.Drive, error), notices ...*driveNotices) *liveDrive {
	l := &liveDrive{
		changes: make(chan struct{}, 1),
		parked:  true,
		ready:   make(chan struct{}),
		now:     time.Now,
		say:     surfaceNotices.SayHomeOnce,
		after:   func(d time.Duration, fn func()) func() bool { return time.AfterFunc(d, fn).Stop },
	}
	if len(notices) > 0 && notices[0] != nil {
		l.notices = notices[0]
		l.say = l.notices.refuse
	}
	l.start = func() (*syncsetup.Drive, error) { return start(l.renewalChanged) }
	return l
}

func (l *liveDrive) nowTime() time.Time {
	if l.now != nil {
		return l.now()
	}
	return time.Now()
}

func (l *liveDrive) afterFunc(d time.Duration, fn func()) func() bool {
	if l.after != nil {
		return l.after(d, fn)
	}
	return time.AfterFunc(d, fn).Stop
}

// try starts the drive side if it is not running and an identity is there now.
// The identity is one file read and the relay is asked only once it exists, so a
// chat that never pairs costs nothing per call.
func (l *liveDrive) try() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.parked || l.closed || l.storageWaiting || l.drive != nil || l.ready != nil || (l.attempted && !l.waiting) || (l.waiting && !syncsetup.MayTalk(home.Dir())) {
		return
	}
	// Seals and idle notifications cannot shorten the relay's requested wait.
	if !l.retryAt.IsZero() && l.nowTime().Before(l.retryAt) {
		return
	}
	if l.stopRetry != nil {
		l.stopRetry()
		l.stopRetry = nil
	}
	if l.retry != nil {
		l.retry.Stop()
		l.retry = nil
	}
	l.retryAt = time.Time{}
	l.ready = make(chan struct{})
	l.launch(l.ready)
}

// begin spends the reserved startup exactly once after the journal opens.
func (l *liveDrive) begin() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.parked {
		return
	}
	l.parked = false
	l.launch(l.ready)
}

// isParked says the startup is still waiting for a journal owner, so an opener
// of the same chat in this process shares that wait rather than starting it.
func (l *liveDrive) isParked() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.parked
}

// abandon releases parked waiters without ever opening the drive side.
func (l *liveDrive) abandon() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.abandonLocked()
}

func (l *liveDrive) abandonLocked() bool {
	if !l.parked {
		return false
	}
	l.parked, l.closed = false, true
	close(l.ready)
	l.ready = nil
	return true
}

// launch shares startup and panic cleanup between immediate and reserved starts.
// The caller holds mu, so closing cannot slip between reserving and launching.
func (l *liveDrive) launch(ready chan struct{}) {
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

// install publishes any earlier local seals before making the drive available.
// Completed seals use the same lock when deciding whether catch-up is still owed.
func (l *liveDrive) install(d *syncsetup.Drive, err error, ready chan struct{}) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	defer close(ready)
	l.ready = nil
	l.drive, l.waiting, l.attempted = d, errors.Is(err, syncsetup.ErrNoIdentity) || errors.Is(err, syncsetup.ErrQuiet) || syncwords.NetworkFailure(err), true
	l.published.Store(d)
	if d == nil && !l.closed {
		if r, ok := cellsync.RefusalOf(err); ok {
			now := l.nowTime()
			if l.refusedSince.IsZero() {
				l.refusedSince = now
			}
			if l.told != r.Is && now.Sub(l.refusedSince) >= r.Quiet {
				l.told = r.Is
				if l.say != nil {
					l.say(r.Say(err))
				}
			}
			if wait, retry := r.StartupWait(l.retryWait, cellsync.DefaultInterval, err); retry {
				l.waiting = true
				l.retryWait, l.retryAt = wait, now.Add(wait)
				if errors.Is(err, wireauth.ErrStorageOff) {
					l.storageWaiting = true
					cancel := syncsetup.WaitStorage(home.Dir(), l.storageOpened)
					l.stopRetry = func() bool { cancel(); return true }
				} else {
					l.stopRetry = l.afterFunc(wait, l.try)
				}
			}
		}
	}
	if d != nil {
		if l.notices != nil {
			l.notices.clear()
		}
		l.told = nil
		l.refusedSince = time.Time{}
		d.Working(l.working)
		d.SetIdleCheck(l.idleReady)
		d.CatchUp()
		l.retryWait = 0
	} else if !l.closed && cellsync.Unreachable(err) {
		l.retryStartup()
	}
	l.renewalChanged()
	return l.closed
}

func (l *liveDrive) setIdleCheck(check func() bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.idleCheck = check
}

func (l *liveDrive) idleReady() bool {
	check, surface, closed := l.idleChecks()
	return !closed && check != nil && (surface == nil || surface()) && check()
}

// idleChecks reads the checks under mu; they run after it is released.
func (l *liveDrive) idleChecks() (check, surface func() bool, closed bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.idleCheck, l.surfaceIdle, l.closed
}

// retryStartup owns the next attempt after a transport failure. It runs under
// mu; gates and seals remain local until the single startup attempt succeeds.
func (l *liveDrive) retryStartup() {
	if l.retryWait == 0 {
		l.retryWait = cellsync.DefaultInterval
	} else {
		l.retryWait = min(2*l.retryWait, cellsync.MaxBackoff)
	}
	resetAttempt := func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.retry = nil
		if !l.closed {
			l.attempted = false
		}
	}
	l.retry = time.AfterFunc(l.retryWait, func() {
		resetAttempt()
		l.try()
	})
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
	if l == nil {
		return nil
	}
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

// Gate checks shared ownership even while account storage has no uploading writer.
func (l *liveDrive) Gate() error {
	// A failed service startup falls back to this call's local writer. Retrying
	// belongs to the next call, or an outage would trap this one in a retry loop.
	l.try()
	if ready := l.pending(); ready != nil {
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
	if l.localGate != nil {
		return l.localGate.Gate()
	}
	return nil
}

func (l *liveDrive) SavedMetadata() {
	if d := l.published.Load(); d != nil {
		d.SavedMetadata()
	}
}

// Working retains the turn state while startup runs, so a first send cannot
// lose its start beat. Installing and changing it share the lock to prevent
// startup from replaying an older state after the turn has ended.
func (l *liveDrive) Working(on bool) {
	l.try()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.turn++
	l.working = on
	if l.drive != nil {
		l.drive.Working(on)
	}
}

// TurnEnded waits off the turn goroutine so the final capture can include the
// closing bookkeeping as before. A newer turn invalidates this notification:
// an older capture must never clear the newer turn's working flag.
func (l *liveDrive) TurnEnded(c cell.Cell, report func(error)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	turn := l.turn
	guard.Go("chat sync turn end", func() {
		ctx, cancel := context.WithTimeout(context.Background(), cellstore.SealWithin)
		defer cancel()
		if err := cellstore.WaitCapture(ctx, c); err != nil {
			report(err)
		}
		if l.finishTurn(turn) {
			l.Idle()
		}
	})
}

// finishTurn changes the flag under one lock so a newer start cannot race
// between the generation check and the end notification.
func (l *liveDrive) finishTurn(turn uint64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || l.turn != turn {
		return false
	}
	l.working = false
	if l.drive != nil {
		l.drive.Working(false)
	}
	return true
}

// Idle uploads what is sealed, once a drive side exists.
func (l *liveDrive) Idle() {
	if d := l.current(); d != nil {
		d.Idle()
	}
}

// Store waits for the initial ownership decision before choosing a writer.
// A computer without an identity still saves locally; an installed drive stamps
// and publishes its turns, while a viewer leaves the history alone.
func (l *liveDrive) Store(e cellstore.Engine) cellstore.Store {
	return lateStore{live: l, engine: e}
}

// Close ends the drive side if one ever started.
func (l *liveDrive) Close(ctx context.Context) error {
	if l.notices != nil {
		l.notices.beginClose()
	}
	if l.localGate != nil {
		l.localGate.Close()
	}
	ready := func() <-chan struct{} {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.abandonLocked()
		l.closed = true
		if l.retry != nil {
			l.retry.Stop()
			l.retry = nil
		}
		if l.stopRetry != nil {
			l.stopRetry()
			l.stopRetry = nil
		}
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
// publishes under. A closed writer must also be replaced, even if its failed
// final save left a live lease. An open drive still waiting for identity is not.
func (l *liveDrive) superseded() bool {
	d, closed := l.driveAndClosed()
	if closed {
		return true
	}
	if d == nil {
		return l.localGate != nil && l.localGate.superseded()
	}
	ctx, cancel := context.WithTimeout(context.Background(), holdsLeaseWithin)
	defer cancel()
	return !d.HoldsLease(ctx)
}

// driveAndClosed reads the drive side and whether it was closed under one lock.
func (l *liveDrive) driveAndClosed() (*syncsetup.Drive, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.drive, l.closed
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

// readyStore waits for startup ownership before touching the history.
// A new local chat can still save while its computer has no identity.
func (s lateStore) readyStore(ctx context.Context) (cellstore.Store, *syncsetup.Drive, error) {
	s.live.try()
	if ready := s.live.pending(); ready != nil {
		select {
		case <-ready:
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
	used := s.live.started()
	if used != nil {
		return used.Store(s.engine), used, nil
	}
	return s.engine, nil, nil
}

// Prepare warms only the store permitted to write this chat.
func (s lateStore) Prepare(ctx context.Context, c cell.Cell) error {
	store, _, err := s.readyStore(ctx)
	if err != nil {
		return err
	}
	return store.(interface {
		Prepare(context.Context, cell.Cell) error
	}).Prepare(ctx, c)
}

// Seal implements cellstore.Store.
func (s lateStore) Seal(ctx context.Context, c cell.Cell, info cellstore.TurnInfo) (cellstore.Sealed, error) {
	store, used, err := s.readyStore(ctx)
	if err != nil {
		return cellstore.Sealed{}, err
	}
	// Admission ends when the local window becomes a viewer. A seal already
	// admitted still finishes, preserving work accepted before the refusal.
	if used == nil && s.live.localGate != nil && s.live.localGate.superseded() {
		return cellstore.Sealed{}, cellstore.ErrReadOnly
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

// storageOpened shortens only a capacity wait; it never replaces an active writer.
func (l *liveDrive) storageOpened() {
	if l.notices != nil {
		l.notices.clearStorageOff()
	}
	if d := l.endStorageWait(); d != nil {
		d.StorageOpened()
	} else {
		l.try()
	}
}

// endStorageWait clears the storage wait and its retry timer under the lock and
// answers the running drive, if there is one, so waking it happens outside.
func (l *liveDrive) endStorageWait() *syncsetup.Drive {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.storageWaiting = false
	if l.stopRetry != nil {
		l.stopRetry()
		l.stopRetry = nil
	}
	l.retryAt = time.Time{}
	return l.drive
}
