//go:build !windows

package app

// Git helpers shared by the submit freeze, the ship decision and the
// unsubmitted-tree finalizer.

// countFailingEntrypoints counts the entrypoints whose command exited
// non-zero.
func countFailingEntrypoints(result projectVerificationResult) int {
	failing := 0
	for _, command := range result.Commands {
		evidence, ok := command.(map[string]any)
		if !ok {
			continue
		}
		if exit, _ := evidence["exit"].(float64); exit != 0 {
			failing++
		}
	}
	return failing
}

// workspaceGit runs git in the workspace with the committer identity pinned,
// the same way eager-commit does. Without it `commit-tree` dies with "Author
// identity unknown" in a container that has no git config -- and commit-tree
// is how submit records the frozen candidate, so the run could not submit at
// all. The identity flags are `-c` overrides, which git ranks below
// GIT_COMMITTER_*/GIT_AUTHOR_*, so an environment that sets those still wins.
// currentTreeSHA identifies the working tree as it stands. It is a thin name
// for the recorder's promise, kept because the run reads better saying what it
// wants than naming the thing that provides it.
func (runner *pipeline) currentTreeSHA() (string, error) {
	return runner.recorder.Snapshot()
}

// rememberVerifiedTree caches the last completed full verification against
// the git tree it measured, so the finalizer can consult the last verdict on
// an unchanged tree without re-verifying. The runs that need the dead-tree
// check end with no wall left to verify anything.
func (runner *pipeline) rememberVerifiedTree(result projectVerificationResult) {
	if result.TimedOut || len(result.Commands) == 0 {
		return
	}
	treeSHA, err := runner.currentTreeSHA()
	if err != nil {
		return
	}
	remembered := result
	runner.lastVerify = &remembered
	runner.lastVerifyTreeSHA = treeSHA
}
