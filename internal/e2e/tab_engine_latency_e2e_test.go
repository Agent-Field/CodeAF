//go:build e2e && !windows

package e2e

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/charmbracelet/x/ansi"
)

// Each transition must reveal destination-only history, then accept a unique
// draft. Seeing a title that was already on a tab cannot finish the stopwatch.
func TestTabAndEngineAttachmentLatency(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux required")
	}
	home := recoveryShortHome(t, newHome(t, map[string]any{"memory.enabled": false, "model_pool": "off"}))
	ws := newWorkspace(t, "tab-engine-latency", false)
	if err := config.MarkSetupSeen(home, time.Now()); err != nil {
		t.Fatal(err)
	}
	var members []teamstore.Member
	for _, name := range []string{"alpha", "beta"} {
		bucket := filepath.Join(home, "v3", "projects", strings.ReplaceAll(ws, string(filepath.Separator), "-"))
		c, err := cell.CreateIn(bucket, cell.Options{Class: cell.HostBound})
		if err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(c.Root, cell.TranscriptPath)
		var journal strings.Builder
		fmt.Fprintf(&journal, "{\"type\":\"session\",\"version\":1,\"id\":%q,\"cwd\":%q,\"model\":\"deepseek/deepseek-v4.1-flash\"}\n{\"type\":\"title\",\"title\":%q}\n", c.ID, ws, "Audit "+name)
		for i := 0; i < 1000; i++ {
			fmt.Fprintf(&journal, "{\"type\":\"message\",\"role\":\"user\",\"content\":%q}\n{\"type\":\"message\",\"role\":\"assistant\",\"content\":%q}\n", fmt.Sprintf("Saved audit request %04d", i), strings.Repeat("Historical answer for the navigation workload. ", 4))
		}
		fmt.Fprintf(&journal, "{\"type\":\"message\",\"role\":\"assistant\",\"content\":%q}\n", "LATENCY_"+strings.ToUpper(name)+"_HISTORY")
		if err := os.WriteFile(file, []byte(journal.String()), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := session.SaveMeta(c.Root, session.Meta{ID: c.ID, Title: "Audit " + name, Workspace: ws}); err != nil {
			t.Fatal(err)
		}
		members = append(members, teamstore.Member{Key: file, File: file, Word: "Audit " + name, Handle: name, Where: ws})
	}
	t.Log("fixture: two movable chats, 2001 message records each; 140 columns by 42 rows")
	if err := teamstore.Save(home, []teamstore.Team{{ID: teamstore.NewID(), Name: "Audit team", Members: members}}); err != nil {
		t.Fatal(err)
	}
	url, calls := efficiencyWire(t)
	env := []string{config.APIKeyEnv + "=mock-efficiency-key", "CODEAF_BASE_URL=" + url}
	stopRecoveryEngineOnCleanup(t, env, home, ws)
	launch := func(name, stage string) *rig {
		t.Helper()
		target := fmt.Sprintf("p%d-%s", os.Getpid(), name)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		ready := make(chan time.Time, 1)
		go func() {
			for ctx.Err() == nil {
				out, _ := exec.CommandContext(ctx, "tmux", "capture-pane", "-p", "-t", target).Output()
				if strings.Contains(string(out), "LATENCY_ALPHA_HISTORY") && strings.Contains(string(out), "/ commands") {
					ready <- time.Now()
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
		}()
		began := time.Now()
		r := startWithEnv(t, env, name, home, ws, 140, 42, "chat", "--session", members[0].File, "--model", "deepseek/deepseek-v4.1-flash", "--one-model")
		select {
		case shown := <-ready:
			t.Logf("%s_ms=%.3f", stage, float64(shown.Sub(began))/float64(time.Millisecond))
		case <-ctx.Done():
			t.Fatalf("%s did not reveal destination history:\n%s", stage, r.capture())
		}
		return r
	}
	first := launch("audit_cold_engine", "cold_engine_launch_to_history")
	r := launch("audit_warm_engine", "existing_engine_attach_to_history")
	if strings.Contains(first.capture(), "LATENCY_ALPHA_HISTORY") {
		first.waitFor(10*time.Second, "type to search or start something new")
	}
	r.keys("C-t")
	r.lit("/resume")
	r.keys("Enter")
	r.lit("Audit beta")
	began := time.Now()
	if out, err := exec.Command("tmux", "send-keys", "-t", r.name, "Enter").CombinedOutput(); err != nil {
		t.Fatalf("resume: %v %s", err, out)
	}
	elapsed := efficiencyWait(t, r, began, "LATENCY_BETA_HISTORY")
	t.Logf("saved_tab_open_to_history_ms=%.3f", float64(elapsed)/float64(time.Millisecond))
	for trial := 0; trial < 12; trial++ {
		name, history := "Audit alpha", "LATENCY_ALPHA_HISTORY"
		if trial%2 == 1 {
			name, history = "Audit beta", "LATENCY_BETA_HISTORY"
		}
		screen := strings.Split(r.capture(), "\n")
		if len(screen) < 3 {
			t.Fatal("missing tab strip")
		}
		x := strings.Index(strings.ToLower(screen[2]), strings.ToLower(name))
		if x < 0 {
			t.Fatalf("no tab %q on strip %q", name, screen[2])
		}
		x = ansi.StringWidth(screen[2][:x])
		began := time.Now()
		press := fmt.Sprintf("\x1b[<0;%d;3M\x1b[<0;%d;3m", x+3, x+3)
		if out, err := exec.Command("tmux", "send-keys", "-t", r.name, "-l", press).CombinedOutput(); err != nil {
			t.Fatalf("tab press: %v %s", err, out)
		}
		elapsed := efficiencyWait(t, r, began, history)
		t.Logf("tab_switch_%02d_to_history_ms=%.3f", trial, float64(elapsed)/float64(time.Millisecond))
		draft := fmt.Sprintf("AUDIT_DRAFT_%02d", trial)
		began = time.Now()
		if out, err := exec.Command("tmux", "send-keys", "-t", r.name, "-l", draft).CombinedOutput(); err != nil {
			t.Fatalf("draft: %v %s", err, out)
		}
		elapsed = efficiencyWait(t, r, began, draft)
		t.Logf("tab_switch_%02d_draft_ms=%.3f", trial, float64(elapsed)/float64(time.Millisecond))
		r.keys("C-u")
	}
	// Pause only this fixture's identified engine. The window must still open
	// the picker, accept a filter and cancel while its engine cannot answer.
	sockets, err := filepath.Glob(filepath.Join(home, "v3", "hosts", "*", "host.sock"))
	if err != nil || len(sockets) != 1 {
		t.Fatalf("expected one fixture engine: %v %v", sockets, err)
	}
	conn, err := net.Dial("unix", sockets[0])
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	self, err := remote.AskHost(conn, remote.WhoIs{})
	conn.Close()
	if err != nil || self.PID <= 1 {
		t.Fatalf("fixture engine did not identify itself: %v", err)
	}
	r.lit("/resume")
	var resumed atomic.Bool
	var once sync.Once
	release := func() { once.Do(func() { _ = syscall.Kill(self.PID, syscall.SIGCONT); resumed.Store(true) }) }
	t.Cleanup(release)
	if err := syscall.Kill(self.PID, syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	timer := time.AfterFunc(1200*time.Millisecond, release)
	defer timer.Stop()
	began = time.Now()
	if out, err := exec.Command("tmux", "send-keys", "-t", r.name, "Enter").CombinedOutput(); err != nil {
		t.Fatalf("open resume: %v %s", err, out)
	}
	elapsed = efficiencyWait(t, r, began, "filter ·")
	t.Logf("delayed_engine_resume_picker_ms=%.3f engine_resumed=%t injected_pause_ms=1200", float64(elapsed)/float64(time.Millisecond), resumed.Load())
	if resumed.Load() {
		t.Error("resume picker waited for the engine instead of accepting input during its read")
	}
	typed := time.Now()
	if out, err := exec.Command("tmux", "send-keys", "-t", r.name, "-l", "audit_filter_pending").CombinedOutput(); err != nil {
		t.Fatalf("filter: %v %s", err, out)
	}
	elapsed = efficiencyWait(t, r, typed, "audit_filter_pending")
	t.Logf("delayed_engine_resume_filter_ms=%.3f engine_resumed=%t", float64(elapsed)/float64(time.Millisecond), resumed.Load())
	if resumed.Load() {
		t.Error("resume filtering waited for the engine to resume")
	}
	escaped := time.Now()
	if out, err := exec.Command("tmux", "send-keys", "-t", r.name, "Escape").CombinedOutput(); err != nil {
		t.Fatalf("cancel picker: %v %s", err, out)
	}
	efficiencyWait(t, r, escaped, "space space home")
	if resumed.Load() || strings.Contains(r.capture(), "audit_filter_pending") {
		t.Error("resume cancellation did not restore the composer while the engine was paused")
	}
	t.Logf("delayed_engine_resume_cancel_ms=%.3f engine_resumed=%t", float64(time.Since(escaped))/float64(time.Millisecond), resumed.Load())
	release()
	// A completed engine round trip ensures the earlier history read can answer.
	// Its late result must not put the cancelled picker back on screen.
	conn, err = net.Dial("unix", sockets[0])
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	_, err = remote.AskHost(conn, remote.WhoIs{})
	conn.Close()
	if err != nil || strings.Contains(r.capture(), "filter ·") {
		t.Fatalf("cancelled picker returned after the engine resumed: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("navigation made %d model calls", calls.Load())
	}
}
