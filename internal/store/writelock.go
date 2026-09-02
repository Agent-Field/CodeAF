package store

// How long a write waits for the write lock, and why the wait has to be bounded
// out here rather than inside SQLite.
//
// Every write in this package is a transaction, and every transaction is
// IMMEDIATE (see [Open]'s `_txlock`): the write lock is taken at BEGIN rather
// than at the first write, so a transaction that is going to lose the race
// loses it before it has read anything and can never fail halfway through. That
// is load-bearing and stays. What was wrong was the WAIT.
//
// ── THE DRIVER IGNORES THE CONTEXT ON BEGIN ──
//
// modernc.org/sqlite's newTx runs `BEGIN IMMEDIATE` with context.Background()
// and throws the caller's context away, so `db.BeginTx(ctx, nil)` is not
// cancellable no matter what is passed to it. Measured against a held write
// lock, a BeginTx handed a 200ms context still took 10.09 seconds — the full
// busy_timeout — before it returned "database is locked". Passing a context
// here is therefore not a fix; it is a fix-shaped comment. The bound has to be
// the CALLER'S clock, kept out here where Go can see it.
//
// ── AND THE DEADLINE MUST NOT BECOME THE TRANSACTION'S ──
//
// The other obvious move is worse: hand BeginTx a context with a timeout on it
// and database/sql will roll the transaction back the moment that timeout
// fires — not the wait, the LIVE TRANSACTION, under a caller that is midway
// through writing. The deadline below bounds only the acquisition; the
// transaction it hands back is bound to nothing and ends when its caller ends
// it.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
)

const (
	// busyWait is how long SQLITE ITSELF retries a lock it cannot take — the
	// `busy_timeout` pragma every connection is opened with. It is the patience
	// of work with no caller watching a clock: the schema and migration
	// statements [Open] runs before there is a Store to bound anything with,
	// and the tail of an attempt this file has already abandoned.
	//
	// It is also, exactly, where the ten-second stall came from. It is left long
	// deliberately — shortening it would trade a stall for a failure to open a
	// database another process is mid-migration on — and it is no longer what
	// any caller waits for, because the bound below fires first.
	busyWait = 10 * time.Second

	// writeLockWait is how long a store write waits for the lock before it gives
	// up and says so.
	//
	// The number is chosen against the work: an ordinary write here is one
	// appended event and one view update, sub-millisecond, so three seconds is
	// thousands of ordinary writes' worth of queue ahead of you. Past it the
	// holder is not busy, it is wedged — another process stopped inside a
	// transaction — and every second spent waiting on a wedged holder is a
	// second of a person's frozen cursor bought for nothing. Three seconds is
	// also inside the window where a person still believes the program is
	// thinking rather than dead.
	writeLockWait = 3 * time.Second
)

// ErrBusy is a write giving up on the lock. It is not corruption and not a
// refusal: something else is writing the graph, and this write did not happen.
// A caller that can drop the write (the chat's transcript copy) drops it; a
// caller that cannot should say so to whoever asked.
var ErrBusy = errors.New("the graph is busy being written by something else")

// errAttemptFell is what the caller is told when the goroutine that opens the
// transaction falls over inside the driver rather than returning. It is
// deliberately NOT ErrBusy: nothing is holding the lock, the attempt broke, and
// a caller that reads busy would retry a call that will break again the same
// way. It is pre-loaded into the attempt's outcome before BeginTx is called and
// stands unless BeginTx returns, which is the only way a recovered panic can
// still reach whoever is waiting.
var errAttemptFell = errors.New("begin write: the transaction attempt fell")

// beginWrite starts an immediate transaction, waiting no longer than
// [writeLockWait] for the write lock.
func (s *Store) beginWrite() (*sql.Tx, error) {
	return beginWriteWithin(s.db, writeLockWait)
}

// beginRead starts a DEFERRED transaction: a reader that wants two statements to
// describe one moment, and takes no lock anybody else waits on.
//
// THE READ-ONLY FLAG IS LOAD-BEARING AND IS NOT DECORATION. This store's DSN
// sets `_txlock=immediate` ([Open]) so that every ordinary transaction is a
// write one, and a reader that began the same way would queue behind — and then
// hold — the write lock, to answer a question that changes nothing. With the
// flag the driver issues a plain `BEGIN`, which in WAL takes its snapshot at the
// first statement inside it and holds that snapshot until the rollback, while
// writers carry on beside it.
//
// A caller ROLLS BACK rather than commits: there is nothing to commit, and a
// rollback says so to anybody reading the call site.
func (s *Store) beginRead() (*sql.Tx, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("begin read: %w: no database", ErrInvalid)
	}
	return s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
}

// beginWriteWithin is beginWrite against a bare handle and a named bound. The
// migrations [Open] runs deliberately do NOT come through here — they hold a
// *sql.DB because there is no Store yet, they run once, and nobody is waiting on
// them with a clock, so [busyWait] is the right patience for them.
func beginWriteWithin(db *sql.DB, limit time.Duration) (*sql.Tx, error) {
	if db == nil {
		return nil, fmt.Errorf("begin write: %w: no database", ErrInvalid)
	}
	tx, _, err := beginWithin(func() (*sql.Tx, error) {
		return db.BeginTx(context.Background(), nil)
	}, limit)
	return tx, err
}

// beginWithin is the clock itself, holding the opener at arm's length and
// handing back the end of the abandoned rollback.
//
// Both of those are here for the same reason, and the reason is that the two
// promises this function makes are otherwise unobservable. The opener is a
// parameter rather than the handle because nothing can make the sqlite driver
// panic on demand, and what happens when the attempt FALLS is the whole
// question. The second return is closed when the rollback goroutine has ended,
// and is nil when the attempt landed in time and no rollback was started — the
// claim that the rollback ALWAYS ends is what this shape exists for, and a
// goroutine ending is not something anybody can see from outside except by
// being told. No caller in the store reads it.
func beginWithin(open func() (*sql.Tx, error), limit time.Duration) (*sql.Tx, <-chan struct{}, error) {
	type attempt struct {
		tx  *sql.Tx
		err error
	}
	// One goroutine per transaction is a real cost and it is paid knowingly: a
	// store write already crosses into SQLite and touches a file, and there is
	// no cheaper way to put a clock on a call the driver will not let anybody
	// interrupt.
	landed := make(chan attempt, 1)
	guard.Go("store/begin-write attempt", func() {
		// A GOROUTINE SOMEBODY WAITS ON DELIVERS ITS RESULT FROM A DEFER. Two
		// readers wait on landed — the select below until the clock runs out,
		// and after that the rollback goroutine, forever — so an attempt that
		// ends without sending is a caller told the store is busy when it is
		// not and a goroutine parked for the life of the process. A panic
		// guard.Go recovers unwinds this literal without reaching a send at
		// the end of it, so the outcome is declared first, pre-loaded with a
		// failure that NAMES the fault rather than blaming the lock, and
		// overwritten only when BeginTx has actually returned.
		outcome := attempt{err: errAttemptFell}
		defer func() { landed <- outcome }()
		tx, err := open()
		outcome = attempt{tx: tx, err: err}
	})
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case got := <-landed:
		return got.tx, nil, got.err
	case <-timer.C:
		// The attempt is ABANDONED, not cancelled — nothing can cancel it. It
		// ends by itself within busyWait, and if it wins the lock on the way out
		// it is rolled back at once: a transaction nobody is holding must not be
		// left holding the lock the next writer is waiting for.
		//
		// This goroutine waits with no clock of its own, which is only safe
		// because the attempt above sends from a defer and therefore always
		// sends — including when it falls. That send is what ends this
		// goroutine; without it there would be one parked here per fault.
		rolledBack := make(chan struct{})
		guard.Go("store/begin-write rollback", func() {
			defer close(rolledBack)
			got := <-landed
			if got.tx != nil {
				_ = got.tx.Rollback()
			}
		})
		return nil, rolledBack, fmt.Errorf("%w (waited %s)", ErrBusy, limit)
	}
}
