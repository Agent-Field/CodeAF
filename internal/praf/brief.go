package praf

// The brief: which pull request to review and what to weigh, or which saved
// review to post. `/review` is handed its brief words alone (Delegate.Words), so
// these are the person's own words or the model's few words for them.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Target is the pull request a brief names.
type Target struct {
	Owner  string
	Repo   string
	Number int
}

// URL is the canonical pull request URL, or "" for the zero Target.
func (t Target) URL() string {
	if t.Number == 0 {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/%s/pull/%d", t.Owner, t.Repo, t.Number)
}

// String is the short form a person reads: owner/repo#N.
func (t Target) String() string {
	if t.Number == 0 {
		return "the pull request"
	}
	return fmt.Sprintf("%s/%s#%d", t.Owner, t.Repo, t.Number)
}

// errNoPullRequest is a brief that names no pull request.
var errNoPullRequest = errors.New("no pull request")

// The three ways a brief names a pull request, tried in this order: a full
// URL, then owner/repo#N, then a bare #N against the folder's GitHub origin.
// The first match wins; the rest of the brief is the focus.
var (
	prURLPattern = regexp.MustCompile(`(?i)https?://(?:www\.)?github\.com/([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)/pull/(\d+)[^\s)\]>"'` + "`" + `]*`)
	prRefPattern = regexp.MustCompile(`(?:^|[\s(\[])([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)#(\d+)\b`)
	prNumPattern = regexp.MustCompile(`(?:^|[\s(\[])#(\d+)\b`)
)

// Request is one run's brief, read.
type Request struct {
	// Posting is a `post <report>` brief, and Post the saved review it names.
	// THEY ARE TWO FIELDS so a bare `post` is a post that names nothing, which
	// ends asking for the report, never a review of the current branch that
	// spends money nobody asked to spend.
	Posting bool
	Post    string
	// Target is the pull request named outright; zero when the brief leaves it
	// to the folder's current branch.
	Target Target
	// Focus is what the reviewers should weigh: the brief without its pull
	// request, "" for none.
	Focus string
}

// currentBranchWords are the briefs that mean "this branch's pull request" in
// so many words, which a bare `/review` runs (DefaultBrief) and which say nothing
// for the reviewers to weigh.
var currentBranchWords = map[string]bool{
	"current branch": true, "this branch": true, "the current branch": true,
	"this branch's pull request": true, "the current branch's pull request": true,
}

// ReadBrief reads a brief. origin answers the folder's GitHub owner/repo for a
// bare #N; it may be nil, and it is only asked when the brief has nothing
// fuller. A brief that names no pull request is a review of the current
// branch's, with the whole brief as its focus.
func ReadBrief(brief string, origin func() (owner, repo string, ok bool)) Request {
	brief = strings.TrimSpace(brief)
	if rest, ok := cutWord(brief, "post"); ok {
		return Request{Posting: true, Post: strings.Trim(strings.TrimSpace(rest), "`\"'")}
	}
	t, focus, err := parsePullRequest(brief, origin)
	if err != nil {
		if currentBranchWords[strings.ToLower(strings.Trim(brief, ". "))] {
			focus = ""
		}
		return Request{Focus: focus}
	}
	return Request{Target: t, Focus: focus}
}

// cutWord answers the rest of text after its first word when that word is
// word, case aside.
func cutWord(text, word string) (string, bool) {
	first, rest, _ := strings.Cut(text, " ")
	if !strings.EqualFold(first, word) {
		return "", false
	}
	return rest, true
}

func parsePullRequest(brief string, origin func() (owner, repo string, ok bool)) (Target, string, error) {
	if m := prURLPattern.FindStringSubmatchIndex(brief); m != nil {
		t, err := target(brief[m[2]:m[3]], brief[m[4]:m[5]], brief[m[6]:m[7]])
		// Sentence punctuation right after a link is the link's sentence
		// ending, and goes with it.
		return t, focus(brief, m[0], m[1]), err
	}
	if m := prRefPattern.FindStringSubmatchIndex(brief); m != nil {
		t, err := target(brief[m[2]:m[3]], brief[m[4]:m[5]], brief[m[6]:m[7]])
		return t, focus(brief, m[2], m[1]), err
	}
	if m := prNumPattern.FindStringSubmatchIndex(brief); m != nil && origin != nil {
		if owner, repo, ok := origin(); ok {
			t, err := target(owner, repo, brief[m[2]:m[3]])
			// m[2]-1 is the '#', which the match's leading context may precede.
			return t, focus(brief, m[2]-1, m[1]), err
		}
	}
	return Target{}, brief, errNoPullRequest
}

func target(owner, repo, number string) (Target, error) {
	n, err := strconv.Atoi(number)
	if err != nil || n <= 0 {
		return Target{}, errNoPullRequest
	}
	return Target{Owner: owner, Repo: strings.TrimSuffix(repo, ".git"), Number: n}, nil
}

// focus is the brief with the pull request reference cut out, whitespace at
// the seam collapsed, and the whole trimmed.
func focus(brief string, start, end int) string {
	before := strings.TrimRight(brief[:start], " \t")
	after := strings.TrimLeft(brief[end:], " \t")
	joined := before
	if before != "" && after != "" && !strings.HasSuffix(before, "\n") && !strings.HasPrefix(after, "\n") {
		joined += " "
	}
	joined += after
	return strings.TrimSpace(joined)
}

// githubRemotePattern reads owner/repo off a GitHub remote URL in any of its
// spellings: https://github.com/o/r(.git), git@github.com:o/r(.git),
// ssh://git@github.com/o/r(.git).
var githubRemotePattern = regexp.MustCompile(`github\.com[:/]([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(?:\.git)?/?$`)

// gitTimeout bounds every git question asked of the person's folder.
const gitTimeout = 5 * time.Second

// folderGit answers one git question about dir, read-only: the review never
// changes the person's folder.
func folderGit(ctx context.Context, dir string, args ...string) (string, bool) {
	if dir == "" {
		return "", false
	}
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

// workspaceOrigin answers the GitHub owner/repo of dir's `origin` remote.
func workspaceOrigin(ctx context.Context, dir string) (string, string, bool) {
	remote, ok := folderGit(ctx, dir, "config", "--get", "remote.origin.url")
	if !ok {
		return "", "", false
	}
	return parseGitHubRemote(remote)
}

func parseGitHubRemote(remote string) (string, string, bool) {
	m := githubRemotePattern.FindStringSubmatch(remote)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// branchPullRequest is the open pull request of dir's current branch.
//
// `gh pr view` ANSWERS IT WHEN gh IS THERE, because it already knows every way
// a branch reaches a pull request — a fork's remote, a renamed upstream, a
// branch pushed under another name. Without gh, the branch's upstream remote
// names the repository the branch was pushed to, and GitHub is asked for an
// open pull request from that branch into origin's repository.
func branchPullRequest(ctx context.Context, dir string, ask func(ctx context.Context, owner, repo, head string) (int, error)) (Target, error) {
	branch, ok := folderGit(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if !ok || branch == "" {
		return Target{}, errors.New("this folder is not a git checkout, so there is no current branch to review")
	}
	if branch == "HEAD" {
		return Target{}, errors.New("this checkout is not on a branch, so it has no pull request")
	}
	if t, ok := ghPullRequest(ctx, dir); ok {
		return t, nil
	}
	owner, repo, ok := workspaceOrigin(ctx, dir)
	if !ok {
		return Target{}, errors.New("this folder's origin is not on GitHub")
	}
	headOwner, headBranch := owner, branch
	if upstream, ok := folderGit(ctx, dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); ok {
		if remote, name, cut := strings.Cut(upstream, "/"); cut {
			headBranch = name
			if url, ok := folderGit(ctx, dir, "config", "--get", "remote."+remote+".url"); ok {
				if forkOwner, _, ok := parseGitHubRemote(url); ok {
					headOwner = forkOwner
				}
			}
		}
	}
	number, err := ask(ctx, owner, repo, headOwner+":"+headBranch)
	if err != nil {
		return Target{}, err
	}
	if number == 0 {
		return Target{}, fmt.Errorf("the branch %s has no open pull request on %s/%s", branch, owner, repo)
	}
	return Target{Owner: owner, Repo: repo, Number: number}, nil
}

// ghLookPath finds gh; a variable so a test can say it is absent.
var ghLookPath = exec.LookPath

// ghPullRequest asks gh for the current branch's pull request.
func ghPullRequest(ctx context.Context, dir string) (Target, bool) {
	gh, err := ghLookPath("gh")
	if err != nil {
		return Target{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, gh, "pr", "view", "--json", "url,state")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return Target{}, false
	}
	var answer struct {
		URL   string `json:"url"`
		State string `json:"state"`
	}
	if json.Unmarshal(out, &answer) != nil || !strings.EqualFold(answer.State, "open") {
		return Target{}, false
	}
	t, _, err := parsePullRequest(answer.URL, nil)
	return t, err == nil
}
