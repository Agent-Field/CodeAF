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

// isLinkedWorktree says whether dir is a git worktree cut off another
// repository: its .git is a file that points at the repository's, not a folder.
func isLinkedWorktree(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && info.Mode().IsRegular()
}

// isRepository says whether dir holds a git repository of its own.
func isRepository(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}
