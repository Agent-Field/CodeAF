//go:build darwin

package session

import "golang.org/x/sys/unix"

// cloneTree clones the folder source to target copy-on-write — one clonefile
// of the whole tree, which on APFS costs neither the space nor the time of a
// copy — and fails on a disk that cannot, leaving nothing at target.
func cloneTree(source, target string) error {
	return unix.Clonefile(source, target, unix.CLONE_NOFOLLOW)
}
