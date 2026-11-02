//go:build e2e

package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/remote"
)

// Timings include actual keyboard delivery, terminal rendering and framebuffer
// observation. rig.keys' 120ms settling sleep is deliberately outside the meter.
func TestNavigationLatency(t *testing.T) {
	requireTmuxAndKey(t)
	const model = "deepseek/deepseek-v4.1-flash"
	if e2eModel != model {
		t.Fatal("pin every live role to " + model)
	}
	seed := newHome(t, map[string]any{"model.talk": model, "models.fallbacks": "", "model_pool": "off", "daily_budget_usd": 0})
	ws := newWorkspace(t, "navigation-latency", false)
	home, err := os.MkdirTemp("", "afnav-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	raw, err := os.ReadFile(filepath.Join(seed, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(home, "config.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	r := startWithEnv(t, []string{config.APIKeyEnv + "=" + liveKey(t), "CODEAF_BASE_URL=https://openrouter.ai/api/v1"}, "navigation_latency", home, ws, 140, 42, "chat", "--model", model, "--one-model")
	r.skipSetup(t)
	sum := sha256.Sum256([]byte(filepath.Clean(ws)))
	socket := filepath.Join(home, "v3", "hosts", hex.EncodeToString(sum[:8]), "host.sock")
	sockets, err := filepath.Glob(filepath.Join(home, "v3", "hosts", "*", "*.sock"))
	if err != nil || len(sockets) != 1 {
		t.Fatalf("expected the test's one engine socket, got %d", len(sockets))
	}
	socket = sockets[0]
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	self, err := remote.AskHost(conn, remote.WhoIs{})
	conn.Close()
	if err != nil {
		t.Fatal(err)
	}
	if self.PID <= 1 {
		t.Fatal("the fixture engine did not identify its process")
	}
	// This PID comes only from the fixture's private socket, never process search.
	t.Cleanup(func() { _ = syscall.Kill(self.PID, syscall.SIGCONT); _ = syscall.Kill(self.PID, syscall.SIGTERM) })
	results := map[string][]float64{}
	send := func(keys ...string) {
		t.Helper()
		args := append([]string{"send-keys", "-t", r.name}, keys...)
		if out, err := exec.Command("tmux", args...).CombinedOutput(); err != nil {
			t.Fatalf("keys: %v %s", err, out)
		}
	}
	wait := func(name string, began time.Time, within time.Duration, ready func(string) bool) string {
		t.Helper()
		for time.Since(began) < within {
			screen := r.capture()
			if ready(screen) {
				results[name] = append(results[name], float64(time.Since(began))/float64(time.Millisecond))
				return screen
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("%s did not draw within %s:\n%s", name, within, r.capture())
		return ""
	}
	chat := func(s string) bool {
		return strings.Contains(s, "space space home") && !strings.Contains(s, "first message starts")
	}
	homeUp := func(s string) bool { return strings.Contains(s, "type to search or start something new") }
	startUp := func(s string) bool { return strings.Contains(s, "first message starts the conversation") }
	// Leave the initial greeting so the measured chat-return predicate is exact.
	r.lit("/status")
	r.keys("Enter")
	r.keys("Escape")
	for i := 0; i < 20; i++ {
		began := time.Now()
		send("Space", "Space")
		wait("home", began, 10*time.Second, homeUp)
		began = time.Now()
		send("Escape")
		wait("home_return", began, 10*time.Second, chat)
		began = time.Now()
		send("C-t")
		wait("new_tab", began, 10*time.Second, startUp)
		began = time.Now()
		send("Escape")
		wait("tab_return", began, 10*time.Second, chat)
		text := fmt.Sprintf("typing_latency_%02d", i)
		began = time.Now()
		send("-l", text)
		wait("typing", began, 10*time.Second, func(screen string) bool {
			lines := strings.Split(screen, "\n")
			return strings.Contains(strings.Join(lines[max(len(lines)-8, 0):], "\n"), text)
		})
		send("C-u")
		wait("typing_clear", time.Now(), 10*time.Second, func(screen string) bool { return !strings.Contains(screen, text) })
		send("-l", "/settings")
		wait("settings_command", time.Now(), 10*time.Second, func(screen string) bool { return strings.Contains(screen, "/settings") })
		began = time.Now()
		send("Enter")
		wait("settings", began, 10*time.Second, func(screen string) bool {
			return strings.Contains(screen, "type to search all") && strings.Contains(screen, "categories")
		})
		began = time.Now()
		send("Escape")
		wait("settings_return", began, 10*time.Second, chat)
	}
	for _, slow := range []bool{false, true} {
		for trial := 0; trial < 10; trial++ {
			where := "new_tab"
			if trial%2 == 1 {
				where = "home"
				r.keys("Space", "Space")
			} else {
				r.keys("C-t")
			}
			answer := fmt.Sprintf("NAV_OK_%t_%d", slow, trial)
			prompt := "Say exactly " + answer + ". Do not use tools."
			r.lit(prompt)
			if slow {
				if err := syscall.Kill(self.PID, syscall.SIGSTOP); err != nil {
					t.Fatal(err)
				}
				time.AfterFunc(800*time.Millisecond, func() { _ = syscall.Kill(self.PID, syscall.SIGCONT) })
			}
			kind := "normal_"
			if slow {
				kind = "delayed_"
			}
			began := time.Now()
			send("Enter")
			screen := wait(kind+where+"_message", began, 10*time.Second, func(s string) bool {
				lines := strings.Split(s, "\n")
				for _, line := range lines[:max(len(lines)-8, 0)] {
					if x := strings.Index(line, prompt); x >= 0 && x < 12 {
						return true
					}
				}
				return false
			})
			if dir := os.Getenv("CODEAF_E2E_EVIDENCE_DIR"); dir != "" {
				os.MkdirAll(dir, 0755)
				os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s%s-%d.txt", kind, where, trial)), []byte(screen), 0644)
			}
			// A complete short answer is measured separately from the UI receipt. It
			// includes provider latency and is diagnostic, never a UI-performance gate.
			wait(kind+"answer", began, 3*time.Minute, func(s string) bool { return cleanChatHasAnswer(s, answer) })
			wait(kind+"idle", time.Now(), 15*time.Second, chat)
		}
	}
	cleanChatAuditModels(t, home, model, "navigation")
	usage, err := os.ReadFile(filepath.Join(home, "v3", "usage.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(usage), "\n") {
		var row struct {
			TTFT float64 `json:"ttft_ms"`
		}
		if json.Unmarshal([]byte(line), &row) == nil && row.TTFT > 0 {
			results["provider_ttft"] = append(results["provider_ttft"], row.TTFT)
		}
	}

	raw, err = json.MarshalIndent(results, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("terminal latency milliseconds (5ms polling plus command overhead):\n%s", raw)
	if dir := os.Getenv("CODEAF_E2E_EVIDENCE_DIR"); dir != "" {
		if err = os.WriteFile(filepath.Join(dir, "timings.json"), raw, 0644); err != nil {
			t.Fatal(err)
		}
	}
}
