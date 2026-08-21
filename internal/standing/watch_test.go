package standing

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// recordingRunner is the host, stood in for. Nothing in these tests touches
// launchctl or systemctl.
type recordingRunner struct {
	calls []string
	fail  map[string]error
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) error {
	call := strings.Join(append([]string{name}, args...), " ")
	r.calls = append(r.calls, call)
	return r.fail[name]
}

func (r *recordingRunner) saw(fragment string) bool {
	for _, call := range r.calls {
		if strings.Contains(call, fragment) {
			return true
		}
	}
	return false
}

func newTimer(t *testing.T, platform, homeDir, wakeLog string, runner WatchRunner, now time.Time) *Timer {
	t.Helper()
	timer, err := NewWatch(WatchOptions{
		Platform:   platform,
		HomeDir:    homeDir,
		Executable: "/usr/local/bin/aforge",
		UID:        501,
		Runner:     runner,
		WakeLog:    wakeLog,
		Now:        held(now),
	})
	if err != nil {
		t.Fatalf("cannot build the timer: %v", err)
	}
	return timer
}

func TestWatchInstallsAndUninstallsOnLinux(t *testing.T) {
	homeDir := t.TempDir()
	runner := &recordingRunner{}
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	timer := newTimer(t, "linux", homeDir, "", runner, now)

	if status, err := timer.Status(); err != nil || status.Installed {
		t.Fatalf("nothing is installed yet, but status says %+v (%v)", status, err)
	}
	if err := timer.Install(context.Background()); err != nil {
		t.Fatalf("install: %v", err)
	}

	unitDir := filepath.Join(homeDir, ".config", "systemd", "user")
	service, err := os.ReadFile(filepath.Join(unitDir, "aforge-tick.service"))
	if err != nil {
		t.Fatalf("no service was written: %v", err)
	}
	if !strings.Contains(string(service), `ExecStart="/usr/local/bin/aforge" tick`) {
		t.Fatalf("the service does not run this build's tick: %q", string(service))
	}
	unit, err := os.ReadFile(filepath.Join(unitDir, "aforge-tick.timer"))
	if err != nil {
		t.Fatalf("no timer was written: %v", err)
	}
	if !strings.Contains(string(unit), "OnCalendar=*:0/5") {
		t.Fatalf("the timer's cadence is not the one this build ticks on: %q", string(unit))
	}
	if !strings.Contains(string(unit), "Persistent=true") {
		t.Fatalf("the timer does not catch up after a sleep: %q", string(unit))
	}
	if !runner.saw("systemctl --user daemon-reload") || !runner.saw("enable --now aforge-tick.timer") {
		t.Fatalf("the host was not asked to start it: %v", runner.calls)
	}
	// v1's resident owns aforge-wake; this must not have touched it.
	if runner.saw("aforge-wake") {
		t.Fatalf("v3's timer reached into v1's units: %v", runner.calls)
	}

	status, err := timer.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.Installed {
		t.Fatal("the definition on disk is this build's own and status says otherwise")
	}

	// STATUS IS DERIVED FROM THE BYTES. A definition somebody edited is not
	// this build's timer any more, whatever the file is called.
	if err := os.WriteFile(filepath.Join(unitDir, "aforge-tick.timer"), []byte("[Timer]\nOnCalendar=hourly\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if status, err := timer.Status(); err != nil || status.Installed {
		t.Fatalf("an edited definition still reads as installed: %+v (%v)", status, err)
	}
	if err := timer.Install(context.Background()); err != nil {
		t.Fatalf("reinstalling over drift: %v", err)
	}
	if status, err := timer.Status(); err != nil || !status.Installed {
		t.Fatalf("reinstalling did not repair the drift: %+v (%v)", status, err)
	}

	// A service that went missing is the same answer.
	if err := os.Remove(filepath.Join(unitDir, "aforge-tick.service")); err != nil {
		t.Fatal(err)
	}
	if status, err := timer.Status(); err != nil || status.Installed {
		t.Fatalf("a half-installed timer reads as installed: %+v (%v)", status, err)
	}

	if err := timer.Uninstall(context.Background()); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := os.Stat(filepath.Join(unitDir, "aforge-tick.timer")); !os.IsNotExist(err) {
		t.Fatalf("the timer is still on disk: %v", err)
	}
	if !runner.saw("disable --now aforge-tick.timer") {
		t.Fatalf("the host was not asked to stop it: %v", runner.calls)
	}
	if status, err := timer.Status(); err != nil || status.Installed {
		t.Fatalf("after uninstalling, status is %+v (%v)", status, err)
	}
	// Uninstalling what is not there is not a failure and touches nothing.
	before := len(runner.calls)
	if err := timer.Uninstall(context.Background()); err != nil {
		t.Fatalf("a second uninstall: %v", err)
	}
	if len(runner.calls) != before {
		t.Fatalf("uninstalling nothing still asked the host: %v", runner.calls[before:])
	}
}

func TestWatchInstallsAndUninstallsOnDarwin(t *testing.T) {
	homeDir := t.TempDir()
	runner := &recordingRunner{}
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	timer := newTimer(t, "darwin", homeDir, "", runner, now)

	if err := timer.Install(context.Background()); err != nil {
		t.Fatalf("install: %v", err)
	}
	path := filepath.Join(homeDir, "Library", "LaunchAgents", "ai.agentfield.aforge.tick.plist")
	plist, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no agent was written: %v", err)
	}
	for _, want := range []string{
		"<string>ai.agentfield.aforge.tick</string>",
		"<string>/usr/local/bin/aforge</string>",
		"<string>tick</string>",
		"<integer>300</integer>",
	} {
		if !strings.Contains(string(plist), want) {
			t.Fatalf("the agent is missing %q: %s", want, string(plist))
		}
	}
	if !runner.saw("launchctl bootstrap gui/501 " + path) {
		t.Fatalf("the agent was not loaded: %v", runner.calls)
	}
	if status, err := timer.Status(); err != nil || !status.Installed {
		t.Fatalf("status is %+v (%v)", status, err)
	}

	if err := timer.Uninstall(context.Background()); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the agent is still on disk: %v", err)
	}
	if !runner.saw("launchctl bootout gui/501 " + path) {
		t.Fatalf("the agent was not unloaded: %v", runner.calls)
	}
	if status, err := timer.Status(); err != nil || status.Installed {
		t.Fatalf("after uninstalling, status is %+v (%v)", status, err)
	}
}

func TestWatchStatusReadsTheLastWakeFromTheLog(t *testing.T) {
	homeDir := t.TempDir()
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	timer := newTimer(t, "linux", homeDir, store.WakeLogPath(), &recordingRunner{}, now)

	// No log at all is no last wake, and no error either: a timer installed one
	// minute ago has honestly never woken.
	if status, err := timer.Status(); err != nil || !status.LastWake.IsZero() {
		t.Fatalf("with no wake log status is %+v (%v)", status, err)
	}

	first := now.Add(-10 * time.Minute)
	second := now.Add(-3 * time.Minute)
	if err := store.appendWake(Pass{At: first, Examined: 2, Checked: 2}); err != nil {
		t.Fatal(err)
	}
	if err := store.appendWake(Pass{At: second, Examined: 2, Checked: 2, Fired: 1, Said: 1}); err != nil {
		t.Fatal(err)
	}
	if err := timer.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, err := timer.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.LastWake.Equal(second) {
		t.Fatalf("the last wake is %s, wanted %s", status.LastWake, second)
	}
	if !status.NextDue.Equal(second.Add(Interval)) {
		t.Fatalf("the next check is %s, wanted one interval after the last wake", status.NextDue)
	}
}

func TestWatchRefusesAPlatformItCannotKeep(t *testing.T) {
	if _, err := NewWatch(WatchOptions{Platform: "windows", HomeDir: t.TempDir(), Executable: "aforge"}); err == nil {
		t.Fatal("a platform with neither launchd nor systemd was accepted")
	}
}
