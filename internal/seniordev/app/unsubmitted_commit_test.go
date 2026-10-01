//go:build !windows

package app

import (
	"path/filepath"
	"strings"
	"testing"
)

// Unsubmitted work is the normal state of a run, and a nudge must not ask
// the model to commit it before the candidate has been checked.
func TestUnsubmittedWorkDoesNotAskForACommitBeforeChecking(t *testing.T) {
	runner, _, base, _ := soloPipeline(t)
	if err := gitRun(runner.workspace, "switch", "-c", "task/run"); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{"README.md": "base\nedit\n", "new.txt": "new work\n"} {
		if err := writeFile(filepath.Join(runner.workspace, path), body); err != nil {
			t.Fatal(err)
		}
	}
	findings := strings.Join(runner.soloUnsubmittedFindings(base), "\n")
	if !strings.Contains(findings, "2 file(s)") {
		t.Fatalf("findings lost the run's work: %q", findings)
	}
	if strings.Contains(strings.ToLower(findings), "commit before") || strings.Contains(findings, "COMMITTED tree") {
		t.Fatalf("unsubmitted work asks the model to commit before checking: %q", findings)
	}
}
