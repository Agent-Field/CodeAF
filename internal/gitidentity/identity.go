// Package gitidentity holds what a run's commits carry and leave out: the
// signature shared by codeaf's own commits and the commands a program runs on
// its behalf, the identity senior-dev's engine commits under, and the caches a
// run's own tests leave behind.
//
// IT BUILDS ON EVERY PLATFORM. internal/seniordev does not build on Windows
// (its own law says so), and codeaf's side of a run still has to know these
// facts there, so they live here rather than in the engine.
package gitidentity

import (
	"path/filepath"
	"strings"
)

const (
	Name  = "codeaf"
	Email = "agentfield-bot@users.noreply.github.com"
)

// Environment makes Git attribute a model-written commit to the run even
// when the repository carries the person's user.name and user.email.
func Environment() []string {
	return []string{
		"GIT_AUTHOR_NAME=" + Name,
		"GIT_AUTHOR_EMAIL=" + Email,
		"GIT_COMMITTER_NAME=" + Name,
		"GIT_COMMITTER_EMAIL=" + Email,
	}
}

// The identity senior-dev's engine commits under: its starting tree, every
// write it checkpoints and the candidate it submits (internal/seniordev/util
// says why it carries one of its own).
const (
	EngineName  = "senior-dev"
	EngineEmail = "senior-dev@localhost"
)

// GeneratedRunPaths is the one narrow list of test droppings a run is known to
// create. A general guess would hide a person's actual deliverable.
const GeneratedRunPaths = "__pycache__/,.pytest_cache/,*.pyc"

// GeneratedRunPath reports whether a path is one of [GeneratedRunPaths].
func GeneratedRunPath(path string) bool {
	for _, pattern := range strings.Split(GeneratedRunPaths, ",") {
		if strings.HasPrefix(pattern, "*.") {
			if strings.HasSuffix(path, strings.TrimPrefix(pattern, "*")) {
				return true
			}
			continue
		}
		for _, part := range strings.Split(filepath.ToSlash(path), "/") {
			if part == strings.TrimSuffix(pattern, "/") {
				return true
			}
		}
	}
	return false
}
