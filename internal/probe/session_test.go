package probe

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func requireTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
}

// openIsolated gives the test its own fake HOME so the probe root, socket
// and homes land under a temp directory, never the real user's.
func openIsolated(t *testing.T, root string) *Manager {
	t.Helper()
	requireTmux(t)
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	// Short base: unix sockets on macOS must live under ~104 bytes.
	base := filepath.Join("/tmp", fmt.Sprintf("probe-test-%d", os.Getpid()))
	ProbeBase = base
	t.Cleanup(func() { ProbeBase = ""; os.RemoveAll(base) })
	m, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _, _ = m.tmux("kill-server") })
	return m
}

func TestStartSendCapture(t *testing.T) {
	m := openIsolated(t, "t1")
	d, err := m.Start("s1", "/bin/sh", "default", []string{}, nil, Dims{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if d.Socket == "" || d.Dims.Width == 0 || d.Dims.Height == 0 {
		t.Fatalf("bad start data: %+v", d)
	}
	if _, err := m.Act("s1", ActRequest{Text: "echo probe-marker\n"}); err != nil {
		t.Fatalf("Act text: %v", err)
	}
	w := ActWait{QuietMs: 200, TimeoutMs: 5000}
	if _, err := m.Act("s1", ActRequest{Wait: &w}); err != nil {
		t.Fatalf("Act wait: %v", err)
	}
	obs, err := m.ObserveWithWait("s1", w)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !strings.Contains(obs.Snapshot, "probe-marker") {
		t.Fatalf("snapshot missing marker:\n%s", obs.Snapshot)
	}
	if len(obs.Processes) == 0 {
		t.Fatal("no processes reported")
	}
}

func TestResizeChangesDims(t *testing.T) {
	m := openIsolated(t, "t2")
	if _, err := m.Start("s2", "/bin/sh", "", []string{}, nil, Dims{Width: 100, Height: 30}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := m.Act("s2", ActRequest{Resize: &Resize{Width: 120, Height: 40}}); err != nil {
		t.Fatalf("Act resize: %v", err)
	}
	out, err := m.tmux("display-message", "-p", "-t", m.tmuxName("s2"), "#{window_width} #{window_height}")
	if err != nil {
		t.Fatalf("display-message: %v", err)
	}
	if got := strings.TrimSpace(out); got != "120 40" {
		t.Fatalf("window dims after resize: %q", got)
	}
	rec, _ := m.rec("s2")
	if rec.Width != 120 || rec.Height != 40 {
		t.Fatalf("registry dims not updated: %+v", rec)
	}
}

func TestRegistrySurvivesFreshProcess(t *testing.T) {
	m := openIsolated(t, "t3")
	if _, err := m.Start("s3", "/bin/sh", "profile-a", []string{}, nil, Dims{}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := m.Act("s3", ActRequest{Text: "hi"}); err != nil {
		t.Fatalf("Act: %v", err)
	}
	// Simulate a fresh CLI process: new Manager from disk.
	m2, err := Open("t3")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	rec, err := m2.rec("s3")
	if err != nil {
		t.Fatalf("session missing after reopen: %v", err)
	}
	if rec.Revision != 1 || rec.Profile != "profile-a" {
		t.Fatalf("registry state lost: %+v", rec)
	}
	if _, err := m2.Act("s3", ActRequest{Text: " again"}); err != nil {
		t.Fatalf("act through fresh process: %v", err)
	}
	obs, err := m2.Observe("s3")
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if obs.Revision != 2 {
		t.Fatalf("revision not carried across processes: %d", obs.Revision)
	}
}

func TestStaleRevisionRejected(t *testing.T) {
	m := openIsolated(t, "t4")
	if _, err := m.Start("s4", "/bin/sh", "", []string{}, nil, Dims{}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	stale := 99
	d, err := m.Act("s4", ActRequest{Text: "x", ExpectRevision: &stale})
	if err != nil {
		t.Fatalf("Act: %v", err)
	}
	if d.Accepted || !d.Stale {
		t.Fatalf("stale act accepted: %+v", d)
	}
	if d.RevisionBefore != 0 || d.RevisionAfter != 0 {
		t.Fatalf("revision moved on stale act: %+v", d)
	}
}

func TestCleanupKillsOnlyItsOwnSessions(t *testing.T) {
	m := openIsolated(t, "t5")
	if _, err := m.Start("s5", "/bin/sh", "", []string{}, nil, Dims{}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// A foreign tmux session on the same server must survive cleanup.
	if _, err := m.tmux("new-session", "-d", "-s", "decoy", "/bin/sh"); err != nil {
		t.Fatalf("decoy session: %v", err)
	}
	home := filepath.Join(m.Root, "homes", "s5")
	if err := m.Finish("s5"); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if _, err := m.tmux("has-session", "-t", "probe-s5"); err == nil {
		t.Fatal("probe session still alive after Finish")
	}
	if _, err := m.tmux("has-session", "-t", "decoy"); err != nil {
		t.Fatal("Finish killed the foreign decoy session")
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatal("isolated home not removed")
	}
	if _, err := m.rec("s5"); err == nil {
		t.Fatal("registry entry not dropped")
	}
}

func TestSanitizeEnvAndRedaction(t *testing.T) {
	home := t.TempDir()
	env := SanitizeEnv(home, map[string]string{
		"CODEAF_API_KEY":       "sk-secret-123",
		"MY_TOKEN":             "tok",
		"SAFE_VAR":             "hello",
		"ANTHROPIC_AUTH_TOKEN": "t2",
	})
	joined := strings.Join(env, " ")
	if strings.Contains(joined, "sk-secret-123") || strings.Contains(joined, "MY_TOKEN=tok") {
		t.Fatalf("secret leaked into sanitized env: %s", Redact(joined))
	}
	if !strings.Contains(joined, "SAFE_VAR=hello") {
		t.Fatalf("opt-in non-secret dropped: %s", joined)
	}
	if !strings.Contains(joined, "HOME="+home) {
		t.Fatalf("isolated HOME missing: %s", joined)
	}
	got := Redact("CODEAF_API_KEY=sk-secret-123 PATH=/bin")
	if strings.Contains(got, "sk-secret-123") || !strings.Contains(got, "CODEAF_API_KEY=***") {
		t.Fatalf("Redact failed: %q", got)
	}
}

func TestBuildIdentityReuse(t *testing.T) {
	m := openIsolated(t, "t6")
	b := BuildIdentity{SHA: "abc", GoVersion: "go1.26", Binary: "/bin/codeaf", Flags: []string{"-trimpath"}}
	p1, err := m.SetBuild(b)
	if err != nil || p1.Reused {
		t.Fatalf("first prepare: reused=%v err=%v", p1.Reused, err)
	}
	p2, err := m.SetBuild(b)
	if err != nil || !p2.Reused {
		t.Fatalf("second prepare should reuse: reused=%v err=%v", p2.Reused, err)
	}
	b2 := b
	b2.SHA = "def"
	p3, err := m.SetBuild(b2)
	if err != nil || p3.Reused {
		t.Fatalf("changed identity must not reuse: %+v err=%v", p3, err)
	}
}

func TestNoSessionAndTimeout(t *testing.T) {
	m := openIsolated(t, "t7")
	if _, err := m.Observe("nope"); err == nil || !strings.Contains(err.Error(), "no such probe session") {
		t.Fatalf("expected NO_SESSION error, got %v", err)
	}
	if _, err := m.Start("", "/bin/sh", "", nil, nil, Dims{}); err == nil {
		t.Fatal("empty session id accepted")
	}
	// A bounded wait against a chatty session must time out honestly.
	if _, err := m.Start("s7", "/bin/sh", "", []string{}, nil, Dims{}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Keep the screen moving with a loop printing dots.
	script := "while true; do echo tick; sleep 0.2; done &"
	if _, err := m.tmux("send-keys", "-t", m.tmuxName("s7"), script, "Enter"); err != nil {
		t.Fatalf("send loop: %v", err)
	}
	w := ActWait{QuietMs: 200, TimeoutMs: 800}
	if _, err := m.Act("s7", ActRequest{Wait: &w}); err == nil {
		t.Fatal("chatty session should not reach quiet")
	}
	time.Sleep(50 * time.Millisecond)
}
