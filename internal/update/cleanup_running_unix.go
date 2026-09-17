//go:build !windows

package update

// CleanupOldRunning does nothing where replacing a running executable leaves
// no backup. In particular, it does not resolve the executable during launch.
func CleanupOldRunning(func() (string, error)) {}
