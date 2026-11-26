//go:build linux

package sessionhost

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 2 && os.Args[1] == "sessionhost" {
		os.Exit(RunHelper(os.Args[2:]))
	}
	os.Exit(m.Run())
}

func rootLauncher(t *testing.T, cfg Config) linuxLauncher {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("requires root in an isolated container")
	}
	l, err := newLauncher(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// The normal test binary can live in a directory inaccessible to the
	// switched identity. Give the second exec a private test copy it can open.
	dir, err := os.MkdirTemp("", "codeaf-sessionhost-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o711); err != nil {
		t.Fatal(err)
	}
	src, err := os.Open(l.self)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	l.self = filepath.Join(dir, "helper")
	dst, err := os.OpenFile(l.self, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatalf("copy re-exec fixture: %v, %v", copyErr, closeErr)
	}
	if err := os.Chmod(l.self, 0o755); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestC2_RealLaunchDropsEverything(t *testing.T) {
	cfg := testConfig(t)
	l := rootLauncher(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := l.command(ctx, cfg.UIDBase, "/", helperEnv(), l.self, "sessionhost", "whoami").CombinedOutput()
	if err != nil {
		t.Fatalf("launch helper failed: %v, %s", err, out)
	}
	if !probeIdentity(string(out), cfg.UIDBase) {
		t.Fatalf("unexpected helper identity: %s", out)
	}
}

func TestC7_KillAllKillsSetsidDescendants(t *testing.T) {
	cfg := testConfig(t)
	l := rootLauncher(t, cfg)
	if pids := ProcessesOf(cfg.UIDBase); len(pids) > 0 {
		t.Fatal("the test identity is already in use")
	}
	setsid, err := exec.LookPath("setsid")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := l.command(ctx, cfg.UIDBase, "/", helperEnv(), "/bin/sh", "-c",
		`"$1" /bin/sleep 300 & printf '%s\n' "$!"; wait`, "sh", setsid)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()
	procs := linuxProcesses{cfg}
	defer func() { _ = procs.KillAll(cfg.UIDBase, stopGrace) }()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	pid, idErr := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || idErr != nil {
		t.Fatalf("no detached child PID: %q, %v, %v", line, err, idErr)
	}
	deadline := time.Now().Add(time.Second)
	for {
		sid, err := unix.Getsid(pid)
		if sid == pid && err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("child %d did not make a separate process session: %d, %v", pid, sid, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := procs.KillAll(cfg.UIDBase, stopGrace); err != nil {
		t.Fatal(err)
	}
	if pids := ProcessesOf(cfg.UIDBase); len(pids) != 0 {
		t.Fatalf("live processes survived: %v", pids)
	}
}
