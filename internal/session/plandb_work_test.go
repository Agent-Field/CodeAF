package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A RUN'S WORKING COPY IS READ AS WHAT THE WORK CHANGED: the patch against the
// commit the copy was cut from, and the files the work added, with every file
// the harness itself writes left out.
func TestReadPlanWorkIsTheCopysOwnChangesWithoutTheHarnessFiles(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q")
	write("load.go", "old line\n")
	run("add", "load.go")
	run("commit", "-q", "-m", "base")
	base := run("rev-parse", "HEAD")

	write("load.go", "new line\n")
	write("notes.txt", "added by the work\n")
	write(planStoreFilename, "the harness's own store\n")

	work := readPlanWork(&TaskCopyRecord{Dir: dir, HomeSha: base})
	if !work.Read || work.Cut {
		t.Fatalf("the copy was not read whole: read %v cut %v", work.Read, work.Cut)
	}
	if !strings.Contains(work.Patch, "-old line") || !strings.Contains(work.Patch, "+new line") {
		t.Fatalf("the patch does not carry the change:\n%s", work.Patch)
	}
	if len(work.Added) != 1 || work.Added[0] != "notes.txt" {
		t.Fatalf("added files = %q, want the work's own file and not the harness's", work.Added)
	}
}

// A PATCH WITH A FILE THE HARNESS WROTE LOSES THAT FILE WHOLE, never half.
func TestPlanWorkPatchDropsAHarnessFileWhole(t *testing.T) {
	patch := "diff --git a/load.go b/load.go\n--- a/load.go\n+++ b/load.go\n@@ -1 +1 @@\n-a\n+b\n" +
		"diff --git a/" + planStoreFilename + " b/" + planStoreFilename + "\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-c\n+d\n"
	got, cut := planWorkPatch(patch)
	if cut || strings.Contains(got, planStoreFilename) || !strings.Contains(got, "+b") {
		t.Fatalf("planWorkPatch kept %q (cut %v)", got, cut)
	}
}

// A RUN THAT HAS ENDED GIVES ITS COPY BACK AND THE FOLDER STAYS ON DISK, as a
// plain folder git no longer answers for. The work is on the run's branch, and
// that is where the work tab reads it: a finished or stopped run whose branch is
// still in the repository has work to show, not a copy that is gone.
func TestReadPlanWorkReadsAGivenBackCopyOffItsBranch(t *testing.T) {
	root := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(dir, name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q")
	write(root, "README.md", "the ground\n")
	run("add", "README.md")
	run("commit", "-q", "-m", "base")
	base := run("rev-parse", "HEAD")
	run("checkout", "-q", "-b", "task/given-back")
	write(root, "made.txt", "the run's work\n")
	run("add", "made.txt")
	run("commit", "-q", "-m", "the run")
	run("checkout", "-q", "--detach", base)

	// The copy as the run leaves it once it is given back: the files, and the
	// note that it was released, in a folder that is no repository.
	given := t.TempDir()
	write(given, "made.txt", "the run's work\n")
	write(given, ".codeaf/released.json", `{"branch":"task/given-back"}`)

	work := readPlanWork(&TaskCopyRecord{Dir: given, Root: root, Branch: "task/given-back", HomeSha: base})
	if !work.Read {
		t.Fatalf("a given-back copy whose branch is kept read as gone: %+v", work)
	}
	if !strings.Contains(work.Patch, "+the run's work") {
		t.Fatalf("the branch's work is not in the patch:\n%s", work.Patch)
	}
}
