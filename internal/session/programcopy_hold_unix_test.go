//go:build !windows

package session

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// THE HOLD HANDED TO THE PROGRAM OUTLIVES THE CODEAF THAT TOOK IT. A codeaf
// killed outright leaves its program the grace to stop, restoring and
// committing in its copy; the next codeaf once finished that copy under it. The
// program's process has the same open file (delegate.Launch.Hold, played here
// by a second descriptor), so the copy is left alone until it has gone too.
func TestACopyIsNotFinishedWhileTheProgramStillHoldsIt(t *testing.T) {
	repo := newTestRepo(t)
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Fix the parser")
	program, err := syscall.Dup(int(folder.Hold().Fd()))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(folder.Dir, "late.go"), "package late\n")
	// THE HOST DIES: its descriptor closes, and nothing unlocks.
	_ = folder.lock.Close()
	folder.lock = nil
	sweepProgramCopies(repo)
	if _, err := os.Stat(folder.Dir); err != nil {
		t.Fatalf("the copy was finished while its program still held it: %v", err)
	}
	if record, ok := readProgramFolder(folder.key); !ok || record.Ended != "" {
		t.Fatal("the run was settled while its program still held its copy")
	}
	// THE PROGRAM GOES TOO, and the next sweep finishes the copy.
	_ = syscall.Close(program)
	sweepProgramCopies(repo)
	if _, err := os.Stat(folder.Dir); !os.IsNotExist(err) {
		t.Fatalf("the copy is still there after its program went: %v", err)
	}
	if files := gitOut(t, repo, "ls-tree", "-r", "--name-only", folder.Branch); !strings.Contains(files, "late.go") {
		t.Fatalf("the program's last edit is not on its branch:\n%s", files)
	}
}
