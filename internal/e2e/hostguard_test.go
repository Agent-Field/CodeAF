package e2e

// THE TEST PROCESS'S HOME IS THE MACHINE WE PROTECT. Tests may later change
// HOME, so capture it before any fixture starts moving the environment.

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/standing"
)

var developerHome = func() string {
	home, _ := os.UserHomeDir()
	return home
}()

type hostGuard struct{ dir, bin, login, log string }

// guardHost gives each state root a sibling login and scheduler stand-ins. The
// sibling keeps a fresh-install state root empty until the product writes to it.
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
	g := hostGuard{dir: dir, bin: filepath.Join(dir, "bin"), login: filepath.Join(dir, "login"), log: filepath.Join(dir, "timer.log")}
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
	for _, name := range []string{"systemctl", "launchctl", "crontab"} {
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

// env folds duplicate assignments before placing the stubs first on PATH. A
// scenario's deliberate throwaway HOME remains its own.
func (g hostGuard) env(base []string) []string {
	result := make([]string, 0, len(base)+2)
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
	for name, value := range map[string]string{"PATH": g.bin + ":" + path, "HOME": home} {
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
// pair or assignment cannot undo the guard at the tmux process boundary.
func (g hostGuard) tokens(scenario []string) []string {
	values := map[string]string{}
	for index := 0; index < len(scenario); index++ {
		if scenario[index] == "-u" && index+1 < len(scenario) {
			index++
			delete(values, scenario[index])
			continue
		}
		name, value, ok := strings.Cut(scenario[index], "=")
		if ok {
			values[name] = value
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
	return result
}

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

// guardedCommand is the one exec door for a child codeaf. A guarded HOME
// catches timer definitions written by Go before the scheduler command runs.
func guardedCommand(t testing.TB, ctx context.Context, stateRoot string, env []string, program string, args ...string) *exec.Cmd {
	t.Helper()
	command := exec.CommandContext(ctx, program, args...)
	command.Env = guardHost(t, stateRoot).env(env)
	return command
}

func machineTimerFiles() []string {
	var pattern string
	switch runtime.GOOS {
	case "linux":
		pattern = filepath.Join(developerHome, ".config", "systemd", "user", strings.TrimSuffix(standing.LinuxTickTimer, ".timer")+".*")
	case "darwin":
		pattern = filepath.Join(developerHome, "Library", "LaunchAgents", standing.DarwinTickLabel+".plist")
	default:
		return nil
	}
	paths, _ := filepath.Glob(pattern)
	return paths
}

func timerHashes(t testing.TB) map[string]string {
	t.Helper()
	hashes := map[string]string{}
	for _, path := range machineTimerFiles() {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read machine timer %s: %v", path, err)
		}
		hashes[path] = fmt.Sprintf("%x", sha256.Sum256(raw))
	}
	return hashes
}

// requireMachineTimerUntouched registers first so its cleanup runs after every
// rig has stopped. It catches a timer file that appeared, vanished or changed.
func requireMachineTimerUntouched(t testing.TB) {
	t.Helper()
	before := timerHashes(t)
	if len(before) == 0 {
		t.Log("this machine has no timer definition")
	}
	for path, hash := range before {
		t.Logf("machine timer %s sha256 %s", path, hash)
	}
	t.Cleanup(func() {
		after := timerHashes(t)
		for path, oldHash := range before {
			if newHash, ok := after[path]; !ok || newHash != oldHash {
				t.Errorf("machine timer %s changed: before %s, after %s", path, oldHash, newHash)
			}
		}
		for path, hash := range after {
			if _, ok := before[path]; !ok {
				t.Errorf("machine timer %s appeared: before absent, after %s", path, hash)
			}
		}
	})
}

func TestTheHostGuardStandsInForTheMachinesScheduler(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the scheduler stubs are POSIX shell")
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
	command := exec.Command("sh", "-c", "command -v systemctl; systemctl --user enable --now codeaf-tick.timer; launchctl bootstrap gui/501 /x.plist; crontab -l")
	command.Env = env
	out, err := command.CombinedOutput()
	if err != nil || !strings.HasPrefix(string(out), filepath.Join(g.bin, "systemctl")+"\n") {
		t.Fatalf("scheduler stubs: %v, output %q", err, out)
	}
	wantCalls := []string{"systemctl --user enable --now codeaf-tick.timer", "launchctl bootstrap gui/501 /x.plist", "crontab -l"}
	if got := g.calls(t); strings.Join(got, "\n") != strings.Join(wantCalls, "\n") {
		t.Errorf("scheduler calls = %v, want %v", got, wantCalls)
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
	if got := g.tokens([]string{"-u", "HOME", "PATH=/opt/x:/usr/bin", "HOME=" + developerHome}); strings.Join(got, "\n") != "PATH="+g.bin+":/opt/x:/usr/bin\nHOME="+g.login {
		t.Errorf("guard tokens = %v", got)
	}
	if got := g.tokens([]string{"-u", "PATH", "HOME=" + other}); strings.Join(got, "\n") != "PATH="+g.bin+":"+os.Getenv("PATH") {
		t.Errorf("scenario-owned HOME tokens = %v", got)
	}
	_ = guardHost(t, root)
	if got := g.calls(t); strings.Join(got, "\n") != strings.Join(wantCalls, "\n") {
		t.Errorf("second guard erased scheduler calls: %v", got)
	}
}

func TestAnInstallUnderTheGuardNeverReachesTheMachinesTimer(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("this machine has no supported timer platform")
	}
	requireMachineTimerUntouched(t)
	root := filepath.Join(t.TempDir(), "state")
	g := guardHost(t, root)
	env := g.env(os.Environ())
	for _, row := range env {
		if strings.HasPrefix(row, "HOME=") {
			t.Setenv("HOME", strings.TrimPrefix(row, "HOME="))
		}
		if strings.HasPrefix(row, "PATH=") {
			t.Setenv("PATH", strings.TrimPrefix(row, "PATH="))
		}
	}
	// THE INSTALL MUST NOT RUN UNTIL BOTH WALLS ARE IN PLACE. The product
	// writes a definition in Go before it calls the scheduler.
	for _, name := range []string{"systemctl", "launchctl"} {
		resolved, err := exec.LookPath(name)
		if err != nil || resolved != filepath.Join(g.bin, name) {
			t.Fatalf("unsafe scheduler resolution for %s: %q, %v", name, resolved, err)
		}
	}
	if got, err := os.UserHomeDir(); err != nil || got != g.login {
		t.Fatalf("unsafe HOME for install: %q, %v", got, err)
	}
	watch, err := standing.NewWatch(standing.WatchOptions{StateRoot: root, Executable: filepath.Join(t.TempDir(), "codeaf")})
	if err != nil {
		t.Fatal(err)
	}
	if err := watch.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	calls := g.calls(t)
	t.Logf("scheduler calls: %v", calls)
	if runtime.GOOS == "linux" {
		for _, want := range []string{"systemctl --user daemon-reload", "systemctl --user enable --now " + standing.LinuxTickTimer} {
			if !strings.Contains(strings.Join(calls, "\n"), want) {
				t.Errorf("scheduler calls %v lack %q", calls, want)
			}
		}
		definition := filepath.Join(g.login, ".config", "systemd", "user", strings.TrimSuffix(standing.LinuxTickTimer, ".timer")+".service")
		raw, err := os.ReadFile(definition)
		if err != nil || !strings.Contains(string(raw), "CODEAF_HOME="+root) {
			t.Errorf("guarded service definition %s: %v, content %q", definition, err, raw)
		}
	} else {
		definition := filepath.Join(g.login, "Library", "LaunchAgents", standing.DarwinTickLabel+".plist")
		if _, err := os.Stat(definition); err != nil {
			t.Errorf("guarded plist %s: %v", definition, err)
		}
		if !strings.Contains(strings.Join(calls, "\n"), "launchctl bootstrap gui/") || !strings.Contains(strings.Join(calls, "\n"), definition) {
			t.Errorf("scheduler calls %v lack plist bootstrap", calls)
		}
	}
}
