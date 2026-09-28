package session

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func runCommitFile(t *testing.T, repo, name, message string) string {
	t.Helper()
	writeFile(t, filepath.Join(repo, name), message+"\n")
	mustGit(t, repo, "add", name)
	mustGit(t, repo, "-c", "user.name=Worker", "-c", "user.email=worker@example.test", "commit", "-m", message)
	return strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
}

// Contract 2: an already-signed worker commit keeps its sha and a second pass
// does not move the branch. A partly signed message gains only its missing line.
func TestSignRunCommitsCompletesOnceAndKeepsWorkerIdentity(t *testing.T) {
	repo := newTestRepo(t)
	base := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	first := runCommitFile(t, repo, "one.txt", "one")
	mustGit(t, repo, "-c", "user.name=Worker", "-c", "user.email=worker@example.test", "commit", "--allow-empty", "-m", "two", "-m", "Assisted-by: CodeAF")
	writeFile(t, filepath.Join(repo, "loose.txt"), "still loose\n")
	beforeStatus := gitOut(t, repo, "status", "--porcelain", "-z")
	count, err := signRunCommits(repo, base, gitSignature{})
	if err != nil || count != 2 {
		t.Fatalf("signRunCommits = %d, %v; want two commits", count, err)
	}
	if after := gitOut(t, repo, "status", "--porcelain", "-z"); after != beforeStatus {
		t.Fatalf("rewrite changed the worktree: %q -> %q", beforeStatus, after)
	}
	if got := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD~2")); got != base {
		t.Fatalf("base moved: %s -> %s", base, got)
	}
	if got := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD~1")); got == first {
		t.Fatal("unsigned worker commit kept its old sha")
	}
	for _, rev := range []string{"HEAD", "HEAD~1"} {
		message := gitOut(t, repo, "log", "-1", "--format=%B", rev)
		if strings.Count(message, "Assisted-by: CodeAF") != 1 || strings.Count(message, "Co-Authored-By: CodeAF") != 1 {
			t.Fatalf("%s has duplicate or missing lines: %q", rev, message)
		}
		if who := strings.TrimSpace(gitOut(t, repo, "log", "-1", "--format=%an <%ae>|%cn <%ce>", rev)); who != "Worker <worker@example.test>|Worker <worker@example.test>" {
			t.Fatalf("%s changed worker identity: %s", rev, who)
		}
	}
	head := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	count, err = signRunCommits(repo, base, gitSignature{})
	if err != nil || count != 0 || strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD")) != head {
		t.Fatalf("second signing moved branch: %d, %v", count, err)
	}
}

// Contract 2: a worker that already wrote both lines keeps its original
// object id when no parent needs remapping.
func TestSignRunCommitsKeepsAlreadySignedWorkerSHA(t *testing.T) {
	repo := newTestRepo(t)
	base := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	writeFile(t, filepath.Join(repo, "already.txt"), "already\n")
	mustGit(t, repo, "add", "already.txt")
	mustGit(t, repo, "-c", "user.name=Worker", "-c", "user.email=worker@example.test",
		"commit", "-m", "already\n\nAssisted-by: CodeAF (worker-model)\nCo-Authored-By: CodeAF <267109073+agentfield-bot@users.noreply.github.com>")
	head := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	count, err := signRunCommits(repo, base, gitSignature{})
	if err != nil || count != 0 || strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD")) != head {
		t.Fatalf("already-signed worker commit moved: %d, %v", count, err)
	}
}

// Contract 3: a merge's outside parent, a tagged commit, and a pushed commit
// keep their original objects while private descendants can still be signed.
func TestSignRunCommitsKeepsOtherRefsAndMergeParents(t *testing.T) {
	repo := newTestRepo(t)
	base := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	mustGit(t, repo, "branch", "outside")
	mustGit(t, repo, "checkout", "outside")
	outside := runCommitFile(t, repo, "outside.txt", "outside")
	mustGit(t, repo, "checkout", "work")
	runCommitFile(t, repo, "private.txt", "private")
	mustGit(t, repo, "-c", "user.name=Worker", "-c", "user.email=worker@example.test", "merge", "--no-ff", "outside", "-m", "merge outside")
	count, err := signRunCommits(repo, base, gitSignature{})
	if err != nil || count != 2 {
		t.Fatalf("sign merge range = %d, %v", count, err)
	}
	parents := strings.Fields(gitOut(t, repo, "rev-list", "--parents", "-n", "1", "HEAD"))
	if len(parents) != 3 || parents[2] != outside {
		t.Fatalf("outside merge parent moved: %v, want %s", parents, outside)
	}
	if strings.Contains(gitOut(t, repo, "log", "-1", "--format=%B", outside), "Assisted-by") {
		t.Fatal("outside branch's commit was signed")
	}
	tagged := runCommitFile(t, repo, "tagged.txt", "tagged")
	mustGit(t, repo, "tag", "published", tagged)
	runCommitFile(t, repo, "after-tag.txt", "after tag")
	count, err = signRunCommits(repo, tagged, gitSignature{})
	if err != nil || count != 1 {
		t.Fatalf("sign after tag = %d, %v", count, err)
	}
	if strings.Contains(gitOut(t, repo, "log", "-1", "--format=%B", tagged), "Assisted-by") {
		t.Fatal("tagged object was signed")
	}
	remote := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	mustGit(t, repo, "update-ref", "refs/remotes/origin/published", remote)
	count, err = signRunCommits(repo, tagged, gitSignature{})
	if err != nil || count != 0 || strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD")) != remote {
		t.Fatalf("remote-tracking object moved: %d, %v", count, err)
	}
}

// Contract 3: a fetched commit belongs to its source even when FETCH_HEAD is
// its only pointer; the worker's own commit and merge still receive the lines.
func TestSignRunCommitsKeepsFetchedMergeParent(t *testing.T) {
	repo := newTestRepo(t)
	base := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	outside := filepath.Join(t.TempDir(), "outside")
	mustGit(t, t.TempDir(), "clone", repo, outside)
	mustGit(t, outside, "checkout", "-b", "outside")
	fetched := runCommitFile(t, outside, "fetched.txt", "fetched")
	runCommitFile(t, repo, "worker.txt", "worker")
	refsBefore := gitOut(t, repo, "for-each-ref", "--format=%(refname) %(objectname)")
	mustGit(t, repo, "fetch", outside, "outside")
	if refsAfter := gitOut(t, repo, "for-each-ref", "--format=%(refname) %(objectname)"); refsAfter != refsBefore {
		t.Fatalf("fetch created a ref: %q -> %q", refsBefore, refsAfter)
	}
	mustGit(t, repo, "-c", "user.name=Worker", "-c", "user.email=worker@example.test", "merge", "--no-ff", "FETCH_HEAD", "-m", "merge fetched")
	count, err := signRunCommits(repo, base, gitSignature{})
	if err != nil || count != 2 {
		t.Fatalf("sign fetched merge = %d, %v; want worker and merge only", count, err)
	}
	parents := strings.Fields(gitOut(t, repo, "rev-list", "--parents", "-n", "1", "HEAD"))
	if len(parents) != 3 || parents[2] != fetched {
		t.Fatalf("fetched parent moved: %v, want %s", parents, fetched)
	}
	if message := gitOut(t, repo, "log", "-1", "--format=%B", fetched); strings.Contains(message, "Assisted-by") {
		t.Fatalf("fetched commit was signed: %q", message)
	}
	for _, rev := range []string{"HEAD", "HEAD^1"} {
		if message := gitOut(t, repo, "log", "-1", "--format=%B", rev); strings.Count(message, "Assisted-by: CodeAF") != 1 {
			t.Fatalf("own commit %s not signed once: %q", rev, message)
		}
	}
}

// Contract 3: the name can contain the header's closing-delimiter spelling.
func TestRunCommitIdentityKeepsAngleBracketInName(t *testing.T) {
	got, err := runCommitIdentity("Jane > Doe <j@example.test> 1700000000 +0000", "AUTHOR")
	want := []string{"GIT_AUTHOR_NAME=Jane > Doe", "GIT_AUTHOR_EMAIL=j@example.test", "GIT_AUTHOR_DATE=1700000000 +0000"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("identity = %q, %v; want %q", got, err, want)
	}
}

// Contract 8: signed headers, detached HEAD and an unknown base all leave
// their objects and refs exactly as they were.
func TestSignRunCommitsLeavesProtectedAndUnbasedHistoryUntouched(t *testing.T) {
	repo := newTestRepo(t)
	base := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	ancestor := runCommitFile(t, repo, "one.txt", "one")
	runCommitFile(t, repo, "two.txt", "two")
	raw := gitOut(t, repo, "cat-file", "commit", "HEAD")
	raw = strings.Replace(raw, "\n\n", "\ngpgsig -----BEGIN PGP SIGNATURE-----\n fake\n -----END PGP SIGNATURE-----\n\n", 1)
	file := filepath.Join(t.TempDir(), "commit")
	writeFile(t, file, raw)
	sha := strings.TrimSpace(gitOut(t, repo, "hash-object", "-t", "commit", "-w", file))
	mustGit(t, repo, "update-ref", "refs/heads/work", sha)
	count, err := signRunCommits(repo, base, gitSignature{})
	if err != nil || count != 0 || strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD")) != sha {
		t.Fatalf("protected range moved: %d, %v", count, err)
	}
	if got := gitOut(t, repo, "cat-file", "commit", sha); got != raw {
		t.Fatal("protected object changed")
	}
	if got := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD~1")); got != ancestor {
		t.Fatalf("unsigned ancestor of protected commit moved: %s -> %s", ancestor, got)
	}
	count, err = signRunCommits(repo, "", gitSignature{})
	if err != nil || count != 0 {
		t.Fatalf("empty base = %d, %v", count, err)
	}
	mustGit(t, repo, "checkout", "--detach")
	count, err = signRunCommits(repo, base, gitSignature{})
	if err != nil || count != 0 {
		t.Fatalf("detached HEAD = %d, %v", count, err)
	}
}

// Contracts 1, 4, 5 and 7: the real landing names worker-committed paths,
// obeys CONTRIBUTING for its own commit, and leaves the repository's hook in
// control of the worker's git commit.
func TestLandRunTreeSignsWorkerCommitsAndNamesTheirPaths(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	repo := newTestRepo(t)
	base := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	hooks := strings.TrimSpace(gitOut(t, repo, "rev-parse", "--git-path", "hooks"))
	if !filepath.IsAbs(hooks) {
		hooks = filepath.Join(repo, hooks)
	}
	marker := filepath.Join(t.TempDir(), "hook-ran")
	previousHookPath, _ := git(repo, "config", "--get", "core.hooksPath")
	writeFile(t, filepath.Join(hooks, "pre-commit"), "#!/bin/sh\nprintf ran > '"+marker+"'\n")
	if err := os.Chmod(filepath.Join(hooks, "pre-commit"), 0o755); err != nil {
		t.Fatal(err)
	}
	first := runCommitFile(t, repo, "worker.txt", "worker")
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("worker's pre-commit hook did not run: %v", err)
	}
	branch, paths, refused, err := LandRunTree(repo, base, "the run", "")
	if err != nil || refused != "" || branch != "work" || !reflect.DeepEqual(paths, []string{"worker.txt"}) {
		t.Fatalf("landing = %q, %v, %q, %v", branch, paths, refused, err)
	}
	if got := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD")); got == first {
		t.Fatal("unsigned worker commit stayed on the branch")
	}
	if message := gitOut(t, repo, "log", "-1", "--format=%B"); !strings.Contains(message, "Assisted-by: CodeAF\n"+"Co-Authored-By: CodeAF") {
		t.Fatalf("worker message is unsigned: %q", message)
	}
	if out, _ := git(repo, "config", "--get", "core.hooksPath"); out != previousHookPath {
		t.Fatalf("landing changed core.hooksPath: %q -> %q", previousHookPath, out)
	}
	writeFile(t, filepath.Join(repo, "CONTRIBUTING.md"), "Do not add AI co-author trailers to commits.\n")
	mustGit(t, repo, "add", "CONTRIBUTING.md")
	mustGit(t, repo, "-c", "user.name=Person", "-c", "user.email=person@example.test", "commit", "-m", "set policy")
	base = strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	runCommitFile(t, repo, "unsigned.txt", "unsigned")
	writeFile(t, filepath.Join(repo, "leftover.txt"), "leftover\n")
	_, paths, refused, err = LandRunTree(repo, base, "the run", "")
	if err != nil || refused != "" || len(paths) != 2 {
		t.Fatalf("refusing repository landing = %v, %q, %v", paths, refused, err)
	}
	for _, rev := range []string{"HEAD", "HEAD~1"} {
		if message := gitOut(t, repo, "log", "-1", "--format=%B", rev); strings.Contains(message, "Assisted-by") {
			t.Fatalf("CONTRIBUTING was ignored at %s: %q", rev, message)
		}
	}
}

// Contract 4: a symlinked CONTRIBUTING file is skipped, including one pointed
// at a device. The reader also accepts case-insensitive names in docs.
func TestRepositoryRefusesTrailersReadsOnlyRegularContributingFiles(t *testing.T) {
	repo := newTestRepo(t)
	if err := os.Symlink("/dev/tty", filepath.Join(repo, "CONTRIBUTING.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if repositoryRefusesTrailers(repo) {
		t.Fatal("symlinked CONTRIBUTING was read")
	}
	writeFile(t, filepath.Join(repo, "docs", "contributing.RST"), "No AI trailers.\n")
	if !repositoryRefusesTrailers(repo) {
		t.Fatal("docs/contributing.RST ban was missed")
	}
	writeFile(t, filepath.Join(repo, "docs", "contributing.RST"), strings.Repeat("x", contributingReadLimit+1)+"\nNo AI trailers.")
	if repositoryRefusesTrailers(repo) {
		t.Fatal("CONTRIBUTING ban after read limit was read")
	}
	writeFile(t, filepath.Join(repo, "docs", "contributing.RST"), "No AI trailers.\n"+strings.Repeat("x", contributingReadLimit))
	if !repositoryRefusesTrailers(repo) {
		t.Fatal("CONTRIBUTING ban before read limit was missed")
	}
	writeFile(t, filepath.Join(repo, "docs", "contributing.RST"), "Please run tests.\n")
	writeFile(t, filepath.Join(repo, ".github", "Contributing.TXT"), "No AI trailers.\n")
	if !repositoryRefusesTrailers(repo) {
		t.Fatal(".github/Contributing.TXT ban was missed")
	}
}

// Contracts 1 and 2: message-only signing retains a worker's raw subject and
// body bytes, including a missing final newline and CRLF paragraphs.
func TestSignRunCommitsKeepsRawWorkerMessageAndCommitIdentity(t *testing.T) {
	repo := newTestRepo(t)
	base := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	for i, message := range []string{"no final newline", "subject\r\n\r\nbody with CRLF\r\nsecond line\r\n"} {
		old := runCommitFile(t, repo, "raw"+string(rune('a'+i))+".txt", "temporary")
		raw := gitOut(t, repo, "cat-file", "commit", old)
		headers, _, ok := strings.Cut(raw, "\n\n")
		if !ok {
			t.Fatal("commit has no message boundary")
		}
		object := headers + "\n\n" + message
		file := filepath.Join(t.TempDir(), "raw-commit")
		writeFile(t, file, object)
		sha := strings.TrimSpace(gitOut(t, repo, "hash-object", "-t", "commit", "-w", file))
		mustGit(t, repo, "update-ref", "refs/heads/work", sha, old)
	}
	oldObjects := []string{gitOut(t, repo, "cat-file", "commit", "HEAD~1"), gitOut(t, repo, "cat-file", "commit", "HEAD")}
	count, err := signRunCommits(repo, base, gitSignature{})
	if err != nil || count != 2 {
		t.Fatalf("sign raw messages = %d, %v", count, err)
	}
	for i, rev := range []string{"HEAD~1", "HEAD"} {
		newObject := gitOut(t, repo, "cat-file", "commit", rev)
		oldHeaders, oldMessage, _ := strings.Cut(oldObjects[i], "\n\n")
		newHeaders, newMessage, _ := strings.Cut(newObject, "\n\n")
		// The second commit's parent must move, but its tree and identities do not.
		for _, key := range []string{"tree ", "author ", "committer "} {
			oldLine := "\n" + key + strings.SplitN(strings.SplitN("\n"+oldHeaders, "\n"+key, 2)[1], "\n", 2)[0]
			if !strings.Contains("\n"+newHeaders, oldLine) {
				t.Fatalf("%s changed %s: %q", rev, key, newHeaders)
			}
		}
		if !strings.HasPrefix(newMessage, strings.TrimRight(oldMessage, "\n")+"\n\nAssisted-by: CodeAF\n") {
			t.Fatalf("%s lost original message bytes: %q", rev, newMessage)
		}
	}
}
