package ci

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
		"go run ./cmd/codeaf-changes check",
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

func heartbeatsIn(t *testing.T, path string) int {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Count(contents, []byte("still running"))
}
