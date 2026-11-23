//go:build !linux && !darwin

package processgroup

// Usage has no answer where the subtree's CPU time and process count cannot be
// read. A bound that cannot say never cuts, preserving the existing scheduler
// rather than guessing at somebody else's process group.
func (g Group) Usage() (Usage, bool) { return Usage{}, false }
