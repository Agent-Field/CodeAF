package session

import (
	"encoding/json"
	"fmt"
	"os"
	osexec "os/exec"
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

// A finishing commit must not start maintenance that can outlive the run.
func TestTheFinishingCommitStartsNoAutomaticGC(t *testing.T) {
	finishingMaintenance(t, false)
}

func TestTheFinishingCommitStartsNoGeometricMaintenance(t *testing.T) {
	out, err := osexec.Command("git", "version").Output()
	if err != nil {
		t.Fatal(err)
	}
	var major, minor int
	if _, err := fmt.Sscanf(string(out), "git version %d.%d", &major, &minor); err != nil {
		t.Fatal(err)
	}
	if major < 2 || major == 2 && minor < 56 {
		t.Skipf("default geometric maintenance requires Git 2.56 or newer; found %s", strings.TrimSpace(string(out)))
	}
	finishingMaintenance(t, true)
}

func finishingMaintenance(t *testing.T, geometric bool) {
	t.Helper()
	for _, control := range []bool{true, false} {
		name := "codeaf"
		if control {
			name = "plain-git-control"
		}
		t.Run(name, func(t *testing.T) {
			repo := newTestRepo(t)
			folder := prepareIn(t, notedProgram(), repo, "Save the fix")
			if control {
				// Release the copy only after checking the plain-git evidence.
				t.Cleanup(func() { folder.Finish("control complete") })
			}
			writeFile(t, filepath.Join(folder.Dir, "fix.go"), "package fix\n")
			marker := filepath.Join(t.TempDir(), "pre-auto-gc-ran")
			hooks := filepath.Join(t.TempDir(), "hooks")
			if err := os.MkdirAll(hooks, 0o755); err != nil {
				t.Fatal(err)
			}
			exit := "1"
			if geometric {
				exit = "0"
			}
			writeFile(t, filepath.Join(hooks, "pre-auto-gc"), "#!/bin/sh\necho ran > '"+marker+"'\nexit "+exit+"\n")
			if err := os.Chmod(filepath.Join(hooks, "pre-auto-gc"), 0o755); err != nil {
				t.Fatal(err)
			}
			gitT(t, folder.Dir, "config", "core.hooksPath", hooks)
			gitT(t, folder.Dir, "config", "gc.autoDetach", "false")
			gitT(t, folder.Dir, "config", "maintenance.autoDetach", "false")
			gitT(t, folder.Dir, "config", "maintenance.auto", "true")
			if geometric {
				// Leave maintenance.strategy unset: exercise Git 2.56's default.
				// Its loose-object threshold rounds up to a multiple of 256.
				gitT(t, folder.Dir, "config", "maintenance.geometric-repack.auto", "1")
			} else {
				gitT(t, folder.Dir, "config", "maintenance.strategy", "gc")
				gitT(t, folder.Dir, "config", "gc.auto", "1")
			}
			looseObjects(t, folder.Dir, 2000)
			trace := filepath.Join(t.TempDir(), "trace.json")
			t.Setenv("GIT_TRACE2_EVENT", trace)
			if control {
				gitT(t, folder.Dir, "add", "fix.go")
				gitT(t, folder.Dir, "commit", "-q", "-m", "Save the fix")
			} else {
				folder.Passed = true
				end := folder.Finish("the fix passed")
				if !end.Kept || end.Refused != "" {
					t.Fatalf("finishing the run: %+v", end)
				}
			}
			_, err := os.Stat(marker)
			if control && err != nil {
				t.Fatalf("plain git never started automatic maintenance; fixture proves nothing: %v", err)
			}
			if !control && !os.IsNotExist(err) {
				t.Errorf("automatic maintenance hook ran during codeaf's finishing commit: %v", err)
			}
			if geometric {
				data, err := os.ReadFile(trace)
				if err != nil {
					t.Fatal(err)
				}
				ran := false
				for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
					var event struct{ Event, Category, Label string }
					if err := json.Unmarshal([]byte(line), &event); err != nil {
						t.Fatal(err)
					}
					if event.Event == "region_enter" && event.Category == "maintenance" && event.Label == "geometric-repack" {
						ran = true
					}
				}
				if ran != control {
					t.Errorf("geometric repack ran = %v, want %v", ran, control)
				}
			}
		})
	}
}
