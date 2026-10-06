package main

import (
	"os"
	"strings"

	"github.com/Agent-Field/codeaf/internal/config"
)

// v3SavedView is only consulted after the far-machine doors have forked.
// Explicit requests and headless work always keep their own session choice.
func v3SavedView(profileDir, launchDir, explicit, once string, pick bool) config.ViewState {
	if strings.TrimSpace(explicit) != "" || strings.TrimSpace(once) != "" || pick {
		return config.ViewState{}
	}
	view, ok := config.ViewStateAt(profileDir, launchDir)
	if !ok {
		return config.ViewState{}
	}
	transcript, err := os.Stat(view.Session)
	if err != nil || !transcript.Mode().IsRegular() {
		return config.ViewState{}
	}
	workspace, err := os.Stat(view.Workspace)
	if err != nil || !workspace.IsDir() {
		return config.ViewState{}
	}
	return view
}
