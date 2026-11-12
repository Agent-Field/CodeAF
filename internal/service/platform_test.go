//go:build linux || darwin

package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// These drive real process groups. Each wait below is a bounded look for an
// outcome that has already been caused, never a sleep standing in for one.

func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("never: %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// gone reports a pid that no longer exists — reaped, not merely a zombie.
func gone(pid int) bool {
	return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

func readPID(t *testing.T, path string) int {
	t.Helper()
	var pid int
	waitFor(t, "the child wrote its pid", func() bool {
		raw, err := os.ReadFile(path)
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(raw)))
		return err == nil && pid > 0
	})
	return pid
}

// startNow launches spec and lets it run at once.
func startNow(t *testing.T, rt Runtime, spec Spec, logPath string, exits chan Exit) processgroup.Identity {
	t.Helper()
	launch, err := rt.Launch(spec, logPath, func(_ processgroup.Identity, x Exit) { exits <- x })
	if err != nil {
		t.Fatal(err)
	}
	if err := launch.Commit(); err != nil {
		t.Fatal(err)
	}
	return launch.Identity()
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func openReal(t *testing.T, dir string) *Supervisor {
	t.Helper()
	s, err := Open(Options{Dir: dir, Runtime: PlatformRuntime()})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestAKeptServiceOutlivesItsSupervisorAndItsWholeGroupStops is the real
// lifecycle: a detached group survives the supervisor closing, the next one
// takes back the same process, and Stop ends the leader and its child.
func TestAKeptServiceOutlivesItsSupervisorAndItsWholeGroupStops(t *testing.T) {
	if processgroup.BootID() == "" {
		t.Skip("this machine names no boot, so nothing can be kept")
	}
	store, work := t.TempDir(), t.TempDir()
	childFile := filepath.Join(work, "child.pid")
	spec := Spec{Name: "pair", Command: "sleep 300 & echo $! > child.pid; echo up; wait", Dir: work, Health: Health{Port: freePort(t)}}

	s := openReal(t, store)
	row, err := s.Keep("agent-a", spec, PolicyOnFailure, Provenance{Label: "start it"})
	if err != nil {
		t.Fatal(err)
	}
	child := readPID(t, childFile)
	t.Cleanup(func() { _ = syscall.Kill(-row.PID, syscall.SIGKILL) })
	s.Close()

	if gone(row.PID) || gone(child) {
		t.Fatal("closing the supervisor ended the service")
	}
	s = openReal(t, store)
	defer s.Close()
	got, err := s.Get("agent-a", row.ID)
	if err != nil || !got.Running || got.PID != row.PID || !got.ReAdopted {
		t.Fatalf("the next supervisor did not take the same process back: %+v %v", got, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.StopAll(ctx, "quit"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the leader and its child ended", func() bool { return gone(row.PID) && gone(child) })
	if body, _ := os.ReadFile(got.LogPath); !strings.Contains(string(body), "up") {
		t.Fatal("the service's output was not kept")
	}
}

// TestAStartedServiceThatExitsTakesItsGroupWithIt: the leader exits, the child
// it left behind is swept before the leader is reaped.
func TestAStartedServiceThatExitsTakesItsGroupWithIt(t *testing.T) {
	if processgroup.BootID() == "" {
		t.Skip("this machine names no boot")
	}
	work := t.TempDir()
	rt := PlatformRuntime()
	exits := make(chan Exit, 1)
	startNow(t, rt, Spec{Command: "sleep 300 & echo $! > child.pid; exit 7", Dir: work, Health: Health{Port: 1}},
		filepath.Join(work, "log"), exits)
	child := readPID(t, filepath.Join(work, "child.pid"))
	select {
	case x := <-exits:
		if !x.Known || x.Code != 7 {
			t.Fatalf("exit %+v", x)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no exit reported")
	}
	waitFor(t, "the orphaned child was swept", func() bool { return gone(child) })
}

// TestATimedOutCheckTakesItsWholeGroup.
func TestATimedOutCheckTakesItsWholeGroup(t *testing.T) {
	work := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := runCheckCommand(ctx, Spec{Dir: work, Health: Health{Kind: HealthCmd, Cmd: "sleep 300 & echo $! > child.pid; wait"}})
	if err == nil || err.Error() != "took over 5s" {
		t.Fatalf("a check past its timeout: %v", err)
	}
	child := readPID(t, filepath.Join(work, "child.pid"))
	waitFor(t, "the check's child ended", func() bool { return gone(child) })
	if err := runCheckCommand(context.Background(), Spec{Dir: work, Health: Health{Kind: HealthCmd, Cmd: "exit 3"}}); err == nil || err.Error() != "exits 3" {
		t.Fatalf("a failing check: %v", err)
	}
	if err := runCheckCommand(context.Background(), Spec{Dir: work, Health: Health{Kind: HealthCmd, Cmd: "test -d ."}}); err != nil {
		t.Fatalf("a passing check: %v", err)
	}
}

// TestALeaderIsNeverSignalledAfterItsReap: the one sweep happens before the
// reap, and every signal after it sends nothing at all — the pid may already
// be somebody else's.
func TestALeaderIsNeverSignalledAfterItsReap(t *testing.T) {
	cmd := exec.Command("true")
	processgroup.ConfigureDetached(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	l, _ := newLeader(cmd.Process)
	var mu sync.Mutex
	var sent []bool
	l.send = func(_ processgroup.Group, kill bool) error {
		mu.Lock()
		defer mu.Unlock()
		if l.reaped {
			t.Error("a signal was sent after the reap")
		}
		sent = append(sent, kill)
		return nil
	}
	l.reap(cmd)
	_ = l.signal(false)
	_ = l.signal(true)
	mu.Lock()
	defer mu.Unlock()
	if len(sent) > 1 {
		t.Fatalf("signals after the reap: %v", sent)
	}
}

// TestAURLCheckRunsNoShell: whatever a URL holds, it is a request, never a
// command line.
func TestAURLCheckRunsNoShell(t *testing.T) {
	work := t.TempDir()
	marker := filepath.Join(work, "pwned")
	health, err := Health{URL: "http://127.0.0.1:1/$(touch%20" + marker + ")"}.Check()
	if err != nil {
		t.Fatal(err)
	}
	_ = PlatformRuntime().Probe(context.Background(), Spec{Dir: work, Health: health})
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a URL check ran a shell")
	}
	for _, bad := range []string{"http://x/ ; touch y", "file:///etc/passwd", "http://user:pw@localhost:1/", "localhost:5173"} {
		if _, err := (Health{URL: bad}).Check(); err == nil {
			t.Fatalf("%q was taken as a health URL", bad)
		}
	}
}

// TestAURLCheckIsStrictAndBounded: 200 means 200, a certificate is verified,
// and an endless body is not read.
func TestAURLCheckIsStrictAndBounded(t *testing.T) {
	rt := PlatformRuntime()
	probe := func(url string) error {
		return rt.Probe(context.Background(), Spec{Health: Health{Kind: HealthURL, URL: url}})
	}
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer ok.Close()
	if err := probe(ok.URL); err != nil {
		t.Fatalf("a 200: %v", err)
	}
	moved := httptest.NewServer(http.RedirectHandler("/elsewhere", http.StatusFound))
	defer moved.Close()
	if err := probe(moved.URL); err == nil || err.Error() != "answered 302" {
		t.Fatalf("a redirect is not a 200: %v", err)
	}
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer secure.Close()
	if err := probe(secure.URL); err == nil {
		t.Fatal("an unverifiable certificate passed the check")
	}
	stop := make(chan struct{})
	endless := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		chunk := []byte(strings.Repeat("x", 32<<10))
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer endless.Close()
	defer close(stop)
	begun := time.Now()
	if err := probe(endless.URL); err != nil {
		t.Fatalf("an endless 200: %v", err)
	}
	if time.Since(begun) > 4*time.Second {
		t.Fatal("the check read the endless body")
	}
}

// TestALogIsCutBackToTwoChunks.
func TestALogIsCutBackToTwoChunks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s1.log")
	if err := os.WriteFile(path, []byte(strings.Repeat("y", 3*logChunk/2)), 0o600); err != nil {
		t.Fatal(err)
	}
	cut, err := rotateLog(path)
	if err != nil || !cut {
		t.Fatalf("not cut: %v", err)
	}
	live, _ := os.Stat(path)
	backup, _ := os.Stat(path + ".1")
	if live.Size() != 0 || backup.Size() != logChunk || backup.Mode().Perm() != 0o600 {
		t.Fatalf("live %d, backup %d %v", live.Size(), backup.Size(), backup.Mode())
	}
	if cut, _ := rotateLog(path); cut {
		t.Fatal("a small log was cut")
	}
}

// TestAServiceRunsInAFolderEndingInASpace.
func TestAServiceRunsInAFolderEndingInASpace(t *testing.T) {
	if processgroup.BootID() == "" {
		t.Skip("this machine names no boot")
	}
	work := filepath.Join(t.TempDir(), "web ")
	if err := os.Mkdir(work, 0o700); err != nil {
		t.Fatal(err)
	}
	spec, err := Spec{Command: "pwd > where", Dir: work, Health: Health{Port: 1}}.Check()
	if err != nil {
		t.Fatal(err)
	}
	exits := make(chan Exit, 1)
	startNow(t, PlatformRuntime(), spec, filepath.Join(work, "log"), exits)
	<-exits
	raw, _ := os.ReadFile(filepath.Join(work, "where"))
	resolved, _ := filepath.EvalSymlinks(work)
	if got := strings.TrimSuffix(string(raw), "\n"); got != work && got != resolved {
		t.Fatalf("ran in %q, not %q", got, work)
	}
}

// TestAGateRunsNothingUntilItIsCommitted: the group exists and its identity is
// known, and the approved command has not run; let go, it never does.
func TestAGateRunsNothingUntilItIsCommitted(t *testing.T) {
	if processgroup.BootID() == "" {
		t.Skip("this machine names no boot")
	}
	work := t.TempDir()
	rt := PlatformRuntime()
	spec := Spec{Command: "touch ran", Dir: work, Health: Health{Port: 1}}
	exits := make(chan Exit, 1)
	launch, err := rt.Launch(spec, filepath.Join(work, "log"), func(_ processgroup.Identity, x Exit) { exits <- x })
	if err != nil {
		t.Fatal(err)
	}
	if !rt.Alive(launch.Identity()) {
		t.Fatal("the gate is not a live group")
	}
	if _, err := os.Stat(filepath.Join(work, "ran")); err == nil {
		t.Fatal("the command ran before its commit")
	}
	launch.Abort()
	select {
	case x := <-exits:
		if x.Code != 125 {
			t.Fatalf("an aborted gate exits 125, not %+v", x)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("an aborted gate never exited")
	}
	if _, err := os.Stat(filepath.Join(work, "ran")); err == nil {
		t.Fatal("an aborted gate ran the command")
	}
	if err := launch.Commit(); err == nil {
		t.Fatal("a let-go gate took a commit")
	}

	committed := Spec{Command: "touch ran", Dir: work, Health: Health{Port: 1}}
	startNow(t, rt, committed, filepath.Join(work, "log"), exits)
	<-exits
	if _, err := os.Stat(filepath.Join(work, "ran")); err != nil {
		t.Fatal("a committed gate did not run the command")
	}
}

// gateParentEnv makes this test binary the parent that dies before its
// commit (TestAGateWhoseParentDiesRunsNothing).
const gateParentEnv = "CODEAF_SERVICE_GATE_PARENT"

// TestAGateWhoseParentDiesRunsNothing is the crash: a real parent launches a
// gate and exits without committing it. The gate reads end-of-file and exits,
// and the command never runs.
func TestAGateWhoseParentDiesRunsNothing(t *testing.T) {
	if dir := os.Getenv(gateParentEnv); dir != "" {
		launch, err := PlatformRuntime().Launch(Spec{Command: "touch ran", Dir: dir, Health: Health{Port: 1}},
			filepath.Join(dir, "log"), func(processgroup.Identity, Exit) {})
		if err != nil {
			os.Exit(2)
		}
		_ = os.WriteFile(filepath.Join(dir, "gate.pid"), []byte(strconv.Itoa(launch.Identity().PID)), 0o600)
		os.Exit(0)
	}
	if processgroup.BootID() == "" {
		t.Skip("this machine names no boot")
	}
	work := t.TempDir()
	parent := exec.Command(os.Args[0], "-test.run=^TestAGateWhoseParentDiesRunsNothing$")
	parent.Env = append(os.Environ(), gateParentEnv+"="+work)
	if out, err := parent.CombinedOutput(); err != nil {
		t.Fatalf("the parent: %v %s", err, out)
	}
	gate := readPID(t, filepath.Join(work, "gate.pid"))
	waitFor(t, "the orphaned gate exited", func() bool { return gone(gate) })
	if _, err := os.Stat(filepath.Join(work, "ran")); err == nil {
		t.Fatal("a gate whose parent died ran the command")
	}
}
