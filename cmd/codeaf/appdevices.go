package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/livedoor"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// appDevicesQuiet is the desktop's line for a home that has not paired yet: the
// desktop pairs by itself (devices.pair), so it never sends people to a terminal.
const appDevicesQuiet = "Only this computer is here so far."

// appSyncOff is the desktop's line when sync is off: the desktop turns it on and
// off in its Relay settings, so the terminal's environment-variable advice does not apply.
const appSyncOff = "Sync is off on this computer."

// appDevices owns the paired engine's device publication and shares its
// directory follower with chat inventory. Opening Settings adds no follower.
type appDevices struct {
	home         string
	watch        *syncsetup.DeviceObserver
	op           sync.Mutex
	done         chan struct{}
	emitMu       sync.Mutex
	lastWire     string
	cancel       context.CancelFunc
	wake         chan struct{}
	owned        chan struct{}
	stateChanged func()
}

func (b *appBackend) listenDevices() *appDevices {
	ctx, cancel := context.WithCancel(context.Background())
	d := &appDevices{home: home.Dir(), done: make(chan struct{}), cancel: cancel, wake: make(chan struct{}, 1), owned: make(chan struct{}), stateChanged: b.chats.wakePublication}
	d.watch = syncsetup.ObserveDevices(d.home)
	publishChange := func() {
		d.op.Lock()
		defer d.op.Unlock()
		d.publish(b.emit)
	}
	guard.Go("app/devices", func() {
		defer close(d.done)
		for range d.watch.Changes() {
			publishChange()
		}
	})
	guard.Go("app/own-device", func() { defer close(d.owned); d.publishOwn(ctx) })
	return d
}

// publishDevices compares the wire value because observer hints can describe
// unrelated directory changes, and explicit actions also publish their result.
func (b *appBackend) publishDevices() {
	d := b.devices
	d.op.Lock()
	defer d.op.Unlock()
	d.publish(b.emit)
}

func (d *appDevices) publish(emit appwire.Emitter) {
	d.emitMu.Lock()
	defer d.emitMu.Unlock()
	view, complete, err := d.snapshotState()
	if err != nil || view.Mode == appwire.DevicesOn && !complete {
		return
	}
	encoded, err := json.Marshal(appwire.DevicesResult{Devices: devicesCompared(view)})
	if err != nil || string(encoded) == d.lastWire {
		return
	}
	d.lastWire = string(encoded)
	emit.Emit(appwire.EventDevicesChanged, appwire.DevicesResult{Devices: view})
}

// devicesCompared is the view a change is judged by. A hosted relay stamps the
// asking computer's last seen on every request, so this computer's own stamp,
// and that of any computer online now, moves on each roster read; publishing
// it made the app read again, which stamped again (B8b). Neither is shown.
func devicesCompared(view appwire.Devices) appwire.Devices {
	view.Devices = slices.Clone(view.Devices)
	for i, row := range view.Devices {
		if row.Self || row.Online != nil && *row.Online {
			view.Devices[i].LastSeen = time.Time{}
		}
	}
	return view
}

func (d *appDevices) close() {
	d.cancel()
	<-d.owned
	d.watch.Close()
	<-d.done
}

func (d *appDevices) snapshot() (appwire.Devices, error) {
	view, _, err := d.snapshotState()
	return view, err
}

func (d *appDevices) snapshotState() (appwire.Devices, bool, error) {
	// A first read needs a stable local device id without turning a fresh
	// identity into a fleet that is allowed to contact the relay.
	id, err := identity.EnsureSolo(d.home)
	if err != nil {
		return appwire.Devices{}, false, err
	}
	self, err := identity.Device(d.home)
	if err != nil {
		return appwire.Devices{}, false, err
	}
	view := appwire.Devices{Mode: appwire.DevicesOn}
	known := d.watch.Snapshot(id, self)
	switch {
	case !syncsetup.Resolve(d.home).On():
		view.Mode, view.Line = appwire.DevicesOff, appSyncOff
	case !cell.Available():
		view.Mode, view.Line = appwire.DevicesUnavailable, cell.UnavailableReason()
	case errors.Is(known.Refused, dirwatch.ErrRevoked):
		view.Mode, view.Line = appwire.DevicesUnavailable, appRemoved
	case errors.Is(known.Refused, dirwatch.ErrRotated):
		view.Mode, view.Line = appwire.DevicesUnavailable, chatlist.Replaced
	case !syncsetup.MayTalk(d.home):
		view.Mode, view.Line = appwire.DevicesQuiet, appDevicesQuiet
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
		if view.Mode == appwire.DevicesOn && !row.Self && !row.Removed {
			row.RemoveLine = pair.ChatsRemoveLine(row.Name)
		}
		if rec.LastSeen > 0 {
			row.LastSeen = time.UnixMilli(rec.LastSeen).UTC()
		}
		if online, ok := known.Online[deviceID]; ok && !rec.Revoked {
			row.Online = &online
		}
		view.Devices = append(view.Devices, row)
	}
	// WHAT EACH COMPUTER MAY DO IN THIS ONE'S CHATS is read from this computer's
	// own permit book, and said only while remote control is on: with it off the
	// rows are as they always were (devices.permit, app_live_open.go).
	if view.Mode == appwire.DevicesOn && config.RemoteControlOn() {
		book := pair.Permits()
		for i := range view.Devices {
			if row := view.Devices[i]; !row.Self && !row.Removed {
				p, _ := book.For(id.ID(), row.ID)
				view.Devices[i].Permit = &appwire.DevicePermit{Watch: p.Watch, Drive: p.Drive}
			}
		}
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
	return view, known.Complete, nil
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
		view.Line = appSyncOff
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
	if _, err := s.Dir.Presence(ctx); err != nil {
		// The successful roster establishes reachability. Presence is advisory
		// and can fail independently; only an authority refusal invalidates it.
		if errors.Is(err, directory.ErrRevoked) || errors.Is(err, directory.ErrRotated) {
			return appwire.Devices{}, deviceError(err)
		}
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
	d.wakePublication()
	view, err := d.snapshot()
	if err != nil {
		return appwire.DeviceRenameResult{}, err
	}
	d.publish(b.emit)
	return appwire.DeviceRenameResult{Device: view.Devices[0], Line: line}, nil
}

var (
	_ appwire.DevicePermitter   = (*appBackend)(nil)
	_ appwire.DevicePermitOffer = (*appBackend)(nil)
)

// DevicePermitsOffered says the desktop may be told about devices.permit: the
// switches only mean something while remote control is on.
func (*appBackend) DevicePermitsOffered() bool { return config.RemoteControlOn() }

// PermitDevice sets what a paired computer may do in THIS computer's chats. It
// edits this computer's own permit book and nothing else: the book never leaves
// the machine, so it asks the relay for no write and no other computer for its
// word. Only the switches named change; the host's own watcher closes the open
// views the change affects.
func (b *appBackend) PermitDevice(p appwire.DevicePermitParams) (appwire.DevicePermitResult, error) {
	d := b.devices
	d.op.Lock()
	defer d.op.Unlock()
	view, err := d.snapshot()
	if err != nil {
		return appwire.DevicePermitResult{}, err
	}
	at := slices.IndexFunc(view.Devices, func(row appwire.Device) bool { return row.ID == p.ID })
	switch {
	case at < 0 || view.Mode != appwire.DevicesOn:
		return appwire.DevicePermitResult{}, appwire.Errorf(appwire.CodeNotFound, "%s", chatlist.DeviceMissing)
	case view.Devices[at].Self:
		return appwire.DevicePermitResult{}, appwire.Errorf(appwire.CodeBadParams, "this computer is always allowed in its own chats")
	case view.Devices[at].Removed:
		return appwire.DevicePermitResult{}, appwire.Errorf(appwire.CodeNotFound, "%s", chatlist.DeviceMissing)
	}
	id, err := identity.Load(d.home)
	if err != nil || id.ID() == "" {
		return appwire.DevicePermitResult{}, appwire.Errorf(appwire.CodeEngine, "this computer has no identity yet")
	}
	book := pair.Permits()
	if err := book.Set(id.ID(), p.ID, p.Watch, p.Drive, "person"); err != nil {
		return appwire.DevicePermitResult{}, appEngineError(err)
	}
	view, err = d.snapshot()
	if err != nil {
		return appwire.DevicePermitResult{}, err
	}
	d.publish(b.emit)
	row := view.Devices[slices.IndexFunc(view.Devices, func(r appwire.Device) bool { return r.ID == p.ID })]
	got, class := book.For(id.ID(), p.ID)
	line := fmt.Sprintf("%s: watch %s, type %s (%s)", row.Name, permitWord(got.Watch), permitWord(got.Drive), permitClassWord(class))
	return appwire.DevicePermitResult{Device: &row, Line: line}, nil
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
		return appwire.DeviceRemoveResult{}, appwire.Errorf(appwire.CodeEngine, "%s", appSyncOff)
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
		if u := activePhoneUnpairer(); u != nil {
			_ = u.Unpair(ctx, deviceID)
		}
		_ = syncsetup.ForgetPhone(home.Dir(), deviceID)
		livedoor.NotifyRemoved(home.Dir(), deviceID)
		_ = pair.Permits().Forget(deviceID)
		forgetViewed(home.Dir(), deviceID)
		line = pair.ChatsRevokedLine(name)
	}
	// Success is retained even if a later user-requested refresh cannot reach
	// the relay. The revoke already updated this process's remembered record.
	view, err = d.snapshot()
	if err == nil {
		d.publish(b.emit)
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
		return appwire.Errorf(appwire.CodeEngine, "%s", appRemoved)
	case errors.Is(err, directory.ErrRotated):
		return appwire.Errorf(appwire.CodeEngine, "%s", chatlist.Replaced)
	case errors.Is(err, wireauth.ErrSkew):
		return appwire.Errorf(appwire.CodeEngine, "%s", chatlist.ClockOff)
	}
	return err
}
