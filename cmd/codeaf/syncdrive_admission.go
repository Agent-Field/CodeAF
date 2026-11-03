package main

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

const (
	// Two seconds keeps ordinary back-to-back submissions local while making
	// the first submission after even a short idle ask who holds the chat.
	driveWorkFreshFor = 2 * time.Second
	// The hosted relay answered a cell read in about 45 ms on a warm connection
	// and 75-200 ms on a fresh TLS one (measured 2026-10-26 from a nearby
	// machine); 750 ms still covers a fresh connection at a 250 ms round trip.
	// A send meets admission at several app and session doors, but only the
	// first can wait: a check that finishes, answered or not, covers the rest.
	driveWorkWithin = 750 * time.Millisecond
	// After a read that failed, work stays local this long without asking
	// again, so an offline stretch does not pay the budget on every send.
	driveWorkOfflineFor = 30 * time.Second
)

// workAdmissions shares a confirmation between the app and session gates of
// one live drive. driveBook removes entries whenever it forgets a drive, so
// closed chats retain neither state nor drive pointers here.
var workAdmissions sync.Map // *liveDrive -> *workAdmission

type workAdmission struct {
	mu sync.Mutex
	// fresh is when admission must next ask the relay: driveWorkFreshFor after
	// a confirmation, driveWorkOfflineFor after a failed read.
	fresh    time.Time
	checking chan struct{}
}

func newAdmissionDrive(start func(func()) (*syncsetup.Drive, error)) *liveDrive {
	check := &workAdmission{}
	d := newPendingLiveDrive(func(changed func()) (*syncsetup.Drive, error) {
		drive, err := start(changed)
		if err == nil && drive != nil && drive.Gate() == nil {
			check.freshFor(driveWorkFreshFor)
		}
		return drive, err
	})
	workAdmissions.Store(d, check)
	return d
}

func (w *workAdmission) admit(d *syncsetup.Drive) error {
	ctx, cancel := context.WithTimeout(context.Background(), driveWorkWithin)
	defer cancel()
	released := d.BeginWork()
	if err := d.Gate(); err != nil && !released {
		return err
	}
	if released {
		w.freshFor(0)
	}
	// Concurrent doors share the read and its result, including a failure.
	// Waiting for another door consumes this caller's budget too.
	done, mine := w.join()
	switch {
	case mine:
		confirmed, err := d.ConfirmWork(ctx)
		w.settle(done, confirmed, err)
		if err != nil {
			log.Print("sync: checking chat ownership; admitting work: ", err)
		}
	case done != nil:
		select {
		case <-done:
		case <-ctx.Done():
			log.Print("sync: checking chat ownership; admitting work: ", ctx.Err())
		}
	}
	return d.Gate()
}

// freshFor lets admission skip the relay for the next span.
func (w *workAdmission) freshFor(span time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.fresh = time.Now().Add(span)
}

// join decides who asks the relay. A fresh confirmation answers nil: nobody
// needs to ask. A read already in flight answers its done channel to wait on.
// Otherwise this caller becomes the reader: join answers a new done channel
// and mine, and the caller must settle it.
func (w *workAdmission) join() (done chan struct{}, mine bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if time.Now().Before(w.fresh) {
		return nil, false
	}
	if w.checking != nil {
		return w.checking, false
	}
	w.checking = make(chan struct{})
	return w.checking, true
}

// settle records the reader's result and releases every door waiting on it.
func (w *workAdmission) settle(done chan struct{}, confirmed bool, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch {
	case confirmed:
		w.fresh = time.Now().Add(driveWorkFreshFor)
	case err != nil:
		w.fresh = time.Now().Add(driveWorkOfflineFor)
	}
	w.checking = nil
	close(done)
}
