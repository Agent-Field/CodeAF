package praf

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestReadBrief(t *testing.T) {
	origin := func() (string, string, bool) { return "acme", "widgets", true }
	for _, tc := range []struct {
		brief string
		want  Request
	}{
		{"https://github.com/o/r/pull/7 focus on retries", Request{Target: Target{"o", "r", 7}, Focus: "focus on retries"}},
		{"Review https://github.com/o/r/pull/7.", Request{Target: Target{"o", "r", 7}, Focus: "Review"}},
		{"o/r.git#12 the cache", Request{Target: Target{"o", "r", 12}, Focus: "the cache"}},
		{"#5 auth", Request{Target: Target{"acme", "widgets", 5}, Focus: "auth"}},
		{"current branch", Request{}},
		{"", Request{}},
		{"look hard at the retries", Request{Focus: "look hard at the retries"}},
		{"post /records/pr-report.json", Request{Post: "/records/pr-report.json"}},
		{"Post `/a b/pr-report.json`", Request{Post: "/a b/pr-report.json"}},
	} {
		if got := ReadBrief(tc.brief, origin); got != tc.want {
			t.Errorf("ReadBrief(%q) = %+v, want %+v", tc.brief, got, tc.want)
		}
	}
	if got := ReadBrief("#5", nil); got.Target.Number != 0 {
		t.Errorf("a bare #N with no origin = %+v", got)
	}
}

func TestParseGitHubRemote(t *testing.T) {
	for _, remote := range []string{"https://github.com/o/r.git", "git@github.com:o/r.git", "ssh://git@github.com/o/r", "https://github.com/o/r/"} {
		if owner, repo, ok := parseGitHubRemote(remote); !ok || owner != "o" || repo != "r" {
			t.Errorf("parseGitHubRemote(%q) = %q %q %v", remote, owner, repo, ok)
		}
	}
	if _, _, ok := parseGitHubRemote("https://gitlab.com/o/r.git"); ok {
		t.Error("a GitLab remote read as GitHub")
	}
}

// TestBranchPullRequestAsksGitHubForTheBranch: with no gh, the branch's
// upstream remote names the head, and GitHub is asked for its open pull
// request into origin's repository.
func TestBranchPullRequestAsksGitHubForTheBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	was := ghLookPath
	ghLookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(func() { ghLookPath = was })
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("commit", "-q", "--allow-empty", "-m", "first")
	run("remote", "add", "origin", "https://github.com/acme/widgets.git")
	run("remote", "add", "fork", "git@github.com:me/widgets.git")
	run("checkout", "-q", "-b", "fix-it")
	run("config", "branch.fix-it.remote", "fork")
	run("config", "branch.fix-it.merge", "refs/heads/fix-it")
	run("update-ref", "refs/remotes/fork/fix-it", "HEAD")

	var asked string
	got, err := branchPullRequest(context.Background(), dir, func(_ context.Context, owner, repo, head string) (int, error) {
		asked = owner + "/" + repo + " " + head
		return 31, nil
	})
	if err != nil || got != (Target{"acme", "widgets", 31}) {
		t.Fatalf("branchPullRequest = %+v, %v", got, err)
	}
	if asked != "acme/widgets me:fix-it" {
		t.Errorf("asked GitHub for %q", asked)
	}
	if _, err := branchPullRequest(context.Background(), dir, func(context.Context, string, string, string) (int, error) { return 0, nil }); err == nil {
		t.Error("a branch with no pull request was reviewed")
	}
	if _, err := branchPullRequest(context.Background(), filepath.Join(dir, "nowhere"), nil); err == nil {
		t.Error("a folder that is no checkout had a current branch")
	}
}
