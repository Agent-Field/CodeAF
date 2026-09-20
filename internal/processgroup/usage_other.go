//go:build !linux

package processgroup

// Usage has no answer where there is no /proc to read: the subtree's CPU time
// and process count are Linux's to report here. A platform that cannot say
// answers so, and a bound that cannot say never cuts — it gets the scheduler it
// had before the bound existed rather than a guess at somebody else's process
// group.
func (g Group) Usage() (Usage, bool) { return Usage{}, false }
