package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func safetyRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	mustGit(t, repo, "init", "-q", "-b", "main")
	mustGit(t, repo, "config", "user.name", "Person")
	mustGit(t, repo, "config", "user.email", "person@example.test")
	writeFile(t, filepath.Join(repo, "shared.txt"), "base\n")
	mustGit(t, repo, "add", "shared.txt")
	mustGit(t, repo, "commit", "-q", "-m", "base")
	return repo
}

// An ignored secret at the start remains outside the run's commit after the
// program changes the ignore rule, while the changed rule is ordinary work.
func TestProgramFolderNeverCommitsPathsIgnoredAtStartOrRunCaches(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	repo := safetyRepo(t)
	writeFile(t, filepath.Join(repo, ".gitignore"), ".env\n")
	mustGit(t, repo, "add", ".gitignore")
	mustGit(t, repo, "commit", "-q", "-m", "ignore")
	writeFile(t, filepath.Join(repo, ".env"), "SECRET=private\n")
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Change ignore rules")
	ignoredRecord, err := os.ReadFile(folder.IgnoredFile())
	if err != nil || !strings.Contains(string(ignoredRecord), ".env\x00") {
		t.Fatalf("the child cannot read its start-time ignore record: %q, %v", ignoredRecord, err)
	}
	writeFile(t, filepath.Join(repo, ".gitignore"), "# changed by run\n")
	writeFile(t, filepath.Join(repo, "__pycache__", "module.pyc"), "bytecode")
	writeFile(t, filepath.Join(repo, ".pytest_cache", "state"), "cache")
	writeFile(t, filepath.Join(repo, "made.txt"), "work\n")
	folder.Finish("done")
	paths := gitOut(t, repo, "ls-tree", "-r", "--name-only", "HEAD")
	for _, want := range []string{".gitignore", "made.txt"} {
		if !strings.Contains(paths, want) {
			t.Fatalf("%s missing from commit: %s", want, paths)
		}
	}
	for _, excluded := range []string{".env", "__pycache__/module.pyc", ".pytest_cache/state"} {
		if strings.Contains(paths, excluded) {
			t.Fatalf("%s entered commit: %s", excluded, paths)
		}
		if _, err := os.Stat(filepath.Join(repo, excluded)); err != nil {
			t.Fatalf("%s was removed: %v", excluded, err)
		}
	}
}

// A folder ignored by its enclosing repository uses the plain folder road and
// leaves the repository's refs alone even when the run writes files.
func TestProgramFolderInsideIgnoredDirectoryStaysPlain(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	repo := safetyRepo(t)
	writeFile(t, filepath.Join(repo, ".gitignore"), "build-out/\n")
	mustGit(t, repo, "add", ".gitignore")
	mustGit(t, repo, "commit", "-q", "-m", "ignore output")
	before := strings.TrimSpace(gitOut(t, repo, "rev-parse", "main"))
	inside := filepath.Join(repo, "build-out")
	if err := os.Mkdir(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	folder := prepareIn(t, testPrograms("fake")[0], inside, "Build here")
	writeFile(t, filepath.Join(inside, "result.txt"), "made\n")
	end := folder.Finish("done")
	if !folder.Plain() || folder.Dir != inside || strings.TrimSpace(gitOut(t, repo, "rev-parse", "main")) != before || currentBranch(repo) != "main" {
		t.Fatalf("ignored folder was treated as repo: %+v, %s", folder, end.Sentence())
	}
	if strings.TrimSpace(gitOut(t, repo, "branch", "--list", "task/*")) != "" {
		t.Fatal("a task branch was cut in the enclosing repo")
	}
	if body, err := os.ReadFile(filepath.Join(inside, "result.txt")); err != nil || string(body) != "made\n" {
		t.Fatalf("plain work missing: %q, %v", body, err)
	}
	if strings.Contains(end.Sentence(), "it changed nothing") || !strings.Contains(end.Sentence(), "nothing was committed") {
		t.Fatalf("ending misstates plain work: %s", end.Sentence())
	}
}

// A moved checkout gets no finishing commit and the ending names the commit
// already on the task branch as well as the uncommitted work left on main.
func TestProgramFolderMovedHeadEndingAccountsForCommittedAndLooseWork(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	repo := safetyRepo(t)
	base := strings.TrimSpace(gitOut(t, repo, "rev-parse", "main"))
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Own branch only")
	writeFile(t, filepath.Join(repo, "task.txt"), "committed on task\n")
	mustGit(t, repo, "add", "task.txt")
	mustGit(t, repo, "commit", "-q", "-m", "task work")
	mustGit(t, repo, "switch", "-q", "main")
	writeFile(t, filepath.Join(repo, "loose.txt"), "uncommitted\n")
	end := folder.Finish("done")
	if currentBranch(repo) != "main" || strings.TrimSpace(gitOut(t, repo, "rev-parse", "main")) != base {
		t.Fatal("the person's branch gained a commit or HEAD moved")
	}
	if status := gitOut(t, repo, "status", "--porcelain"); !strings.Contains(status, "loose.txt") {
		t.Fatalf("run's loose work disappeared: %s", status)
	}
	said := end.Sentence()
	for _, want := range []string{"the branch main", "uncommitted", "holds 1 file", "your branch main was not given a commit by codeaf"} {
		if !strings.Contains(said, want) {
			t.Fatalf("ending missing %q: %s", want, said)
		}
	}
	if strings.Contains(said, "nothing was committed") {
		t.Fatalf("ending denied a real task commit: %s", said)
	}
}

func TestProgramFolderMovedHeadWithCleanCheckoutDoesNotClaimLooseWork(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	repo := safetyRepo(t)
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Look only")
	mustGit(t, repo, "switch", "-q", "main")
	said := folder.Finish("done").Sentence()
	if !strings.Contains(said, "no uncommitted files were left in that checkout") {
		t.Fatalf("clean moved checkout misreported as uncommitted work: %s", said)
	}
}

// A process that vanished after moving HEAD still leaves a truthful account
// of loose files on the person's checkout, without committing them there.
func TestProgramFolderMovedHeadAfterVanishedRunNamesLooseWork(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	repo := safetyRepo(t)
	base := strings.TrimSpace(gitOut(t, repo, "rev-parse", "main"))
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Stopped after switch")
	t.Cleanup(folder.release)
	mustGit(t, repo, "switch", "-q", "main")
	writeFile(t, filepath.Join(repo, "loose.txt"), "still here\n")
	said := folder.settleGone().Sentence()
	if !strings.Contains(said, "the checkout has 1 file uncommitted") || !strings.Contains(said, "your branch main was not given a commit by codeaf") {
		t.Fatalf("vanished run's moved checkout was misstated: %s", said)
	}
	if currentBranch(repo) != "main" || strings.TrimSpace(gitOut(t, repo, "rev-parse", "main")) != base {
		t.Fatal("the person's branch gained a commit or HEAD moved")
	}
}
