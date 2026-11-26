package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
)

// V15 (the host's own switches): devices.permit edits THIS computer's permit
// book and nothing else, only for the switches named, answers the row as it now
// stands with the host's sentence, and emits devices.changed. The roster shows
// the switches on every row but this computer's own, only while remote control
// is on; a computer the roster does not name is refused and nothing is written.
func TestAppDevicesPermitEditsTheHostsOwnBookOnly(t *testing.T) {
	celltest.Available(t)
	_, hits, _ := appDevicesRelay(t)
	t.Setenv("CODEAF_REMOTE_CONTROL", "on")
	a, other := t.TempDir(), t.TempDir()
	id, err := identity.Ensure(a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(other, id, false); err != nil {
		t.Fatal(err)
	}
	sa, sb := appDeviceSync(t, a), appDeviceSync(t, other)
	for dir, name := range map[string]string{a: "desk", other: "laptop"} {
		if _, err := devname.Set(dir, name); err != nil {
			t.Fatal(err)
		}
		if err := appDeviceSync(t, dir).Rename(context.Background(), name); err != nil {
			t.Fatal(err)
		}
	}
	_, rec, wire := appDeviceBackend(t, a)
	if !slices.Contains(wire.hello, appwire.FeatureDevicesPermit) {
		t.Fatalf("with remote control on hello must name devices.permit: %v", wire.hello)
	}
	view := appDeviceList(t, wire)
	if len(view.Devices) != 2 || view.Devices[0].Permit != nil || view.Devices[1].Permit == nil {
		t.Fatalf("the switches belong on the other computer's row and never on this one's: %+v", view.Devices)
	}
	before := *view.Devices[1].Permit

	book := pair.Permits()
	bookFile := filepath.Join(a, "v3", "remote", "permits.json")
	untouched, _ := os.ReadFile(bookFile)
	if _, err := wire.call("devices.permit", map[string]any{"id": "nosuchdevice", "watch": true}); err == nil || err.Code != appwire.CodeNotFound {
		t.Fatalf("an unknown computer must be not_found, got %v", err)
	}
	if _, err := wire.call("devices.permit", map[string]any{"id": sa.Device.ID(), "drive": true}); err == nil || err.Code != appwire.CodeBadParams {
		t.Fatalf("this computer's own row has no switches, got %v", err)
	}
	if _, err := wire.call("devices.permit", map[string]any{"id": sb.Device.ID()}); err == nil || err.Code != appwire.CodeBadParams {
		t.Fatalf("a call that names no switch changes nothing, got %v", err)
	}
	if after, _ := os.ReadFile(bookFile); string(after) != string(untouched) {
		t.Fatalf("a refused change wrote the permit book:\n%s\nwas:\n%s", after, untouched)
	}

	requests := hits.Load()
	on := !before.Drive
	got := machineCall[appwire.DevicePermitResult](t, wire, "devices.permit", map[string]any{"id": sb.Device.ID(), "drive": on})
	if got.Device == nil || got.Device.ID != sb.Device.ID() || got.Device.Permit == nil || got.Device.Permit.Drive != on || got.Device.Permit.Watch != before.Watch {
		t.Fatalf("only the named switch changes, and the row comes back as it now stands: %+v (was %+v)", got.Device, before)
	}
	if !strings.Contains(got.Line, "laptop") {
		t.Fatalf("the receipt names the computer: %q", got.Line)
	}
	if hits.Load() != requests {
		t.Fatalf("a permit change is this computer's own book and sent the relay %d requests", hits.Load()-requests)
	}
	appDeviceEvent(t, rec, func(v appwire.Devices) bool {
		return len(v.Devices) == 2 && v.Devices[1].Permit != nil && v.Devices[1].Permit.Drive == on
	})
	if p, _ := book.For(id.ID(), sb.Device.ID()); p.Drive != on || p.By != "person" {
		t.Fatalf("the host's own book says %+v", p)
	}
	// The other computer's own book is never touched.
	if _, err := os.Stat(filepath.Join(other, "v3", "remote", "permits.json")); err == nil {
		t.Fatal("a permit change reached another computer's book")
	}
	// A switch the call did not name stays where it was.
	got = machineCall[appwire.DevicePermitResult](t, wire, "devices.permit", map[string]any{"id": sb.Device.ID(), "watch": false})
	if got.Device.Permit.Watch || got.Device.Permit.Drive != on {
		t.Fatalf("naming watch must leave drive alone: %+v", got.Device.Permit)
	}
}
