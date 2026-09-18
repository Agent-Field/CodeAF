//go:build !windows && !linux

package processgroup

// groupHasLiveMember is the signal probe off Linux: there the shell is reaped
// before any sweep looks at its group (internal/exec, shellwait_other.go), so
// no zombie leader is ever in the group when this is asked, and kill(-pgid, 0)
// answers correctly.
func groupHasLiveMember(pgid int) bool {
	return Alive(pgid)
}
