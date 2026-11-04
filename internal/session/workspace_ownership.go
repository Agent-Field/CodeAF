package session

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/home"
)

// WorkspaceOwnershipPrototypeEnabled requires an explicit experiment switch,
// an absolute profile, and support for applying the retained output.
func WorkspaceOwnershipPrototypeEnabled() bool {
	return workspaceApplicationSupported && env.Get("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP") == "1" && filepath.IsAbs(strings.TrimSpace(env.Get(home.EnvVar)))
}

// ownedRunPlacement puts delegated tasks on the same durable layout road
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
	redirect := "The task works in its own private copy of " + dir + "; review and apply its results with /task review and /task apply."
	if mode == TaskModeReference {
		redirect = homeTaskRedirect(dir)
	}
	return taskStand{dir: dir, mode: mode, rung: taskGroundNamed, redirect: redirect}, true
}

// Home is a reference, not a project snapshot. A general task must not copy a
// person's unrelated files merely because the conversation was opened there.
func ownershipPlainMode(dir string) TaskMode {
	if HoldsHomeFolder(dir) {
		return TaskModeReference
	}
	return TaskModeMirror
}

// neverMirrorHome is the same rule for a stand that already carries a mode: a
// mirror of a folder holding the home folder reads it instead of copying it.
func neverMirrorHome(ground string, mode TaskMode) TaskMode {
	if mode == TaskModeMirror && HoldsHomeFolder(ground) {
		return TaskModeReference
	}
	return mode
}

// homeTaskRedirect is what a person reads when a task named a folder holding home.
func homeTaskRedirect(dir string) string {
	return "The task reads " + dir + " and keeps what it makes in its own folder; nothing in " + dir + " changes."
}

// withHomeReadingNote is [taskCopy.ownFolderNote] for a run, whose workers learn
// their folder as the assigned working directory and read the run's words.
func withHomeReadingNote(brief string, stand taskStand) string {
	if stand.mode != TaskModeReference || !HoldsHomeFolder(stand.dir) {
		return brief
	}
	return brief + "\n\n" + cleanFolder(stand.dir) + " is yours to read, not to change. Everything you make goes in your assigned working directory. In your report, say where in that folder the results are."
}
