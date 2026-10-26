package appwire

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
	"time"
)

// V6: registration alone does not advertise device management.
func TestDevicesV6OldEngineHasNoFeatureOrMethods(t *testing.T) {
	w := startWire(t, &fakeEngine{})
	if slices.ContainsFunc(features(w.next()), func(v any) bool { return v == FeatureDevicesManage }) {
		t.Fatal("an old engine advertised device management")
	}
	for _, name := range []string{"devices.list", "devices.rename", "devices.remove"} {
		if _, err := methods[name].call(&fakeEngine{}, json.RawMessage(`{}`)); codeOf(err) != CodeUnknownMethod {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

// V1, V5: unknown facts are absent and the event has the same snapshot envelope as list.
func TestDevicesV1V5WireShapes(t *testing.T) {
	device := Device{ID: "self", Name: "desk", Self: true}
	raw, err := json.Marshal(DevicesResult{Devices: Devices{Devices: []Device{device}, Mode: DevicesQuiet, Line: "pair from a terminal"}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"devices":{"devices":[{"id":"self","name":"desk","self":true,"removed":false}],"mode":"quiet","line":"pair from a terminal"}}`
	if string(raw) != want {
		t.Fatalf("list = %s, want %s", raw, want)
	}
	for _, reply := range []struct {
		value any
		want  string
	}{
		{DeviceRenameResult{Device: device, Line: "renamed"}, `{"device":{"id":"self","name":"desk","self":true,"removed":false},"line":"renamed"}`},
		{DeviceRemoveResult{ID: "other", Line: "removed"}, `{"id":"other","line":"removed"}`},
	} {
		raw, err := json.Marshal(reply.value)
		if err != nil || string(raw) != reply.want {
			t.Fatalf("%T = %s, %v; want %s", reply.value, raw, err, reply.want)
		}
	}
	offline := false
	device.Online, device.LastSeen = &offline, time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	raw, err = json.Marshal(device)
	if err != nil || string(raw) != `{"id":"self","name":"desk","self":true,"removed":false,"last_seen":"2026-10-26T12:00:00Z","online":false}` {
		t.Fatalf("known facts = %s, %v", raw, err)
	}
	if EventShape(EventDevicesChanged) != reflect.TypeFor[DevicesResult]() {
		t.Fatal("devices.changed is not the list envelope")
	}
	schema := Schema()
	for _, name := range []string{"devices.list", "devices.rename", "devices.remove"} {
		if !slices.Contains(Methods(), name) || schema["methods"].(map[string]any)[name] == nil {
			t.Fatalf("missing method or schema: %s", name)
		}
	}
	if schema["events"].(map[string]any)[EventDevicesChanged] == nil {
		t.Fatal("missing event schema")
	}
	for name, want := range map[string]string{
		"devices.list": "DevicesResult", "devices.rename": "DeviceRenameResult", "devices.remove": "DeviceRemoveResult",
	} {
		if got := methods[name].result.Name(); got != want {
			t.Fatalf("%s result = %s, want %s", name, got, want)
		}
	}
}
