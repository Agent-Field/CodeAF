package appwire

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// remoteControlEngine is an engine that serves the Remote control switch: the
// plain fake plus the marker the hello word follows.
type remoteControlEngine struct{ *fakeEngine }

func (remoteControlEngine) RemoteControlSettings() {}

// R2, R7: the switch is announced by an engine that serves it, and only by one.
// A desktop that sees no word draws no row and sends no field, because an engine
// that has the field not is refusing a whole write over it.
func TestTheRemoteControlSwitchIsAnnouncedOnlyByAnEngineThatServesIt(t *testing.T) {
	plain := startWire(t, &fakeEngine{})
	if slices.Contains(features(plain.next()), any(FeatureSettingsRemoteControl)) {
		t.Fatal("an engine with no remote control setting announced one")
	}
	fake := &fakeEngine{}
	served := startWireOver(t, remoteControlEngine{fake}, fake)
	if !slices.Contains(features(served.next()), any(FeatureSettingsRemoteControl)) {
		t.Fatal("an engine that serves the switch did not announce it")
	}
	if FeatureSettingsRemoteControl != "settings.remote_control" {
		t.Fatalf("the word is %q, and the desktop spells it settings.remote_control", FeatureSettingsRemoteControl)
	}
}

// R3: the switch is one more field of settings.set, read as a plain true or
// false and nothing else, the way the memory switch is; and an engine that does
// not serve it refuses it in the words an older engine used for any field it
// lacked, so the desktop's own check and the engine's agree.
func TestTheRemoteControlSwitchTravelsThroughTheSettingsDoor(t *testing.T) {
	fake := &fakeEngine{}
	e := remoteControlEngine{fake}
	for _, tc := range []struct {
		params string
		want   bool
	}{{`{"remote_control":true}`, true}, {`{"remote_control":false}`, false}} {
		fake.patch = SettingsPatch{}
		if _, err := methods["settings.set"].call(e, json.RawMessage(tc.params)); err != nil {
			t.Fatalf("%s: %v", tc.params, err)
		}
		if fake.patch.RemoteControl == nil || *fake.patch.RemoteControl != tc.want {
			t.Fatalf("%s reached the engine as %+v", tc.params, fake.patch.RemoteControl)
		}
		if fake.patch.MemoryEnabled != nil || fake.patch.DefaultModel != nil {
			t.Fatalf("%s changed a field it did not name: %+v", tc.params, fake.patch)
		}
	}
	for _, bad := range []string{`{"remote_control":null}`, `{"remote_control":"on"}`, `{"remote_control":1}`} {
		fake.patch = SettingsPatch{}
		_, err := methods["settings.set"].call(e, json.RawMessage(bad))
		if codeOf(err) != CodeBadParams {
			t.Fatalf("%s = %v, want bad_params", bad, err)
		}
		if fake.patch.RemoteControl != nil {
			t.Fatalf("%s reached the engine", bad)
		}
	}
	_, err := methods["settings.set"].call(&fakeEngine{}, json.RawMessage(`{"remote_control":true}`))
	if codeOf(err) != CodeBadParams || !strings.Contains(err.Error(), "settings has no field remote_control") {
		t.Fatalf("an engine without the switch answered %v", err)
	}
}

// R3, R4, R7: the read carries the value and, when the environment fixes it,
// the name that does; an engine that does not serve the switch sends neither, so
// a reader that predates it sees the same bytes it always saw.
func TestTheRemoteControlSettingIsReadAsTheSwitchAndWhatPinsIt(t *testing.T) {
	on := true
	raw, err := json.Marshal(Settings{RemoteControl: &on, RemoteControlPinnedBy: "CODEAF_REMOTE_CONTROL"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"remote_control":true`) || !strings.Contains(string(raw), `"remote_control_pinned_by":"CODEAF_REMOTE_CONTROL"`) {
		t.Fatalf("the read says %s", raw)
	}
	off := false
	raw, err = json.Marshal(Settings{RemoteControl: &off})
	if err != nil || !strings.Contains(string(raw), `"remote_control":false`) || strings.Contains(string(raw), "pinned_by") {
		t.Fatalf("an off switch says %s, %v", raw, err)
	}
	raw, err = json.Marshal(Settings{})
	if err != nil || strings.Contains(string(raw), "remote_control") {
		t.Fatalf("an engine without the switch says %s, %v", raw, err)
	}
}
