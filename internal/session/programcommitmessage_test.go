package session

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/exec"
)

// notedProgram is the fake program with a notes folder of its own, which is
// where a program is asked to write its commit's message.
func notedProgram() delegate.Delegate {
	program := testPrograms("fake")[0]
	program.Notes = ".fake"
	return program
}

// THE COMMIT THAT ENDS A RUN CARRIES THE MESSAGE THE PROGRAM WROTE FOR IT. The
// brief asks for one in the program's notes; the commit takes it whole, signed
// once, and the notes still leave the copy without reaching the branch.
func TestTheFinishingCommitCarriesTheMessageTheProgramWrote(t *testing.T) {
	repo := newTestRepo(t)
	folder := prepareIn(t, notedProgram(), repo, "Fix up PR 1699 so it is merge-ready")
	if note := folder.BriefNote(); !strings.Contains(note, "Leave your work uncommitted") ||
		!strings.Contains(note, ".fake/"+programCommitMessageFile) {
		t.Fatalf("the brief does not steer away from commits and ask for a message: %q", note)
	}
	writeFile(t, filepath.Join(folder.Dir, "fix.go"), "package fix\n")
	message := "fix(chat): a demoted slash tag stays plain\n\nThe transcript subtracts the demoted ranges.\n\n" + exec.AttributionTrailer
	writeFile(t, filepath.Join(folder.Dir, ".fake", programCommitMessageFile), "\r\n"+strings.ReplaceAll(message, "\n", "\r\n")+"\r\n")

	folder.Passed = true
	end := folder.Finish("senior-dev finished: the model claimed the fix and its tests passed")
	if !end.Kept || end.Refused != "" {
		t.Fatalf("finishing the run: %+v", end)
	}
	got := gitOut(t, repo, "log", "-1", "--format=%B", folder.Branch)
	if !strings.HasPrefix(got, "fix(chat): a demoted slash tag stays plain\n\nThe transcript subtracts the demoted ranges.\n") {
		t.Fatalf("the commit's message = %q, want the program's own", got)
	}
	if strings.Contains(got, "Fix up PR 1699") || strings.Contains(got, "the model claimed") || strings.Contains(got, "\r") {
		t.Fatalf("the commit's message fell back to the title and ending, or kept carriage returns: %q", got)
	}
	if strings.Count(got, exec.AttributionTrailer) != 1 || !strings.Contains(got, exec.AttributionAssistedBy) {
		t.Fatalf("the commit is not signed exactly once: %q", got)
	}
	if tree := gitOut(t, repo, "ls-tree", "-r", "--name-only", folder.Branch); strings.Contains(tree, ".fake") {
		t.Fatalf("the program's notes reached the branch:\n%s", tree)
	}
}

// A PROGRAM THAT WROTE NO MESSAGE, OR AN EMPTY ONE, GETS THE ONE IT ALWAYS
// GOT: the task's title, and the run's ending under it.
func TestTheFinishingCommitFallsBackToTheTitleWithoutAMessage(t *testing.T) {
	for name, written := range map[string]string{"none": "", "blank": "\n  \n\t\n"} {
		t.Run(name, func(t *testing.T) {
			repo := newTestRepo(t)
			folder := prepareIn(t, notedProgram(), repo, "Add the fix")
			writeFile(t, filepath.Join(folder.Dir, "fix.go"), "package fix\n")
			if written != "" {
				writeFile(t, filepath.Join(folder.Dir, ".fake", programCommitMessageFile), written)
			}
			folder.Finish("the ending")
			got := gitOut(t, repo, "log", "-1", "--format=%B", folder.Branch)
			if !strings.HasPrefix(got, "Add the fix\n\nthe ending\n") {
				t.Fatalf("the commit's message = %q, want the title and the ending", got)
			}
		})
	}
}

// A PROGRAM WITH NO NOTES FOLDER IS STILL TOLD TO LEAVE ITS WORK UNCOMMITTED,
// and is not asked for a message it has nowhere to write.
func TestABriefWithoutNotesSteersAwayFromCommitsAndAsksForNoMessage(t *testing.T) {
	repo := newTestRepo(t)
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Add the fix")
	note := folder.BriefNote()
	if !strings.Contains(note, "Leave your work uncommitted") || strings.Contains(note, programCommitMessageFile) {
		t.Fatalf("the brief for a program without notes = %q", note)
	}
}
