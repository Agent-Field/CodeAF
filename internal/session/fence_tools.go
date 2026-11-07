package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	procexec "github.com/Agent-Field/codeaf/internal/executor"
)

// fencedFileTools are the session's tools that open a file inside codeaf's
// own process, and whether each one writes. Every other tool that touches the
// disk does it through a command, which the jail holds to the fence.
var fencedFileTools = map[string]bool{"read": false, "write": true, "edit": true}

// fencedFileTool is the refusal for a file tool call the fence does not allow,
// "" when it is allowed or nothing is fenced. The path is read the way the
// tools read it: relative to the workspace, `~/` the home folder.
func fencedFileTool(fence *procexec.Fence, workspace, tool string, args json.RawMessage) string {
	write, ok := fencedFileTools[tool]
	if fence == nil || !ok {
		return ""
	}
	var call struct {
		Path     string `json:"path"`
		FilePath string `json:"file_path"`
	}
	if json.Unmarshal(args, &call) != nil {
		return ""
	}
	path := strings.TrimSpace(call.Path)
	if path == "" {
		path = strings.TrimSpace(call.FilePath)
	}
	if path == "" {
		return ""
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspace, path)
	}
	if fence.Allows(path, write) {
		return ""
	}
	return procexec.FenceLine
}
