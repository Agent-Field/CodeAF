package watchdog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type runnerCall struct {
	name string
	args []string
}

type fakeRunner struct {
	calls  []runnerCall
	errors map[int]error
}

func (runner *fakeRunner) Run(_ context.Context, name string, args ...string) error {
	runner.calls = append(runner.calls, runnerCall{name: name, args: append([]string(nil), args...)})
	return runner.errors[len(runner.calls)-1]
}

func TestDarwinPlistGoldenAndLifecycleUsesRunner(t *testing.T) {
	home := t.TempDir()
	runner := &fakeRunner{}
	manager, err := New(Options{
		Platform: "darwin", HomeDir: home, Executable: "/opt/Aforge & Co/aforge",
		UID: 501, Runner: runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "Library", "LaunchAgents", "ai.agentfield.aforge.wake.plist")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>ai.agentfield.aforge.wake</string>
  <key>ProgramArguments</key>
  <array>
    <string>/opt/Aforge &amp; Co/aforge</string>
    <string>wake</string>
  </array>
  <key>StartInterval</key>
  <integer>300</integer>
  <key>RunAtLoad</key>
  <false/>
</dict>
</plist>
`
	if string(raw) != want {
		t.Fatalf("plist differs\n--- got ---\n%s--- want ---\n%s", raw, want)
	}
	if wantCalls := []runnerCall{{name: "launchctl", args: []string{"bootstrap", "gui/501", path}}}; !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("install calls = %#v, want %#v", runner.calls, wantCalls)
	}

	unload := &fakeRunner{}
	manager.runner = unload
	if err := manager.Uninstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("plist remains after uninstall: %v", err)
	}
	if wantCalls := []runnerCall{{name: "launchctl", args: []string{"bootout", "gui/501", path}}}; !reflect.DeepEqual(unload.calls, wantCalls) {
		t.Fatalf("uninstall calls = %#v, want %#v", unload.calls, wantCalls)
	}
}

func TestDarwinFallsBackForOlderHosts(t *testing.T) {
	runner := &fakeRunner{errors: map[int]error{0: errors.New("unsupported")}}
	manager, err := New(Options{
		Platform: "darwin", HomeDir: t.TempDir(), Executable: "/usr/local/bin/aforge",
		UID: 502, Runner: runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 2 || runner.calls[0].args[0] != "bootstrap" || runner.calls[1].args[0] != "load" {
		t.Fatalf("fallback calls = %#v", runner.calls)
	}
}

func TestLinuxUnitsGoldenAndLifecycleUsesRunner(t *testing.T) {
	home := t.TempDir()
	runner := &fakeRunner{}
	manager, err := New(Options{
		Platform: "linux", HomeDir: home, Executable: "/opt/Aforge Tools/aforge%bin",
		UID: 1000, Runner: runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	servicePath := filepath.Join(unitDir, "aforge-wake.service")
	timerPath := filepath.Join(unitDir, "aforge-wake.timer")
	service, err := os.ReadFile(servicePath)
	if err != nil {
		t.Fatal(err)
	}
	timer, err := os.ReadFile(timerPath)
	if err != nil {
		t.Fatal(err)
	}
	wantService := `[Unit]
Description=Keep aforge standing watches current

[Service]
Type=oneshot
ExecStart="/opt/Aforge Tools/aforge%%bin" wake
`
	wantTimer := `[Unit]
Description=Keep aforge standing watches current

[Timer]
OnActiveSec=5min
OnUnitActiveSec=5min
Unit=aforge-wake.service

[Install]
WantedBy=timers.target
`
	if string(service) != wantService {
		t.Fatalf("service differs\n--- got ---\n%s--- want ---\n%s", service, wantService)
	}
	if string(timer) != wantTimer {
		t.Fatalf("timer differs\n--- got ---\n%s--- want ---\n%s", timer, wantTimer)
	}
	wantInstall := []runnerCall{
		{name: "systemctl", args: []string{"--user", "daemon-reload"}},
		{name: "systemctl", args: []string{"--user", "enable", "--now", "aforge-wake.timer"}},
	}
	if !reflect.DeepEqual(runner.calls, wantInstall) {
		t.Fatalf("install calls = %#v, want %#v", runner.calls, wantInstall)
	}

	remove := &fakeRunner{}
	manager.runner = remove
	if err := manager.Uninstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{servicePath, timerPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("unit remains after uninstall: %s: %v", path, err)
		}
	}
	wantRemove := []runnerCall{
		{name: "systemctl", args: []string{"--user", "disable", "--now", "aforge-wake.timer"}},
		{name: "systemctl", args: []string{"--user", "daemon-reload"}},
	}
	if !reflect.DeepEqual(remove.calls, wantRemove) {
		t.Fatalf("uninstall calls = %#v, want %#v", remove.calls, wantRemove)
	}
}

func TestFailedLinuxStartLeavesRepairableStatus(t *testing.T) {
	home := t.TempDir()
	runner := &fakeRunner{errors: map[int]error{1: errors.New("not ready")}}
	manager, err := New(Options{
		Platform: "linux", HomeDir: home, Executable: "/usr/bin/aforge", UID: 1000, Runner: runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Install(context.Background()); err == nil {
		t.Fatal("install succeeded despite fake start failure")
	}
	status, err := manager.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.Installed {
		t.Fatalf("failed install looked healthy: %+v", status)
	}
	for _, name := range []string{linuxTimerName, linuxServiceName} {
		if _, err := os.Stat(filepath.Join(home, ".config", "systemd", "user", name)); !os.IsNotExist(err) {
			t.Fatalf("failed install left %s: %v", name, err)
		}
	}
}

func TestStatusUsesJournalWakeAndTimerCadenceWithoutShellingOut(t *testing.T) {
	home := t.TempDir()
	runner := &fakeRunner{}
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	last := now.Add(-2 * time.Minute)
	manager, err := New(Options{
		Platform: "linux", HomeDir: home, Executable: "/usr/bin/aforge", Runner: runner,
		LastWake: func() (time.Time, bool, error) { return last, true, nil },
		Now:      func() time.Time { return now }, UID: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	definitions := map[string]string{
		linuxTimerName: linuxTimer(), linuxServiceName: linuxService("/usr/bin/aforge"),
	}
	for name, definition := range definitions {
		if err := os.WriteFile(filepath.Join(unitDir, name), []byte(definition), 0o644); err != nil {
			t.Fatal(err)
		}
		old := now.Add(-10 * time.Minute)
		if err := os.Chtimes(filepath.Join(unitDir, name), old, old); err != nil {
			t.Fatal(err)
		}
	}
	status, err := manager.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !status.Installed || !status.LastWake.Equal(last) || !status.NextDue.Equal(last.Add(Interval)) {
		t.Fatalf("status = %+v", status)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("status shelled out: %#v", runner.calls)
	}
	if err := os.WriteFile(filepath.Join(unitDir, linuxServiceName), []byte("drifted"), 0o644); err != nil {
		t.Fatal(err)
	}
	drifted, err := manager.Status()
	if err != nil {
		t.Fatal(err)
	}
	if drifted.Installed || !drifted.LastWake.Equal(last) || !drifted.NextDue.IsZero() {
		t.Fatalf("drifted status = %+v", drifted)
	}
}
