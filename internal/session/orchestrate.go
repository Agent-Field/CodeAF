package session

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/orchestrate"
)

// This file is the CONTRACT STUB for the adaptive-run seam: the shapes every
// surface codes against while the core lands. The orchestrate package owns
// the shapes; this file owns the session's doorways into a run. Bodies here
// are placeholders the core slice replaces — the signatures are the promise.

// ResolveOrchestrate answers one EventOrchestratePause: "topup:<dollars>"
// resumes with a raised cap, "finish" jumps to synthesis over partial
// results, "stop" settles the run with its partial trace.
func (a *Agent) ResolveOrchestrate(id, answer string) (string, error) {
	return "", nil
}

// OrchestrateSnapshot is the room's poll: the run's latest published shape,
// or false when the id names no run this session knows.
func (a *Agent) OrchestrateSnapshot(id string) (orchestrate.Snapshot, bool) {
	return orchestrate.Snapshot{}, false
}

// SteerOrchestrate appends one steering note; the planner sees it on its next
// call. Steering outranks the plan.
func (a *Agent) SteerOrchestrate(id, text string) error { return nil }

// startOrchestrate launches one adaptive run for goal on a fuel cap, through
// the runner the config was handed (Config.OrchestrateRunner). NIL RUNNER IS
// ORCHESTRATION OFF, the same posture RunHarness keeps one seam over.
func (a *Agent) startOrchestrate(ctx context.Context, goal, model string, capDollars float64) (string, error) {
	run := a.config.OrchestrateRunner
	if run == nil {
		return "", nil
	}
	return run(ctx, goal, model, capDollars)
}

// WorktreePath resolves where job id's isolated worktree would live. Empty
// root means empty path, and an empty path means the node shares the
// workspace — the planner's worktree flag degrades, it never errors.
func (c Config) WorktreePath(jobID string) string {
	if c.WorktreeRoot == "" {
		return ""
	}
	clean := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		return '-'
	}, strings.ToLower(jobID))
	return filepath.Join(c.WorktreeRoot, clean)
}
