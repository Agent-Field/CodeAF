//go:build linux || darwin

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// fixture runs this test binary as the wrapper, with the directory lock named.
func fixture(lock, dir string, argv ...string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], append([]string{lock}, argv...)...)
	cmd.Env = append(os.Environ(), "CODEAF_SUITE_LOCK_FIXTURE=1", dirLockEnv+"="+dir)
	return cmd
}

// waitUntil polls a condition for a bounded time, because the holder releases
// within its own poll rather than the instant its suite exits.
func waitUntil(t *testing.T, within time.Duration, done func() bool) bool {
	t.Helper()
	for deadline := time.Now().Add(within); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if done() {
			return true
		}
	}
	return done()
}

// A DEAD DIRECTORY LOCK IS TAKEN BACK (#1324).
//
// A SIGKILL or an out-of-memory sweep of the holder frees the flock and leaves
// the directory, and until #1324 nothing asked whether the directory's holder
// was alive: every later run on the box was refused naming a pid that no
// longer existed. The flock is free here and the directory names a pid that
// has exited, which is exactly what that leaves behind.
func TestADeadDirectoryLockIsTakenBackUnderTheFlock(t *testing.T) {
	root := t.TempDir()
	lock, dir := filepath.Join(root, "suite.lockfile"), filepath.Join(root, "suite.lock")
	gone := exec.Command("true")
	if err := gone.Run(); err != nil {
		t.Fatal(err)
	}
	if pidVisibleHere(gone.Process.Pid) {
		t.Skipf("pid %d is still visible, so it cannot stand in for a dead holder", gone.Process.Pid)
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pid"), []byte(strconv.Itoa(gone.Process.Pid)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "since"), []byte("2026-09-23T00:00:00Z\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	output, err := fixture(lock, dir, "true").CombinedOutput()
	if err != nil {
		t.Fatalf("a dead directory lock (pid %d) refused the next run: %v\n%s", gone.Process.Pid, err, output)
	}
	if !waitUntil(t, 2*time.Second, func() bool { _, err := os.Stat(dir); return os.IsNotExist(err) }) {
		t.Fatalf("the run that took back the dead lock left its directory behind")
	}
	if stale, _ := filepath.Glob(dir + ".stale.*"); len(stale) > 0 {
		t.Fatalf("the dead directory was moved aside and left there: %v", stale)
	}
}
