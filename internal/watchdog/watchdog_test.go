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
		Platform: "darwin", HomeDir: home, Executable: "/opt/codeaf & Co/codeaf",
		UID: 501, Runner: runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "Library", "LaunchAgents", "ai.agentfield.codeaf.wake.plist")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>ai.agentfield.codeaf.wake</string>
  <key>ProgramArguments</key>
  <array>
    <string>/opt/codeaf &amp; Co/codeaf</string>
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
		Platform: "darwin", HomeDir: t.TempDir(), Executable: "/usr/local/bin/codeaf",
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
		Platform: "linux", HomeDir: home, Executable: "/opt/codeaf Tools/codeaf%bin",
		UID: 1000, Runner: runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	servicePath := filepath.Join(unitDir, "codeaf-wake.service")
	timerPath := filepath.Join(unitDir, "codeaf-wake.timer")
	service, err := os.ReadFile(servicePath)
	if err != nil {
		t.Fatal(err)
	}
	timer, err := os.ReadFile(timerPath)
	if err != nil {
		t.Fatal(err)
	}
	wantService := `[Unit]
Description=Keep codeaf standing watches current

[Service]
Type=oneshot
ExecStart="/opt/codeaf Tools/codeaf%%bin" wake
`
	wantTimer := `[Unit]
Description=Keep codeaf standing watches current

[Timer]
OnActiveSec=5min
OnUnitActiveSec=5min
Unit=codeaf-wake.service

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
		{name: "systemctl", args: []string{"--user", "enable", "--now", "codeaf-wake.timer"}},
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
		{name: "systemctl", args: []string{"--user", "disable", "--now", "codeaf-wake.timer"}},
		{name: "systemctl", args: []string{"--user", "daemon-reload"}},
	}
	if !reflect.DeepEqual(remove.calls, wantRemove) {
		t.Fatalf("uninstall calls = %#v, want %#v", remove.calls, wantRemove)
	}
}

func TestFormerWakeTimersAreVisibleRepairableAndRemovable(t *testing.T) {
	for _, platform := range []string{"darwin", "linux"} {
		t.Run(platform+"/old-only-status-and-startup-repair", func(t *testing.T) {
			manager := newWatchdogManager(t, platform)
			writeFormerWatchdogDefinitions(t, manager)
			status, err := manager.Status()
			if err != nil || !status.Installed || !status.Stale {
				t.Fatalf("former timer status = %+v (%v)", status, err)
			}
			if err := manager.Install(context.Background()); err != nil {
				t.Fatalf("startup repair: %v", err)
			}
			assertWatchdogGenerations(t, manager, false, true)
			if status, err := manager.Status(); err != nil || !status.Installed || status.Stale {
				t.Fatalf("repaired status = %+v (%v)", status, err)
			}
		})

		t.Run(platform+"/old-only-off", func(t *testing.T) {
			manager := newWatchdogManager(t, platform)
			writeFormerWatchdogDefinitions(t, manager)
			if err := manager.Uninstall(context.Background()); err != nil {
				t.Fatalf("turn off former timer: %v", err)
			}
			assertWatchdogGenerations(t, manager, true, true)
		})

		t.Run(platform+"/both-generations-off", func(t *testing.T) {
			manager := newWatchdogManager(t, platform)
			if err := manager.Install(context.Background()); err != nil {
				t.Fatal(err)
			}
			writeFormerWatchdogDefinitions(t, manager)
			if err := manager.Uninstall(context.Background()); err != nil {
				t.Fatalf("turn off both generations: %v", err)
			}
			assertWatchdogGenerations(t, manager, true, true)
		})
	}
}

func newWatchdogManager(t *testing.T, platform string) *Manager {
	t.Helper()
	manager, err := New(Options{
		Platform: platform, HomeDir: t.TempDir(), Executable: "/usr/bin/codeaf",
		UID: 501, Runner: &fakeRunner{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func writeFormerWatchdogDefinitions(t *testing.T, manager *Manager) {
	t.Helper()
	definitions := map[string]string{manager.legacyDarwinPlistPath(): legacyDarwinPlist(manager.executable)}
	if manager.platform == "linux" {
		definitions = map[string]string{
			manager.legacyLinuxTimerPath():   legacyLinuxTimer(),
			manager.legacyLinuxServicePath(): legacyLinuxService(manager.executable),
		}
	}
	for path, body := range definitions {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func assertWatchdogGenerations(t *testing.T, manager *Manager, currentGone, formerGone bool) {
	t.Helper()
	current := []string{manager.primaryPath()}
	former := []string{manager.legacyPrimaryPath()}
	if manager.platform == "linux" {
		current = []string{manager.linuxTimerPath(), manager.linuxServicePath()}
		former = []string{manager.legacyLinuxTimerPath(), manager.legacyLinuxServicePath()}
	}
	for _, group := range []struct {
		paths []string
		gone  bool
	}{{current, currentGone}, {former, formerGone}} {
		for _, path := range group.paths {
			_, err := os.Stat(path)
			if group.gone && !os.IsNotExist(err) {
				t.Fatalf("%s remains: %v", path, err)
			}
			if !group.gone && err != nil {
				t.Fatalf("%s missing: %v", path, err)
			}
		}
	}
}

func TestFailedLinuxStartLeavesRepairableStatus(t *testing.T) {
	home := t.TempDir()
	runner := &fakeRunner{errors: map[int]error{1: errors.New("not ready")}}
	manager, err := New(Options{
		Platform: "linux", HomeDir: home, Executable: "/usr/bin/codeaf", UID: 1000, Runner: runner,
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
		Platform: "linux", HomeDir: home, Executable: "/usr/bin/codeaf", Runner: runner,
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
		linuxTimerName: linuxTimer(), linuxServiceName: linuxService("/usr/bin/codeaf"),
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
