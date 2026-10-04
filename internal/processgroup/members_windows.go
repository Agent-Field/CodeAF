//go:build windows

package processgroup

// Members is unknown on Windows; callers fall back to what they started.
func Members(pgid int) []int { return nil }
