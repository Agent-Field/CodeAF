package session

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// nonWorkspaceTool reports whether name is a pure harness or web tool that
// never reads, writes, or executes in the workspace folder.
func nonWorkspaceTool(name string) bool {
	switch name {
	case "manual", "settings", "change_setting", "web_search", "web_fetch", "memory_search", "memory_save", "conversations", "start_chat":
		return true
	}
	return false
}

// waitWorkspaceReady blocks workspace tools until any background workspace
// fetch for this chat has completed and put the files in place (C3, C4).
func (a *Agent) waitWorkspaceReady(ctx context.Context, toolName string) error {
	if a == nil || nonWorkspaceTool(toolName) {
		return nil
	}
	return cell.WaitWorkspace(ctx, a.arrivalPaths()...)
}

// arrivalPaths are the places a background workspace arrival for this chat is
// filed under: the gate and the prompt ask about the same ones.
func (a *Agent) arrivalPaths() []string {
	var sessionDir string
	if a.config.SessionFile != "" {
		sessionDir = DirOf(a.config.SessionFile)
	}
	return []string{a.config.Place.Dir, a.config.Workspace, sessionDir}
}

// followArrivedFiles re-renders the system prompt once the files of a moved
// chat have finished arriving, before the model is asked again.
//
// A CHAT THAT MOVED HERE OPENS BEFORE ITS FILES DO. The agent is built, and its
// instructions rendered, at the open, when the workspace is still empty; the
// project's own rules (AGENTS.md, CLAUDE.md) are read from the workspace at that
// render, so a chat that opened mid-arrival would go on working without them
// until the clock's refresh, ten minutes on ([clockRefresh]). The gate above
// holds back tools until the files land; this is the same moment for the words:
// the first request after the arrival completes is the first to carry them.
//
// IT TOUCHES NOTHING FOR A CHAT THAT NEVER WAITED ON FILES: the flag is set only
// by an arrival this agent saw begun and not yet complete, so an ordinary chat
// pays one lookup per request and its prompt stays byte-identical. The page is
// re-rendered at the moment it was last rendered ([Agent.followModelOnThePageLocked]),
// so the only bytes that move are the ones the files changed; a prompt a caller
// handed in is left alone, as everywhere else.
func (a *Agent) followArrivedFiles() {
	if a == nil {
		return
	}
	arriving := cell.WorkspaceArriving(a.arrivalPaths()...)
	a.mu.Lock()
	defer a.mu.Unlock()
	if arriving {
		a.filesArriving = true
		return
	}
	if !a.filesArriving {
		return
	}
	a.filesArriving = false
	a.followModelOnThePageLocked()
}
