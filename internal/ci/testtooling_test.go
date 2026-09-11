package ci

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// THE LIGHT JOB IN .github/workflows/ci.yml IS THE SOURCE OF THIS LIST, and a step
// added there is owed a line here. `test-quick` is what somebody runs instead of
// waiting for that job, and the three commands below are the ones it went a week
// without: the change entries, the manual corpus, and the two -run Manual packages.
// A slash command with no manual page kept `test-quick` green and turned the pull
// request red, which is the whole reason the target exists (#735).
func TestTheQuickTargetRunsEveryDeterministicLightGate(t *testing.T) {
	makePath, err := exec.LookPath("make")
	if err != nil {
		t.Skip("make is not on PATH")
	}
	cmd := exec.Command(makePath, "-n", "test-quick")
	cmd.Dir = repositoryRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make -n test-quick: %v\n%s", err, out)
	}
	for _, command := range []string{
		"go run ./cmd/aforge-changes check",
		"go test ./internal/manual/",
		"go test -run 'Manual' ./internal/tui3/ ./internal/session/",
	} {
		if !bytes.Contains(out, []byte(command)) {
			t.Errorf("make -n test-quick does not name %q:\n%s", command, out)
		}
	}
}

func TestTheDefaultTimingReportDoesNotDirtyTheSharedCheckout(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not on PATH")
	}
	root := repositoryRoot(t)
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	var report string
	for _, line := range strings.Split(string(makefile), "\n") {
		name, value, found := strings.Cut(line, "?=")
		if found && strings.TrimSpace(name) == "REPORT" {
			report = strings.TrimSpace(value)
			break
		}
	}
	if report == "" {
		t.Fatal("Makefile has no non-empty REPORT ?= default")
	}
	cmd := exec.Command(gitPath, "check-ignore", "-q", "--", report)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("default REPORT %q is not ignored: %v\n%s", report, err, out)
	}
}

func TestTheHeartbeatStopsAfterItsParentIsKilled(t *testing.T) {
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH")
	}
	sleepPath, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep is not on PATH")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not on PATH")
	}
	root := repositoryRoot(t)
	stderrPath := filepath.Join(t.TempDir(), "stderr")
	stderr, err := os.Create(stderrPath)
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()
	cmd := exec.Command(bashPath, filepath.Join(root, "scripts/test-report.sh"), filepath.Join(t.TempDir(), "r.json"), sleepPath, "60")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "TEST_REPORT_HEARTBEAT=1", "GOFLAGS=-buildvcs=false")
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_ = cmd.Process.Kill()
	})

	deadline := time.Now().Add(10 * time.Second)
	for {
		contents, readErr := os.ReadFile(stderrPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if bytes.Contains(contents, []byte("still running")) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("heartbeat did not report within 10 seconds; stderr = %q", contents)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	// COUNT THE HEARTBEATS AND NOT THE BYTES. The killed run's own command is still
	// in the pipeline and may yet say something on this stream; what is being asked
	// is whether the heartbeat loop is still speaking for a run that is gone.
	before := heartbeatsIn(t, stderrPath)
	time.Sleep(4 * time.Second)
	if after := heartbeatsIn(t, stderrPath); after != before {
		contents, _ := os.ReadFile(stderrPath)
		t.Fatalf("the heartbeat outlived its run: %d reports before the kill, %d four seconds after:\n%s", before, after, contents)
	}
}

func heartbeatsIn(t *testing.T, path string) int {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Count(contents, []byte("still running"))
}
