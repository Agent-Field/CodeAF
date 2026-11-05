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
