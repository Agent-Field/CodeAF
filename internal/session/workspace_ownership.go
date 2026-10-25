package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/home"
)

// WorkspaceOwnershipPrototypeEnabled retains the explicit legacy experiment
// switch. Production placement reads each conversation's persisted policy.
func WorkspaceOwnershipPrototypeEnabled() bool {
	return env.Get("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP") == "1" && filepath.IsAbs(strings.TrimSpace(env.Get(home.EnvVar)))
}

// ownedRunPlacement puts ordinary Bash tasks on the same durable layout road
// as explicitly selected roots. A placement names input, never landing consent.
// Reference tasks retain their empty scratch and borrowed context; in particular
// opening a conversation at home must not copy the person's home directory.
func (a *Agent) ownedRunPlacement(ctx context.Context, stand taskStand) (taskStand, error) {
	if !WorkspaceOwnershipEnabled(a.config.Place) || !bashBeltAsked() || stand.workspaceSet != nil || stand.ask != "" || stand.refusal != "" || stand.dir == "" || stand.mode == TaskModeReference || stand.mode == TaskModeInPlace {
		return stand, nil
	}
	if place, found := a.knownPlace(stand.dir); found && TaskMode(place.Mode) == TaskModeInPlace {
		stand.mode = TaskModeInPlace
		return stand, nil
	}
	if ownershipPlainMode(stand.dir) == TaskModeReference {
		return stand, nil
	}
	request := &WorkspaceSetRequest{Primary: "work", Roots: []WorkspaceRootRequest{{Source: canonicalPath(stand.dir), Slot: "work", Access: WorkspaceWrite}}}
	if err := admitWorkspaceSetRequest(ctx, a.config.Place, request); err != nil {
		return stand, err
	}
	stand.workspaceSet = request
	return stand, nil
}

// ownershipPlacement reads a model's placement as the project to copy, never as
// the person's permission to write that project in place. A saved place mode
// remains the person's own choice through the existing place-selection door.
func (a *Agent) ownershipPlacement(spec taskSpec, workspace string) (taskStand, bool) {
	if !WorkspaceOwnershipEnabled(a.config.Place) || strings.TrimSpace(spec.where) == "" {
		return taskStand{}, false
	}
	dir := workspace
	if !strings.EqualFold(strings.TrimSpace(spec.where), "in place") {
		var err error
		dir, err = resolveTaskWhere(spec.where, workspace)
		if err != nil {
			return taskStand{refusal: "this task names a folder it cannot work in: " + spec.where}, true
		}
	}
	dir = groundRoot(dir)
	if place, found := a.knownPlace(dir); found && place.Mode != "" {
		return placeStand(place), true
	}
	mode := ownershipPlainMode(dir)
	if root, ok := repositoryRoot(dir); ok && hasCommit(root) {
		dir, mode = root, TaskModeWorktree
	}
	return taskStand{dir: dir, mode: mode, rung: taskGroundNamed,
		redirect: "The task works in its own copy of " + dir + "; its results come back through the task's normal landing."}, true
}

// Home is a reference, not a project snapshot. A general task must not copy a
// person's unrelated files merely because the conversation was opened there.
func ownershipPlainMode(dir string) TaskMode {
	if personHome, err := os.UserHomeDir(); err == nil && canonicalPath(personHome) == canonicalPath(dir) {
		return TaskModeReference
	}
	return TaskModeMirror
}
