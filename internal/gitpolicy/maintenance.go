// Package gitpolicy holds the policy for git commands codeaf starts itself.
package gitpolicy

var noAutoMaintenance = []string{"-c", "maintenance.auto=false", "-c", "gc.auto=0"}

// WithoutAutoMaintenance prepends command-scoped settings so git cannot start
// maintenance that detaches and keeps writing after its caller has finished.
// The repository configuration stays unchanged for the user's own git calls.
func WithoutAutoMaintenance(args ...string) []string {
	out := make([]string, 0, len(noAutoMaintenance)+len(args))
	out = append(out, noAutoMaintenance...)
	return append(out, args...)
}

var attachedMaintenance = []string{"-c", "gc.autoDetach=false", "-c", "maintenance.autoDetach=false"}

// WithAttachedMaintenance keeps git's automatic maintenance but makes it finish
// inside the command. It is for a repository only codeaf commits to, such as the
// craft history, where nobody else's git would ever clean up: maintenance still
// runs, and no background git is left writing after the call returns.
func WithAttachedMaintenance(args ...string) []string {
	out := make([]string, 0, len(attachedMaintenance)+len(args))
	out = append(out, attachedMaintenance...)
	return append(out, args...)
}
