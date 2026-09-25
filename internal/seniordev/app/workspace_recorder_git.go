//go:build !windows

package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/seniordev/util"
)

// gitRecorder keeps the workspaceRecorder promises with git. It is the default
// and the only recorder that leaves the run's work in the repository's own
// history.
type gitRecorder struct {
	workspace string
	note      func(string)
}

func newGitRecorder(workspace string, note func(string)) *gitRecorder {
	return &gitRecorder{workspace: workspace, note: note}
}

func (recorder *gitRecorder) Kind() string         { return "git" }
func (recorder *gitRecorder) CommitsOnWrite() bool { return true }

// git runs a git command in the workspace and keeps NUL-delimited path lists
// byte-for-byte while trimming ordinary human-readable output.
func (recorder *gitRecorder) git(args ...string) (string, error) {
	argv := util.GitArgv(args...)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = recorder.workspace
	out, err := cmd.CombinedOutput()
	if err != nil {
		// The logical args, not the identity flags: the message is read by a
		// model deciding what to fix, and the flags are never the problem.
		return "", fmt.Errorf(
			"git %s: %v: %s",
			strings.Join(args, " "), err, strings.TrimSpace(string(out)),
		)
	}
	// NUL-delimited path lists may begin with whitespace that belongs to the
	// first filename. Trimming those bytes would make a rescue miss that file.
	for _, arg := range args {
		if arg == "-z" {
			return string(out), nil
		}
	}
	return strings.TrimSpace(string(out)), nil
}

func (recorder *gitRecorder) Prepare(ctx context.Context) error {
	if gitOutput(ctx, recorder.workspace, "rev-parse", "--show-toplevel") == "" {
		// Unreachable in practice: newWorkspaceRecorder picks this recorder
		// only after reading a work tree with a commit. It stays for a folder
		// whose repository vanished between that reading and this one.
		return fmt.Errorf("workspace is not a git repository: %s; run with --in-place to work in a plain folder", recorder.workspace)
	}
	// Exclude senior-dev's own artifacts on the workspace at bootstrap
	// (non-fatal): without it the run's commits sweep senior-dev's bookkeeping
	// into the repository history, and the final patch carries files the
	// request never asked for.
	if _, err := util.EnsureSeniorDevExcluded(ctx, recorder.workspace); err != nil {
		recorder.note("[senior-dev] ensureSeniorDevExcluded failed (non-fatal): " + err.Error() + "\n")
	}
	return nil
}

func (recorder *gitRecorder) Base(ctx context.Context) (string, error) {
	baseSHA := gitOutput(ctx, recorder.workspace, "rev-parse", "HEAD")
	if baseSHA == "" {
		return "", fmt.Errorf("senior-dev run requires a git repository with at least one commit, or --in-place")
	}
	resolved := gitOutput(ctx, recorder.workspace, "rev-parse", "--verify", baseSHA+"^{commit}")
	if resolved == "" {
		return "", fmt.Errorf("base commit %q is not available in the workspace", baseSHA)
	}
	return resolved, nil
}

// Snapshot captures the full working tree (tracked and untracked, minus
// ignored) as a git tree object, through a temporary index so the real index
// and working tree are untouched.
func (recorder *gitRecorder) Snapshot() (string, error) {
	gitDir, err := recorder.git("rev-parse", "--git-dir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(recorder.workspace, gitDir)
	}
	tmp, err := os.CreateTemp(gitDir, "senior-dev-tree-index-*")
	if err != nil {
		return "", fmt.Errorf("create temporary git index: %w", err)
	}
	tmpIndex := tmp.Name()
	if closeErr := tmp.Close(); closeErr != nil {
		_ = os.Remove(tmpIndex)
		return "", fmt.Errorf("close temporary git index: %w", closeErr)
	}
	// GIT_INDEX_FILE requires either a valid index or no file. CreateTemp gives
	// us a collision-free name; remove the empty file before asking Git to
	// initialize it from HEAD. Loading HEAD is essential: git add -A against an
	// empty index omits tracked-but-ignored files and invents phantom deletions.
	if err := os.Remove(tmpIndex); err != nil {
		return "", fmt.Errorf("prepare temporary git index: %w", err)
	}
	defer os.Remove(tmpIndex)
	env := append(os.Environ(), "GIT_INDEX_FILE="+tmpIndex)
	read := exec.Command("git", "read-tree", "HEAD")
	read.Dir, read.Env = recorder.workspace, env
	if out, err := read.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git read-tree HEAD: %v: %s", err, strings.TrimSpace(string(out)))
	}
	add := exec.Command("git", "add", "-A", ".")
	add.Dir, add.Env = recorder.workspace, env
	if out, err := add.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git add -A: %v: %s", err, strings.TrimSpace(string(out)))
	}
	// The candidate must obey the ignore rules from the START of the run,
	// even after the model rewrote .gitignore. This is a private temporary index;
	// the real index never stages these paths.
	listed := exec.Command("git", "ls-files", "--cached", "-z")
	listed.Dir, listed.Env = recorder.workspace, env
	staged, err := listed.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git ls-files in temporary index: %v: %s", err, strings.TrimSpace(string(staged)))
	}
	ignoredAtStart, err := util.InitialIgnoredPaths()
	if err != nil {
		return "", err
	}
	var excluded []string
	for _, path := range strings.Split(string(staged), "\x00") {
		if path != "" && (util.PathIgnoredAtStart(path, ignoredAtStart) || util.GeneratedRunPath(path)) {
			excluded = append(excluded, path)
		}
	}
	if len(excluded) > 0 {
		reset := exec.Command("git", append([]string{"reset", "-q", "HEAD", "--"}, excluded...)...)
		reset.Dir, reset.Env = recorder.workspace, env
		if out, err := reset.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git reset temporary index: %v: %s", err, strings.TrimSpace(string(out)))
		}
	}
	write := exec.Command("git", "write-tree")
	write.Dir, write.Env = recorder.workspace, env
	out, err := write.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git write-tree: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// Record writes a commit object for an already-written tree without moving
// HEAD, the index, or the working tree. The commit exists so the candidate can
// be restored later by a single git command even if the run dies before
// finalize.
func (recorder *gitRecorder) Record(treeID, label string) (string, error) {
	parent, parentErr := recorder.git("rev-parse", "HEAD")
	args := []string{"commit-tree", treeID, "-m", label}
	if parentErr == nil && parent != "" {
		args = []string{"commit-tree", treeID, "-p", parent, "-m", label}
	}
	return recorder.git(args...)
}

func (recorder *gitRecorder) Publish(name, handle string) error {
	_, err := recorder.git("update-ref", name, handle)
	return err
}

// Restore makes the captured working tree match a recorded commit's tree,
// while preserving files ignored at the start, and proves it by re-hashing.
//
// `checkout --force <commit> -- .` alone is OVERLAY checkout: it writes the
// commit's files and deletes nothing. Resetting the index to the checkpoint
// makes files added later untracked, so they can be removed one by one. A
// blanket clean would also remove a person's file that was ignored at the
// start if the run later removed its .gitignore rule.
func (recorder *gitRecorder) Restore(handle, wantTree string) error {
	if _, err := recorder.git("checkout", "--force", handle, "--", "."); err != nil {
		return err
	}
	if _, err := recorder.git("reset", "-q", handle, "--", "."); err != nil {
		return err
	}
	newFiles, err := recorder.git("ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return err
	}
	ignoredAtStart, err := util.InitialIgnoredPaths()
	if err != nil {
		return err
	}
	for _, path := range strings.Split(newFiles, "\x00") {
		if path == "" || util.PathIgnoredAtStart(path, ignoredAtStart) {
			continue
		}
		if err := os.Remove(filepath.Join(recorder.workspace, filepath.FromSlash(path))); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	actual, err := recorder.Snapshot()
	if err != nil {
		return err
	}
	if actual != wantTree {
		return fmt.Errorf(
			"restored tree %s, want %s", shortSHA(actual), shortSHA(wantTree),
		)
	}
	return nil
}

// DifferentPaths includes edits to tracked files and new non-ignored files.
// Both can be removed by Restore, including a file already eagerly committed
// after the checkpoint, whose change is measured against the checkpoint.
func (recorder *gitRecorder) DifferentPaths(handle string) ([]string, error) {
	changed, err := recorder.git("diff", "--name-only", "-z", handle, "--")
	if err != nil {
		return nil, err
	}
	newFiles, err := recorder.git("ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, listing := range []string{changed, newFiles} {
		for _, path := range strings.Split(listing, "\x00") {
			if path != "" {
				seen[path] = true
			}
		}
	}
	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func (recorder *gitRecorder) BaseTree(base string) (string, bool) {
	tree, err := recorder.git("rev-parse", base+"^{tree}")
	if err != nil || tree == "" {
		return "", false
	}
	return tree, true
}

// Change compares the workspace against the base commit's tree, ignoring
// senior-dev's own artifacts.
//
// It deliberately does not use `git diff <base>` against the working copy,
// which reports only tracked changes. A run whose whole deliverable is a new
// file -- which is most of them -- produces an empty `git diff` while having
// changed everything that matters, so diffing that way would refuse exactly
// the submissions worth accepting. Snapshot stages everything through a
// temporary index, so comparing against that tree sees new files the way a
// diff of the final tree will.
func (recorder *gitRecorder) Change(base string) (soloTreeChange, error) {
	treeSHA, err := recorder.Snapshot()
	if err != nil {
		return soloTreeChange{}, err
	}
	change := soloTreeChange{treeSHA: treeSHA}
	baseTree, ok := recorder.BaseTree(base)
	if !ok {
		// No resolvable base: any tree at all is a change, and refusing to
		// submit because we cannot name the starting point would be worse than
		// accepting one we cannot size.
		change.changed = true
		return change, nil
	}
	diffArgs := func(extra ...string) []string {
		args := append([]string{"diff"}, extra...)
		args = append(args, baseTree, treeSHA, "--")
		return append(args, seniorDevArtifactPathspecs...)
	}
	names, err := recorder.git(diffArgs("--name-only")...)
	if err != nil {
		return change, err
	}
	change.files = len(nonEmptyLines(names))
	change.changed = change.files > 0
	if !change.changed {
		return change, nil
	}
	if patch, err := recorder.git(diffArgs()...); err == nil {
		change.patch = patch
	}
	return change, nil
}

func (recorder *gitRecorder) ListPaths(
	ctx context.Context, maxBytes int,
) ([]string, int, bool, error) {
	command := exec.CommandContext(
		ctx, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard",
	)
	command.Dir = recorder.workspace
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, 0, false, err
	}
	if err := command.Start(); err != nil {
		return nil, 0, false, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(stdout, int64(maxBytes)+1))
	if len(raw) > maxBytes {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, len(raw), true, nil
	}
	waitErr := command.Wait()
	if readErr != nil {
		return nil, len(raw), false, readErr
	}
	if waitErr != nil {
		return nil, len(raw), false, waitErr
	}
	paths := strings.Split(string(raw), "\x00")
	if len(paths) > 0 && paths[len(paths)-1] == "" {
		paths = paths[:len(paths)-1]
	}
	sort.Strings(paths)
	return paths, len(raw), false, nil
}

// Summary records the shape of the run's final diff against the base commit --
// files, line counts, binaries, patch bytes, untracked files.
func (recorder *gitRecorder) Summary(
	ctx context.Context, base string,
) (map[string]any, string) {
	workspace := recorder.workspace
	data := map[string]any{"base_sha": base}
	if head := gitOutput(ctx, workspace, "rev-parse", "HEAD"); head != "" {
		data["head_sha"] = head
	}
	nameOutput := gitOutput(ctx, workspace, "diff", "--name-only", "--no-renames", base, "--")
	files := 0
	if strings.TrimSpace(nameOutput) != "" {
		files = len(strings.Split(strings.TrimSpace(nameOutput), "\n"))
	}
	data["files"] = files

	additions, deletions, binaries := int64(0), int64(0), 0
	for _, line := range strings.Split(
		gitOutput(ctx, workspace, "diff", "--numstat", "--no-renames", base, "--"), "\n",
	) {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		if fields[0] == "-" || fields[1] == "-" {
			binaries++
			continue
		}
		if value, err := strconv.ParseInt(fields[0], 10, 64); err == nil {
			additions += value
		}
		if value, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
			deletions += value
		}
	}
	data["additions"], data["deletions"], data["binary_files"] = additions, deletions, binaries

	var patchBytes countingWriter
	var diffError bytes.Buffer
	command := exec.CommandContext(ctx, "git", "diff", "--binary", "--no-renames", base, "--")
	command.Dir, command.Stdout, command.Stderr = workspace, &patchBytes, &diffError
	status := "completed"
	if err := command.Run(); err != nil {
		status = "error"
		data["error"] = strings.TrimSpace(diffError.String())
	} else {
		data["patch_bytes"] = int64(patchBytes)
	}
	untracked := gitOutput(ctx, workspace, "ls-files", "--others", "--exclude-standard")
	if strings.TrimSpace(untracked) != "" {
		data["untracked_files"] = len(strings.Split(strings.TrimSpace(untracked), "\n"))
	} else {
		data["untracked_files"] = 0
	}
	return data, status
}

// gitStatusFindings reports an unclean index as a landing finding. It exists
// only under the git recorder: the advice it gives -- commit before verifying
// -- is meaningless where nothing commits.
func (recorder *gitRecorder) statusFindings() []string {
	status, err := recorder.git("status", "--porcelain")
	if err != nil {
		return nil
	}
	entries := nonEmptyLines(status)
	if len(entries) == 0 {
		return nil
	}
	return []string{fmt.Sprintf(
		"git status is not clean (%d uncommitted entr%s) — the pinned command must pass "+
			"on the COMMITTED tree, so commit before verifying",
		len(entries), plural(len(entries), "y", "ies"),
	)}
}

// summaryTimeout bounds the observational patch summary.
const summaryTimeout = 15 * time.Second
