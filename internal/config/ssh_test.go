package config

import "testing"

func TestSSHTransportUsesTheFastResponsiveDefaults(t *testing.T) {
	got := SSHTransportAt(t.TempDir())
	if got.ControlPersistSeconds != DefaultSSHControlPersist ||
		got.ServerAliveSeconds != DefaultSSHServerAlive ||
		got.ServerAliveMisses != DefaultSSHServerMisses ||
		got.IPQoS != DefaultSSHIPQoS {
		t.Fatalf("SSHTransportAt = %+v", got)
	}
}

func TestSSHTransportChangesOnlyThroughRegistryRows(t *testing.T) {
	dir := t.TempDir()
	rows := NewSettings(SettingsOptions{ProfileDir: dir})
	values := map[string]string{
		KeySSHControlPersist: "45",
		KeySSHServerAlive:    "7",
		KeySSHServerMisses:   "2",
		KeySSHIPQoS:          "af21",
	}
	for key, value := range values {
		row, ok := rows.Row(key)
		if !ok {
			t.Fatalf("no registry row for %s", key)
		}
		if err := row.Apply(value); err != nil {
			t.Fatalf("write %s: %v", key, err)
		}
	}
	got := SSHTransportAt(dir)
	if got.ControlPersistSeconds != 45 || got.ServerAliveSeconds != 7 ||
		got.ServerAliveMisses != 2 || got.IPQoS != "af21" {
		t.Fatalf("SSHTransportAt = %+v", got)
	}
}
