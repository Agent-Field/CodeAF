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
// program changes the ignore rule, while the changed rule is ordinary work. The
// secret is linked into the copy (programcopy.go), and neither the link nor
// the file it points at is ever committed or removed.
func TestProgramFolderNeverCommitsPathsIgnoredAtStartOrRunCaches(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	repo := safetyRepo(t)
	writeFile(t, filepath.Join(repo, ".gitignore"), ".env\n")
	mustGit(t, repo, "add", ".gitignore")
	mustGit(t, repo, "commit", "-q", "-m", "ignore")
	writeFile(t, filepath.Join(repo, ".env"), "SECRET=private\n")
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Change ignore rules")
	ignoredRecord, err := os.ReadFile(folder.IgnoredFile())
	if err != nil || !strings.Contains(string(ignoredRecord), ".env") {
		t.Fatalf("the child cannot read its start-time ignore record: %q, %v", ignoredRecord, err)
	}
	writeFile(t, filepath.Join(folder.Dir, ".gitignore"), "# changed by run\n")
	writeFile(t, filepath.Join(folder.Dir, "__pycache__", "module.pyc"), "bytecode")
	writeFile(t, filepath.Join(folder.Dir, ".pytest_cache", "state"), "cache")
	writeFile(t, filepath.Join(folder.Dir, "made.txt"), "work\n")
	folder.Finish("done")
	if frozen, err := os.ReadFile(folder.IgnoredFile()); err != nil || string(frozen) != string(ignoredRecord) {
		t.Fatalf("the repository's frozen ignore list changed during the run: %q, %v", frozen, err)
	}
	paths := gitOut(t, repo, "ls-tree", "-r", "--name-only", folder.Branch)
	for _, want := range []string{".gitignore", "made.txt"} {
		if !strings.Contains(paths, want) {
			t.Fatalf("%s missing from commit: %s", want, paths)
		}
	}
	for _, excluded := range []string{".env", "__pycache__/module.pyc", ".pytest_cache/state"} {
		if strings.Contains(paths, excluded) {
			t.Fatalf("%s entered commit: %s", excluded, paths)
		}
	}
	if body := readFile(t, filepath.Join(repo, ".env")); body != "SECRET=private\n" {
		t.Fatalf("the person's secret was touched: %q", body)
	}
}

// dependencyRepo is a repository whose ignored dependencies, environment
// files, build output and Python environment are all on disk, the way a
// person's working project is.
func dependencyRepo(t *testing.T) string {
	t.Helper()
	repo := safetyRepo(t)
	writeFile(t, filepath.Join(repo, ".gitignore"), "node_modules/\nbin/\n.env\n.env.*\n.venv/\n")
	mustGit(t, repo, "add", ".gitignore")
	mustGit(t, repo, "commit", "-q", "-m", "ignore")
	writeFile(t, filepath.Join(repo, "node_modules", "left-pad", "index.js"), "module.exports = 1\n")
	writeFile(t, filepath.Join(repo, "bin", "codeaf"), "the person's binary\n")
	writeFile(t, filepath.Join(repo, ".env"), "A=1\n")
	writeFile(t, filepath.Join(repo, ".env.local"), "B=2\n")
	writeFile(t, filepath.Join(repo, ".venv", "pyvenv.cfg"), "home = /usr/bin\n")
	return repo
}

// A COPY WITH THE NETWORK ON HAS DEPENDENCIES OF ITS OWN, NOT LINKS INTO THE
// PERSON'S. Its node_modules is a copy-on-write clone where the disk can make
// one and absent where it cannot, for the program to install; its `.env` files
// are copies; a Python environment is never carried, because an editable
// install in it points at the person's own source; a build folder is never
// carried at all — a run's `make build` once overwrote the binary its person
// was running. So what the program installs or edits there never reaches the
// person's folder, and none of it is committed.
func TestACopyWithTheNetworkOnHasDependenciesOfItsOwn(t *testing.T) {
	t.Setenv(programNetworkEnv, "")
	repo := dependencyRepo(t)
	probe := t.TempDir()
	writeFile(t, filepath.Join(probe, "from", "f"), "x")
	clones := cloneTree(filepath.Join(probe, "from"), filepath.Join(probe, "to")) == nil
	t.Logf("this disk clones folders: %v", clones)
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Use the dependencies")
	for _, name := range []string{".env", ".env.local"} {
		info, err := os.Lstat(filepath.Join(folder.Dir, name))
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("%s is not a file of the copy's own: %v %v", name, info, err)
		}
	}
	modules := filepath.Join(folder.Dir, "node_modules")
	if info, err := os.Lstat(modules); clones && (err != nil || !info.IsDir()) {
		t.Fatalf("node_modules was not cloned where the disk clones: %v %v", info, err)
	} else if !clones && !os.IsNotExist(err) {
		t.Fatalf("node_modules is in a copy on a disk that cannot clone: %v", err)
	}
	for _, gone := range []string{".venv", "bin"} {
		if _, err := os.Lstat(filepath.Join(folder.Dir, gone)); !os.IsNotExist(err) {
			t.Fatalf("%s was carried into the copy: %v", gone, err)
		}
	}
	if status := strings.TrimSpace(gitOut(t, folder.Dir, "status", "--porcelain")); status != "" {
		t.Fatalf("the copy's tree is not clean with its dependencies in it:\n%s", status)
	}
	if clones {
		writeFile(t, filepath.Join(modules, "left-pad", "index.js"), "the program's install\n")
	}
	writeFile(t, filepath.Join(folder.Dir, ".env"), "A=the program's\n")
	writeFile(t, filepath.Join(folder.Dir, "made.txt"), "work\n")
	end := folder.Finish("done")
	if paths := gitOut(t, repo, "ls-tree", "-r", "--name-only", folder.Branch); strings.Contains(paths, "node_modules") || strings.Contains(paths, ".env") || !end.Kept {
		t.Fatalf("a dependency entered the commit, or the work did not: %s", paths)
	}
	if body := readFile(t, filepath.Join(repo, "node_modules", "left-pad", "index.js")); body != "module.exports = 1\n" {
		t.Fatalf("the program's install reached the person's node_modules: %q", body)
	}
	if body := readFile(t, filepath.Join(repo, ".env")); body != "A=1\n" {
		t.Fatalf("the program's edit reached the person's .env: %q", body)
	}
}

// A COPY WITH THE NETWORK OFF LINKS WHAT A FRESH CHECKOUT LACKS, because
// nothing can be installed: the ignored node_modules, .venv and `.env` files
// are links, a build folder is still not carried, the copy's tree is clean with
// them in it, and the links go before anything is committed. A project names
// its own list in `.codeaf/config.json`.
func TestACopyWithTheNetworkOffLinksTheIgnoredDependencies(t *testing.T) {
	t.Setenv(programNetworkEnv, "off")
	repo := dependencyRepo(t)
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Use the dependencies")
	for _, name := range []string{"node_modules", ".env", ".env.local", ".venv"} {
		if target, err := os.Readlink(filepath.Join(folder.Dir, name)); err != nil || target != filepath.Join(repo, name) {
			t.Fatalf("%s is not linked into the copy: %q, %v", name, target, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(folder.Dir, "bin")); !os.IsNotExist(err) {
		t.Fatalf("a build folder was linked into the copy: %v", err)
	}
	if status := strings.TrimSpace(gitOut(t, folder.Dir, "status", "--porcelain")); status != "" {
		t.Fatalf("the copy's tree is not clean with its links in it:\n%s", status)
	}
	writeFile(t, filepath.Join(folder.Dir, "made.txt"), "work\n")
	end := folder.Finish("done")
	if paths := gitOut(t, repo, "ls-tree", "-r", "--name-only", folder.Branch); strings.Contains(paths, "node_modules") || strings.Contains(paths, ".env") || !end.Kept {
		t.Fatalf("a link entered the commit, or the work did not: %s", paths)
	}

	writeFile(t, filepath.Join(repo, ".codeaf", "config.json"), `{"program.links": "bin"}`)
	own := prepareIn(t, testPrograms("fake")[0], repo, "Use the project's own list")
	defer own.Finish("")
	if _, err := os.Readlink(filepath.Join(own.Dir, "bin")); err != nil {
		t.Fatalf("the project's own list was not linked: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(own.Dir, "node_modules")); !os.IsNotExist(err) {
		t.Fatalf("a folder the project's list does not name was linked: %v", err)
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
	if ignored, err := os.ReadFile(folder.IgnoredFile()); err != nil || len(ignored) != 0 {
		t.Fatalf("the ignored subfolder needs a readable empty safety list: %q, %v", ignored, err)
	}
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

// A copy the program moved off its branch gets no finishing commit: its
// branch keeps what it committed there, what is loose is kept as a patch, and
// the person's own branch and checkout are never touched.
func TestProgramFolderMovedHeadEndingAccountsForCommittedAndLooseWork(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	repo := safetyRepo(t)
	base := strings.TrimSpace(gitOut(t, repo, "rev-parse", "main"))
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Own branch only")
	writeFile(t, filepath.Join(folder.Dir, "task.txt"), "committed on task\n")
	mustGit(t, folder.Dir, "add", "task.txt")
	mustGit(t, folder.Dir, "commit", "-q", "-m", "task work")
	mustGit(t, folder.Dir, "switch", "-q", "-c", "elsewhere")
	writeFile(t, filepath.Join(folder.Dir, "loose.txt"), "uncommitted\n")
	end := folder.Finish("done")
	if currentBranch(repo) != "main" || strings.TrimSpace(gitOut(t, repo, "rev-parse", "main")) != base {
		t.Fatal("the person's branch gained a commit or HEAD moved")
	}
	if end.Patch == "" || !strings.Contains(readFile(t, end.Patch), "loose.txt") {
		t.Fatalf("the run's loose work was not kept: %+v", end)
	}
	said := end.Sentence()
	for _, want := range []string{"left its copy on the branch elsewhere", "holds 1 file", "kept as a patch at " + end.Patch} {
		if !strings.Contains(said, want) {
			t.Fatalf("ending missing %q: %s", want, said)
		}
	}
	if paths := gitOut(t, repo, "ls-tree", "-r", "--name-only", folder.Branch); strings.Contains(paths, "loose.txt") {
		t.Fatalf("loose work was committed on the task branch: %s", paths)
	}
}

func TestProgramFolderMovedHeadWithCleanCheckoutDoesNotClaimLooseWork(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	repo := safetyRepo(t)
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Look only")
	mustGit(t, folder.Dir, "switch", "-q", "-c", "elsewhere")
	end := folder.Finish("done")
	if end.Patch != "" || strings.Contains(end.Sentence(), "patch") {
		t.Fatalf("clean moved copy misreported as uncommitted work: %s", end.Sentence())
	}
}

// A process that vanished after moving its copy's HEAD is finished the same
// way by the next codeaf: loose work kept as a patch, the copy removed.
func TestProgramFolderMovedHeadAfterVanishedRunNamesLooseWork(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	repo := safetyRepo(t)
	base := strings.TrimSpace(gitOut(t, repo, "rev-parse", "main"))
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Stopped after switch")
	t.Cleanup(folder.release)
	mustGit(t, folder.Dir, "switch", "-q", "-c", "elsewhere")
	writeFile(t, filepath.Join(folder.Dir, "loose.txt"), "still here\n")
	end := folder.settleGone()
	if !end.Moved || end.Patch == "" || !strings.Contains(end.Sentence(), "kept as a patch") {
		t.Fatalf("vanished run's moved copy was misstated: %s", end.Sentence())
	}
	if _, err := os.Stat(folder.Dir); !os.IsNotExist(err) {
		t.Fatalf("the vanished run's copy is still there: %v", err)
	}
	if currentBranch(repo) != "main" || strings.TrimSpace(gitOut(t, repo, "rev-parse", "main")) != base {
		t.Fatal("the person's branch gained a commit or HEAD moved")
	}
}
