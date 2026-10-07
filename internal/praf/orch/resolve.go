package orch

// resolve.go ports the repo-resolution / PR-branch-checkout code that lives in
// src/pr_af/app.py (_resolve_repo, _checkout_pr_branch, _extract_pr_number) plus
// the orchestrator's _compute_repo_diff. Python's github/client.py::clone_repo
// was deliberately NOT ported into internal/github, so the clone + checkout
// semantics live here and shell out to git directly.
//
// INSIDE CODEAF IT READS NO ENVIRONMENT OF ITS OWN. pr-af read its workspace
// folder, its GitHub token, five git timeouts and a fallback repository from
// PR_AF_* variables; codeaf hands the folder and the token in ([Access]), the
// timeouts are the shipped defaults, and a review with nothing to clone is
// refused rather than run on the working directory.
//
// AND THE TOKEN NEVER REACHES THE DISK. pr-af wrote GH_TOKEN into the clone's
// remote URL, which git keeps in the clone's .git/config; here it rides one
// git invocation's environment as an extra HTTP header for github.com
// (GIT_CONFIG_COUNT), which nothing persists.
//
// The verbatim error strings (design §B.4) are reproduced for the fetch/checkout
// failures so callers (and tests) see byte-identical messages.

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Git timeouts: pr-af's shipped defaults (src/pr_af/gitconfig.py). Variables
// only so a test can shorten one.
var (
	cloneTimeout    = 600 * time.Second // large repos need time
	fetchAllTimeout = 600 * time.Second // reused workspace refresh
	prFetchTimeout  = 600 * time.Second // fetch of the PR head
	checkoutTimeout = 600 * time.Second
	diffTimeout     = 120 * time.Second
)

// Access is what a review needs to reach a repository: the folder its
// checkouts go in, and the GitHub token for a private one (empty for none).
type Access struct {
	Workdir string
	Token   string
}

// gitEnv reproduces app.py's git_env — the process environment plus
// GIT_TERMINAL_PROMPT=0 and GIT_ASKPASS=echo, so a missing credential fails
// fast instead of blocking on a prompt — with LFS smudging off (the review
// reads code, not large files) and, when there is a token, an Authorization
// header for github.com carried in this one process's git config.
func gitEnv(token string) []string {
	env := os.Environ()
	env = setEnvValue(env, "GIT_TERMINAL_PROMPT", "0")
	env = setEnvValue(env, "GIT_ASKPASS", "echo")
	env = setEnvValue(env, "GIT_LFS_SKIP_SMUDGE", "1")
	if token = strings.TrimSpace(token); token != "" {
		index := 0
		for _, entry := range env {
			if count, ok := strings.CutPrefix(entry, "GIT_CONFIG_COUNT="); ok {
				if n, err := strconv.Atoi(count); err == nil && n > 0 {
					index = n
				}
			}
		}
		basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
		env = setEnvValue(env, "GIT_CONFIG_COUNT", strconv.Itoa(index+1))
		env = setEnvValue(env, fmt.Sprintf("GIT_CONFIG_KEY_%d", index), "http.https://github.com/.extraheader")
		env = setEnvValue(env, fmt.Sprintf("GIT_CONFIG_VALUE_%d", index), "AUTHORIZATION: basic "+basic)
	}
	return env
}

func setEnvValue(env []string, name, value string) []string {
	prefix := name + "="
	for index, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			env[index] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

// runGit executes a git command with a hard timeout and returns stdout, stderr,
// and the error. dir is passed via -C by the caller (kept out of here so the
// clone command — which has no -C — works too).
func runGit(parent context.Context, token string, timeout time.Duration, args ...string) (stdout, stderr string, err error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	configureGitProcess(cmd)
	cmd.Env = gitEnv(token)
	cmd.WaitDelay = time.Second
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	if err != nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	return outBuf.String(), errBuf.String(), err
}

func removeTimedOutGitLocks(targetDir string) {
	gitDir := filepath.Join(targetDir, ".git")
	for _, lockName := range []string{"index.lock", "shallow.lock"} {
		_ = os.Remove(filepath.Join(gitDir, lockName))
	}
}

// ExtractPRNumber ports app.py::_extract_pr_number: the integer following
// "/pull/" in a github.com URL, or (0, false) when absent/unparsable.
func ExtractPRNumber(prURL string) (int, bool) {
	if !strings.Contains(prURL, "github.com") || !strings.Contains(prURL, "/pull/") {
		return 0, false
	}
	tail := prURL[strings.LastIndex(prURL, "/pull/")+len("/pull/"):]
	// split on "/" and strip, matching .split("/")[0].strip("/").
	seg := tail
	if i := strings.Index(seg, "/"); i >= 0 {
		seg = seg[:i]
	}
	seg = strings.Trim(seg, "/")
	n, convErr := strconv.Atoi(seg)
	if convErr != nil {
		return 0, false
	}
	return n, true
}

// checkoutPRBranch ports app.py::_checkout_pr_branch. It fetches the PR head into
// FETCH_HEAD (which always succeeds, even when the workspace is reused and
// pr-review is the current branch) and then checkout -B (re)points pr-review at
// it — the fix for the silent "reused workspace reviews the first PR forever"
// bug. The two failure strings are the §B.4 verbatim contracts.
func checkoutPRBranch(ctx context.Context, token, targetDir string, prNumber int) error {
	_, stderr, err := runGit(ctx, token, prFetchTimeout,
		"-C", targetDir, "fetch", "--depth", "1", "origin", fmt.Sprintf("pull/%d/head", prNumber))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			removeTimedOutGitLocks(targetDir)
			return err
		}
		return fmt.Errorf("git fetch of PR #%d head failed: %s", prNumber, strings.TrimSpace(stderr))
	}
	_, stderr, err = runGit(ctx, "", checkoutTimeout,
		"-C", targetDir, "checkout", "-B", "pr-review", "FETCH_HEAD")
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			removeTimedOutGitLocks(targetDir)
			return err
		}
		return fmt.Errorf("git checkout of PR #%d (pr-review) failed: %s", prNumber, strings.TrimSpace(stderr))
	}
	return nil
}

// ResolveRepo ports app.py::_resolve_repo. It resolves repoPath / prURL to a
// local directory: an existing dir is returned as-is (resolved absolute), and
// an http(s)/git@ URL is (shallow) cloned into access.Workdir, with the PR
// branch checked out when a PR number is known. Anything else is refused:
// pr-af fell back to $PR_AF_REPO_PATH or its working directory.
//
// The workspace is keyed by repo AND PR number when one is known
// (<repoName>-pr<N>): the shared per-repo dir re-pointed its pr-review branch
// on every review, so two concurrent reviews of different PRs of the same repo
// silently reviewed the wrong checkout. Plain <repoName> is kept when no PR
// number is known (repo_path / diff_text flows), preserving the old layout.
//
// Empty strings stand in for Python's None. Errors mirror Python's ValueError
// ("git clone failed: …", plus the checkout strings via checkoutPRBranch).
func ResolveRepo(ctx context.Context, access Access, repoPath, prURL string) (string, error) {
	workdir := access.Workdir
	if workdir == "" {
		return "", errors.New("git clone failed: no folder to clone into")
	}
	target := repoPath
	prNumber := 0
	hasPR := false

	if target == "" && strings.Contains(prURL, "github.com") && strings.Contains(prURL, "/pull/") {
		// parts = pr_url.split("github.com/")[-1].split("/pull/")[0].strip("/")
		afterHost := prURL[strings.LastIndex(prURL, "github.com/")+len("github.com/"):]
		parts := afterHost
		if i := strings.Index(parts, "/pull/"); i >= 0 {
			parts = parts[:i]
		}
		parts = strings.Trim(parts, "/")
		if strings.Count(parts, "/") == 1 {
			target = fmt.Sprintf("https://github.com/%s.git", parts)
		}
		prNumber, hasPR = ExtractPRNumber(prURL)
	}

	// Existing directory → return resolved absolute path.
	if target != "" && isDir(target) {
		abs, err := filepath.Abs(target)
		if err != nil {
			return target, nil
		}
		return abs, nil
	}

	// Remote URL → clone (or refresh) into the workspace.
	if target != "" && (strings.HasPrefix(target, "https://") ||
		strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "git@")) {
		repoName := strings.TrimSuffix(lastSegment(strings.TrimRight(target, "/")), ".git")
		workspaceName := repoName
		if hasPR && prNumber != 0 {
			// Per-PR workspace: isolates concurrent reviews of different PRs
			// of the same repo (see the doc comment above).
			workspaceName = fmt.Sprintf("%s-pr%d", repoName, prNumber)
		}
		targetDir := filepath.Join(workdir, workspaceName)
		if err := os.MkdirAll(workdir, 0o755); err != nil {
			return "", fmt.Errorf("git clone failed: %s", strings.TrimSpace(err.Error()))
		}

		cloneURL := target

		if isDir(targetDir) && isDir(filepath.Join(targetDir, ".git")) {
			// Reused workspace: refresh all refs. Non-timeout errors are swallowed,
			// as Python does; timeouts are propagated after lock cleanup.
			_, _, err := runGit(ctx, access.Token, fetchAllTimeout,
				"-C", targetDir, "fetch", "--all")
			if errors.Is(err, context.DeadlineExceeded) {
				removeTimedOutGitLocks(targetDir)
				return "", err
			}
		} else {
			cloneCmd := []string{"clone", "--depth", "1", "--no-tags", cloneURL, targetDir}
			if hasPR && prNumber != 0 {
				// Skip default-branch checkout; the PR ref is fetched next.
				cloneCmd = []string{"clone", "--depth", "1", "--no-tags", "--no-checkout", cloneURL, targetDir}
			}
			_, stderr, err := runGit(ctx, access.Token, cloneTimeout, cloneCmd...)
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					removeTimedOutGitLocks(targetDir)
					return "", err
				}
				return "", fmt.Errorf("git clone failed: %s", strings.TrimSpace(stderr))
			}
		}

		if hasPR && prNumber != 0 {
			if err := checkoutPRBranch(ctx, access.Token, targetDir, prNumber); err != nil {
				return "", err
			}
		}
		return targetDir, nil
	}

	return "", badInput("there is no repository to review: name a GitHub pull request")
}

// computeRepoDiff ports orchestrator._compute_repo_diff: a `git diff` over a
// revision range derived from base/head refs. A non-zero exit is a ValueError in
// Python → wrapped in ErrBadInput here (the review()-caught 400 class).
func computeRepoDiff(ctx context.Context, repoPath, baseRef, headRef string) (string, error) {
	if headRef != "" && baseRef == "" {
		baseRef = "HEAD"
	}
	var revision string
	switch {
	case baseRef != "" && headRef != "":
		revision = fmt.Sprintf("%s...%s", baseRef, headRef)
	case baseRef != "":
		revision = fmt.Sprintf("%s...HEAD", baseRef)
	default:
		revision = "HEAD~1...HEAD"
	}
	stdout, stderr, err := runGit(ctx, "", diffTimeout,
		"-C", repoPath, "diff", "--no-color", revision)
	if err != nil {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = "Failed to compute git diff"
		}
		return "", badInput(msg)
	}
	return stdout, nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func lastSegment(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}
