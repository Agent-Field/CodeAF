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
	var sessionDir string
	if a.config.SessionFile != "" {
		sessionDir = DirOf(a.config.SessionFile)
	}
	return cell.WaitWorkspace(ctx, a.config.Place.Dir, a.config.Workspace, sessionDir)
}
