//go:build linux || darwin

package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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

// A LIVE OLD CHECKOUT STILL TURNS A CURRENT RUN AWAY.
//
// Taking back a dead lock must not take a live one. An old checkout holds only
// the directory, so the flock reads free beside it; what tells it from a dead
// lock is the old checkout's own test — the pid is alive and its command line
// says one-suite.sh.
func TestALiveOldCheckoutStillHoldsTheDirectoryLock(t *testing.T) {
	root := t.TempDir()
	lock, dir := filepath.Join(root, "suite.lockfile"), filepath.Join(root, "suite.lock")
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep binary to stand in for an old checkout")
	}
	old := &exec.Cmd{Path: sleep, Args: []string{legacyReaderMark, "30"}}
	if err := old.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = old.Process.Kill()
		_ = old.Wait()
	}()
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	named := strconv.Itoa(old.Process.Pid)
	if err := os.WriteFile(filepath.Join(dir, "pid"), []byte(named+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !waitUntil(t, 2*time.Second, func() bool { return strings.Contains(commandLine(old.Process.Pid), legacyReaderMark) }) {
		t.Skipf("the stand-in's command line never showed %q: %q", legacyReaderMark, commandLine(old.Process.Pid))
	}

	output, err := fixture(lock, dir, "true").CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Fatalf("a run beside a live old checkout: err %v, output %s", err, output)
	}
	want := "another heavy suite is already running on this box (directory lock " + dir + ", pid " + named + ")."
	if !strings.Contains(string(output), want) {
		t.Fatalf("output %q lacks %q", output, want)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "pid"))
	if err != nil || strings.TrimSpace(string(raw)) != named {
		t.Fatalf("a refused run disturbed the old checkout's lock: %q, %v", raw, err)
	}
}

// THE DIRECTORY NAMES A HOLDER AN OLD READER ACCEPTS AS ALIVE (#1324).
//
// An old checkout's liveness test is two facts: the pid is alive, and its
// command line contains one-suite.sh. The directory used to name the SUITE,
// whose command line is the suite's own, so an old tree judged a live lock
// stale and ran beside it. It names the holder now, and the holder carries the
// mark for as long as it holds the lock.
func TestTheDirectoryNamesAHolderAnOldReaderAcceptsAsAlive(t *testing.T) {
	root := t.TempDir()
	lock, dir := filepath.Join(root, "suite.lockfile"), filepath.Join(root, "suite.lock")
	suiteInput, childInput, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	suiteOutput, childOutput, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	wrapper := fixture(lock, dir, "sh", "-c", "echo ready; read release")
	wrapper.Stdin, wrapper.Stdout = suiteInput, childOutput
	if err := wrapper.Start(); err != nil {
		t.Fatal(err)
	}
	_ = suiteInput.Close()
	_ = childOutput.Close()
	defer func() {
		_ = childInput.Close()
		_ = suiteOutput.Close()
		_ = wrapper.Wait()
	}()
	if _, err := bufio.NewReader(suiteOutput).ReadString('\n'); err != nil {
		t.Fatalf("the suite never started: %v", err)
	}

	holder := ""
	if !waitUntil(t, 5*time.Second, func() bool {
		raw, _ := os.ReadFile(lock)
		for _, field := range strings.Fields(string(raw)) {
			if strings.HasPrefix(field, "holder=") {
				holder = strings.TrimPrefix(field, "holder=")
			}
		}
		return holder != "" && holder != "0" && readDirLockHolder(dir) == holder
	}) {
		t.Fatalf("the directory lock names %q, want the holder %q", readDirLockHolder(dir), holder)
	}
	pid, _ := strconv.Atoi(holder)
	if !pidVisibleHere(pid) {
		t.Fatalf("the directory names pid %d, which is not running", pid)
	}
	if line := commandLine(pid); !strings.Contains(line, legacyReaderMark) {
		t.Fatalf("an old reader would call the live holder %d stale: its command line %q lacks %q", pid, line, legacyReaderMark)
	}
	if !dirLockHolderAlive(holder) {
		t.Fatalf("the holder %s is not alive by the old reader's test", holder)
	}

	if _, err := childInput.Write([]byte("release\n")); err != nil {
		t.Fatal(err)
	}
	if !waitUntil(t, 5*time.Second, func() bool { _, err := os.Stat(dir); return os.IsNotExist(err) }) {
		t.Fatalf("the directory lock outlived its holder")
	}
}

// A DIRECTORY THAT NAMES SOMEBODY ELSE IS SOMEBODY ELSE'S (#1324).
//
// An old checkout that moved our directory aside made its own under the same
// name. Dropping ours by path deleted its pid file and left its `since`, a
// directory no rmdir could remove that named nobody.
func TestDroppingTheDirectoryLockLeavesSomebodyElsesAlone(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "suite.lock")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	nameDirLockHolder(dir, os.Getpid()+1)
	dropDirLock(dir, os.Getpid())
	if got := readDirLockHolder(dir); got != strconv.Itoa(os.Getpid()+1) {
		t.Fatalf("dropping our lock rewrote somebody else's: it names %q", got)
	}
	dropDirLock(dir, os.Getpid()+1)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the owner's own drop left the directory: %v", err)
	}
}
