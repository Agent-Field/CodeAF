package util

import (
	"path/filepath"
	"strings"
)

// THE FACTS ABOUT A RUN THAT CODEAF ITSELF READS BUILD ON EVERY PLATFORM.
//
// Everything else in this package is the engine's own, and the engine does not
// build on Windows. codeaf's side of a run (internal/session's program folder)
// still needs to know which paths a run's tests leave behind and which identity
// the engine commits under, on every platform codeaf ships for, so those two
// facts live here, in the one file of this package without a build constraint.

// The identity senior-dev's own commits carry (gitidentity.go says why it has one).
const (
	CommitterName  = "senior-dev"
	CommitterEmail = "senior-dev@localhost"
)

// GeneratedRunPaths is the one narrow list of test droppings this run is
// known to create. A general guess would hide a person's actual deliverable.
const GeneratedRunPaths = "__pycache__/,.pytest_cache/,*.pyc"

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
