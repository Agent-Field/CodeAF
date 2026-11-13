package cellsync

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// Contract D1 lets a successful publish renew the same hold and clear its outage.
func TestHeldPublishRenewsHealthAndRemovalEndsIt(t *testing.T) {
	r := newRig(t)
	r.publishFirst(map[string]string{"reply": "first"})
	b := r.batcher()
	b.Now = r.clock.Now
	changes := make(chan struct{}, 8)
	b.OnRenewalChange = func() { changes <- struct{}{} }
	ctx, cancel := context.WithCancel(t.Context())
	ended := make(chan struct{})
	go func() { defer close(ended); b.Run(ctx) }()
	defer func() { cancel(); <-ended }()
	wait := func(what string, want func(time.Time, time.Duration, bool) bool) {
		t.Helper()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for !want(b.RenewalStatus()) {
			select {
			case <-changes:
			case <-timer.C:
				t.Fatalf("did not observe %s", what)
			}
		}
	}
	r.store.set(blobstore.ErrUnreachable, nil)
	note(b, r.seal(map[string]string{"reply": "second"}))
	wait("failed publish", func(since time.Time, period time.Duration, held bool) bool {
		return since.Equal(r.clock.Now()) && period > 0 && held
	})
	r.store.set(nil, nil)
	b.Freed()
	wait("publish recovery", func(since time.Time, _ time.Duration, held bool) bool { return since.IsZero() && held })
	r.dirA.set(wireauth.ErrRevoked)
	note(b, r.seal(map[string]string{"reply": "third"}))
	wait("removal", func(_ time.Time, _ time.Duration, held bool) bool { return !held })
}

// Contract D1 withdraws the hold as soon as a renewal reports displacement.
func TestStaleRenewalWithdrawsHoldWhilePublishIsStillWaiting(t *testing.T) {
	r := newRig(t)
	r.publishFirst(map[string]string{"reply": "first"})
	b := r.batcher()
	entered, release, ticks := make(chan struct{}), make(chan struct{}), make(chan struct{})
	r.store.set(nil, func(ctx context.Context) {
		select {
		case <-entered:
		default:
			close(entered)
		}
		select {
		case <-release:
		case <-ctx.Done():
		}
	})
	b.Sleep = func(ctx context.Context, _ time.Duration) error {
		select {
		case <-ticks:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	changes := make(chan struct{}, 8)
	b.OnRenewalChange = func() { changes <- struct{}{} }
	ctx, cancel := context.WithCancel(t.Context())
	ended := make(chan struct{})
	go func() { defer close(ended); b.Run(ctx) }()
	defer func() { cancel(); close(release); <-ended }()
	note(b, r.seal(map[string]string{"reply": "second"}))
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("publish did not reach the held connection")
	}
	r.dirA.set(directory.ErrFenceStale)
	ticks <- struct{}{}
	select {
	case <-changes:
	case <-time.After(5 * time.Second):
		t.Fatal("a stale renewal waited for the unfinished publish before withdrawing its hold")
	}
	if _, _, held := b.RenewalStatus(); held {
		t.Fatal("a displaced writer still reports holding the chat")
	}
}
