package e2e

// hostguard_test.go is THE WALL BETWEEN THIS SUITE AND THE MACHINE IT RUNS ON.
//
// A codeaf this suite starts is a real codeaf, and three things a real codeaf
// does on its own reach past its state root into the machine:
//
//   - EVERY START TAKES THE OLD TIMERS OFF THE LOGIN. The feature automations
//     replaced installed one launchd agent or systemd timer per login, and
//     internal/automation's legacy.go removes every definition it finds under
//     HOME on every start — `launchctl bootout` and `systemctl --user disable
//     --now` included. A launch pointed at the developer's HOME would boot the
//     developer's own timers out from under them.
//   - EVERY WINDOW STARTS A CLOCK. `codeaf clock` is a detached process that
//     outlives the window that started it by half a minute, so a test window
//     that started one would leave it running after the test had gone.
//   - A RUN THE CLOCK FINISHES RAISES A DESKTOP NOTIFICATION, through
//     `osascript` on macOS and `notify-send` on Linux, which would put a banner
//     on the screen of whoever is running the suite.
//
// So every launch stands behind the guard: a login folder of its own for HOME,
// stand-ins for launchctl, systemctl, crontab and both notifiers first on PATH
// (each one writes what it was asked into a log a scenario can read), and the
// clock switched off unless the scenario says it is about the clock.
// hostguardgate_test.go is the law that every launcher in this package comes
// through here (#1631).

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// developerHome is THE MACHINE WE PROTECT. Tests may later change HOME, so it
// is captured before any fixture starts moving the environment.
var developerHome = func() string {
	home, _ := os.UserHomeDir()
	return home
}()

// hostStandIns are the commands the guard answers in the machine's place: the
// three schedulers a timer could be put on, and the two notifiers an
// automation's news is raised through (internal/tui3's desktopnotify.go).
var hostStandIns = []string{"systemctl", "launchctl", "crontab", "osascript", "notify-send"}

// noClockEnv is the variable that keeps a launched window from starting the
// automations clock (internal/automation's Startable). The guard sets it on
// every launch whose scenario does not name it, so the one scenario that is
// about the clock says so by naming it — normally `-u CODEAF_NO_AUTOMATIONS`.
const noClockEnv = "CODEAF_NO_AUTOMATIONS"

// hostGuard is one state root's wall: the folder beside the root, the stand-ins
// in its bin, the throwaway login HOME points at, and the log the stand-ins
// write what they were asked into.
type hostGuard struct{ dir, bin, login, log string }

// guardHost gives each state root a sibling login and the machine's stand-ins.
// The sibling keeps a fresh-install state root empty until the product writes
// to it.
func guardHost(t testing.TB, stateRoot string) hostGuard {
	t.Helper()
	if stateRoot == "" {
		t.Fatal("the host guard needs a state root")
	}
	root, err := filepath.Abs(stateRoot)
	if err != nil {
		t.Fatalf("host guard state root: %v", err)
	}
	dir := filepath.Clean(root) + ".host"
	g := hostGuard{dir: dir, bin: filepath.Join(dir, "bin"), login: filepath.Join(dir, "login"), log: filepath.Join(dir, "host.log")}
	if err := os.Mkdir(dir, 0o700); err == nil {
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
	} else if !os.IsExist(err) {
		t.Fatalf("host guard directory: %v", err)
	}
	for _, path := range []struct {
		name string
		mode os.FileMode
	}{{g.bin, 0o755}, {g.login, 0o700}} {
		if err := os.MkdirAll(path.name, path.mode); err != nil {
			t.Fatalf("host guard directory %s: %v", path.name, err)
		}
	}
	for _, name := range hostStandIns {
		stub := "#!/bin/sh\nprintf '%s\\n' \"" + name + " $*\" >> " + shellQuote(g.log) + "\nexit 0\n"
		if err := os.WriteFile(filepath.Join(g.bin, name), []byte(stub), 0o755); err != nil {
			t.Fatalf("host guard stub %s: %v", name, err)
		}
	}
	return g
}

// shellQuote keeps a fixture path with an apostrophe from escaping the stub's
// log argument or the tmux launch command.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// env folds duplicate assignments before placing the stand-ins first on PATH.
// A scenario's deliberate throwaway HOME remains its own, and so does a
// scenario's own word about the clock.
func (g hostGuard) env(base []string) []string {
	result := make([]string, 0, len(base)+3)
	positions := map[string]int{}
	for _, row := range base {
		name, _, ok := strings.Cut(row, "=")
		if !ok {
			result = append(result, row)
			continue
		}
		if at, exists := positions[name]; exists {
			result[at] = row
		} else {
			positions[name] = len(result)
			result = append(result, row)
		}
	}
	path := os.Getenv("PATH")
	if at, ok := positions["PATH"]; ok {
		path = strings.TrimPrefix(result[at], "PATH=")
	}
	home := ""
	if at, ok := positions["HOME"]; ok {
		home = strings.TrimPrefix(result[at], "HOME=")
	}
	if home == "" || filepath.Clean(home) == filepath.Clean(developerHome) {
		home = g.login
	}
	guarded := map[string]string{"PATH": g.bin + ":" + path, "HOME": home}
	if _, named := positions[noClockEnv]; !named {
		guarded[noClockEnv] = "1"
	}
	for name, value := range guarded {
		row := name + "=" + value
		if at, ok := positions[name]; ok {
			result[at] = row
		} else {
			result = append(result, row)
		}
	}
	return result
}

// tokens appends assignments after a scenario's env argv, so a preceding -u
// pair or assignment cannot undo the guard at the tmux process boundary. The
// clock's switch is the one exception, and on purpose: a scenario that names
// it, with an assignment or with -u, has said what it wants of the clock.
func (g hostGuard) tokens(scenario []string) []string {
	values := map[string]string{}
	named := map[string]bool{}
	for index := 0; index < len(scenario); index++ {
		if scenario[index] == "-u" && index+1 < len(scenario) {
			index++
			delete(values, scenario[index])
			named[scenario[index]] = true
			continue
		}
		name, value, ok := strings.Cut(scenario[index], "=")
		if ok {
			values[name] = value
			named[name] = true
		}
	}
	path, ok := values["PATH"]
	if !ok {
		path = os.Getenv("PATH")
	}
	result := []string{"PATH=" + g.bin + ":" + path}
	if home := values["HOME"]; home == "" || filepath.Clean(home) == filepath.Clean(developerHome) {
		result = append(result, "HOME="+g.login)
	}
	if !named[noClockEnv] {
		result = append(result, noClockEnv+"=1")
	}
	return result
}

// calls is everything the stand-ins were asked, oldest first.
func (g hostGuard) calls(t testing.TB) []string {
	t.Helper()
	raw, err := os.ReadFile(g.log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("host guard call log: %v", err)
	}
	if len(raw) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
}

// guardedCommand is the one exec door for a child codeaf. A guarded HOME is
// what the old-timer removal looks under, and the stand-ins are what it calls.
func guardedCommand(t testing.TB, ctx context.Context, stateRoot string, env []string, program string, args ...string) *exec.Cmd {
	t.Helper()
	command := exec.CommandContext(ctx, program, args...)
	command.Env = guardHost(t, stateRoot).env(env)
	return command
}

// ── the old timers, named the way the machine names them ───────────────────

// oldTimerLabels and oldTimerUnits are every timer the feature automations
// replaced ever installed: the five-minute tick and the v1 resident's wake,
// under both of the product's names.
//
// THEY ARE SPELLED HERE AND NOT BORROWED FROM internal/automation, whose lists
// are the code under test. A guard that asked the uninstaller which files it
// might touch could only ever watch the files the uninstaller already agrees
// it touches; this list is the machine's own account, and a removal that
// reached a file outside it would still be caught by the hashes below.
var (
	oldTimerLabels = []string{
		"ai.agentfield.codeaf.tick",
		"ai.agentfield.aforge.tick", // legacy-name
		"ai.agentfield.codeaf.wake",
		"ai.agentfield.aforge.wake", // legacy-name
	}
	oldTimerUnits = []string{
		"codeaf-tick",
		"aforge-tick", // legacy-name
		"codeaf-wake",
		"aforge-wake", // legacy-name
	}
)

// oldTimerFiles is every file under one login that holds or locks an old
// timer, on either platform: a launchd agent's plist and its lock, and a
// systemd timer, its service, its enable link and its lock.
func oldTimerFiles(login string) []string {
	var paths []string
	agents := filepath.Join(login, "Library", "LaunchAgents")
	for _, label := range oldTimerLabels {
		plist := filepath.Join(agents, label+".plist")
		paths = append(paths, plist, plist+".lock")
	}
	units := filepath.Join(login, ".config", "systemd", "user")
	for _, unit := range oldTimerUnits {
		paths = append(paths,
			filepath.Join(units, unit+".timer"),
			filepath.Join(units, unit+".service"),
			filepath.Join(units, "timers.target.wants", unit+".timer"),
			filepath.Join(units, unit+".timer.lock"))
	}
	return paths
}

// machineTimerHashes is what the developer's own old-timer files hold right
// now. AN ENABLE LINK IS HASHED AS A LINK, by where it points, so a link whose
// target has gone is still a file this guard watches rather than a read error.
func machineTimerHashes(t testing.TB) map[string]string {
	t.Helper()
	hashes := map[string]string{}
	if developerHome == "" {
		return hashes
	}
	for _, path := range oldTimerFiles(developerHome) {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("look at machine timer %s: %v", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				t.Fatalf("read machine timer link %s: %v", path, err)
			}
			hashes[path] = "link to " + target
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read machine timer %s: %v", path, err)
		}
		hashes[path] = fmt.Sprintf("sha256 %x", sha256.Sum256(raw))
	}
	return hashes
}

// requireMachineTimerUntouched registers first so its cleanup runs after every
// rig has stopped. It catches an old-timer file of the developer's that
// appeared, vanished or changed while the test ran — tick and wake, under both
// names, on either platform.
func requireMachineTimerUntouched(t testing.TB) {
	t.Helper()
	before := machineTimerHashes(t)
	if len(before) == 0 {
		t.Log("this machine has no old timer definition")
	}
	for path, hash := range before {
		t.Logf("machine timer %s: %s", path, hash)
	}
	t.Cleanup(func() {
		after := machineTimerHashes(t)
		for path, oldHash := range before {
			if newHash, ok := after[path]; !ok || newHash != oldHash {
				t.Errorf("machine timer %s changed: before %s, after %q", path, oldHash, newHash)
			}
		}
		for path, hash := range after {
			if _, ok := before[path]; !ok {
				t.Errorf("machine timer %s appeared: before absent, after %s", path, hash)
			}
		}
	})
}

// seedOldTimers writes, under one throwaway login, every definition the
// uninstaller looks for on this platform, and answers the paths it wrote.
func seedOldTimers(t testing.TB, login string) []string {
	t.Helper()
	write := func(path, body string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("seed %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("seed %s: %v", path, err)
		}
	}
	var seeded []string
	switch runtime.GOOS {
	case "darwin":
		agents := filepath.Join(login, "Library", "LaunchAgents")
		for _, label := range oldTimerLabels {
			plist := filepath.Join(agents, label+".plist")
			write(plist, "<plist><dict><key>Label</key><string>"+label+"</string></dict></plist>\n")
			write(plist+".lock", "")
			seeded = append(seeded, plist, plist+".lock")
		}
	case "linux":
		units := filepath.Join(login, ".config", "systemd", "user")
		for _, unit := range oldTimerUnits {
			timer := filepath.Join(units, unit+".timer")
			write(timer, "[Timer]\nOnUnitActiveSec=5min\n")
			write(filepath.Join(units, unit+".service"), "[Service]\nExecStart=/bin/true\n")
			write(timer+".lock", "")
			link := filepath.Join(units, "timers.target.wants", unit+".timer")
			if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
				t.Fatalf("seed %s: %v", link, err)
			}
			if err := os.Symlink(timer, link); err != nil {
				t.Fatalf("seed %s: %v", link, err)
			}
			seeded = append(seeded, timer, filepath.Join(units, unit+".service"), link, timer+".lock")
		}
	}
	return seeded
}

// oldTimerRemover is internal/automation's RemoveOldTimers alone, built into a
// program of its own from testdata/oldtimers.
//
// IT IS A BUILT PROGRAM AND NOT A CALL IN THIS PROCESS, because the removal
// refuses to act inside a test binary (testing.Testing) — which is the right
// refusal for every unit test in the tree and exactly the one this gate cannot
// accept: what has to be proved is what the code does in a process that is NOT
// a test, the one every launch of `bin/codeaf` is. The program is the same
// first act main() performs, run through the same exec door as every other
// launch here.
func oldTimerRemover(t *testing.T) string {
	t.Helper()
	program := filepath.Join(t.TempDir(), "oldtimers")
	build := exec.Command("go", "build", "-o", program, "./internal/e2e/testdata/oldtimers")
	build.Dir = moduleRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the old-timer remover: %v\n%s", err, out)
	}
	return program
}

func TestTheHostGuardStandsInForTheMachinesScheduler(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-ins are POSIX shell")
	}
	root := filepath.Join(t.TempDir(), "state")
	g := guardHost(t, root)
	env := g.env(append(os.Environ(), "CODEAF_HOME="+root))
	value := func(rows []string, name string) string {
		for _, row := range rows {
			if strings.HasPrefix(row, name+"=") {
				return strings.TrimPrefix(row, name+"=")
			}
		}
		return ""
	}
	if got := value(env, "HOME"); got != g.login {
		t.Errorf("HOME = %q, want %q", got, g.login)
	}
	if got := value(env, "PATH"); !strings.HasPrefix(got, g.bin+":") {
		t.Errorf("PATH = %q, want guard first", got)
	}
	command := exec.Command("sh", "-c", "command -v systemctl; systemctl --user enable --now codeaf-tick.timer; "+
		"launchctl bootstrap gui/501 /x.plist; crontab -l; osascript -e 'display notification'; notify-send codeaf hello")
	command.Env = env
	out, err := command.CombinedOutput()
	if err != nil || !strings.HasPrefix(string(out), filepath.Join(g.bin, "systemctl")+"\n") {
		t.Fatalf("scheduler stubs: %v, output %q", err, out)
	}
	wantCalls := []string{"systemctl --user enable --now codeaf-tick.timer", "launchctl bootstrap gui/501 /x.plist",
		"crontab -l", "osascript -e display notification", "notify-send codeaf hello"}
	if got := g.calls(t); strings.Join(got, "\n") != strings.Join(wantCalls, "\n") {
		t.Errorf("stand-in calls = %v, want %v", got, wantCalls)
	}
	other := filepath.Join(t.TempDir(), "login")
	if got := value(g.env([]string{"HOME=" + other}), "HOME"); got != other {
		t.Errorf("scenario HOME = %q, want %q", got, other)
	}
	if got := value(g.env([]string{"HOME=" + developerHome}), "HOME"); got != g.login {
		t.Errorf("developer HOME = %q, want %q", got, g.login)
	}
	if got := value(g.env([]string{"PATH=/opt/x:/usr/bin"}), "PATH"); got != g.bin+":/opt/x:/usr/bin" {
		t.Errorf("scenario PATH = %q", got)
	}
	// THE CLOCK IS OFF UNLESS THE SCENARIO SAID OTHERWISE, through either door.
	if got := value(env, noClockEnv); got != "1" {
		t.Errorf("%s = %q on a launch that said nothing about the clock, want 1", noClockEnv, got)
	}
	if rows := g.env([]string{noClockEnv + "="}); value(rows, noClockEnv) != "" || strings.Count(strings.Join(rows, "\n"), noClockEnv+"=") != 1 {
		t.Errorf("a scenario's own %s was overridden: %v", noClockEnv, rows)
	}
	want := "PATH=" + g.bin + ":/opt/x:/usr/bin\nHOME=" + g.login + "\n" + noClockEnv + "=1"
	if got := g.tokens([]string{"-u", "HOME", "PATH=/opt/x:/usr/bin", "HOME=" + developerHome}); strings.Join(got, "\n") != want {
		t.Errorf("guard tokens = %v", got)
	}
	want = "PATH=" + g.bin + ":" + os.Getenv("PATH") + "\n" + noClockEnv + "=1"
	if got := g.tokens([]string{"-u", "PATH", "HOME=" + other}); strings.Join(got, "\n") != want {
		t.Errorf("scenario-owned HOME tokens = %v", got)
	}
	want = "PATH=" + g.bin + ":" + os.Getenv("PATH") + "\nHOME=" + g.login
	if got := g.tokens([]string{"-u", noClockEnv}); strings.Join(got, "\n") != want {
		t.Errorf("a scenario that is about the clock still had it switched off: %v", got)
	}
	_ = guardHost(t, root)
	if got := g.calls(t); strings.Join(got, "\n") != strings.Join(wantCalls, "\n") {
		t.Errorf("second guard erased stand-in calls: %v", got)
	}
}

// TestTheOldTimersComeOffOnlyTheThrowawayLogin is the guarantee the guard
// exists for, held against the code that would break it.
//
// Every launch of `bin/codeaf` takes the old timers off whatever login it runs
// as (internal/automation's legacy.go), so the one thing this suite must never
// do is run that removal as the developer. This runs it as a real program under
// the guard and proves three things: a login with nothing on it is left alone;
// a login holding every old timer is cleared, through the stand-ins and only
// at its own paths; and the developer's own tick and wake files, under both
// names, are byte for byte what they were.
func TestTheOldTimersComeOffOnlyTheThrowawayLogin(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("the old timers were launchd agents and systemd units, and this machine has neither")
	}
	requireMachineTimerUntouched(t)
	root := filepath.Join(t.TempDir(), "state")
	g := guardHost(t, root)
	remover := oldTimerRemover(t)
	run := func(what string) {
		t.Helper()
		command := guardedCommand(t, context.Background(), root, append(os.Environ(), "CODEAF_HOME="+root), remover)
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("the remover failed on %s: %v\n%s", what, err, out)
		}
	}

	// IT ONLY EVER ACTS ON A FILE IT FOUND: a login with no old timer on it is
	// a login nothing is asked about.
	run("an empty login")
	if calls := g.calls(t); len(calls) != 0 {
		t.Fatalf("the remover asked the scheduler about a login holding no old timer: %v", calls)
	}

	seeded := seedOldTimers(t, g.login)
	run("a login holding every old timer")
	calls := g.calls(t)
	t.Logf("stand-in calls: %v", calls)
	for _, path := range seeded {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("the throwaway login's %s survived the removal (%v)", path, err)
		}
	}
	var want []string
	switch runtime.GOOS {
	case "darwin":
		domain := "gui/" + strconv.Itoa(os.Getuid())
		for _, label := range oldTimerLabels {
			want = append(want, "launchctl bootout "+domain+" "+filepath.Join(g.login, "Library", "LaunchAgents", label+".plist"))
		}
	case "linux":
		for _, unit := range oldTimerUnits {
			want = append(want, "systemctl --user disable --now "+unit+".timer")
		}
		want = append(want, "systemctl --user daemon-reload")
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("the stand-ins were asked:\n  %s\nwant:\n  %s", strings.Join(calls, "\n  "), strings.Join(want, "\n  "))
	}
	// AND NOTHING IT NAMED IS THE DEVELOPER'S. Every path the removal handed a
	// scheduler is under the throwaway login; a call naming any other folder is
	// a call that would have reached a real one.
	for _, call := range calls {
		for _, field := range strings.Fields(call) {
			if filepath.IsAbs(field) && !strings.HasPrefix(field, g.login+string(filepath.Separator)) {
				t.Errorf("the removal named %s, which is outside the throwaway login %s: %q", field, g.login, call)
			}
		}
	}

	// IT RUNS ON EVERY START, and a start after the clean one finds nothing.
	run("the same login again")
	if again := g.calls(t); len(again) != len(calls) {
		t.Errorf("a second start asked the scheduler again: %v", again[len(calls):])
	}
}
