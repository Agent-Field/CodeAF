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

// ── the drift a launch repairs ──────────────────────────────────────────────

// THE DEFINITION EMBEDS THE PROGRAM'S PATH, so a binary that moves leaves a
// timer that runs nothing. This is the reading a launch repairs on
// (cmd/aforge's repairBackgroundChecks), on both platforms.
func TestDriftSeesADefinitionPointingAtAnotherProgram(t *testing.T) {
	for _, platform := range []string{"darwin", "linux"} {
		home := t.TempDir()
		runner := &recordingRunner{}
		// A REAL FILE, because a definition naming a program that is not there
		// is itself drift — the case the test below is about.
		program := filepath.Join(t.TempDir(), "aforge")
		if err := os.WriteFile(program, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatalf("seed: %v", err)
		}
		timer, err := NewWatch(WatchOptions{
			Platform: platform, HomeDir: home, Executable: program, UID: 501, Runner: runner,
		})
		if err != nil {
			t.Fatalf("%s: NewWatch: %v", platform, err)
		}
		// Nothing installed at all is nothing to repair, and it is not a fault.
		drift, err2 := timer.Drift()
		if err2 != nil || drift.Present || drift.Stale {
			t.Fatalf("%s: a machine with no timer = %+v (%v)", platform, drift, err2)
		}
		if err := timer.Install(context.Background()); err != nil {
			t.Fatalf("%s: Install: %v", platform, err)
		}
		drift, err = timer.Drift()
		if err != nil || !drift.Present || drift.Stale {
			t.Fatalf("%s: a fresh install reads as drifted: %+v (%v)", platform, drift, err)
		}
		if drift.Executable != program {
			t.Fatalf("%s: the definition names %q", platform, drift.Executable)
		}
		// The same machine, a program that has moved: the definition is still
		// there and it is stale.
		moved, err := NewWatch(WatchOptions{
			Platform: platform, HomeDir: home, Executable: "/opt/aforge/bin/aforge",
			UID: 501, Runner: runner,
		})
		if err != nil {
			t.Fatalf("%s: NewWatch: %v", platform, err)
		}
		drift, err = moved.Drift()
		if err != nil || !drift.Present || !drift.Stale {
			t.Fatalf("%s: a moved program did not read as drift: %+v (%v)", platform, drift, err)
		}
		if drift.Executable != program {
			t.Fatalf("%s: the drift did not say what it was pointing at: %q", platform, drift.Executable)
		}
		// And Status agrees the honest way: nothing is checking.
		if status, err := moved.Status(); err != nil || status.Installed {
			t.Fatalf("%s: a drifted timer claimed to be installed: %+v (%v)", platform, status, err)
		}
	}
}

// AND A DEFINITION THAT STILL READS RIGHT CAN STILL POINT AT NOTHING. The
// bytes match when the program was replaced in place under its own name; they
// also match when that name was deleted, and a timer firing at a path with no
// program on it fails every five minutes in silence.
func TestDriftSeesADefinitionWhoseProgramIsGone(t *testing.T) {
	home := t.TempDir()
	program := filepath.Join(t.TempDir(), "aforge")
	if err := os.WriteFile(program, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	timer, err := NewWatch(WatchOptions{
		Platform: "darwin", HomeDir: home, Executable: program, UID: 501,
		Runner: &recordingRunner{},
	})
	if err != nil {
		t.Fatalf("NewWatch: %v", err)
	}
	if err := timer.Install(context.Background()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if drift, err := timer.Drift(); err != nil || drift.Stale {
		t.Fatalf("a program that is there read as drift: %+v (%v)", drift, err)
	}
	if err := os.Remove(program); err != nil {
		t.Fatalf("remove: %v", err)
	}
	drift, err := timer.Drift()
	if err != nil || !drift.Present || !drift.Stale {
		t.Fatalf("a program that is gone did not read as drift: %+v (%v)", drift, err)
	}
}
