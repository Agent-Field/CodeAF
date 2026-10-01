package handoff

import "golang.org/x/sys/unix"

// cloneTreeOnPlatform clones the tree with APFS's clonefile, which copies a
// folder and everything in it by reference. CLONE_NOFOLLOW keeps a link at the
// top from being followed out of the tree. It fails across volumes.
func cloneTreeOnPlatform(src, dst string) error {
	return unix.Clonefile(src, dst, unix.CLONE_NOFOLLOW)
}
