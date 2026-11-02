//go:build !windows

package main

import "golang.org/x/sys/unix"

// folderRoom is whether this account may create files in dir and how many
// bytes its disk still offers this account. An unreadable disk reads as no
// room rather than a guess.
func folderRoom(dir string) (writable bool, free int64) {
	var st unix.Statfs_t
	if unix.Statfs(dir, &st) == nil {
		free = int64(st.Bavail) * int64(st.Bsize)
	}
	return unix.Access(dir, unix.W_OK) == nil, free
}
