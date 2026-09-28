//go:build !windows

package fullverification

import "testing"

// A command whose executed portion does nothing must never be accepted as
// build or test evidence, whatever a trailing comment claims: `true # pytest`
// exits 0 and runs no tests, and a substring classifier that saw "pytest"
// would record a green suite.
func TestNoOpCommandsAreNeverEvidence(t *testing.T) {
	for _, command := range []string{
		"true # pytest",
		"true # npm test",
		": # go test ./...",
		`echo "go build"`,
		"echo go test ./...",
		"true # go build ./...",
		"  true   #   cargo test  ",
	} {
		if isTestCommand(command) {
			t.Errorf("no-op accepted as TEST evidence: %q", command)
		}
		if isBuildCommand(command) {
			t.Errorf("no-op accepted as BUILD evidence: %q", command)
		}
	}
	// Real commands must still classify, including with a trailing comment.
	for _, command := range []string{
		"go test ./...",
		"go test ./... # run the suite",
		"pytest -q",
		"npm test",
	} {
		if !isTestCommand(command) {
			t.Errorf("real test command rejected: %q", command)
		}
	}
	for _, command := range []string{"go build ./...", "npm run build", "tsc --noEmit"} {
		if !isBuildCommand(command) {
			t.Errorf("real build command rejected: %q", command)
		}
	}
}
