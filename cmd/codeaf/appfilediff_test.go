package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// diffRepo is a fresh repository with one committed file, a.txt, and the
// person's own git configuration kept out of it.
func diffRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	gitT(t, dir, "init", "-q", "-b", "main")
	write(t, filepath.Join(dir, "a.txt"), "one\ntwo\nthree\n")
	gitT(t, dir, "add", "a.txt")
	gitT(t, dir, "commit", "-q", "-m", "first")
	return dir
}

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func diffT(t *testing.T, repo, rel, base string) appwire.FileDiff {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), appwire.FileDiffSeconds*time.Second)
	defer cancel()
	return appDiff(ctx, rel, filepath.Join(repo, rel), appCanonical(repo), base)
}

func counts(d appwire.FileDiff) (int, int) {
	if d.Add == nil || d.Del == nil {
		return -1, -1
	}
	return *d.Add, *d.Del
}

// A TRACKED FILE IS COMPARED WITH HEAD, STAGED AND UNSTAGED TOGETHER, and
// nothing in the repository is written by asking.
func TestAppDiffStagedAndUnstagedAgainstHead(t *testing.T) {
	repo := diffRepo(t)
	head := gitT(t, repo, "rev-parse", "HEAD")
	write(t, filepath.Join(repo, "a.txt"), "one\nTWO\nthree\n")
	gitT(t, repo, "add", "a.txt")
	write(t, filepath.Join(repo, "a.txt"), "one\nTWO\nthree\nfour\n")
	index, _ := os.Stat(filepath.Join(repo, ".git", "index"))

	d := diffT(t, repo, "a.txt", "")
	if d.State != appwire.DiffChanged || d.Change != "modified" || d.Base != appwire.DiffBaseHead || d.Commit != head {
		t.Fatalf("diff = %+v", d)
	}
	if a, del := counts(d); a != 2 || del != 1 {
		t.Fatalf("counts +%d −%d, want +2 −1", a, del)
	}
	if !strings.HasPrefix(d.Text, "@@ ") || !strings.Contains(d.Text, "-two\n") || !strings.Contains(d.Text, "+TWO\n") || !strings.Contains(d.Text, "+four\n") || strings.Contains(d.Text, "diff --git") || d.Truncated {
		t.Fatalf("text = %q", d.Text)
	}
	if after, _ := os.Stat(filepath.Join(repo, ".git", "index")); !after.ModTime().Equal(index.ModTime()) {
		t.Fatal("the index was written")
	}
	if same := diffT(t, repo, "a.txt", ""); same.Text != d.Text {
		t.Fatal("asking twice answered differently")
	}
	gitT(t, repo, "commit", "-qam", "second")
	if d := diffT(t, repo, "a.txt", ""); d.State != appwire.DiffUnchanged || d.Text != "" {
		t.Fatalf("committed = %+v", d)
	}
}

// A REQUESTED BASE IS RESOLVED TO ITS HASH; ONE THAT IS GONE FALLS BACK TO
// HEAD AND SAYS SO; ONE THAT COULD BE READ AS AN OPTION NEVER REACHES GIT.
func TestAppDiffRequestedBase(t *testing.T) {
	repo := diffRepo(t)
	first := gitT(t, repo, "rev-parse", "HEAD")
	write(t, filepath.Join(repo, "a.txt"), "one\ntwo\nthree\nfour\n")
	gitT(t, repo, "commit", "-qam", "second")
	head := gitT(t, repo, "rev-parse", "HEAD")

	d := diffT(t, repo, "a.txt", first)
	if d.Base != appwire.DiffBaseRequested || d.Commit != first || d.InHistory == nil || !*d.InHistory || d.State != appwire.DiffChanged || d.Requested != first {
		t.Fatalf("against the first commit = %+v", d)
	}
	gone := diffT(t, repo, "a.txt", "0123456789abcdef0123456789abcdef01234567")
	if gone.Base != appwire.DiffBaseHead || gone.Commit != head || gone.RequestedUnavailable != appwire.DiffRequestedUnknown || gone.State != appwire.DiffUnchanged {
		t.Fatalf("a commit that is gone = %+v", gone)
	}
	// A commit that exists but is not in HEAD's history is still compared.
	gitT(t, repo, "checkout", "-q", "-b", "side", first)
	write(t, filepath.Join(repo, "a.txt"), "side\n")
	gitT(t, repo, "commit", "-qam", "side")
	side := gitT(t, repo, "rev-parse", "HEAD")
	gitT(t, repo, "checkout", "-q", "main")
	if d := diffT(t, repo, "a.txt", side); d.Base != appwire.DiffBaseRequested || d.InHistory == nil || *d.InHistory {
		t.Fatalf("a commit off the branch = %+v", d)
	}
	for _, bad := range []string{"--output=/tmp/x", "-p", "HEAD..main", "a b", "HEAD~1", "x:y", "@{-1}", strings.Repeat("a", 300)} {
		if appwire.ValidDiffBase(bad) {
			t.Errorf("%q passed as a base", bad)
		}
	}
	for _, good := range []string{"", "main", "v1.2", "refs/heads/main", first} {
		if !appwire.ValidDiffBase(good) {
			t.Errorf("%q refused as a base", good)
		}
	}
}

// DELETED, RENAMED, ADDED, UNTRACKED AND IGNORED FILES, AND A REPOSITORY WITH
// NO COMMIT, EACH ANSWER WHAT THEY ARE.
func TestAppDiffFileStates(t *testing.T) {
	repo := diffRepo(t)
	write(t, filepath.Join(repo, "b.txt"), "bee\n")
	write(t, filepath.Join(repo, ".gitignore"), "*.log\n")
	gitT(t, repo, "add", "b.txt", ".gitignore")
	gitT(t, repo, "commit", "-qm", "b")

	if err := os.Remove(filepath.Join(repo, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if d := diffT(t, repo, "a.txt", ""); d.State != appwire.DiffChanged || d.Change != "deleted" || !strings.Contains(d.Text, "-three\n") {
		t.Fatalf("deleted = %+v", d)
	} else if a, del := counts(d); a != 0 || del != 3 {
		t.Fatalf("deleted counts +%d −%d", a, del)
	}

	gitT(t, repo, "mv", "b.txt", "c.txt")
	if d := diffT(t, repo, "c.txt", ""); d.State != appwire.DiffChanged || d.Change != "renamed" || d.OldPath != "b.txt" {
		t.Fatalf("renamed = %+v", d)
	}

	write(t, filepath.Join(repo, "new.txt"), "x\ny")
	if d := diffT(t, repo, "new.txt", ""); d.State != appwire.DiffUntracked || d.Base != appwire.DiffBaseNone || d.Commit != "" || d.Text != "@@ -0,0 +1,2 @@\n+x\n+y\n\\ No newline at end of file\n" {
		t.Fatalf("untracked = %+v %q", d, d.Text)
	}
	gitT(t, repo, "add", "new.txt")
	if d := diffT(t, repo, "new.txt", ""); d.State != appwire.DiffChanged || d.Change != "added" {
		t.Fatalf("staged new = %+v", d)
	}
	write(t, filepath.Join(repo, "out.log"), "noise\n")
	if d := diffT(t, repo, "out.log", ""); d.State != appwire.DiffIgnored {
		t.Fatalf("ignored = %+v", d)
	}
	if d := diffT(t, repo, "never.txt", ""); d.State != appwire.DiffMissing {
		t.Fatalf("missing = %+v", d)
	}
	write(t, filepath.Join(repo, "blob.bin"), "a\x00b")
	gitT(t, repo, "add", "blob.bin")
	if d := diffT(t, repo, "blob.bin", ""); d.State != appwire.DiffChanged || !d.Binary || d.Add != nil || d.Text != "" {
		t.Fatalf("binary = %+v", d)
	}

	empty := t.TempDir()
	gitT(t, empty, "init", "-q")
	write(t, filepath.Join(empty, "x.txt"), "x\n")
	if d := diffT(t, empty, "x.txt", ""); d.State != appwire.DiffNoCommits || d.Text != "" || d.Add != nil {
		t.Fatalf("no commit yet = %+v", d)
	}
}

// OUTSIDE GIT THERE IS NO DIFF AND NO COUNT, ONLY WHERE THE FILE IS; A LINK IS
// DIFFED WHERE IT LEADS, AND SAID TO BE OUTSIDE WHEN IT LEADS OUT.
func TestAppDiffOutsideGitAndLinks(t *testing.T) {
	plain := t.TempDir()
	write(t, filepath.Join(plain, "notes.go"), "scratch\n")
	d := diffT(t, plain, "notes.go", "")
	if d.State != appwire.DiffNotGit || d.Add != nil || d.Text != "" {
		t.Fatalf("outside git = %+v", d)
	}
	if want, _ := filepath.EvalSymlinks(plain); d.Folder != want {
		t.Fatalf("folder %q, want %q", d.Folder, want)
	}

	repo := diffRepo(t)
	write(t, filepath.Join(repo, "a.txt"), "changed\n")
	work := t.TempDir()
	if err := os.Symlink(filepath.Join(repo, "a.txt"), filepath.Join(work, "link.txt")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*appwire.FileDiffSeconds)
	defer cancel()
	l := appDiff(ctx, "link.txt", filepath.Join(work, "link.txt"), appCanonical(work), "")
	if !l.Outside || l.State != appwire.DiffChanged || !strings.Contains(l.Text, "+changed\n") {
		t.Fatalf("a link out = %+v", l)
	}
}

// NOTHING IN THE REPOSITORY'S CONFIGURATION RUNS A PROGRAM: an external diff
// and a textconv are ignored, and a filtered file is refused, not diffed.
func TestAppDiffRunsNoRepositoryPrograms(t *testing.T) {
	repo := diffRepo(t)
	mark := filepath.Join(t.TempDir(), "ran")
	gitT(t, repo, "config", "diff.external", "touch "+mark)
	gitT(t, repo, "config", "diff.conv.textconv", "touch "+mark+";cat")
	gitT(t, repo, "config", "filter.evil.clean", "touch "+mark+";cat")
	gitT(t, repo, "config", "core.fsmonitor", "touch "+mark+";true")
	write(t, filepath.Join(repo, ".gitattributes"), "a.txt diff=conv\n*.f filter=evil\n")
	write(t, filepath.Join(repo, "x.f"), "plain\n")
	gitT(t, repo, "-c", "filter.evil.clean=cat", "-c", "core.fsmonitor=false", "add", ".gitattributes", "x.f")
	gitT(t, repo, "-c", "filter.evil.clean=cat", "-c", "core.fsmonitor=false", "commit", "-qm", "attrs")
	write(t, filepath.Join(repo, "a.txt"), "one\n")
	write(t, filepath.Join(repo, "x.f"), "changed\n")

	if d := diffT(t, repo, "a.txt", ""); d.State != appwire.DiffChanged || !strings.Contains(d.Text, "-two\n") {
		t.Fatalf("textconv file = %+v", d)
	}
	if d := diffT(t, repo, "x.f", ""); d.State != appwire.DiffUnavailable || d.Reason != appwire.DiffReasonFilter || d.Detail != "evil" {
		t.Fatalf("filtered file = %+v", d)
	}
	if _, err := os.Stat(mark); err == nil {
		t.Fatal("a program from the repository's configuration ran")
	}
}

// A DIFF IS CUT AT FileDiffLines AND SAYS SO; the git behind it is killed as
// soon as it has written more than can be shown.
func TestAppDiffBounds(t *testing.T) {
	repo := diffRepo(t)
	var big strings.Builder
	for i := range 3 * appwire.FileDiffLines {
		big.WriteString(strings.Repeat("x", 8) + string(rune('a'+i%26)) + "\n")
	}
	write(t, filepath.Join(repo, "a.txt"), big.String())
	d := diffT(t, repo, "a.txt", "")
	if !d.Truncated || d.Lines != appwire.FileDiffLines || strings.Count(d.Text, "\n") != appwire.FileDiffLines || len(d.Text) > appwire.FileDiffMax {
		t.Fatalf("lines %d truncated %v bytes %d", d.Lines, d.Truncated, len(d.Text))
	}
	if a, _ := counts(d); a != 3*appwire.FileDiffLines {
		t.Fatalf("counts are the whole diff's, got +%d", a)
	}

	killed := &appCap{limit: 10, kill: func() {}}
	_, _ = killed.Write([]byte("0123456789abc"))
	if !killed.over || killed.String() != "0123456789" {
		t.Fatalf("cap kept %q", killed.String())
	}
	text, overflow, fail := appGit{ctx: context.Background()}.capped(repo, 64, 0, "log", "-p")
	if fail != nil || !overflow || len(text) != 64 {
		t.Fatalf("capped = %d %v %+v", len(text), overflow, fail)
	}
	done, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, fail := (appGit{ctx: done}).capped(repo, 64, 0, "status"); fail == nil || !fail.timedOut {
		t.Fatalf("an expired deadline = %+v", fail)
	}
}

// AT MOST FileDiffJobs DIFFS RUN AT ONCE; ONE MORE IS BUSY, NOT QUEUED. A bad
// base is refused before anything runs.
func TestAppDiffFileThroughTheBackend(t *testing.T) {
	backend, _, agent := workBackend(t, &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}})
	write(t, filepath.Join(agent.Workspace, "n.txt"), "n\n")
	d, err := backend.DiffFile(agent.ID, "n.txt", "")
	if err != nil || d.Path != "n.txt" || d.Outside {
		t.Fatalf("diff = %+v, %v", d, err)
	}
	if _, err := backend.DiffFile(agent.ID, "n.txt", "--output=x"); err == nil || !strings.Contains(err.Error(), "base") {
		t.Fatalf("an option as a base = %v", err)
	}
	for range appwire.FileDiffJobs {
		appDiffSlots <- struct{}{}
	}
	_, err = backend.DiffFile(agent.ID, "n.txt", "")
	for range appwire.FileDiffJobs {
		<-appDiffSlots
	}
	if err == nil || !strings.Contains(err.Error(), "still being read") {
		t.Fatalf("one diff too many = %v", err)
	}
}
