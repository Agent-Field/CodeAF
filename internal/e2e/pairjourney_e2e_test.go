//go:build e2e

package e2e

// THE PAIRING JOURNEY, DRIVEN AS A PERSON WOULD DRIVE IT.
//
// Two separate state roots, one local sync service, the real binary in tmux,
// keystrokes only, and a read of the screen (capture-pane -e) after every step.
// A step that the product fails is written down with the screen that showed it
// and the journey goes on where it still can; nothing here papers over a miss.
// The per-step times and the verdicts are written to $UX_EVIDENCE (default
// ~/ux-evidence/e2e-run.json), outside the repository.

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

// journeyBudget is the whole journey's time limit: install to takeover.
const journeyBudget = 2 * time.Minute

// forbidden are the words the vocabulary law keeps off every screen of the flow.
var forbidden = regexp.MustCompile(`(?i)\b(node|relay|take|lease|manifest)s?\b`)

var escSeq = regexp.MustCompile(`\x1b\[[0-9;:?<>]*[ -/]*[@-~]|\x1b[()][A-Z0-9]|\x1b\][^\a]*\a`)

// stepRecord is one line of the evidence file.
type stepRecord struct {
	Name   string   `json:"name"`
	Status string   `json:"status"` // pass | fail | blocked
	Millis int64    `json:"ms"`
	Notes  []string `json:"notes,omitempty"`
	Screen string   `json:"screen,omitempty"`
}

type journey struct {
	t       *testing.T
	began   time.Time
	steps   []stepRecord
	outDir  string
	dead    bool // a step the rest depends on failed
	relay   string
	key     string
	current *stepRecord
	stopped time.Duration
}

// check records a verdict on the current step without stopping the journey.
func (j *journey) check(ok bool, format string, args ...any) bool {
	if !ok {
		j.current.Status = "fail"
		j.current.Notes = append(j.current.Notes, "FAIL: "+fmt.Sprintf(format, args...))
	}
	return ok
}

// require is check for a fact the later steps stand on: when it fails they are
// reported as blocked rather than run against a screen that cannot answer them.
func (j *journey) require(ok bool, format string, args ...any) bool {
	if !j.check(ok, format, args...) {
		j.dead = true
	}
	return ok
}

// see keeps the screen the step ended on, as plain text and with its colours,
// and holds it to the vocabulary law.
func (j *journey) see(r *rig) string {
	raw := captureStyled(r)
	plain := escSeq.ReplaceAllString(raw, "")
	_ = os.WriteFile(filepath.Join(j.outDir, fmt.Sprintf("%02d-%s.ansi", len(j.steps)+1, slug(j.current.Name))), []byte(raw), 0o644)
	j.current.Screen = strings.TrimRight(plain, " \n")
	if bad := forbidden.FindString(plain); bad != "" {
		j.check(false, "the screen says %q, which the vocabulary law keeps out", bad)
	}
	return plain
}

// step runs fn and times it. After a failed step marked fatal the rest are blocked.
func (j *journey) step(name string, needsPrevious bool, fn func()) {
	rec := stepRecord{Name: name, Status: "pass"}
	j.current = &rec
	if j.dead && needsPrevious {
		rec.Status = "blocked"
		rec.Notes = []string{"an earlier step failed, so this one could not be reached"}
		j.steps = append(j.steps, rec)
		return
	}
	began := time.Now()
	fn()
	rec.Millis = time.Since(began).Milliseconds()
	j.steps = append(j.steps, rec)
}

func slug(s string) string {
	return strings.Trim(regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(s), "-"), "-")
}

func captureStyled(r *rig) string {
	out, err := exec.Command("tmux", "capture-pane", "-e", "-J", "-p", "-S", "-", "-t", r.name).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// plain is the screen without colours, wrapped lines joined.
func plain(r *rig) string { return escSeq.ReplaceAllString(captureStyled(r), "") }

func waitPlain(r *rig, within time.Duration, want ...string) (string, bool) {
	deadline := time.Now().Add(within)
	for {
		screen := plain(r)
		all := true
		for _, w := range want {
			all = all && strings.Contains(screen, w)
		}
		if all {
			return screen, true
		}
		if time.Now().After(deadline) {
			return screen, false
		}
		time.Sleep(pollEvery)
	}
}

// startRelay runs the local sync service this repository ships and returns its address.
func startRelay(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "relay")
	build := exec.Command("go", "build", "-o", bin, "./cmd/relay")
	build.Dir = repoRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the sync service: %v\n%s", err, out)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	cmd := guardedCommand(t, context.Background(), t.TempDir(), nil, bin, "--listen", addr, "--store", filepath.Join(t.TempDir(), "store"), "--quiet")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if c, err := net.Dial("tcp", addr); err == nil {
			_ = c.Close()
			return "http://" + addr
		}
	}
	t.Fatal("the sync service never listened")
	return ""
}

// startCommand opens a terminal on one command of the binary (not the chat).
func startCommand(t *testing.T, env []string, name, home, ws string, args ...string) *rig {
	t.Helper()
	return startWithEnv(t, env, name, home, ws, 120, 40, args...)
}

// paste sends text the way a terminal pastes it: one bracketed paste.
func (r *rig) paste(text string) {
	r.t.Helper()
	buf := "ux-" + r.name
	if out, err := exec.Command("tmux", "set-buffer", "-b", buf, text).CombinedOutput(); err != nil {
		r.t.Fatalf("tmux set-buffer: %v\n%s", err, out)
	}
	if out, err := exec.Command("tmux", "paste-buffer", "-p", "-d", "-b", buf, "-t", r.name).CombinedOutput(); err != nil {
		r.t.Fatalf("tmux paste-buffer: %v\n%s", err, out)
	}
	time.Sleep(300 * time.Millisecond)
}

var linkShape = regexp.MustCompile(`https://codeaf\.link/p/\S+`)

func TestPairJourney(t *testing.T) {
	key := requireTmuxAndKey(t)
	out := os.Getenv("UX_EVIDENCE")
	if out == "" {
		out = filepath.Join(os.Getenv("HOME"), "ux-evidence")
	}
	if err := os.MkdirAll(filepath.Join(out, "screens"), 0o755); err != nil {
		t.Fatal(err)
	}
	j := &journey{t: t, began: time.Now(), outDir: filepath.Join(out, "screens"), key: key}
	defer j.write(filepath.Join(out, "e2e-run.json"))

	j.relay = startRelay(t)
	env := []string{config.APIKeyEnv + "=" + key, "CODEAF_SYNC_URL=" + j.relay, "CODEAF_CELLS=1", "CODEAF_SYNC_INTERVAL_MS=1000"}
	homeA, homeB := newHome(t, nil), newHome(t, nil)
	wsA := newWorkspace(t, "proj", true)
	wsB := newWorkspace(t, "proj", true)
	if err := os.WriteFile(filepath.Join(wsB, "Makefile"), []byte("test:\n\t@echo 3 passed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	journeyRun(j, env, homeA, homeB, wsA, wsB)
}

// freeze fixes the journey's length; probes that follow are not part of it.
func (j *journey) freeze() { j.stopped = time.Since(j.began) }

func (j *journey) write(path string) {
	total := j.stopped
	if total == 0 {
		total = time.Since(j.began)
	}
	doc := map[string]any{
		"model":        e2eModel,
		"total_ms":     total.Milliseconds(),
		"budget_ms":    journeyBudget.Milliseconds(),
		"under_budget": total < journeyBudget,
		"steps":        j.steps,
		"note":         "step 0 is a probe run after the timed journey",
	}
	raw, _ := json.MarshalIndent(doc, "", " ")
	_ = os.WriteFile(path, raw, 0o644)
	for _, s := range j.steps {
		j.t.Logf("%-9s %6dms  %s", strings.ToUpper(s.Status), s.Millis, s.Name)
		for _, n := range s.Notes {
			j.t.Logf("            %s", n)
		}
	}
	j.t.Logf("whole journey: %s (budget %s)", total.Round(time.Millisecond), journeyBudget)
}

// loseLid ends the program the way a closed lid does: no goodbye, nothing
// released. The pid is the pane's own, read from tmux.
func (r *rig) loseLid() {
	r.t.Helper()
	raw, _ := exec.Command("tmux", "display-message", "-p", "-t", r.name, "#{pane_pid}").Output()
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	if pid > 0 {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	r.kill()
}
