package session

// An owned conversation starts in aforge's scratch directory, not in a project.
// This file is the one door that turns it into a conversation about a project:
// both the model's `workspace` tool and the surface's `/workspace` command call
// [Agent.AnchorWorkspace], so persistence, prompt replacement and belt rebuilding
// cannot drift into two implementations.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

const anchorWorkspaceDescription = "Anchor this project-less conversation to the named repository or folder."

var anchorWorkspaceSchema = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Named path"}},"required":["path"]}`)

func (a *Agent) anchorWorkspaceTools() []bare.Tool {
	if a.config.InTask || !a.config.Place.Owned {
		return nil
	}
	return []bare.Tool{{
		Name:        "workspace",
		Description: anchorWorkspaceDescription,
		Schema:      anchorWorkspaceSchema,
		Execute: func(_ context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Path string `json:"path"`
			}
			if err := decodeToolArguments(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			path, err := a.AnchorWorkspace(parsed.Path)
			if err != nil {
				return "Could not set the workspace: " + err.Error(), true, nil
			}
			return "Workspace set to " + path, false, nil
		},
	}}
}

// AnchorWorkspace changes an owned conversation into a borrowed one rooted at
// path. It is deliberately unavailable after the first successful anchor: a
// project-less conversation may acquire its subject, but an ordinary project
// conversation does not silently become a different project midway through.
func (a *Agent) AnchorWorkspace(path string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.config.Place.Owned {
		return "", fmt.Errorf("this conversation already has a workspace")
	}
	resolved, err := resolveWorkspaceAnchor(path, a.config.Workspace)
	if err != nil {
		return "", err
	}

	place := a.config.Place
	place.Workspace = resolved
	place.Owned = false
	if strings.TrimSpace(place.Dir) != "" {
		meta, loadErr := LoadMeta(place.Dir)
		if loadErr != nil {
			return "", fmt.Errorf("read conversation place: %w", loadErr)
		}
		meta.Workspace = resolved
		meta.Owned = false
		if saveErr := SaveMeta(place.Dir, meta); saveErr != nil {
			return "", fmt.Errorf("save conversation place: %w", saveErr)
		}
	}

	// The config is the source every future worker copies. Rebuild the hands as
	// well as the words: bare tools close over their workspace when the belt is
	// assembled, so changing only Config would leave their cwd behind.
	a.config.Workspace = resolved
	a.config.Place = place
	tools := a.belt()
	definitions, err := toolDefinitions(tools)
	if err != nil {
		return "", err
	}
	a.tools = tools
	a.definitions = definitions
	if a.jobs != nil {
		a.jobs.mu.Lock()
		a.jobs.workspace = resolved
		a.jobs.place = place
		a.jobs.mu.Unlock()
	}
	if a.systemOwn {
		a.system = renderSystemAt(a.config, time.Now())
		a.systemAt = time.Now()
		a.refreshSystemLocked()
	}
	return resolved, nil
}

func resolveWorkspaceAnchor(path, relativeTo string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		path = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), string(filepath.Separator)))
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(relativeTo, path)
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", path)
	}
	if root, ok := repositoryRoot(path); ok {
		return root, nil
	}
	return filepath.Clean(path), nil
}
