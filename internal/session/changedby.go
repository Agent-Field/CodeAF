package session

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// pathWriters are the tools whose whole effect is on the one file their `path`
// argument names. A tool absent from this set may touch anything (bash above
// all), and its calls leave the changed paths unknown.
var pathWriters = map[string]bool{"edit": true, "write": true}

// changedBy is the workspace-relative paths a tool call is known to change,
// or nil when that is not known.
func (a *Agent) changedBy(tool string, args json.RawMessage) []string {
	if !pathWriters[tool] {
		return nil
	}
	var parsed struct {
		Path string `json:"path"`
	}
	if decodeToolArguments(args, &parsed) != nil || strings.TrimSpace(parsed.Path) == "" {
		return nil
	}
	return relativeTo(a.config.Workspace, bare.ResolvePath(parsed.Path, a.config.Workspace))
}

// relativeTo names path relative to root, as a one-element list, or nil when the
// path lies outside root and the workspace seal cannot vouch for it.
func relativeTo(root, path string) []string {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil
	}
	return []string{filepath.ToSlash(rel)}
}
