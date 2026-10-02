//go:build !windows

// What codeaf froze about a run's folder before it started
package util

import (
	"fmt"
	"os"
	"strings"

	"github.com/Agent-Field/codeaf/internal/gitidentity"
)

// SENIOR-DEV MAKES NO COMMIT ON THE RUN'S BRANCH. Until 2026-09-30 every file
// its model wrote or edited was committed there as it happened
// (`wip(write): <path>`, `wip(edit): <path>`, under senior-dev's own name), a
// checkpoint from the benchmark harness it was built in. Under codeaf it bought
// nothing: the run works in a private copy on a branch of its own, its
// recorder keeps its own snapshots and candidates outside that branch, and
// codeaf commits what the run leaves as one commit when it ends, crash or
// not. What it did cost was a pull request carrying fifty `wip` commits with
// senior-dev as an author, every line of which a squash merge wrote into the
// trunk's history. What is left here are the lists codeaf froze at the start,
// which the recorders still read.

// InitialInputs reads the list of inputs codeaf copied into the run's copy,
// with their fingerprints; none for a run started any other way.
func InitialInputs() (gitidentity.Inputs, error) {
	inputs, err := gitidentity.ReadInputs(os.Getenv(gitidentity.InputsEnv))
	if err != nil {
		return nil, fmt.Errorf("read the copy's inputs: %w", err)
	}
	return inputs, nil
}

// InitialIgnoredPaths reads the one list codeaf captured before the run.
func InitialIgnoredPaths() ([]string, error) {
	list := os.Getenv("SENIOR_DEV_IGNORED_AT_START")
	if list == "" {
		return nil, nil
	}
	body, err := os.ReadFile(list)
	if err != nil {
		return nil, fmt.Errorf("read start-time ignore list: %w", err)
	}
	return strings.Split(string(body), "\x00"), nil
}

// PathIgnoredAtStart matches a file or a child of an ignored directory.
func PathIgnoredAtStart(path string, paths []string) bool {
	for _, ignored := range paths {
		if ignored != "" && (path == ignored || strings.HasPrefix(path, strings.TrimSuffix(ignored, "/")+"/")) {
			return true
		}
	}
	return false
}

// GeneratedRunPaths and GeneratedRunPath are internal/gitidentity's, where
// codeaf's own side of a run reads them on every platform.
const GeneratedRunPaths = gitidentity.GeneratedRunPaths

func GeneratedRunPath(path string) bool { return gitidentity.GeneratedRunPath(path) }
