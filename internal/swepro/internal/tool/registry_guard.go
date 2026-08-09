package tool

import (
	"context"
	"path/filepath"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
)

func (r *Registry) planDBContext(ctx context.Context, call steploop.ToolCall) PlanDBGuardContext {
	messages := []string{}
	for _, message := range steploop.ToolMessagesFromContext(ctx) {
		for _, part := range message.Parts {
			if text, ok := part.(msgmodel.TextPart); ok {
				messages = append(messages, text.Text)
			}
		}
	}
	worktree := r.workDir
	if r.instance != nil && r.instance.Worktree != "" {
		worktree = filepath.Clean(r.instance.Worktree)
	}
	return PlanDBGuardContext{
		Messages: messages, SessionID: call.SessionID, MessageID: call.MessageID,
		Agent: call.Agent, Directory: r.workDir, Worktree: worktree,
	}
}

func (r *Registry) worktree() string {
	if r.instance != nil && r.instance.Worktree != "" {
		return filepath.Clean(r.instance.Worktree)
	}
	return r.workDir
}

func (r *Registry) guardMutation(ctx context.Context, call steploop.ToolCall, paths []string) *PlanDBPackageBinding {
	return EnsurePlanDBMutationPackage(r.planDBContext(ctx, call), paths, r.planRun, r.planActive)
}

func (r *Registry) guardShell(ctx context.Context, call steploop.ToolCall, command string) *PlanDBPackageBinding {
	return EnsurePlanDBShellPackage(r.planDBContext(ctx, call), command, r.planRun, r.planActive)
}
