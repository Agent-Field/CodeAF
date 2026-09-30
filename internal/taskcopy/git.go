package taskcopy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// git runs one git command in dir and answers its standard output as it came.
// The error carries git's own words, because a person reads them when a copy
// cannot be put back.
func git(dir string, args ...string) (string, error) {
	var stderr strings.Builder
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", &gitError{args: args, text: strings.TrimSpace(stderr.String()), err: err}
	}
	return string(out), nil
}

type gitError struct {
	args []string
	text string
	err  error
}

func (e *gitError) Error() string {
	return "git " + strings.Join(e.args, " ") + ": " + e.err.Error() + ": " + e.text
}

func (e *gitError) Unwrap() error { return e.err }

// isLinkedWorktree says whether dir is a worktree cut off another repository:
// its .git is a file that points at the repository's, not a folder.
func isLinkedWorktree(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && info.Mode().IsRegular()
}

// isRepository says whether dir is a git working tree of any kind: a folder with
// a repository of its own (what a task's fork is) or a linked worktree, whose
// .git is a file. A task copy is either, so a copy is recognised by the folder
// and never by what the project's own repository has registered.
func isRepository(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// bundleOwnCommits writes into path the commits of tree's HEAD that no branch,
// tag or remote ref other than branch holds, and answers false when there are
// none. A fork keeps its commits in a repository of its own, which the seal does
// not capture, so without this the next machine would find the branch's tip
// missing; for a worktree these commits are also in the project's .git and the
// bundle is redundant but harmless. branch is left out of the "held elsewhere"
// set because it is the ref that names these very commits.
func bundleOwnCommits(tree, branch, path string) (bool, error) {
	held := []string{"--not"}
	if branch != "" {
		held = append(held, "--exclude="+branch)
	}
	held = append(held, "--branches", "--tags", "--remotes")
	own, err := git(tree, append([]string{"rev-list", "-n1", "HEAD"}, held...)...)
	if err != nil || strings.TrimSpace(own) == "" {
		return false, err
	}
	_, err = git(tree, append([]string{"bundle", "create", path, "HEAD"}, held...)...)
	return err == nil, err
}
