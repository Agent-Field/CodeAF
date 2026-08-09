// This file ports src/session/plandb-scheduler.ts:1107-1286 from swe-pro
// (commit 3b25a1a).
package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/isolation"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/isolationfurrow"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/leafoutcome"
)

type osCommandRunner struct{}

const plumbingCommandTimeout = 5 * time.Minute

// plumbingContext preserves request-scoped values while deliberately dropping
// cancellation. The TypeScript scheduler polls its wall budget and never
// passes an abort signal to git or `plandb list`; attaching Go's run deadline
// to those subprocesses can turn a killed command's empty stdout into a false
// claim that the repository has no changes. Keep plumbing bounded separately
// so repository bookkeeping can still report the truth after budget expiry.
func plumbingContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), plumbingCommandTimeout)
}

func (osCommandRunner) Run(ctx context.Context, argv []string, opts runOptions) runResult {
	if len(argv) == 0 {
		return runResult{Code: 1, Stderr: []byte("empty command")}
	}
	commandCtx, cancel := plumbingContext(ctx)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, argv[0], argv[1:]...)
	cmd.Dir = opts.Cwd
	cmd.Env = mergedEnvironment(opts.Env)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return runResult{Code: 0, Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return runResult{Code: exitErr.ExitCode(), Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	}
	return runResult{Code: 1, Stdout: []byte{}, Stderr: []byte(err.Error())}
}

func mergedEnvironment(overrides map[string]string) []string {
	if len(overrides) == 0 {
		return os.Environ()
	}
	env := os.Environ()
	index := make(map[string]int, len(env))
	for i, item := range env {
		if eq := strings.IndexByte(item, '='); eq >= 0 {
			index[item[:eq]] = i
		}
	}
	for key, value := range overrides {
		item := key + "=" + value
		if i, ok := index[key]; ok {
			env[i] = item
			continue
		}
		index[key] = len(env)
		env = append(env, item)
	}
	return env
}

type markedStringSet struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

func newMarkedStringSet() *markedStringSet {
	return &markedStringSet{seen: map[string]struct{}{}}
}

func (s *markedStringSet) markIfAbsent(value string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[value]; ok {
		return false
	}
	s.seen[value] = struct{}{}
	return true
}

var (
	gcSweptWorkspaces       = newMarkedStringSet()
	isolationDecisionLogged = newMarkedStringSet()
)

type noWorktreeHygiene struct{}

func (noWorktreeHygiene) EnsureCodeafExcluded(context.Context, string) error {
	return nil
}

func (noWorktreeHygiene) SuppressCaseCollisions(context.Context, string) error {
	return nil
}

// AllocateWorktreeOptions contains the three TS test seams plus phase-one Go
// seams for process execution and the two out-of-bundle hygiene helpers.
type AllocateWorktreeOptions struct {
	RequiresMerge *bool
	IsolationEnv  map[string]string
	CowProbe      func() bool

	furrowFactory furrowFactory
	runner        commandRunner
	hygiene       worktreeHygiene
}

func defaultFurrowFactory() furrowForker {
	adapter := isolationfurrow.CreateFurrowIsolation()
	if adapter == nil {
		return nil
	}
	return adapter
}

// AllocateWorktree mirrors allocateWorktree
// (plandb-scheduler.ts:1176-1286). A nil result is the TS undefined failure
// result. Process failures are nothrow results and cleanup is best effort.
func AllocateWorktree(
	ctx context.Context,
	workspace string,
	taskID string,
	baseRef string,
	options ...AllocateWorktreeOptions,
) *WorktreeAllocation {
	opts := AllocateWorktreeOptions{}
	if len(options) > 0 {
		opts = options[0]
	}
	runner := opts.runner
	if runner == nil {
		runner = osCommandRunner{}
	}
	hygiene := opts.hygiene
	if hygiene == nil {
		hygiene = noWorktreeHygiene{}
	}

	branch := "plandb/" + taskID
	worktreePath := filepath.Join(workspace, ".plandb", "wt-"+taskID)
	baseRev := runner.Run(ctx, []string{"git", "rev-parse", baseRef}, runOptions{Cwd: workspace})
	baseSHA := jscompat.Trim(string(baseRev.Stdout))
	if baseSHA == "" {
		return nil
	}

	env := opts.IsolationEnv
	if env == nil {
		env = map[string]string{"CODEAF_ISOLATION": os.Getenv("CODEAF_ISOLATION")}
	}
	requested := isolation.ResolveIsolationBackend(env)
	requiresMerge := true
	if opts.RequiresMerge != nil {
		requiresMerge = *opts.RequiresMerge
	}
	if !requested.Recognized {
		schedulerLog.Warn("CODEAF_ISOLATION unrecognized — using default isolation backend", map[string]any{
			"taskID":  taskID,
			"raw":     requested.Raw,
			"backend": requested.Backend,
		})
	}
	if requested.Backend != isolation.BackendWorktree {
		factory := opts.furrowFactory
		if factory == nil {
			factory = defaultFurrowFactory
		}
		adapter := factory()
		cowSupported := false
		if !requiresMerge && adapter != nil {
			probe := opts.CowProbe
			if probe == nil {
				probe = func() bool { return isolation.ProbeCowSupport() }
			}
			cowSupported = probe()
		}
		decision := isolation.DecideIsolation(isolation.IsolationOptions{
			Requested:        requested.Backend,
			RequiresMerge:    requiresMerge,
			AdapterAvailable: adapter != nil,
			CowSupported:     cowSupported,
		})
		if decision.Backend == isolation.BackendFurrow {
			forked := adapter.Fork(workspace, "plandb-"+taskID)
			if forked.Path != "" {
				if isolationDecisionLogged.markIfAbsent(workspace) {
					schedulerLog.Info("isolation backend selected", map[string]any{
						"backend":   "furrow",
						"requested": requested.Backend,
					})
				}
				schedulerLog.Info("furrow fork allocated", map[string]any{
					"taskID":  taskID,
					"path":    forked.Path,
					"baseSha": prefixBytes(baseSHA, 8),
				})
				return &WorktreeAllocation{
					Path:    forked.Path,
					BaseSHA: baseSHA,
					BaseRef: baseRef,
					Backend: WorktreeBackendFurrow,
				}
			}
			schedulerLog.Warn("furrow fork failed at runtime — falling back to git worktree", map[string]any{
				"taskID": taskID,
				"error":  prefixUTF16(forked.Error, 300),
			})
		} else if isolationDecisionLogged.markIfAbsent(workspace) {
			fields := map[string]any{
				"backend":   "worktree",
				"requested": requested.Backend,
				"reason":    decision.Reason,
			}
			if requested.Backend == isolation.BackendFurrow {
				schedulerLog.Warn("isolation backend selected", fields)
			} else {
				schedulerLog.Info("isolation backend selected", fields)
			}
		}
	}

	leafoutcome.PreserveRejectedWork(leafoutcome.PreserveRejectedWorkArgs{
		Workspace: workspace,
		Worktree:  worktreePath,
		TaskID:    taskID,
	})
	runner.Run(ctx, []string{"git", "worktree", "remove", "--force", worktreePath}, runOptions{Cwd: workspace})
	runner.Run(ctx, []string{"git", "branch", "-D", branch}, runOptions{Cwd: workspace})
	created := runner.Run(
		ctx,
		[]string{"git", "worktree", "add", "-b", branch, worktreePath, baseRef},
		runOptions{Cwd: workspace},
	)
	if created.Code != 0 {
		return nil
	}
	_ = hygiene.EnsureCodeafExcluded(ctx, worktreePath)
	_ = hygiene.SuppressCaseCollisions(ctx, worktreePath)
	return &WorktreeAllocation{
		Path:    worktreePath,
		BaseSHA: baseSHA,
		BaseRef: baseRef,
		Backend: WorktreeBackendGit,
	}
}

type gcWorktreeOptions struct {
	runner commandRunner
}

// gcStaleWorktrees mirrors plandb-scheduler.ts:1131-1174, including the kept
// wrong-database shellout. The workspace is marked before any I/O, so even a
// failed sweep is never retried in this process.
func gcStaleWorktrees(
	ctx context.Context,
	workspace string,
	dbPath string,
	projectID string,
	options ...gcWorktreeOptions,
) int {
	if !gcSweptWorkspaces.markIfAbsent(workspace) {
		return 0
	}
	runner := commandRunner(osCommandRunner{})
	if len(options) > 0 && options[0].runner != nil {
		runner = options[0].runner
	}
	return sweepWorktrees(ctx, workspace, dbPath, projectID, runner, true, false)
}

// ForceSweepWorktrees reclaims every scheduler worktree after dispatch has
// quiesced. Unlike the cycle-time GC, teardown is neither latched nor filtered
// by task liveness.
func ForceSweepWorktrees(
	ctx context.Context,
	workspace string,
	dbPath string,
	projectID string,
) int {
	return sweepWorktrees(ctx, workspace, dbPath, projectID, osCommandRunner{}, false, true)
}

func sweepWorktrees(
	ctx context.Context,
	workspace string,
	dbPath string,
	projectID string,
	runner commandRunner,
	filterLive bool,
	preserveCommitted bool,
) int {
	plandbDir := filepath.Join(workspace, ".plandb")
	directory, err := os.Open(plandbDir)
	if err != nil {
		return 0
	}
	names, err := directory.Readdirnames(-1)
	_ = directory.Close()
	if err != nil {
		return 0
	}
	worktreeNames := make([]string, 0, len(names))
	for _, name := range names {
		if strings.HasPrefix(name, "wt-") {
			worktreeNames = append(worktreeNames, name)
		}
	}
	if len(worktreeNames) == 0 {
		return 0
	}

	liveTaskIDs := map[string]struct{}{}
	if filterLive {
		argv := []string{"plandb", "list", "--json"}
		if projectID != "" {
			argv = append(argv, "--project", projectID)
		}
		list := runner.Run(ctx, argv, runOptions{
			Cwd: workspace,
			Env: map[string]string{"PLANDB_DB": dbPath},
		})
		liveTaskIDs = liveTaskSet(list.Stdout)
	}
	cleaned := 0
	for _, name := range worktreeNames {
		taskID := name[3:]
		if _, live := liveTaskIDs[taskID]; live {
			continue
		}
		worktreePath := filepath.Join(plandbDir, name)
		var baseSHA *string
		if preserveCommitted {
			base := runner.Run(ctx,
				[]string{"git", "merge-base", "HEAD", "refs/heads/plandb/" + taskID},
				runOptions{Cwd: workspace},
			)
			if value := jscompat.Trim(string(base.Stdout)); base.Code == 0 && value != "" {
				baseSHA = &value
			}
		}
		leafoutcome.PreserveRejectedWork(leafoutcome.PreserveRejectedWorkArgs{
			Workspace: workspace,
			Worktree:  worktreePath,
			TaskID:    taskID,
			BaseSha:   baseSHA,
		})
		runner.Run(ctx, []string{"git", "worktree", "remove", "--force", worktreePath}, runOptions{Cwd: workspace})
		runner.Run(ctx, []string{"git", "branch", "-D", "plandb/" + taskID}, runOptions{Cwd: workspace})
		_ = os.RemoveAll(worktreePath)
		cleaned++
	}
	return cleaned
}

func prefixBytes(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func prefixUTF16(value string, limit int) string {
	units := utf16.Encode([]rune(value))
	if len(units) <= limit {
		return value
	}
	return string(utf16.Decode(units[:limit]))
}

func liveTaskSet(stdout []byte) map[string]struct{} {
	var tasks []struct {
		ID     any    `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(jscompat.Trim(string(stdout))), &tasks); err != nil {
		return map[string]struct{}{}
	}
	live := map[string]struct{}{}
	for _, task := range tasks {
		switch task.Status {
		case "pending", "ready", "claimed", "running":
		default:
			continue
		}
		id, ok := task.ID.(string)
		if ok {
			live[id] = struct{}{}
		}
	}
	return live
}
