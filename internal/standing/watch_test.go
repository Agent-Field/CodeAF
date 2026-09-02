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

// newTimer is a timer for a real program: STATUS LOOKS FOR THE PROGRAM, so a
// path with nothing on it would read as a timer running nothing.
func newTimer(t *testing.T, platform, homeDir, wakeLog string, runner WatchRunner, now time.Time) *Timer {
	t.Helper()
	timer, err := NewWatch(WatchOptions{
		Platform:   platform,
		HomeDir:    homeDir,
		Executable: realProgram(t),
		StateRoot:  filepath.Join(homeDir, ".aforge"),
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
	if !strings.Contains(string(service), `ExecStart="`+timer.executable+`" tick`) {
		t.Fatalf("the service does not run this build's tick: %q", string(service))
	}
	// THE TICK RUNS AGAINST THE HOME THAT INSTALLED IT. A unit that carried no
	// home ticked the login's default one, whatever AFORGE_HOME the window
	// that turned the row on was running under.
	if !strings.Contains(string(service), `Environment="AFORGE_HOME=`+filepath.Join(homeDir, ".aforge")+`"`) {
		t.Fatalf("the service does not carry the home it ticks: %q", string(service))
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
		"<string>" + timer.executable + "</string>",
		"<string>tick</string>",
		"<key>AFORGE_HOME</key>",
		"<string>" + filepath.Join(homeDir, ".aforge") + "</string>",
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

// pairTimer is a timer for one (home, program) pair on a machine stood in for,
// with the program a real file so that "gone" can be made true by deleting it.
func pairTimer(t *testing.T, platform, homeDir, root, program string) *Timer {
	t.Helper()
	timer, err := NewWatch(WatchOptions{
		Platform: platform, HomeDir: homeDir, StateRoot: root, Executable: program,
		UID: 501, Runner: &recordingRunner{},
	})
	if err != nil {
		t.Fatalf("%s: NewWatch: %v", platform, err)
	}
	return timer
}

func realProgram(t *testing.T) string {
	t.Helper()
	program := filepath.Join(t.TempDir(), "aforge")
	if err := os.WriteFile(program, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return program
}

// THE TIMER IS A (HOME, PROGRAM) PAIR AND A LAUNCH SPEAKS ONLY FOR ITS OWN.
// Another build of aforge that can still run is not drift: the machine has one
// timer per login, and two builds taking it from each other on every launch
// was the flip this law ends. Status agrees — something IS checking this home.
func TestDriftLeavesATimerRunningAnotherLiveProgramAlone(t *testing.T) {
	for _, platform := range []string{"darwin", "linux"} {
		homeDir := t.TempDir()
		root := filepath.Join(homeDir, ".aforge")
		first, second := realProgram(t), realProgram(t)
		one := pairTimer(t, platform, homeDir, root, first)
		// Nothing installed at all is nothing to repair, and it is not a fault.
		if drift, err := one.Drift(); err != nil || drift.Present || drift.Stale {
			t.Fatalf("%s: a machine with no timer = %+v (%v)", platform, drift, err)
		}
		if err := one.Install(context.Background()); err != nil {
			t.Fatalf("%s: Install: %v", platform, err)
		}
		if drift, err := one.Drift(); err != nil || !drift.Present || drift.Stale || drift.Executable != first {
			t.Fatalf("%s: a fresh install reads as drifted: %+v (%v)", platform, drift, err)
		}
		other := pairTimer(t, platform, homeDir, root, second)
		drift, err := other.Drift()
		if err != nil || !drift.Present || drift.Stale || drift.Gone {
			t.Fatalf("%s: a timer running another live build read as drift: %+v (%v)", platform, drift, err)
		}
		if drift.Executable != first {
			t.Fatalf("%s: the reading does not say what the timer runs: %q", platform, drift.Executable)
		}
		if status, err := other.Status(); err != nil || !status.Installed {
			t.Fatalf("%s: the row would say off while the timer runs the other build: %+v (%v)", platform, status, err)
		}
		// And the one hand that moves it on purpose still does.
		if err := other.Install(context.Background()); err != nil {
			t.Fatalf("%s: Install: %v", platform, err)
		}
		if drift, err := other.Drift(); err != nil || drift.Executable != second || drift.Stale {
			t.Fatalf("%s: the row's own install did not move the timer: %+v (%v)", platform, drift, err)
		}
	}
}

// A TIMER NAMING ANOTHER HOME IS SOMEBODY ELSE'S. A launch under an isolated
// AFORGE_HOME reads the machine's timer as neither installed for it nor drift,
// so it neither claims it nor rewrites it — and a definition an earlier build
// wrote, which carries no home at all, ticks the login's default one and reads
// the same way from anywhere else.
func TestDriftLeavesAnotherHomesTimerAlone(t *testing.T) {
	for _, platform := range []string{"darwin", "linux"} {
		homeDir := t.TempDir()
		program := realProgram(t)
		theirs := pairTimer(t, platform, homeDir, filepath.Join(homeDir, ".aforge"), program)
		if err := theirs.Install(context.Background()); err != nil {
			t.Fatalf("%s: Install: %v", platform, err)
		}
		isolated := pairTimer(t, platform, homeDir, filepath.Join(t.TempDir(), "elsewhere"), program)
		if drift, err := isolated.Drift(); err != nil || !drift.Present || drift.Stale {
			t.Fatalf("%s: another home's timer read as this home's drift: %+v (%v)", platform, drift, err)
		}
		if status, err := isolated.Status(); err != nil || status.Installed {
			t.Fatalf("%s: another home's timer read as checking this one: %+v (%v)", platform, status, err)
		}
		// The definition as a build before the pair law wrote it: no home.
		if err := os.Remove(program); err != nil {
			t.Fatal(err)
		}
		older := strings.ReplaceAll(string(mustRead(t, theirs.primaryPath())), "<key>AFORGE_HOME</key>", "<key>Unused</key>")
		if platform == "linux" {
			older = strings.ReplaceAll(string(mustRead(t, theirs.linuxServicePath())), "Environment=", "X-Was=")
			if err := os.WriteFile(theirs.linuxServicePath(), []byte(older), 0o644); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(theirs.primaryPath(), []byte(older), 0o644); err != nil {
			t.Fatal(err)
		}
		if drift, err := isolated.Drift(); err != nil || drift.Stale {
			t.Fatalf("%s: an isolated launch would repair the default home's timer: %+v (%v)", platform, drift, err)
		}
		if drift, err := theirs.Drift(); err != nil || !drift.Stale || !drift.Gone {
			t.Fatalf("%s: the default home did not read its own older timer as drift: %+v (%v)", platform, drift, err)
		}
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// AND A DEFINITION THAT IS NOT THE SHAPE THIS BUILD WRITES IS DRIFT for the
// home it names: an earlier build wrote it, or somebody edited it. It is put
// back, once, in this build's shape — which is how a timer that carried no home
// comes to carry one.
func TestDriftSeesADefinitionAnOlderBuildWrote(t *testing.T) {
	homeDir := t.TempDir()
	program := realProgram(t)
	timer := pairTimer(t, "linux", homeDir, filepath.Join(homeDir, ".aforge"), program)
	if err := timer.Install(context.Background()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	older := strings.Replace(string(mustRead(t, timer.linuxServicePath())), "Environment=", "X-Was=", 1)
	if err := os.WriteFile(timer.linuxServicePath(), []byte(older), 0o644); err != nil {
		t.Fatal(err)
	}
	drift, err := timer.Drift()
	if err != nil || !drift.Present || !drift.Stale || drift.Gone || drift.Executable != program {
		t.Fatalf("an older build's definition did not read as drift: %+v (%v)", drift, err)
	}
	if status, err := timer.Status(); err != nil || status.Installed {
		t.Fatalf("an older build's definition read as installed: %+v (%v)", status, err)
	}
	if err := timer.Install(context.Background()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if drift, err := timer.Drift(); err != nil || drift.Stale {
		t.Fatalf("the repair did not take: %+v (%v)", drift, err)
	}
}

func TestDriftSeesADefinitionWhoseProgramIsGone(t *testing.T) {
	homeDir := t.TempDir()
	program := realProgram(t)
	timer := pairTimer(t, "darwin", homeDir, filepath.Join(homeDir, ".aforge"), program)
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
	if err != nil || !drift.Present || !drift.Stale || !drift.Gone {
		t.Fatalf("a program that is gone did not read as drift: %+v (%v)", drift, err)
	}
	if status, err := timer.Status(); err != nil || status.Installed {
		t.Fatalf("a timer running nothing read as installed: %+v (%v)", status, err)
	}
}
