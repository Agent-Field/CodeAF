package config

import (
	"reflect"
	"testing"
)

func TestSyncedPreferenceNotificationsS1(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	var received [][]string
	stop := ObserveSyncedPreferences(a, func(keys []string) {
		// The notification must run after the profile transaction has unlocked.
		if err := WriteDefaultEffort(b, DefaultEffortAt(a)); err != nil {
			t.Error(err)
		}
		received = append(received, keys)
	})
	defer stop()
	apply := func(dir string, changes, expected map[string]string) {
		t.Helper()
		if err := ApplyPreferencesIf(dir, changes, expected); err != nil {
			t.Fatal(err)
		}
	}
	apply(a, map[string]string{KeyEffort: `"high"`}, nil)
	apply(a, map[string]string{KeyEffort: `"high"`}, nil)
	apply(a, map[string]string{KeyEffort: `"low"`}, map[string]string{KeyEffort: `"medium"`})
	apply(b, map[string]string{KeyEffort: `"low"`}, nil)
	if !reflect.DeepEqual(received, [][]string{{KeyEffort}}) {
		t.Fatalf("S1 changed/no-op/race/profile scope: %v", received)
	}
	if err := ApplyPreferences(a, map[string]string{KeyEffort: `invalid`}); err == nil {
		t.Fatal("S1 malformed restore succeeded")
	}
	apply(a, map[string]string{KeyEffort: ""}, nil)
	if len(received) != 2 {
		t.Fatalf("S1 removal notifications: %v", received)
	}
	stop()
	apply(a, map[string]string{KeyEffort: `"low"`}, nil)
	if len(received) != 2 {
		t.Fatal("S1 closed listener notified")
	}
}
