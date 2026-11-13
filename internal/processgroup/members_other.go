//go:build !windows && !linux && !darwin

package processgroup

// groupHasLiveMember retains the signal probe on other Unix platforms: the
// shell is reaped before any sweep looks at its group (internal/exec,
// shellwait_other.go), so no zombie leader is in the group when this is asked,
// and kill(-pgid, 0) answers correctly.
func groupHasLiveMember(pgid int) bool {
	return Alive(pgid)
}

// Members is unknown off Linux and macOS; callers fall back to what they started.
func Members(pgid int) []int { return nil }
