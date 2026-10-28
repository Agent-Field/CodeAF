package main

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/identity"
)

func TestAppSyncedSettingsS1(t *testing.T) {
	celltest.Available(t)
	relay := e2WireRelay(t)
	t.Setenv("CODEAF_SYNC_URL", relay.URL)
	a, b := t.TempDir(), t.TempDir()
	id, err := identity.Ensure(a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = identity.Adopt(b, id, false); err != nil {
		t.Fatal(err)
	}
	sa, sb := appDeviceSync(t, a), appDeviceSync(t, b)
	backend, rec, wire := appDeviceBackend(t, b)
	_ = machineCall[appwire.Settings](t, wire, "settings.get", struct{}{})
	if err = config.WriteDefaultEffort(a, effort.High); err != nil {
		t.Fatal(err)
	}
	if err = sa.SyncState(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = sb.SyncState(t.Context()); err != nil {
		t.Fatal(err)
	}
	settings, err := backend.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Effort != "high" {
		t.Fatalf("S1 preference did not arrive: %+v", settings)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	var events []appwire.SettingsChanged
	for i, e := range rec.events {
		if e == appwire.EventSettingsChanged {
			events = append(events, rec.data[i].(appwire.SettingsChanged))
		}
	}
	if len(events) != 1 || !reflect.DeepEqual(events[0].Settings, settings) {
		t.Fatalf("S1 events=%+v, want one full settings object %+v", events, settings)
	}
	rec.mu.Unlock()
	if err = sb.SyncState(t.Context()); err != nil {
		t.Fatal(err)
	}
	rec.mu.Lock()
	count := 0
	for _, e := range rec.events {
		if e == appwire.EventSettingsChanged {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("S1 unchanged round emitted again: %d", count)
	}
}

func TestAppDevicesChangedS3(t *testing.T) {
	celltest.Available(t)
	appDevicesRelay(t)
	backend, rec, wire := appDeviceBackend(t, t.TempDir())
	initial := appDeviceList(t, wire)
	for range 3 {
		if _, err := backend.RenameDevice("S3 computer"); err != nil {
			t.Fatal(err)
		}
	}
	appDeviceEvent(t, rec, func(v appwire.Devices) bool { return v.Devices[0].Name == "S3 computer" })
	rec.mu.Lock()
	defer rec.mu.Unlock()
	previous := initial
	for i, e := range rec.events {
		if e != appwire.EventDevicesChanged {
			continue
		}
		view := rec.data[i].(appwire.DevicesResult).Devices
		if reflect.DeepEqual(previous, view) {
			t.Fatalf("S3 duplicate devices.changed: %+v", view)
		}
		previous = view
	}
}
