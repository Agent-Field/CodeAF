package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/exec"
)

// An unusable message must never keep otherwise committable work off the
// run's branch; the title and ending still describe that work.
func TestUnusableCommitMessagesKeepWorkWithTheTitleAndEnding(t *testing.T) {
	for name, message := range map[string]string{
		"nul": "Add work\x00bad bytes", "credits": exec.AttributionTrailers("test-model"),
		"assisted": exec.AttributionAssistedBy, "coauthor": exec.AttributionTrailer,
		"large": strings.Repeat("x", programCommitMessageMax+1), "invalid-utf8": string([]byte{0xff}),
	} {
		t.Run(name, func(t *testing.T) {
			repo := newTestRepo(t)
			folder := prepareIn(t, notedProgram(), repo, "Add the fix")
			writeFile(t, filepath.Join(folder.Dir, "fix.go"), "package fix\n")
			writeFile(t, filepath.Join(folder.Dir, folder.Notes, programCommitMessageFile), message)
			end := folder.Finish("the ending")
			if !end.Kept || !end.Committed || end.Refused != "" || end.Patch != "" {
				t.Fatalf("unusable message kept work off the branch: %+v", end)
			}
			got := gitOut(t, repo, "log", "-1", "--format=%B", folder.Branch)
			if !strings.HasPrefix(got, "Add the fix\n\nthe ending\n") {
				t.Fatalf("unusable message replaced the title and ending: %q", got)
			}
			if body := gitOut(t, repo, "show", folder.Branch+":fix.go"); strings.TrimSpace(body) != "package fix" {
				t.Fatalf("committed work = %q", body)
			}
		})
	}
}

func TestAnUnchangedTrackedCommitMessageIsNotThisRunsMessage(t *testing.T) {
	for name, changed := range map[string]bool{"unchanged": false, "changed": true} {
		t.Run(name, func(t *testing.T) {
			repo := newTestRepo(t)
			path := filepath.Join(".fake", programCommitMessageFile)
			writeFile(t, filepath.Join(repo, path), "An old run's message\n")
			gitOut(t, repo, "add", "--", path)
			gitOut(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "old notes")
			folder := prepareIn(t, notedProgram(), repo, "Add the fix")
			writeFile(t, filepath.Join(folder.Dir, "fix.go"), "package fix\n")
			if changed {
				writeFile(t, filepath.Join(folder.Dir, path), "This run's message\n")
			}
			end := folder.Finish("the ending")
			if !end.Committed || end.Refused != "" {
				t.Fatalf("finishing: %+v", end)
			}
			got := gitOut(t, repo, "log", "-1", "--format=%B", folder.Branch)
			if changed {
				if !strings.HasPrefix(got, "This run's message\n") {
					t.Fatalf("this run's replacement message was lost: %q", got)
				}
			} else if !strings.HasPrefix(got, "Add the fix\n\nthe ending\n") || strings.Contains(got, "An old run's message") {
				t.Fatalf("unchanged tracked message was taken as this run's: %q", got)
			}
		})
	}
}

func TestACommitMessageSymlinkNeverReadsItsOutsideTarget(t *testing.T) {
	repo := newTestRepo(t)
	folder := prepareIn(t, notedProgram(), repo, "Add the fix")
	writeFile(t, filepath.Join(folder.Dir, "fix.go"), "package fix\n")
	outside := filepath.Join(t.TempDir(), "outside")
	writeFile(t, outside, "Outside target's private words\n")
	path := filepath.Join(folder.Dir, folder.Notes, programCommitMessageFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	end := folder.Finish("the ending")
	if !end.Committed || end.Refused != "" {
		t.Fatalf("finishing: %+v", end)
	}
	got := gitOut(t, repo, "log", "-1", "--format=%B", folder.Branch)
	if !strings.HasPrefix(got, "Add the fix\n\nthe ending\n") || strings.Contains(got, "Outside target") {
		t.Fatalf("symlink supplied outside bytes to the commit: %q", got)
	}
}

// A hand-back counts work from the start of the line, but message ownership
// is measured from this run's own start, including notes an earlier run tracked.
func TestACarriedRunRejectsTheTrackedMessageItStartedWith(t *testing.T) {
	repo := newTestRepo(t)
	path := filepath.Join(".fake", programCommitMessageFile)
	writeFile(t, filepath.Join(repo, path), "Original tracked message\n")
	gitOut(t, repo, "add", "--", path)
	gitOut(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "old notes")
	first := prepareIn(t, notedProgram(), repo, "First work")
	writeFile(t, filepath.Join(first.Dir, path), "An earlier run's tracked message\n")
	gitOut(t, first.Dir, "add", "--", path)
	gitOut(t, first.Dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "replace notes")
	first.Finish("first ending")
	second, err := PrepareProgramFolder(ProgramFolderOrder{Program: notedProgram(), Dir: repo, Title: "Second work", Keep: t.TempDir(), Carry: &programCarry{Root: repo, Branch: first.Branch, Home: first.Home, Start: first.Start, Snapshot: first.Snapshot}})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(second.Dir, "second.go"), "package second\n")
	end := second.Finish("second ending")
	if !end.Committed || end.Refused != "" {
		t.Fatalf("finishing: %+v", end)
	}
	got := gitOut(t, repo, "log", "-1", "--format=%B", second.Branch)
	if !strings.HasPrefix(got, "Second work\n\nsecond ending\n") || strings.Contains(got, "earlier run's") {
		t.Fatalf("carried run reused its starting tracked message: %q", got)
	}
}
