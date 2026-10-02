package session

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/exec"
)

// A message written early cannot claim that an interrupted or incomplete
// run finished. The ending follows its account before codeaf's credits.
func TestAnIncompleteRunsCommitCarriesItsEndingAfterItsMessage(t *testing.T) {
	for name, ending := range map[string]string{
		"stopped": "stopped by the person", "failed": "handed in no finished change",
		"limit": "stopped at its ceiling", "crashed": "the program crashed",
		"unverified": "its own check did not pass or did not finish",
	} {
		t.Run(name, func(t *testing.T) {
			repo := newTestRepo(t)
			folder := prepareIn(t, notedProgram(), repo, "Add the fix")
			writeFile(t, filepath.Join(folder.Dir, "fix.go"), "package fix\n")
			message := "Add the fix\n\nThe program's account.\n\n" + exec.AttributionTrailers("test-model")
			writeFile(t, filepath.Join(folder.Dir, folder.Notes, programCommitMessageFile), message)
			folder.Finish(ending)
			got := gitOut(t, repo, "log", "-1", "--format=%B", folder.Branch)
			if !strings.HasPrefix(got, "Add the fix\n\nThe program's account.\n\n"+ending+"\n\n") {
				t.Fatalf("incomplete commit hides its ending behind the program's message: %q", got)
			}
			if strings.Count(got, exec.AttributionTrailer) != 1 || !strings.HasSuffix(strings.TrimSpace(got), exec.AttributionTrailer) {
				t.Fatalf("credits did not follow the ending once: %q", got)
			}
		})
	}
}

func TestAGoneRunsCommitSaysCodeafFoundItGoneBeforeFinishing(t *testing.T) {
	repo := newTestRepo(t)
	folder := prepareIn(t, notedProgram(), repo, "Add the fix")
	// The recovery path borrows the hold a later process would have claimed.
	defer folder.releaseCopy()
	folder.Passed = true
	writeFile(t, filepath.Join(folder.Dir, "fix.go"), "package fix\n")
	writeFile(t, filepath.Join(folder.Dir, folder.Notes, programCommitMessageFile), "Add the fix\n\nThe program's account.")
	end := folder.settleGone()
	if !end.Committed || !end.Gone {
		t.Fatalf("gone run was not kept: %+v", end)
	}
	got := gitOut(t, repo, "log", "-1", "--format=%B", folder.Branch)
	if !strings.Contains(got, "The program's account.\n\ncodeaf found its run had gone before it finished.\n") {
		t.Fatalf("gone commit hides that its run disappeared: %q", got)
	}
}
