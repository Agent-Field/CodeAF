package main

import (
	"cmp"
	"context"
	"errors"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// appDevices observes traffic already owned by the engine. Opening Settings
// does not start a follower; only a person's list request reads the relay.
type appDevices struct {
	home  string
	watch *syncsetup.DeviceObserver
	op    sync.Mutex
	done  chan struct{}
}

func (b *appBackend) listenDevices() *appDevices {
	d := &appDevices{home: home.Dir(), done: make(chan struct{})}
	d.watch = syncsetup.ObserveDevices(d.home)
	guard.Go("app/devices", func() {
		defer close(d.done)
		for range d.watch.Changes() {
			if view, err := d.snapshot(); err == nil {
				b.emit.Emit(appwire.EventDevicesChanged, appwire.DevicesResult{Devices: view})
			}
		}
	})
	return d
}

func (d *appDevices) close() {
	d.watch.Close()
	<-d.done
}

func (d *appDevices) snapshot() (appwire.Devices, error) {
	// A first read needs a stable local device id without turning a fresh
	// identity into a fleet that is allowed to contact the relay.
	id, err := identity.EnsureSolo(d.home)
	if err != nil {
		return appwire.Devices{}, err
	}
	self, err := identity.Device(d.home)
	if err != nil {
		return appwire.Devices{}, err
	}
	view := appwire.Devices{Mode: appwire.DevicesOn}
	known := d.watch.Snapshot()
	switch {
	case !syncsetup.Resolve(d.home).On():
		view.Mode, view.Line = appwire.DevicesOff, chatlist.SyncOff
	case !cell.Available():
		view.Mode, view.Line = appwire.DevicesUnavailable, cell.UnavailableReason()
	case errors.Is(known.Refused, dirwatch.ErrRevoked):
		view.Mode, view.Line = appwire.DevicesUnavailable, chatlist.Removed
	case errors.Is(known.Refused, dirwatch.ErrRotated):
		view.Mode, view.Line = appwire.DevicesUnavailable, chatlist.Replaced
	case !syncsetup.MayTalk(d.home):
		view.Mode, view.Line = appwire.DevicesQuiet, chatlist.DevicesQuiet
	}
	if view.Mode != appwire.DevicesOn {
		if rec, ok := known.Devices[self.ID()]; ok {
			known.Devices = map[string]directory.Device{self.ID(): rec}
		} else {
			known.Devices = nil
		}
		known.Online = nil
	}
	open := func(sealed string) (string, error) {
		return directory.OpenName(directory.MetadataKey(id.CellKey()), sealed)
	}
	view.Devices = make([]appwire.Device, 0, len(known.Devices)+1)
	for deviceID, rec := range known.Devices {
		row := appwire.Device{ID: deviceID, Name: chatlist.DeviceName(known.Devices, deviceID, open), Platform: rec.Platform, Self: deviceID == self.ID(), Removed: rec.Revoked}
		if rec.LastSeen > 0 {
			row.LastSeen = time.UnixMilli(rec.LastSeen).UTC()
		}
		if online, ok := known.Online[deviceID]; ok && !rec.Revoked {
			row.Online = &online
		}
		view.Devices = append(view.Devices, row)
	}
	selfAt := slices.IndexFunc(view.Devices, func(row appwire.Device) bool { return row.Self })
	if selfAt < 0 {
		view.Devices = append(view.Devices, appwire.Device{ID: self.ID(), Self: true})
		selfAt = len(view.Devices) - 1
	}
	view.Devices[selfAt].Name, view.Devices[selfAt].Platform = devname.Name(d.home), runtime.GOOS
	slices.SortFunc(view.Devices, func(a, b appwire.Device) int {
		return cmp.Or(-cmp.Compare(rank(a.Self), rank(b.Self)), cmp.Compare(rank(a.Removed), rank(b.Removed)), cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
	})
	return view, nil
}

func (b *appBackend) ListDevices() (appwire.Devices, error) {
	d := b.devices
	d.op.Lock()
	defer d.op.Unlock()
	view, err := d.snapshot()
	if err != nil || view.Mode != appwire.DevicesOn {
		return view, err
	}
	s, ok, err := syncsetup.Open(d.home)
	if err != nil || !ok {
		view.Devices, view.Mode = view.Devices[:1], appwire.DevicesUnavailable
		view.Devices[0].Online = nil
		view.Line = chatlist.SyncOff
		if err != nil {
			view.Line = err.Error()
		}
		return view, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), syncsetup.DeviceRequestWithin)
	defer cancel()
	if _, err := s.Dir.List(ctx); err != nil {
		return appwire.Devices{}, deviceError(err)
	}
	if _, err := s.Dir.Presence(ctx); err != nil && !errors.Is(err, directory.ErrTooOld) {
		return appwire.Devices{}, deviceError(err)
	}
	return d.snapshot()
}

func (b *appBackend) RenameDevice(typed string) (appwire.DeviceRenameResult, error) {
	d := b.devices
	d.op.Lock()
	defer d.op.Unlock()
	if _, err := devname.Clean(typed); err != nil {
		return appwire.DeviceRenameResult{}, appwire.Errorf(appwire.CodeBadParams, "%s", err)
	}
	if reason := cell.UnavailableReason(); reason != "" {
		return appwire.DeviceRenameResult{}, appwire.Errorf(appwire.CodeEngine, "%s", reason)
	}
	line, err := syncsetup.RenameDevice(context.Background(), d.home, typed)
	if err != nil {
		return appwire.DeviceRenameResult{}, err
	}
	view, err := d.snapshot()
	if err != nil {
		return appwire.DeviceRenameResult{}, err
	}
	b.emit.Emit(appwire.EventDevicesChanged, appwire.DevicesResult{Devices: view})
	return appwire.DeviceRenameResult{Device: view.Devices[0], Line: line}, nil
}

func (b *appBackend) RemoveDevice(deviceID string) (appwire.DeviceRemoveResult, error) {
	d := b.devices
	d.op.Lock()
	defer d.op.Unlock()
	view, err := d.snapshot()
	if err != nil {
		return appwire.DeviceRemoveResult{}, err
	}
	if deviceID == view.Devices[0].ID {
		return appwire.DeviceRemoveResult{}, appwire.Errorf(appwire.CodeBadParams, "%s", chatlist.RemoveSelf)
	}
	if view.Mode != appwire.DevicesOn {
		return appwire.DeviceRemoveResult{}, appwire.Errorf(appwire.CodeNotFound, "%s", chatlist.DeviceMissing)
	}
	s, ok, err := syncsetup.Open(d.home)
	if err != nil {
		return appwire.DeviceRemoveResult{}, deviceError(err)
	}
	if !ok {
		return appwire.DeviceRemoveResult{}, appwire.Errorf(appwire.CodeEngine, "%s", chatlist.SyncOff)
	}
	ctx, cancel := context.WithTimeout(context.Background(), syncsetup.DeviceRequestWithin)
	defer cancel()
	listing, err := s.Dir.List(ctx)
	if err != nil {
		return appwire.DeviceRemoveResult{}, deviceError(err)
	}
	rec, exists := listing.Devices[deviceID]
	if !exists {
		return appwire.DeviceRemoveResult{}, deviceError(directory.ErrNotFound)
	}
	open := func(sealed string) (string, error) {
		return directory.OpenName(directory.MetadataKey(s.Identity.CellKey()), sealed)
	}
	name := chatlist.DeviceName(listing.Devices, deviceID, open)
	line := name + " is already removed."
	if !rec.Revoked {
		if err := s.Dir.Revoke(ctx, deviceID); err != nil {
			return appwire.DeviceRemoveResult{}, deviceError(err)
		}
		line = pair.ChatsRevokedLine(name)
	}
	// Success is retained even if a later user-requested refresh cannot reach
	// the relay. The revoke already updated this process's remembered record.
	view, err = d.snapshot()
	if err == nil {
		b.emit.Emit(appwire.EventDevicesChanged, appwire.DevicesResult{Devices: view})
	}
	return appwire.DeviceRemoveResult{ID: deviceID, Line: line}, nil
}

func deviceError(err error) error {
	switch {
	case errors.Is(err, directory.ErrSelfRevoke):
		return appwire.Errorf(appwire.CodeBadParams, "%s", chatlist.RemoveSelf)
	case errors.Is(err, directory.ErrNotFound):
		return appwire.Errorf(appwire.CodeNotFound, "%s", chatlist.DeviceMissing)
	case errors.Is(err, directory.ErrUnreachable):
		return appwire.Errorf(appwire.CodeUnreachable, "%s", chatlist.Unreachable)
	case errors.Is(err, directory.ErrRevoked):
		return appwire.Errorf(appwire.CodeEngine, "%s", chatlist.Removed)
	case errors.Is(err, directory.ErrRotated):
		return appwire.Errorf(appwire.CodeEngine, "%s", chatlist.Replaced)
	case errors.Is(err, wireauth.ErrSkew):
		return appwire.Errorf(appwire.CodeEngine, "%s", chatlist.ClockOff)
	}
	return err
}
