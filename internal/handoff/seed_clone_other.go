//go:build !darwin

package handoff

import "syscall"

// cloneTreeOnPlatform reports that this platform has no whole-tree clone, so a
// seed is made of hard links, which Linux makes at the same cost as any file
// creation and which the engine never writes through.
func cloneTreeOnPlatform(string, string) error { return syscall.ENOTSUP }
