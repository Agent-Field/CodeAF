package main

import (
	"context"
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func (d *appDevices) wakePublication() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// publishOwn owns one retry and shares the inventory's existing watch. A paired
// computer is present even when it has no chats; a successful upsert sleeps
// until a name, pairing or connection changes.
func (d *appDevices) publishOwn(ctx context.Context) {
	var source *syncsetup.Sync
	var feed dirwatch.Follower
	var changes <-chan struct{}
	var retry *time.Timer
	var later <-chan time.Time
	var wait time.Duration
	var up bool
	var connected bool
	var version uint64
	defer func() {
		if retry != nil {
			retry.Stop()
		}
		if feed != nil {
			feed.Close()
		}
	}()
	attempt := func() error {
		d.op.Lock()
		defer d.op.Unlock()
		if !syncsetup.MayTalk(d.home) {
			return nil
		}
		s, ok, err := syncsetup.Open(d.home)
		if err != nil || !ok {
			return err
		}
		if source != nil && (source.Relay != s.Relay || source.Identity.ID() != s.Identity.ID() || source.Device.ID() != s.Device.ID()) && feed != nil {
			feed.Close()
			feed, changes, up, connected, version = nil, nil, false, false, 0
		}
		source = s
		// Bind the observer before this new pairing's first request returns.
		d.watch.Snapshot(s.Identity, s.Device)
		one, cancel := context.WithTimeout(ctx, syncsetup.DeviceRequestWithin)
		defer cancel()
		if feed == nil {
			feed = s.Follow()
			if feed != nil {
				changes = feed.Changes()
				state := feed.State()
				up, connected, version = state.Up, state.Up, state.Version
			}
		}
		// Subscribe before the upsert's listing so an intervening directory
		// version remains queued. The listing and successful PUT already leave
		// a complete roster; a second read must not restart a successful write.
		return syncsetup.PutOwnDevice(one, s.Dir, s.Identity, s.Device, devname.Name(d.home))
	}
	refresh := func() {
		d.op.Lock()
		defer d.op.Unlock()
		one, cancel := context.WithTimeout(ctx, syncsetup.DeviceRequestWithin)
		defer cancel()
		_, _ = source.Dir.List(one)
	}
	for {
		err := attempt()
		if retry != nil {
			retry.Stop()
			later = nil
		}
		if err != nil && !errors.Is(err, directory.ErrRevoked) && !errors.Is(err, directory.ErrRotated) {
			if wait == 0 {
				wait = cellsync.DefaultInterval
			} else {
				wait = min(2*wait, cellsync.MaxBackoff)
			}
			retry = time.NewTimer(wait)
			later = retry.C
		} else {
			wait = 0
		}
	waiting:
		for {
			select {
			case <-ctx.Done():
				return
			case <-d.wake:
				break waiting
			case <-later:
				break waiting
			case <-changes:
				state := feed.State()
				reconnected := state.Up && !up && connected
				changed := state.Version != version
				up = state.Up
				connected = connected || up
				version = state.Version
				if reconnected {
					break waiting
				}
				if state.Up && changed {
					refresh()
				}
			}
		}
	}
}
