package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var runObjectID = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

type runCommit struct {
	old, tree, message string
	parents            []string
	author, committer  string
	protected          bool
}

// signRunCommits signs only commits private to the run's checked-out branch.
// A missing base on this landing road cannot identify the run's work.
func signRunCommits(dir, base string, sign gitSignature) (int, error) {
	return signRunCommitsFrom(dir, base, false, sign)
}

// signUnbornRunCommits is for an in-place run whose snapshot saw an existing
// repository with no HEAD commit. Only that observation makes an empty base
// mean that the run's first commit is its own work.
func signUnbornRunCommits(dir string, sign gitSignature) (int, error) {
	return signRunCommitsFrom(dir, "", true, sign)
}

// signRunCommitsFrom rewrites messages at the end of a run, when its private
// commits are known. A commit hook on every worker git command would also run
// in the worker's test repositories and replace the project's own hooks.
// Plumbing creates objects without moving the index or worktree, then one
// compare-and-swap moves the branch if it still points at the observed head.
func signRunCommitsFrom(dir, base string, unborn bool, sign gitSignature) (int, error) {
	if (base == "" && !unborn) || repositoryRefusesTrailers(dir) {
		return 0, nil
	}
	refOut, err := git(dir, "symbolic-ref", "-q", "HEAD")
	if err != nil {
		return 0, nil // A detached HEAD has no branch to move.
	}
	ref := strings.TrimSpace(refOut)
	if !strings.HasPrefix(ref, "refs/heads/") {
		return 0, nil
	}
	headOut, err := git(dir, "rev-parse", ref)
	if err != nil {
		return 0, fmt.Errorf("read run branch: %w", err)
	}
	head := strings.TrimSpace(headOut)
	if !runObjectID.MatchString(head) {
		return 0, fmt.Errorf("read run branch: invalid object id %q", headOut)
	}
	if head == base {
		return 0, nil
	}
	list, err := runCommitRange(dir, ref, head, base)
	if err != nil {
		return 0, err
	}
	var commits []runCommit
	for _, line := range strings.Split(strings.TrimSpace(list), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		// git() combines stderr and stdout, so a warning must never become
		// a commit or parent id in the history we rewrite.
		for _, field := range fields {
			if !runObjectID.MatchString(field) {
				return 0, fmt.Errorf("list run commits: invalid object id %q", field)
			}
		}
		commit, err := readRunCommit(dir, fields[0])
		if err != nil {
			return 0, err
		}
		if commit.protected {
			// A rewritten ancestor would change the parent of this protected
			// commit, invalidating its signature or other special header.
			return 0, nil
		}
		commits = append(commits, commit)
	}
	if len(commits) == 0 {
		return 0, nil
	}
	remap := make(map[string]string, len(commits))
	rewritten := 0
	for _, commit := range commits {
		parents := append([]string(nil), commit.parents...)
		parentMoved := false
		for i, parent := range parents {
			if replacement, ok := remap[parent]; ok {
				parents[i] = replacement
				parentMoved = parentMoved || replacement != parent
			}
		}
		message := sign.signOnce(commit.message)
		if message == commit.message && !parentMoved {
			remap[commit.old] = commit.old
			continue
		}
		sha, err := writeRunCommit(dir, commit, parents, message)
		if err != nil {
			return 0, err
		}
		remap[commit.old] = sha
		rewritten++
	}
	newHead := remap[head]
	if newHead == "" || newHead == head {
		return 0, nil
	}
	if out, err := git(dir, "update-ref", "-m", "codeaf: sign the run's own commits", ref, newHead, head); err != nil {
		return 0, fmt.Errorf("move run branch after signing: %w: %s", err, strings.TrimSpace(out))
	}
	return rewritten, nil
}

// runCommitRange is the one definition of commits this run may sign. Other
// branch, tag, remote and FETCH_HEAD pointers protect published or fetched
// history, even when a worker merged it into its own branch. --exclude applies
// to --branches alone, so the run's branch remains eligible.
func runCommitRange(dir, ref, head, base string) (string, error) {
	args := []string{"rev-list", "--reverse", "--topo-order", "--parents", head}
	if base != "" {
		args = append(args, "^"+base)
	}
	args = append(args, "--not", "--exclude="+strings.TrimPrefix(ref, "refs/heads/"), "--branches", "--tags", "--remotes")
	fetchPath, err := git(dir, "rev-parse", "--git-path", "FETCH_HEAD")
	if err != nil {
		return "", fmt.Errorf("find FETCH_HEAD: %w: %s", err, strings.TrimSpace(fetchPath))
	}
	fetchPath = strings.TrimSpace(fetchPath)
	if !filepath.IsAbs(fetchPath) {
		fetchPath = filepath.Join(dir, fetchPath)
	}
	fetched, err := os.ReadFile(fetchPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read FETCH_HEAD: %w", err)
	}
	for _, line := range strings.Split(string(fetched), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 || !runObjectID.MatchString(fields[0]) {
			return "", fmt.Errorf("read FETCH_HEAD: invalid object id in %q", line)
		}
		args = append(args, fields[0])
	}
	list, err := git(dir, args...)
	if err != nil {
		return "", fmt.Errorf("list run commits: %w: %s", err, strings.TrimSpace(list))
	}
	return list, nil
}

// readRunCommit preserves the object's identities and message exactly. Headers
// other than the four ordinary kinds are deliberately not reconstructed.
func readRunCommit(dir, sha string) (runCommit, error) {
	raw, err := git(dir, "cat-file", "commit", sha)
	if err != nil {
		return runCommit{}, fmt.Errorf("read run commit %s: %w", sha, err)
	}
	headers, message, ok := strings.Cut(raw, "\n\n")
	if !ok {
		return runCommit{}, fmt.Errorf("run commit %s has no message boundary", sha)
	}
	commit := runCommit{old: sha, message: message}
	for _, line := range strings.Split(headers, "\n") {
		key, value, ok := strings.Cut(line, " ")
		if !ok {
			commit.protected = true
			continue
		}
		switch key {
		case "tree":
			commit.tree = value
		case "parent":
			commit.parents = append(commit.parents, value)
		case "author":
			commit.author = value
		case "committer":
			commit.committer = value
		default:
			commit.protected = true
		}
	}
	if commit.tree == "" || commit.author == "" || commit.committer == "" {
		return runCommit{}, fmt.Errorf("run commit %s lacks an ordinary identity or tree", sha)
	}
	return commit, nil
}

// writeRunCommit keeps the worker's original identities and tree. THE WORKER'S
// AUTHOR AND COMMITTER DO NOT MOVE; only the message and remapped parents do.
func writeRunCommit(dir string, commit runCommit, parents []string, message string) (string, error) {
	author, err := runCommitIdentity(commit.author, "AUTHOR")
	if err != nil {
		return "", err
	}
	committer, err := runCommitIdentity(commit.committer, "COMMITTER")
	if err != nil {
		return "", err
	}
	file, err := os.CreateTemp("", "codeaf-run-commit-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	if _, err := file.WriteString(message); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	args := []string{"commit-tree", "--no-gpg-sign", commit.tree}
	for _, parent := range parents {
		args = append(args, "-p", parent)
	}
	args = append(args, "-F", file.Name())
	out, err := gitWith(dir, append(author, committer...), args...)
	if err != nil {
		return "", fmt.Errorf("sign run commit %s: %w: %s", commit.old, err, strings.TrimSpace(out))
	}
	// commit-tree may write a warning before its object id on combined output.
	lines := strings.Fields(strings.TrimSpace(out))
	if len(lines) == 0 || !runObjectID.MatchString(lines[len(lines)-1]) {
		return "", fmt.Errorf("sign run commit %s: invalid object id in %q", commit.old, out)
	}
	return lines[len(lines)-1], nil
}

// runCommitIdentity splits at the final address delimiter because a person's
// name may itself contain the same "> " spelling.
func runCommitIdentity(raw, role string) ([]string, error) {
	close := strings.LastIndex(raw, "> ")
	if close < 0 {
		return nil, fmt.Errorf("run commit has an invalid %s identity", role)
	}
	identity, date := raw[:close], raw[close+2:]
	open := strings.LastIndex(identity, " <")
	if open < 0 {
		return nil, fmt.Errorf("run commit has an invalid %s address", role)
	}
	return []string{
		"GIT_" + role + "_NAME=" + identity[:open],
		"GIT_" + role + "_EMAIL=" + identity[open+2:],
		"GIT_" + role + "_DATE=" + date,
	}, nil
}
