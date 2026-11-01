//go:build unix

package diskspace

import "golang.org/x/sys/unix"

func of(path string) (Usage, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return Usage{}, err
	}
	size := uint64(st.Bsize)
	return Usage{FreeBytes: uint64(st.Bavail) * size, TotalBytes: uint64(st.Blocks) * size}, nil
}
