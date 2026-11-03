package config

import "testing"

// Contract 7 keeps placement stable when a paired computer is renamed.
func TestTaskRunsOnKeepsADeviceIDAndDefaultsToThisComputer(t *testing.T) {
	dir := t.TempDir()
	if got := TaskRunsOnAt(dir); got != "" {
		t.Fatalf("default = %q", got)
	}
	settings := NewSettings(SettingsOptions{ProfileDir: dir})
	row, ok := settings.Row(KeyTaskRunsOn)
	if !ok {
		t.Fatal("missing Run tasks on setting")
	}
	if err := row.Apply("work computer"); err == nil {
		t.Fatal("accepted a name instead of a device id")
	}
	const device = "dev_0123456789abcdef0123456789abcdef"
	if err := row.Apply(device); err != nil {
		t.Fatal(err)
	}
	if got := TaskRunsOnAt(dir); got != device {
		t.Fatalf("saved = %q", got)
	}
	if err := row.Apply(""); err != nil {
		t.Fatal(err)
	}
	if got := TaskRunsOnAt(dir); got != "" {
		t.Fatalf("here = %q", got)
	}
}
