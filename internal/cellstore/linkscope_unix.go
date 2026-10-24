//go:build !windows

package cellstore

// needsLinkScope leaves Unix link expansion to the engine's token checks.
func needsLinkScope(_ string, _ []string) bool { return false }
