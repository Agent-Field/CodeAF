package main

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// Use the same terminal doors as codeaf pair and codeaf pair approve, with
// real link approval, identity/device installation and own-device publication.
func appDevicePairByLink(t *testing.T, rig *pairRig, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	screen, done := &pairScreenText{}, make(chan error, 1)
	go func() { done <- rig.door(rig.homeB, strings.NewReader(""), screen).run(ctx, args) }()
	typed := screen.waitFor(t, `codeaf pair approve (\S+)`)[1]
	if err := rig.door(rig.homeA, strings.NewReader("a\n"), &pairScreenText{}).run(ctx, []string{"approve", typed}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("terminal pairing did not finish")
	}
}

// V1, V3 amendment: terminal re-pairing clears a learned self removal on the
// same running app backend, even when an old client later reports its refusal.
func TestAppDevicesV1V3RePairClearsRemovalWithoutRestart(t *testing.T) {
	rig := newPairRig(t)
	t.Setenv(home.EnvVar, rig.homeA)
	t.Setenv(syncsetup.URLVar, rig.url)
	t.Setenv(syncsetup.IntervalVar, "")
	appDevicePairByLink(t, rig)
	sa, old := appDeviceSync(t, rig.homeA), appDeviceSync(t, rig.homeB)
	_, _, w := appDeviceBackend(t, rig.homeB)
	if view := appDeviceList(t, w); len(view.Devices) != 2 || view.Devices[0].ID != old.Device.ID() {
		t.Fatalf("initial pairing: %+v", view)
	}
	if err := sa.Dir.Revoke(context.Background(), old.Device.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := w.call("devices.list", struct{}{}); err == nil || err.Code != appwire.CodeEngine || err.Message != chatlist.Removed {
		t.Fatalf("learned removal: %v", err)
	}
	if view := appDeviceList(t, w); view.Mode != appwire.DevicesUnavailable || view.Line != chatlist.Removed || len(view.Devices) != 1 || !view.Devices[0].Removed {
		t.Fatalf("remembered self removal: %+v", view)
	}
	appDevicePairByLink(t, rig, "--replace")
	fresh := appDeviceSync(t, rig.homeB)
	if fresh.Identity.ID() != old.Identity.ID() || fresh.Device.ID() == old.Device.ID() {
		t.Fatal("terminal re-pairing must keep the identity and install a new device")
	}
	for _, late := range []bool{false, true} {
		if late {
			if _, err := old.Dir.List(context.Background()); !errors.Is(err, directory.ErrRevoked) {
				t.Fatalf("the old client must still be refused: %v", err)
			}
		}
		view := appDeviceList(t, w)
		if view.Mode != appwire.DevicesOn || view.Line != "" || len(view.Devices) != 3 || view.Devices[0].ID != fresh.Device.ID() || !view.Devices[0].Self || view.Devices[0].Removed || view.Devices[0].RemoveLine != "" {
			t.Fatalf("re-paired roster (late refusal %v): %+v", late, view)
		}
		at := slices.IndexFunc(view.Devices, func(row appwire.Device) bool { return row.ID == old.Device.ID() })
		if at < 0 || !view.Devices[at].Removed || view.Devices[at].Self || view.Devices[at].RemoveLine != "" {
			t.Fatalf("the old device must remain a separate removed row: %+v", view)
		}
	}
}

// V1 amendment: replacing the pairing accepts its lower directory version and
// drops the old roster, presence and callbacks, including a delayed old list.
func TestAppDevicesV1ReplacementPairingDropsOldRoster(t *testing.T) {
	celltest.Available(t)
	delay := &appDeviceDelayedList{ready: make(chan struct{}), release: make(chan struct{})}
	_, _, down := appDevicesRelay(t, delay.wrap)
	t.Cleanup(delay.resume)
	a, b, c := t.TempDir(), t.TempDir(), t.TempDir()
	id, err := identity.Ensure(a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(b, id, false); err != nil {
		t.Fatal(err)
	}
	old, peer := appDeviceSync(t, a), appDeviceSync(t, b)
	for i := 0; i < 8; i++ {
		if err := old.Rename(context.Background(), "old desk "+strings.Repeat("x", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := peer.Rename(context.Background(), "old partner"); err != nil {
		t.Fatal(err)
	}
	_, rec, w := appDeviceBackend(t, a)
	_ = appDeviceList(t, w)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	before, err := old.Dir.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	watch, err := old.Dir.(directory.Watcher).Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Close()
	done := make(chan error, 1)
	delay.armed.Store(true)
	go func() { _, err := old.Dir.List(ctx); done <- err }()
	delay.wait(t)
	nextID, err := identity.Ensure(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(a, nextID, true); err != nil {
		t.Fatal(err)
	}
	fresh, partner := appDeviceSync(t, a), appDeviceSync(t, c)
	for _, s := range []*syncsetup.Sync{fresh, partner} {
		if _, err := devname.Set(s.Home, "new computer"); err != nil {
			t.Fatal(err)
		}
		if err := s.Rename(ctx, "new computer"); err != nil {
			t.Fatal(err)
		}
	}
	after, err := partner.Dir.List(ctx)
	if err != nil || after.Version == 0 || after.Version >= before.Version {
		t.Fatalf("replacement directory must have a lower version: %d -> %d, %v", before.Version, after.Version, err)
	}
	view := appDeviceList(t, w)
	if view.Mode != appwire.DevicesOn || len(view.Devices) != 2 || view.Devices[0].ID != fresh.Device.ID() || view.Devices[1].ID != partner.Device.ID() {
		t.Fatalf("replacement pairing retained old ids or version: %+v", view)
	}
	delay.resume()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("the old pairing's delayed list did not finish")
	}
	// The old socket and clients remain alive; their later facts must not
	// enter the observer now bound to the replacement pairing.
	if _, err := watch.Next(ctx); err != nil {
		t.Fatal(err)
	}
	if err := old.Rename(ctx, "stale old desk"); err != nil {
		t.Fatal(err)
	}
	if _, err := old.Dir.Presence(ctx); err != nil {
		t.Fatal(err)
	}
	if err := old.Dir.Revoke(ctx, peer.Device.ID()); err != nil {
		t.Fatal(err)
	}
	down.Store(true)
	_ = machineCall[appwire.DeviceRenameResult](t, w, "devices.rename", appwire.DeviceRenameParams{Name: "new cached desk"})
	appDeviceEvent(t, rec, func(v appwire.Devices) bool {
		return v.Mode == appwire.DevicesOn && len(v.Devices) == 2 && v.Devices[0].ID == fresh.Device.ID() && v.Devices[0].Name == "new cached desk" && v.Devices[1].ID == partner.Device.ID() && !v.Devices[0].Removed && !v.Devices[1].Removed && v.Devices[0].Online != nil && !*v.Devices[0].Online && v.Devices[1].Online != nil && !*v.Devices[1].Online
	})
}

// V1 amendment: changing the relay with unchanged identity/device files also
// drops the former relay's roster, version and later callbacks.
func TestAppDevicesV1RelayChangeDropsOldRoster(t *testing.T) {
	celltest.Available(t)
	_, _, _ = appDevicesRelay(t)
	a, b, c := t.TempDir(), t.TempDir(), t.TempDir()
	id, err := identity.Ensure(a)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{b, c} {
		if _, err := identity.Adopt(dir, id, false); err != nil {
			t.Fatal(err)
		}
	}
	old, peer := appDeviceSync(t, a), appDeviceSync(t, b)
	for i := 0; i < 5; i++ {
		if err := old.Rename(context.Background(), "old desk"); err != nil {
			t.Fatal(err)
		}
	}
	if err := peer.Rename(context.Background(), "old partner"); err != nil {
		t.Fatal(err)
	}
	_, rec, w := appDeviceBackend(t, a)
	if view := appDeviceList(t, w); len(view.Devices) != 2 || view.Devices[1].ID != peer.Device.ID() {
		t.Fatalf("former relay's roster: %+v", view)
	}
	_, _, down := appDevicesRelay(t)
	fresh, partner := appDeviceSync(t, a), appDeviceSync(t, c)
	if fresh.Identity.ID() != old.Identity.ID() || fresh.Device.ID() != old.Device.ID() {
		t.Fatal("only the relay should change")
	}
	for _, s := range []*syncsetup.Sync{fresh, partner} {
		if err := s.Rename(context.Background(), "new computer"); err != nil {
			t.Fatal(err)
		}
	}
	view := appDeviceList(t, w)
	if view.Mode != appwire.DevicesOn || len(view.Devices) != 2 || view.Devices[0].ID != fresh.Device.ID() || view.Devices[1].ID != partner.Device.ID() {
		t.Fatalf("new relay retained the former roster or version: %+v", view)
	}
	if _, err := old.Dir.List(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := old.Dir.Revoke(context.Background(), peer.Device.ID()); err != nil {
		t.Fatal(err)
	}
	down.Store(true)
	_ = machineCall[appwire.DeviceRenameResult](t, w, "devices.rename", appwire.DeviceRenameParams{Name: "new relay desk"})
	appDeviceEvent(t, rec, func(v appwire.Devices) bool {
		return len(v.Devices) == 2 && v.Devices[0].Name == "new relay desk" && v.Devices[1].ID == partner.Device.ID() && !v.Devices[1].Removed
	})
}
