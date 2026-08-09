//go:build linux

// Real statfs measurement for src/session/resource-guard.ts:40-45.
package resourceguard

import "golang.org/x/sys/unix"

func defaultStatFS(path string) (StatFSResult, error) {
	var stats unix.Statfs_t
	if err := unix.Statfs(path, &stats); err != nil {
		return StatFSResult{}, err
	}
	return StatFSResult{
		Bavail: float64(stats.Bavail),
		Bsize:  float64(stats.Bsize),
	}, nil
}
