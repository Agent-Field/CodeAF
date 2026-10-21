package cellstore

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// idlingEngine writes a program that idles like a daemon and exits on SIGTERM,
// and answers its path.
func idlingEngine(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "furrow-0.0.0-fake")
	script := "#!/bin/sh\ntrap 'kill $!' TERM\nsleep 60 &\nwait\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// aliveUnder lists the live processes whose command line mentions dir.
func aliveUnder(dir string) []int {
	var pids []int
	entries, _ := os.ReadDir("/proc")
	for _, entry := range entries {
		raw, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil || !bytes.Contains(raw, []byte(dir)) {
			continue
		}
		if stat, _ := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat")); bytes.Contains(stat, []byte(") Z ")) {
			continue // a zombie is already dead, only unreaped
		}
		if pid, err := parsePID(entry.Name()); err == nil && pid != os.Getpid() {
			pids = append(pids, pid)
		}
	}
	return pids
}

func parsePID(name string) (int, error) {
	pid := 0
	for _, r := range name {
		if r < '0' || r > '9' {
			return 0, os.ErrInvalid
		}
		pid = pid*10 + int(r-'0')
	}
	return pid, nil
}

// awaitRunning waits until pid has become the program, so that its descriptors
// are the ones the program was started with.
func awaitRunning(t *testing.T, pid int, dir string) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		raw, _ := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
		if bytes.Contains(raw, []byte(dir)) {
			time.Sleep(50 * time.Millisecond)
			return
		}
	}
	t.Fatal("the daemon never started")
}

func TestStopSpawnedLeavesNoDaemonBehind(t *testing.T) {
	if _, err := os.Stat("/proc/self/cmdline"); err != nil {
		t.Skip("no /proc to scan")
	}
	dir := t.TempDir()
	// A descriptor handed down without close-on-exec, as a wrapper's lock is.
	lock, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if _, _, errno := syscall.Syscall(syscall.SYS_FCNTL, lock.Fd(), syscall.F_SETFD, 0); errno != 0 {
		t.Fatal(errno)
	}
	program := idlingEngine(t, dir)
	if _, err := launch(Daemon{Binary: program, Socket: filepath.Join(dir, "v3", "e.sock")}); err != nil {
		t.Fatal(err)
	}
	spawnedMu.Lock()
	pid := spawned[len(spawned)-1].process.Pid
	spawnedMu.Unlock()
	awaitRunning(t, pid, dir)
	held, _ := os.ReadDir(filepath.Join("/proc", strconv.Itoa(pid), "fd"))
	for _, entry := range held {
		target, _ := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "fd", entry.Name()))
		if target == program { // the shell holds its own script open
			continue
		}
		if entry.Name() != "0" && entry.Name() != "1" && entry.Name() != "2" {
			t.Errorf("the daemon inherited descriptor %s; only stdin, stdout and stderr may pass", entry.Name())
		}
	}
	if len(aliveUnder(dir)) == 0 {
		t.Fatal("the daemon is not running before the teardown, so the test proves nothing")
	}
	StopSpawned()
	if left := aliveUnder(dir); len(left) != 0 {
		t.Fatalf("daemons %v outlived StopSpawned", left)
	}
}
