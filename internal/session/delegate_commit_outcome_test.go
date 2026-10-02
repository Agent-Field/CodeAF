package session

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
)

// A program's early message says what it intended, while its summary says
// whether that run passed. Landing must use the latter to choose the footer.
func TestDelegateCommitUsesItsOutcomeToDecideWhetherToKeepTheEnding(t *testing.T) {
	for _, row := range []struct {
		name    string
		summary RunSummary
		passed  bool
	}{
		{"passed", RunSummary{Outcome: beltRunOutcomeDone, ProgramVerdict: "pass", Result: "its checks passed"}, true},
		{"unchecked", RunSummary{Outcome: beltRunOutcomeDone, ProgramVerdict: "pass-unverified", Result: "nothing finished checking"}, false},
		{"failed", RunSummary{Program: &ProgramEnding{Status: delegate.StatusFail, Result: "nothing was handed in"}}, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			repo := newTestRepo(t)
			folder := prepareIn(t, notedProgram(), repo, "Add the fix")
			writeFile(t, filepath.Join(folder.Dir, "fix.go"), "package fix\n")
			writeFile(t, filepath.Join(folder.Dir, folder.Notes, programCommitMessageFile), "The program's message\n")
			agent := &Agent{}
			agent.landDelegateRun(&beltRun{folder: folder}, row.summary)
			got := gitOut(t, repo, "log", "-1", "--format=%B", folder.Branch)
			_, ending := runEndingWords(row.summary)
			if !strings.HasPrefix(got, "The program's message\n") || strings.Contains(got, ending) == row.passed {
				t.Fatalf("landing's commit = %q, passed %v", got, row.passed)
			}
		})
	}
}
