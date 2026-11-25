package factory

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/gitpolicy"
)

// ── A FOLDER ON THIS COMPUTER ───────────────────────────────────────────────
//
// In Personal a job can be pointed at a folder on this computer that holds a
// git repository, instead of a repository the factory watches on GitHub
// (owner decision 2, 2026-11-17). IT IS THE SAME JOB ON THE SAME ROAD: its
// repository is the folder's absolute path, the path IS the checkout, and the
// run works in its own worktree off it exactly as it would off a clone. What
// differs is the end. Nothing leaves the machine: the job finishes with a
// local branch and its proof ([FolderFinishedWords]), and "Open a pull
// request" is offered afterwards only when the folder has a GitHub remote and
// GitHub is connected.
//
// IN AN ORGANIZATION THE DOOR IS ABSENT, NOT BROKEN. An organization's server
// cannot reach a person's laptop, so its jobs stay on the repositories it
// turned on; a team factory neither lists the feature nor takes a folder.
//
// A path is told from `owner/name` by being absolute, which no GitHub name
// is, so every repository-shaped door (the switch, the watched list, the
// clone, the poll) passes it by without a special case of its own.

// FeatureLocalFolder is the GET /v1/features word of a factory that takes a
// folder on its own computer as a job's place: only a Personal (solo) one.
const FeatureLocalFolder = "local_folder"

// The sentences a folder door says, spelled once so the refusals, the
// manual and the tests say the same words.
const (
	// FolderNotGitWords: the folder holds no git repository.
	FolderNotGitWords = "isn't a git repository · pick the folder that holds one."
	// FolderGoneWords ends a [NeedsCheckout] for a folder that has moved,
	// been deleted, or no longer holds a repository.
	FolderGoneWords = "the folder is gone or no longer holds a git repository."
	// FolderOnlyPersonalWords refuses a folder on an organization's factory.
	FolderOnlyPersonalWords = "A folder on this computer can only be used in Personal. An organization's jobs run on the repositories it turned on."
	// FolderNoGitHubWords refuses Open a pull request on a folder whose
	// origin is not on GitHub, or with GitHub not connected.
	FolderNoGitHubWords = "A pull request needs the folder to have a GitHub remote and GitHub to be connected."
)

// IsFolderRepo says repo is a folder on this computer: an absolute path.
func IsFolderRepo(repo string) bool {
	repo = strings.TrimSpace(repo)
	return repo != "" && filepath.IsAbs(repo)
}

// FolderOf is the folder a job works in when it was pointed at one, "" for
// a job on a repository.
func FolderOf(it Item) string {
	if !IsFolderRepo(it.Repo) {
		return ""
	}
	return filepath.Clean(strings.TrimSpace(it.Repo))
}

// FolderName is what a person calls the folder: its last part.
func FolderName(dir string) string { return filepath.Base(filepath.Clean(dir)) }

// FolderHoldsRepo says dir is the top of a git repository right now: a
// `.git` directory, or the `.git` file a worktree or submodule leaves.
func FolderHoldsRepo(dir string) bool {
	if !IsFolderRepo(dir) {
		return false
	}
	_, err := os.Stat(filepath.Join(filepath.Clean(dir), ".git"))
	return err == nil
}

// ReadFolder is the folder a person picked, as a job's place: the top of the
// git repository it is in (so picking a subfolder works the whole
// repository), refused in [FolderNotGitWords] when there is none.
func ReadFolder(ctx context.Context, path string) (string, error) {
	path = strings.TrimSpace(path)
	if !IsFolderRepo(path) {
		return "", errors.New("say the folder as a full path")
	}
	path = filepath.Clean(path)
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return "", errors.New(path + " isn't a folder on this computer")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", gitpolicy.WithoutAutoMaintenance("-C", path, "rev-parse", "--show-toplevel")...).Output()
	top := strings.TrimSpace(string(out))
	if err != nil || top == "" {
		return "", errors.New(path + " " + FolderNotGitWords)
	}
	return filepath.Clean(top), nil
}

// FolderGitHub is the GitHub repository (`owner/name`, lower case) the
// folder's `origin` names, "" when it has no origin or one not on GitHub. It
// reads the folder's own git config and nothing on the network, bounded to
// two seconds.
func FolderGitHub(ctx context.Context, dir string) string {
	if !FolderHoldsRepo(dir) {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", gitpolicy.WithoutAutoMaintenance("-C", filepath.Clean(dir), "remote", "get-url", "origin")...).Output()
	if err != nil {
		return ""
	}
	return GitHubRemoteName(strings.TrimSpace(string(out)))
}

// GitHubRemoteName is the `owner/name` a git remote address names on
// github.com, lower case, "" for any other host or shape.
func GitHubRemoteName(remote string) string {
	if strings.HasPrefix(remote, "git@github.com:") {
		remote = "ssh://git@github.com/" + strings.TrimPrefix(remote, "git@github.com:")
	}
	u, err := url.Parse(remote)
	if err != nil || !strings.EqualFold(u.Hostname(), "github.com") {
		return ""
	}
	full := strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git"), "/"))
	if !RepoName(full) {
		return ""
	}
	return full
}

// FolderFinishedWords is the line a folder job ends on: where its work is.
func FolderFinishedWords(branch, dir string) string {
	if branch == "" {
		return "Finished on this computer · the work is in " + dir + "."
	}
	return "Finished on branch " + branch + " in " + dir + ". Nothing was pushed."
}

// WorkBranch is the branch the job's worktree recorded (its last `branch: `
// log line), "" before it has one.
func WorkBranch(it Item) string {
	if it.Stream == nil {
		return ""
	}
	for i := len(it.Stream.Log) - 1; i >= 0; i-- {
		if b, ok := strings.CutPrefix(strings.TrimSpace(it.Stream.Log[i].Text), "branch:"); ok {
			if b = strings.TrimSpace(b); b != "" {
				return b
			}
		}
	}
	return ""
}

// FolderPROffered says a finished folder job may open a pull request now:
// it is done on its branch, has not opened one, and github is the
// `owner/name` its folder's origin names with GitHub connected ("" when
// either is missing, [FolderNoGitHubWords]).
func FolderPROffered(it Item, github string) bool {
	return FolderOf(it) != "" && it.State == StateShipped && it.DoneHow == "" && it.PullRequest == nil &&
		it.FolderPR == nil && strings.TrimSpace(it.HeadCommit) != "" && WorkBranch(it) != "" && RepoName(github)
}
