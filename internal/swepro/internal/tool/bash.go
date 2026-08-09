// This file ports the process execution and termination slice of
// swe-pro/src/tool/shell.ts:456-629 and swe-pro/src/shell/shell.ts:9-57 at
// commit 3b25a1a.
package tool

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/adaptiveflag"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/outputoffload"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/testmemo"
)

const (
	defaultBashTimeoutMS = 120000
	maxBashOutputBytes   = 30000
	// TS src/tool/shell.ts:725-759 makes memo lookup an optional fast path, so
	// dirty-tree hashing gets a hard budget rather than delaying execution.
	// Eight MiB covers ordinary source edits without letting generated fixtures
	// dominate every test command; entry/status caps and a two-second git probe
	// bound tree shape and repository discovery work.
	testMemoFingerprintMaxBytes int64 = 8 << 20
	testMemoFingerprintMaxFiles       = 4096
	testMemoGitStatusMaxBytes         = 1 << 20
	testMemoGitProbeTimeout           = 2 * time.Second
)

var errTestMemoFingerprintBudget = errors.New("test memo fingerprint budget exceeded")

var bashAfter = time.After
var bashOffloader = outputoffload.DefaultOffloader
var bashReadOOMCount = readShellOOMCount
var bashReadMemoryLimit = readShellMemoryLimit

type testMemoDisabledContextKey struct{}

// WithTestMemoDisabled forces Bash test commands to execute in a fresh
// process. The session-end verification floor uses this because its evidence
// must never inherit a worker-facing optimization.
func WithTestMemoDisabled(ctx context.Context) context.Context {
	return context.WithValue(ctx, testMemoDisabledContextKey{}, true)
}

type bashOutput struct {
	mu         sync.Mutex
	buffer     bytes.Buffer
	wrote      bool
	lastOutput time.Time
}

func (o *bashOutput) Write(data []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.wrote = true
	o.lastOutput = time.Now()
	return o.buffer.Write(data)
}

func (o *bashOutput) bytes() []byte {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]byte(nil), o.buffer.Bytes()...)
}

func (o *bashOutput) lastOutputAt() (time.Time, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.lastOutput, o.wrote
}

func (r *Registry) executeBash(ctx context.Context, call steploop.ToolCall) (steploop.ToolResult, error) {
	var input bashInput
	if err := decodeInput(call.Input, &input, "command"); err != nil {
		return steploop.ToolResult{}, err
	}
	timeoutMS := defaultBashTimeoutMS
	if input.TimeoutMS != nil {
		timeoutMS = *input.TimeoutMS
	}
	if timeoutMS < 1 || timeoutMS > 600000 {
		return steploop.ToolResult{}, fmt.Errorf("timeout_ms must be between 1 and 600000")
	}
	if err := ctx.Err(); err != nil {
		return steploop.ToolResult{}, err
	}
	cwd := r.workDir
	if input.Workdir != "" {
		resolved, err := r.resolvePath(input.Workdir)
		if err != nil {
			return steploop.ToolResult{}, err
		}
		cwd = resolved
		info, statErr := os.Stat(cwd)
		if statErr != nil {
			return steploop.ToolResult{}, statErr
		}
		if !info.IsDir() {
			return steploop.ToolResult{}, fmt.Errorf("workdir must be a directory: %s", cwd)
		}
		if err := r.askExternalDirectory(ctx, call, cwd, "directory"); err != nil {
			return steploop.ToolResult{}, err
		}
	}
	shell, err := r.executionShell()
	if err != nil {
		return steploop.ToolResult{}, err
	}
	scan := ScanShellPermissions(input.Command, ShellScanOptions{
		CWD: cwd, Workspace: r.worktree(), Shell: shell,
		IsDir: func(path string) bool {
			info, err := os.Stat(path)
			return err == nil && info.IsDir()
		},
	})
	if r.hardConfineShell && len(scan.Dirs) > 0 {
		return steploop.ToolResult{}, fmt.Errorf("path escapes workspace: %s", scan.Dirs[0])
	}
	if len(scan.Dirs) > 0 {
		globs := make([]string, 0, len(scan.Dirs))
		for _, dir := range scan.Dirs {
			globs = append(globs, filepath.Join(dir, "*"))
		}
		if err := r.askWithAlways(ctx, call, "external_directory", globs, globs, map[string]any{}); err != nil {
			return steploop.ToolResult{}, err
		}
	}
	if len(scan.Patterns) > 0 {
		if err := r.askWithAlways(ctx, call, "bash", scan.Patterns, scan.Always, map[string]any{}); err != nil {
			return steploop.ToolResult{}, err
		}
	}
	binding := r.guardShell(ctx, call, input.Command)
	directCommandExitCode := 0
	directCommandCompleted := false
	if binding != nil {
		// Reserve the bound task as in-flight for the whole command so the
		// root drain (which pumps concurrently with tool execution) neither
		// sweep-closes a created package nor releases a reused planner task
		// while the command is still running. Released only after the
		// tool-level close below, so there is no window where the task is
		// open, unreserved, and mid-command.
		endGuardTask := r.BeginGuardTask(binding.TaskID)
		defer func() {
			if binding.Created {
				result := "Direct command did not complete (aborted/timed out/errored)"
				if directCommandCompleted {
					result = fmt.Sprintf("Direct command completed: exit=%d", directCommandExitCode)
				}
				closed := r.planRun([]string{
					"plandb", "done", binding.TaskID,
					"--result", result, "--agent", call.Agent, "--json",
				})
				if closed.Code != 0 {
					log.Printf(
						"codeaf: could not close harness PlanDB shell package %s: %s",
						binding.TaskID, strings.TrimSpace(string(closed.Stderr)),
					)
				}
				r.planActive.Clear(call.SessionID, r.workDir, binding.TaskID)
			}
			endGuardTask()
		}()
	}

	var memoKey string
	var memoState *bashTestMemoState
	if adaptiveflag.AdaptiveCutsEnabled() && testmemo.IsTestCommand(input.Command) &&
		ctx.Value(testMemoDisabledContextKey{}) != true {
		var memoSafe bool
		memoKey, memoState, memoSafe = bashTestMemoKey(ctx, cwd, input.Command)
		if !memoSafe {
			memoKey = ""
		}
	}
	if memoKey != "" {
		r.testMemoMu.Lock()
		cached := r.testMemo.Get(memoKey)
		r.testMemoMu.Unlock()
		if cached != nil {
			directCommandExitCode = int(cached.Code)
			directCommandCompleted = true
			fullOutput := cached.Stdout
			if fullOutput == "" {
				fullOutput = cached.Stderr
			}
			if fullOutput == "" {
				fullOutput = "(no output)"
			}
			fullOutput += "\n\n" + testmemo.TEST_MEMO_ANNOTATION
			return r.bashResult(call, input.Command, []byte(fullOutput), int(cached.Code), true), nil
		}
	}

	command := shellExecCommand(shell, input.Command)
	command.Dir = cwd
	command.Env = shellEnvironment(call.SessionID)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var output bashOutput
	command.Stdout = &output
	command.Stderr = &output
	envSignalsOn := os.Getenv("CODEAF_ENV_SIGNALS") != "0"
	startedAt := time.Now()
	var oomBefore *int64
	if envSignalsOn {
		oomBefore = bashReadOOMCount()
	}
	if err := command.Start(); err != nil {
		return steploop.ToolResult{}, fmt.Errorf("start shell command: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- command.Wait()
	}()

	var runErr error
	expired := false
	select {
	case runErr = <-done:
	case <-bashAfter(time.Duration(timeoutMS) * time.Millisecond):
		expired = true
		killProcessGroup(command.Process.Pid)
		<-done
		appendOutputLine(&output, fmt.Sprintf("command timed out after %dms", timeoutMS))
	case <-ctx.Done():
		killProcessGroup(command.Process.Pid)
		<-done
		return steploop.ToolResult{}, ctx.Err()
	}

	var exitCode *int
	if runErr == nil && !expired {
		code := 0
		exitCode = &code
	} else if !expired {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			return steploop.ToolResult{}, fmt.Errorf("wait for shell command: %w", runErr)
		}
		code := normalizedExitCode(exitErr)
		exitCode = &code
		appendOutputLine(&output, fmt.Sprintf("exit status %d", code))
	}

	if envSignalsOn {
		duration := time.Since(startedAt)
		oomAfter := bashReadOOMCount()
		var oomDelta *int64
		if oomBefore != nil && oomAfter != nil {
			delta := *oomAfter - *oomBefore
			oomDelta = &delta
		}
		var sinceLast *time.Duration
		if lastOutput, ok := output.lastOutputAt(); ok {
			quiet := time.Since(lastOutput)
			sinceLast = &quiet
		}
		metadata := []string{}
		if death := classifyShellDeath(shellDeathInput{
			ExitCode: exitCode, Expired: expired, OOMDelta: oomDelta,
			MemoryLimitBytes: bashReadMemoryLimit(), SinceLastOutput: sinceLast,
			Timeout: time.Duration(timeoutMS) * time.Millisecond, CommandDuration: duration,
		}); death != "" {
			metadata = append(metadata, death)
		}
		failed := exitCode == nil || *exitCode != 0
		if repeat := registerShellOutcome(call.SessionID, input.Command, duration, failed); repeat != "" {
			metadata = append(metadata, repeat)
		}
		if len(metadata) > 0 {
			appendOutputLine(&output, "\n<shell_metadata>\n"+strings.Join(metadata, "\n")+"\n</shell_metadata>")
		}
	}

	fullOutput := output.bytes()
	code := -1
	if exitCode != nil {
		code = *exitCode
		directCommandExitCode = code
		directCommandCompleted = true
	}
	result := r.bashResult(call, input.Command, fullOutput, code, exitCode != nil)
	// TS src/tool/shell.ts:773-778 stores under the lookup key after execution.
	// A test can mutate source or configuration while it runs, so cache only
	// when the post-process tree still matches that lookup key.
	postMemoKey, postMemoSafe := "", false
	if memoKey != "" && exitCode != nil {
		postMemoKey, postMemoSafe, _ = bashTestMemoPostKey(ctx, memoState)
	}
	if memoKey != "" && exitCode != nil && postMemoSafe && postMemoKey == memoKey {
		r.testMemoMu.Lock()
		r.testMemo.Set(memoKey, &testmemo.CachedTestResult{
			Code: jscompat.JSNumber(*exitCode), Stdout: string(fullOutput), Stderr: "",
		})
		r.testMemoMu.Unlock()
	}
	return result, nil
}

type bashTestMemoState struct {
	key         string
	command     string
	physicalCWD string
	git         bool
	worktree    string
	status      []byte
	head        []byte
	gitDir      []byte
	files       []memoFileState
}

type memoFileState struct {
	relative string
	mode     os.FileMode
	size     int64
	modTime  int64
	link     string
	missing  bool
}

type memoFingerprintBudget struct {
	bytes int64
	files int
}

func (budget *memoFingerprintBudget) addFile() error {
	if budget.files >= testMemoFingerprintMaxFiles {
		return errTestMemoFingerprintBudget
	}
	budget.files++
	return nil
}

func (budget *memoFingerprintBudget) addBytes(size int64) error {
	if size < 0 || size > testMemoFingerprintMaxBytes-budget.bytes {
		return errTestMemoFingerprintBudget
	}
	budget.bytes += size
	return nil
}

func bashTestMemoKey(ctx context.Context, cwd, command string) (string, *bashTestMemoState, bool) {
	ctx, cancel := context.WithTimeout(ctx, testMemoGitProbeTimeout)
	defer cancel()
	physicalCWD, err := filepath.Abs(cwd)
	if err != nil {
		return "", nil, false
	}
	if resolved, resolveErr := filepath.EvalSymlinks(physicalCWD); resolveErr == nil {
		physicalCWD = resolved
	}
	state := &bashTestMemoState{command: command, physicalCWD: physicalCWD}
	topOutput, topErr := runMemoGit(ctx, physicalCWD, "rev-parse", "--show-toplevel")
	if topErr != nil {
		fingerprint, files, fingerprintErr := hashFilesystemTreeState(physicalCWD)
		if fingerprintErr != nil {
			return "", nil, false
		}
		state.files = files
		state.key = testmemo.BuildTestMemoKey(testmemo.BuildTestMemoKeyInput{
			Command: command, TreeFingerprint: fingerprint, WorktreeID: physicalCWD,
		})
		return state.key, state, true
	}
	worktree := strings.TrimSpace(string(topOutput))
	if resolved, resolveErr := filepath.EvalSymlinks(worktree); resolveErr == nil {
		worktree = resolved
	}
	status, err := runMemoGitStatus(ctx, physicalCWD)
	if err != nil {
		return "", nil, false
	}
	fingerprint, files, err := hashGitDirtyTreeState(worktree, status)
	if err != nil {
		return "", nil, false
	}
	head, _ := runMemoGit(ctx, physicalCWD, "rev-parse", "HEAD")
	gitDir, err := runMemoGit(ctx, physicalCWD, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", nil, false
	}
	identity := strings.Join([]string{
		strings.TrimSpace(string(gitDir)), worktree, physicalCWD,
	}, "\x00")
	state.git = true
	state.worktree = worktree
	state.status = append([]byte(nil), status...)
	state.head = append([]byte(nil), head...)
	state.gitDir = append([]byte(nil), gitDir...)
	state.files = files
	state.key = testmemo.BuildTestMemoKey(testmemo.BuildTestMemoKeyInput{
		Command: command, HeadSHA: string(head), TreeFingerprint: fingerprint,
		WorktreeID: identity,
	})
	return state.key, state, true
}

func hashGitDirtyTree(worktree string, status []byte) (string, error) {
	fingerprint, _, err := hashGitDirtyTreeState(worktree, status)
	return fingerprint, err
}

func hashGitDirtyTreeState(worktree string, status []byte) (string, []memoFileState, error) {
	if len(status) > testMemoGitStatusMaxBytes {
		return "", nil, errTestMemoFingerprintBudget
	}
	digest := sha256.New()
	_, _ = digest.Write(status)
	paths, err := gitPorcelainPaths(status)
	if err != nil {
		return "", nil, err
	}
	budget := &memoFingerprintBudget{}
	files := []memoFileState{}
	seen := map[string]bool{}
	for _, relative := range paths {
		if seen[relative] {
			continue
		}
		seen[relative] = true
		if err := fingerprintFilesystemPath(
			digest, worktree, relative, budget, &files, true,
		); err != nil {
			return "", nil, err
		}
	}
	return hex.EncodeToString(digest.Sum(nil)), files, nil
}

func gitPorcelainPaths(status []byte) ([]string, error) {
	paths := []string{}
	for offset := 0; offset < len(status); {
		end := bytes.IndexByte(status[offset:], 0)
		if end < 0 {
			return nil, errors.New("unterminated git status record")
		}
		end += offset
		record := status[offset:end]
		if len(record) < 4 || record[2] != ' ' {
			return nil, errors.New("invalid git status record")
		}
		paths = append(paths, string(record[3:]))
		offset = end + 1
		if record[0] == 'R' || record[0] == 'C' || record[1] == 'R' || record[1] == 'C' {
			end = bytes.IndexByte(status[offset:], 0)
			if end < 0 {
				return nil, errors.New("unterminated git rename record")
			}
			end += offset
			paths = append(paths, string(status[offset:end]))
			offset = end + 1
		}
	}
	return paths, nil
}

func hashFilesystemTree(root string) (string, error) {
	fingerprint, _, err := hashFilesystemTreeState(root)
	return fingerprint, err
}

func hashFilesystemTreeState(root string) (string, []memoFileState, error) {
	digest := sha256.New()
	budget := &memoFingerprintBudget{}
	files := []memoFileState{}
	if err := fingerprintFilesystemPath(digest, root, ".", budget, &files, true); err != nil {
		return "", nil, err
	}
	return hex.EncodeToString(digest.Sum(nil)), files, nil
}

func hashFilesystemPath(digest hash.Hash, root, relative string) error {
	return fingerprintFilesystemPath(
		digest, root, relative, &memoFingerprintBudget{}, &[]memoFileState{}, true,
	)
}

func fingerprintFilesystemPath(
	digest hash.Hash,
	root string,
	relative string,
	budget *memoFingerprintBudget,
	files *[]memoFileState,
	hashContent bool,
) error {
	cleanRelative := filepath.Clean(filepath.FromSlash(relative))
	if filepath.IsAbs(cleanRelative) || cleanRelative == ".." ||
		strings.HasPrefix(cleanRelative, ".."+string(filepath.Separator)) {
		return errors.New("test memo path escapes worktree")
	}
	path := filepath.Join(root, cleanRelative)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := budget.addFile(); err != nil {
			return err
		}
		normalized := filepath.ToSlash(cleanRelative)
		if hashContent {
			writeMemoHashField(digest, normalized)
			writeMemoHashField(digest, "missing")
		}
		*files = append(*files, memoFileState{relative: normalized, missing: true})
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fingerprintFilesystemFile(digest, root, path, info, budget, files, hashContent)
	}
	return walkMemoDirectory(digest, root, path, path, budget, files, hashContent)
}

func walkMemoDirectory(
	digest hash.Hash,
	root string,
	initialPath string,
	current string,
	budget *memoFingerprintBudget,
	files *[]memoFileState,
	hashContent bool,
) error {
	info, err := os.Lstat(current)
	if err != nil {
		return err
	}
	if err := fingerprintFilesystemFile(
		digest, root, current, info, budget, files, hashContent,
	); err != nil {
		return err
	}
	directory, err := os.Open(current)
	if err != nil {
		return err
	}
	defer directory.Close()
	for {
		// filepath.WalkDir reads and sorts an entire directory before visiting
		// it, which would defeat the entry cap. Incremental ReadDir keeps the
		// non-git fallback bounded even for one enormous generated directory.
		entries, readErr := directory.ReadDir(1)
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
		entry := entries[0]
		child := filepath.Join(current, entry.Name())
		if entry.Name() == ".git" && child != initialPath {
			continue
		}
		childInfo, err := os.Lstat(child)
		if err != nil {
			return err
		}
		if childInfo.IsDir() {
			if err := walkMemoDirectory(
				digest, root, initialPath, child, budget, files, hashContent,
			); err != nil {
				return err
			}
			continue
		}
		if err := fingerprintFilesystemFile(
			digest, root, child, childInfo, budget, files, hashContent,
		); err != nil {
			return err
		}
	}
}

func hashFilesystemFile(digest hash.Hash, root, path string, info os.FileInfo) error {
	return fingerprintFilesystemFile(
		digest, root, path, info, &memoFingerprintBudget{}, &[]memoFileState{}, true,
	)
}

func fingerprintFilesystemFile(
	digest hash.Hash,
	root string,
	path string,
	info os.FileInfo,
	budget *memoFingerprintBudget,
	files *[]memoFileState,
	hashContent bool,
) error {
	if err := budget.addFile(); err != nil {
		return err
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	relative = filepath.ToSlash(relative)
	state := memoFileState{
		relative: relative, mode: info.Mode(), size: info.Size(),
	}
	if info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		state.modTime = info.ModTime().UnixNano()
	}
	if hashContent {
		writeMemoHashField(digest, relative)
		writeMemoHashField(digest, info.Mode().String())
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return err
		}
		if err := budget.addBytes(int64(len(target))); err != nil {
			return err
		}
		state.link = target
		if hashContent {
			writeMemoHashField(digest, target)
		}
		*files = append(*files, state)
		return nil
	}
	if !info.Mode().IsRegular() {
		*files = append(*files, state)
		return nil
	}
	if !hashContent {
		*files = append(*files, state)
		return nil
	}
	if err := budget.addBytes(info.Size()); err != nil {
		return err
	}
	writeMemoHashSize(digest, uint64(info.Size()))
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := io.CopyN(digest, file, info.Size()); err != nil {
		return err
	}
	after, err := file.Stat()
	if err != nil {
		return err
	}
	if after.Mode() != info.Mode() || after.Size() != info.Size() ||
		after.ModTime() != info.ModTime() {
		return errors.New("test memo file changed while fingerprinting")
	}
	*files = append(*files, state)
	return nil
}

func bashTestMemoPostKey(
	ctx context.Context, state *bashTestMemoState,
) (key string, safe bool, rehashed bool) {
	if state == nil {
		return "", false, false
	}
	unchanged, safe := bashTestMemoSnapshotUnchanged(ctx, state)
	if !safe {
		return "", false, false
	}
	if unchanged {
		return state.key, true, false
	}
	key, _, safe = bashTestMemoKey(ctx, state.physicalCWD, state.command)
	return key, safe, true
}

func bashTestMemoSnapshotUnchanged(ctx context.Context, state *bashTestMemoState) (bool, bool) {
	ctx, cancel := context.WithTimeout(ctx, testMemoGitProbeTimeout)
	defer cancel()
	top, topErr := runMemoGit(ctx, state.physicalCWD, "rev-parse", "--show-toplevel")
	if !state.git {
		if topErr == nil {
			return false, true
		}
		files, err := collectFilesystemTreeState(state.physicalCWD)
		return memoFileStatesEqual(state.files, files), err == nil
	}
	if topErr != nil {
		return false, true
	}
	worktree := strings.TrimSpace(string(top))
	if resolved, err := filepath.EvalSymlinks(worktree); err == nil {
		worktree = resolved
	}
	if worktree != state.worktree {
		return false, true
	}
	status, err := runMemoGitStatus(ctx, state.physicalCWD)
	if err != nil {
		return false, false
	}
	head, _ := runMemoGit(ctx, state.physicalCWD, "rev-parse", "HEAD")
	gitDir, err := runMemoGit(ctx, state.physicalCWD, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return false, false
	}
	if !bytes.Equal(status, state.status) || !bytes.Equal(head, state.head) ||
		!bytes.Equal(gitDir, state.gitDir) {
		return false, true
	}
	files, err := collectGitDirtyTreeState(worktree, status)
	if err != nil {
		return false, false
	}
	return memoFileStatesEqual(state.files, files), true
}

func collectFilesystemTreeState(root string) ([]memoFileState, error) {
	budget := &memoFingerprintBudget{}
	files := []memoFileState{}
	err := fingerprintFilesystemPath(nil, root, ".", budget, &files, false)
	return files, err
}

func collectGitDirtyTreeState(worktree string, status []byte) ([]memoFileState, error) {
	if len(status) > testMemoGitStatusMaxBytes {
		return nil, errTestMemoFingerprintBudget
	}
	paths, err := gitPorcelainPaths(status)
	if err != nil {
		return nil, err
	}
	budget := &memoFingerprintBudget{}
	files := []memoFileState{}
	seen := map[string]bool{}
	for _, relative := range paths {
		if seen[relative] {
			continue
		}
		seen[relative] = true
		if err := fingerprintFilesystemPath(
			nil, worktree, relative, budget, &files, false,
		); err != nil {
			return nil, err
		}
	}
	return files, nil
}

func memoFileStatesEqual(left, right []memoFileState) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func runMemoGit(ctx context.Context, cwd string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	return cmd.Output()
}

type boundedMemoOutput struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (output *boundedMemoOutput) Write(data []byte) (int, error) {
	remaining := output.limit - output.Len()
	if remaining < len(data) {
		output.exceeded = true
		if remaining < 0 {
			remaining = 0
		}
		_, _ = output.Buffer.Write(data[:remaining])
		return remaining, errTestMemoFingerprintBudget
	}
	return output.Buffer.Write(data)
}

func runMemoGitStatus(ctx context.Context, cwd string) ([]byte, error) {
	cmd := exec.CommandContext(
		ctx, "git", "status", "--porcelain=v1", "-z", "--untracked-files=all",
	)
	cmd.Dir = cwd
	output := &boundedMemoOutput{limit: testMemoGitStatusMaxBytes}
	cmd.Stdout = output
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	if output.exceeded {
		return nil, errTestMemoFingerprintBudget
	}
	return append([]byte(nil), output.Bytes()...), nil
}

func writeMemoHashField(digest hash.Hash, value string) {
	writeMemoHashSize(digest, uint64(len(value)))
	_, _ = digest.Write([]byte(value))
}

func writeMemoHashSize(digest hash.Hash, size uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], size)
	_, _ = digest.Write(encoded[:])
}

func (r *Registry) bashResult(
	call steploop.ToolCall, command string, fullOutput []byte, exitCode int, hasExitCode bool,
) steploop.ToolResult {
	inline := truncateMiddle(fullOutput, maxBashOutputBytes)
	if len(fullOutput) > maxBashOutputBytes {
		offloaded := bashOffloader.OffloadLargeOutput(
			outputoffload.OutputOffloadInput{
				Output: string(fullOutput), Workspace: r.workDir,
				ToolName: "bash", CallID: call.ID, SessionID: call.SessionID,
			},
			outputoffload.OutputOffloadOptions{Force: true},
		)
		if offloaded.OffloadPath != nil {
			inline += "\n\nThe tool call succeeded but the output was truncated. Full output saved to: " + *offloaded.OffloadPath +
				"\nUse Grep to search the full content or Read with offset/limit to view specific sections."
		} else if fallback := strings.TrimSpace(offloaded.Inline); fallback != "" {
			if index := strings.LastIndex(fallback, "\n"); index >= 0 {
				fallback = fallback[index+1:]
			}
			inline += "\n\n" + fallback
		}
	}
	metadata := msgmodel.RawObject("{}")
	if hasExitCode {
		metadata = msgmodel.RawObject(fmt.Sprintf(`{"exitCode":%d}`, exitCode))
	}
	return steploop.ToolResult{Title: firstRunes(command, 60), Metadata: metadata, Output: inline}
}

func shellExecCommand(shell, command string) *exec.Cmd {
	switch ShellName(shell) {
	case "cmd":
		return exec.Command(shell, "/c", command)
	case "powershell", "pwsh":
		return exec.Command(shell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", command)
	default:
		return exec.Command(shell, "-c", command)
	}
}

func normalizedExitCode(exitErr *exec.ExitError) int {
	if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	return exitErr.ExitCode()
}

func killProcessGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}

func appendOutputLine(output *bashOutput, line string) {
	output.mu.Lock()
	defer output.mu.Unlock()
	if output.buffer.Len() > 0 && output.buffer.Bytes()[output.buffer.Len()-1] != '\n' {
		output.buffer.WriteByte('\n')
	}
	output.buffer.WriteString(line)
}

func firstRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func truncateMiddle(data []byte, limit int) string {
	if len(data) <= limit {
		return string(data)
	}

	removed := len(data) - limit
	var marker string
	var kept int
	for {
		marker = fmt.Sprintf("[... %d bytes truncated ...]", removed)
		kept = limit - len(marker)
		if kept < 0 {
			return marker[:limit]
		}
		actualRemoved := len(data) - kept
		if actualRemoved == removed {
			break
		}
		removed = actualRemoved
	}

	head := kept / 2
	tail := kept - head
	result := make([]byte, 0, limit)
	result = append(result, data[:head]...)
	result = append(result, marker...)
	result = append(result, data[len(data)-tail:]...)
	return string(result)
}
