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

	"github.com/Agent-Field/codeaf/internal/gitpolicy"
)

const (
	Name  = "codeaf"
	Email = "agentfield-bot@users.noreply.github.com"
)

// ConfigArgs supplies the identity for commits codeaf creates.
func ConfigArgs() []string {
	return []string{"-c", "user.name=" + Name, "-c", "user.email=" + Email}
}

// PrivateArgs keeps commands in disposable copies from leaving a detached Git
// maintenance process writing after their owner starts walking or removing them.
// Callers must keep the person's working repository on its ordinary Git path.
func PrivateArgs(args ...string) []string {
	return gitpolicy.WithoutAutoMaintenance(args...)
}

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
