package session

// THE HOLD A PROGRAM'S RUN HAS ON ITS FOLDER (programhold.go), in real folders
// and real git: a folder is busy when a held folder is it, holds it or is
// inside it, and a folder nobody holds is left exactly as it was by every
// door that asks.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// holdFolder readies dir for a run of the fake program and answers the
// folder, held until the test ends.
func holdFolder(t *testing.T, dir, title string) *ProgramFolder {
	t.Helper()
	folder, err := PrepareProgramFolder(ProgramFolderOrder{Program: testPrograms("fake")[0], Dir: dir, Title: title, Holder: "task 4 (" + title + ")", Keep: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { folder.Finish("") })
	return folder
}

// prepareErr is the refusal a run readied on dir meets, "" when it was let
// through (and then finished at once).
func prepareErr(t *testing.T, dir string) string {
	t.Helper()
	folder, err := PrepareProgramFolder(ProgramFolderOrder{Program: testPrograms("fake")[0], Dir: dir, Title: "The second run", Holder: "task 5 (The second run)", Keep: t.TempDir()})
	if err != nil {
		return err.Error()
	}
	folder.Finish("")
	return ""
}

// ONE FOLDER TAKES ONE PROGRAM RUN, AND SO DO THE FOLDERS INSIDE IT. A run on a
// plain folder of projects puts back whatever changed under it once it has
// submitted, so a second run in one of those projects had its work reverted
// under it while each held only its own exact path. A run on a folder inside a
// held one, or around one, is refused naming the run and the folder it holds,
// at its start and at its card alike; two runs side by side are not.
func TestAProgramRunIsRefusedAFolderInsideOrAroundAHeldOne(t *testing.T) {
	work := t.TempDir()
	project := filepath.Join(work, "proj")
	sibling := filepath.Join(work, "other")
	for _, dir := range []string{project, sibling} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("inside", func(t *testing.T) {
		held := holdFolder(t, work, "Tidy the projects")
		want := project + " is busy: fake, task 4 (Tidy the projects), is working in " + canonicalPath(work) +
			", which holds it, and one folder takes one program run at a time; ask again when that run has ended"
		if got := prepareErr(t, project); got != want {
			t.Fatalf("a run inside a held folder = %q, want %q", got, want)
		}
		folder := project
		if got := programGroundRefusal(testPrograms("fake")[0], &folder); got != want {
			t.Fatalf("its card = %q, want %q", got, want)
		}
		held.Finish("")
		if got := prepareErr(t, project); got != "" {
			t.Fatalf("the folder was still refused after the run around it ended: %q", got)
		}
	})
	t.Run("around", func(t *testing.T) {
		held := holdFolder(t, project, "Fix the parser")
		want := work + " is busy: fake, task 4 (Fix the parser), is working in " + canonicalPath(project) +
			", which is inside it, and one folder takes one program run at a time; ask again when that run has ended"
		if got := prepareErr(t, work); got != want {
			t.Fatalf("a run around a held folder = %q, want %q", got, want)
		}
		if got := prepareErr(t, sibling); got != "" {
			t.Fatalf("a run beside a held folder was refused: %q", got)
		}
		held.Finish("")
	})
	t.Run("a repository inside", func(t *testing.T) {
		repo := newTestRepo(t)
		parent := filepath.Dir(repo)
		held := holdFolder(t, parent, "Tidy the projects")
		got := prepareErr(t, repo)
		if !strings.HasPrefix(got, repo+" is busy: fake, task 4 (Tidy the projects), is working in "+canonicalPath(parent)+", which holds it") {
			t.Fatalf("a repository inside a held folder = %q", got)
		}
		if head := currentBranch(repo); head != "work" {
			t.Fatalf("a refused repository was switched to %q", head)
		}
		held.Finish("")
	})
}
