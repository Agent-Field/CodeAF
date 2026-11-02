package main

import (
	"context"
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// A watch socket is the directory's online signal. Merely upserting a record
// does not advance last seen, because those timestamps belong to the relay.
func keepServePresence(ctx context.Context, _ string) {
	ticks := time.NewTicker(syncsetup.DefaultInterval)
	defer ticks.Stop()
	keepServeHomePresence(ctx, home.Dir(), ticks.C)
}

// Reopening uses the home-sync gates and releases the watch on a changed binding.
// Serving itself can continue while a newly paired identity starts its own lifetime.
func keepServeHomePresence(ctx context.Context, dir string, ticks <-chan time.Time) {
	if ctx.Err() != nil || cell.UnavailableReason() != "" || syncsetup.Quiet(dir) {
		return
	}
	current, ok, err := syncsetup.Open(dir)
	if err != nil || !ok {
		return
	}
	var refreshed time.Time
	servePresence(ctx, current.Follow(), ticks, func(ctx context.Context) error {
		if cell.UnavailableReason() != "" || syncsetup.Quiet(dir) {
			return directory.ErrRevoked
		}
		next, ok, err := syncsetup.Open(dir)
		if err != nil || !ok {
			return directory.ErrRevoked
		}
		if next.Identity.ID() != current.Identity.ID() || next.Device.ID() != current.Device.ID() || next.Relay != current.Relay {
			return directory.ErrRevoked
		}
		if !refreshed.IsZero() && time.Since(refreshed) < serveRecordEvery {
			return nil
		}
		err = syncsetup.PutOwnDevice(ctx, next.Dir, next.Identity, next.Device, devname.Name(dir))
		if err == nil {
			refreshed = time.Now()
		}
		return err
	})
}

// Follow shares the one process-wide socket; ticks refresh records synchronously.
func servePresence(ctx context.Context, feed dirwatch.Follower, ticks <-chan time.Time, put func(context.Context) error) {
	if feed != nil {
		defer feed.Close()
	}
	if ctx.Err() != nil {
		return
	}
	refreshServeRecord(ctx, ticks, put)
}

const serveRecordEvery = 2 * time.Minute

func refreshServeRecord(ctx context.Context, ticks <-chan time.Time, put func(context.Context) error) {
	for ctx.Err() == nil {
		request, cancel := context.WithTimeout(ctx, listRequestWithin)
		err := put(request)
		cancel()
		if errors.Is(err, directory.ErrRevoked) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case _, ok := <-ticks:
			if !ok {
				return
			}
		}
	}
}
