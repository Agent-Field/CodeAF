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
	d := newLiveDrive(func(changed func()) (*syncsetup.Drive, error) {
		drive, err := start(changed)
		if err == nil && drive != nil && drive.Gate() == nil {
			check.mu.Lock()
			check.fresh = time.Now().Add(driveWorkFreshFor)
			check.mu.Unlock()
		}
		return drive, err
	})
	workAdmissions.Store(d, check)
	return d
}

func (w *workAdmission) admit(d *syncsetup.Drive) error {
	if err := d.Gate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), driveWorkWithin)
	defer cancel()
	// Concurrent doors share the read and its result, including a failure.
	// Waiting for another door consumes this caller's budget too.
	w.mu.Lock()
	if time.Now().Before(w.fresh) {
		w.mu.Unlock()
		return d.Gate()
	}
	if done := w.checking; done != nil {
		w.mu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
			log.Print("sync: checking chat ownership; admitting work: ", ctx.Err())
		}
		return d.Gate()
	}
	done := make(chan struct{})
	w.checking = done
	w.mu.Unlock()
	confirmed, err := d.ConfirmWork(ctx)
	w.mu.Lock()
	switch {
	case confirmed:
		w.fresh = time.Now().Add(driveWorkFreshFor)
	case err != nil:
		w.fresh = time.Now().Add(driveWorkOfflineFor)
	}
	w.checking = nil
	close(done)
	w.mu.Unlock()
	if err != nil {
		log.Print("sync: checking chat ownership; admitting work: ", err)
	}
	return d.Gate()
}
