//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

// The old binary is built separately with make build and an earlier build stamp.
// Both windows and the background engine are real; no protocol answers or UI
// frames are fabricated. Only this fixture's workspace is explicitly stopped.
func TestOlderEngineUpgradeOnTheRealTerminal(t *testing.T) {
	oldBinary := os.Getenv("CODEAF_E2E_OLD_BINARY")
	if oldBinary == "" {
		t.Skip("set CODEAF_E2E_OLD_BINARY to an older make-build binary")
	}
	requireTmuxAndKey(t)
	const model = "deepseek/deepseek-v4.1-flash"
	if e2eModel != model {
		t.Fatalf("requires CODEAF_E2E_MODEL=%s", model)
	}
	home := newHome(t, map[string]any{"memory.enabled": false})
	ws := newWorkspace(t, "engine-upgrade", false)
	env := []string{config.APIKeyEnv + "=" + liveKey(t), "CODEAF_BASE_URL=" + config.DefaultBaseURL}
	old := startUpgradeOldWindow(t, oldBinary, env, home, ws)
	old.skipSetup(t)
	prompt := "Create upgrade-proof.txt containing exactly amber lighthouse. Do not modify any other file, delegate, or use memory tools."
	old.lit(prompt)
	old.keys("Enter")
	if answer, _ := privateMemoryAwaitAnswer(home, prompt, 2*time.Minute); answer == "" {
		t.Fatal("old window did not finish the real file request")
	}
	assertUpgradeFile(t, ws)
	before := upgradeEngineCommand(t, binary(t), env, home, ws, "--status")
	pid := upgradePID(before)
	if pid == "" || !strings.Contains(before, "older build") || !strings.Contains(before, "still in use") {
		t.Fatalf("fixture did not hold an older busy engine: %s", before)
	}
	current := startWithEnv(t, env, "engine_upgrade_current", home, ws, 180, 52, "chat", "--model", model)
	current.skipSetup(t)
	if strings.Contains(current.capture(), "Move this conversation here?") {
		current.keys("1", "Enter")
		time.Sleep(time.Second)
	}
	screen := current.waitFor(15*time.Second, "older codeaf background engine", "restarting one window", "close every window")
	if !strings.Contains(screen, "older codeaf background engine") {
		t.Fatal("matched-shape current window omitted older engine guidance")
	}
	t.Logf("Actual terminal explains the older engine and safe update:\n%s", screen)
	if !strings.Contains(before, "restarting one window is insufficient") || !strings.Contains(before, "close every window") {
		t.Fatalf("engine status omitted safe upgrade guidance: %s", before)
	}
	during := upgradeEngineCommand(t, binary(t), env, home, ws, "--status")
	if upgradePID(during) != pid {
		t.Fatal("opening a new window interrupted the older busy engine")
	}
	current.lit("/status")
	current.keys("Enter")
	status := current.waitFor(15*time.Second, "window build", "engine build")
	if !strings.Contains(status, "engine build") {
		t.Fatal("matched-shape attached window omitted engine build status")
	}
	t.Logf("Actual status distinguishes window and engine builds:\n%s", status)
	// The old surface must be closed before the explicit stop, so its old binary
	// cannot win the redial race and put the workspace back on the old build.
	old.kill()
	stopped := upgradeEngineCommand(t, binary(t), env, home, ws, "--stop")
	t.Logf("Explicit stop of the isolated fixture engine: %s", strings.TrimSpace(stopped))
	deadline := time.Now().Add(30 * time.Second)
	var after string
	for {
		after = upgradeEngineCommand(t, binary(t), env, home, ws, "--status")
		if strings.Contains(after, "engine: this build") && upgradePID(after) != "" && upgradePID(after) != pid {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("current window did not reconnect to current engine: %s", after)
		}
		time.Sleep(pollEvery)
	}
	current.keys("Escape")
	current.lit("/status")
	current.keys("Enter")
	status = current.waitFor(15*time.Second, "window build", "engine build")
	// The current build's revision is printed by the same shipped binary. Its
	// presence on the engine-build line proves the reconnect refreshed the getter.
	version := upgradeEngineVersion(t, binary(t), env, home)
	fields := strings.Fields(version)
	if len(fields) < 2 {
		t.Fatalf("bad version output: %s", version)
	}
	engineLine := ""
	for _, line := range strings.Split(status, "\n") {
		if strings.Contains(line, "engine build") {
			engineLine = line
		}
	}
	if !strings.Contains(engineLine, fields[1]) {
		t.Fatalf("reconnected status retained stale engine build: %s; current %s", engineLine, version)
	}
	assertUpgradeFile(t, ws)
	for _, m := range callModels(t, home) {
		if m != model {
			t.Fatalf("unexpected actual model %q", m)
		}
	}
	t.Logf("PASS: busy older engine kept its PID; window explains update; explicit stop reconnects to this build; original file preserved.\n%s", status)
}

func startUpgradeOldWindow(t *testing.T, oldBinary string, env []string, home, ws string) *rig {
	t.Helper()
	g := guardHost(t, home)
	name := fmt.Sprintf("p%d-engine_upgrade_old", os.Getpid())
	command := []string{"env", "-u", "CODEAF_TASK_BELT"}
	command = append(command, env...)
	command = append(command, g.tokens(env)...)
	command = append(command, "CODEAF_HOME="+home, "TERM=xterm-256color", oldBinary, "chat", "--model", e2eModel)
	quoted := make([]string, len(command))
	for i, p := range command {
		quoted[i] = shellQuote(p)
	}
	if out, err := guardedCommand(t, context.Background(), home, os.Environ(), "tmux", "new-session", "-d", "-s", name, "-c", ws, "sleep 600").CombinedOutput(); err != nil {
		t.Fatalf("old tmux: %v %s", err, out)
	}
	r := &rig{t: t, name: name, home: home, host: g, ws: ws}
	t.Cleanup(func() {
		if t.Failed() {
			r.dump()
		}
		r.kill()
	})
	r.resize(180, 52)
	if out, err := guardedCommand(t, context.Background(), home, os.Environ(), "tmux", "respawn-window", "-k", "-t", name, "-c", ws, strings.Join(quoted, " ")).CombinedOutput(); err != nil {
		t.Fatalf("old launch: %v %s", err, out)
	}
	r.waitFor(45*time.Second, "codeaf")
	return r
}

func upgradeEngineCommand(t *testing.T, bin string, env []string, home, ws, flag string) string {
	t.Helper()
	cmd := guardedCommand(t, context.Background(), home, append(append(os.Environ(), env...), "CODEAF_HOME="+home), bin, "engine", flag, "--workspace", ws)
	out, err := cmd.CombinedOutput()
	if err != nil && flag != "--status" {
		t.Fatalf("engine %s: %v %s", flag, err, out)
	}
	return string(out)
}
func upgradeEngineVersion(t *testing.T, bin string, env []string, home string) string {
	t.Helper()
	cmd := guardedCommand(t, context.Background(), home, append(append(os.Environ(), env...), "CODEAF_HOME="+home), bin, "version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
func upgradePID(status string) string {
	for _, line := range strings.Split(status, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "pid" {
			return fields[1]
		}
	}
	return ""
}
func assertUpgradeFile(t *testing.T, ws string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(ws, "upgrade-proof.txt"))
	if err != nil || strings.TrimSpace(string(raw)) != "amber lighthouse" {
		t.Fatalf("upgrade lost original mock file: %q %v", raw, err)
	}
}
