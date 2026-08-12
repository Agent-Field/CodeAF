//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

// aforge-embed: D6 — the volume measurement behind the disk-floor cap.
package scheduler

import "golang.org/x/sys/unix"

// volumeTotalBytes is the size of the volume containing path. Block counts and
// block sizes are differently typed per platform, so both are widened before
// the multiply.
func volumeTotalBytes(path string) (float64, bool) {
	var stats unix.Statfs_t
	if err := unix.Statfs(path, &stats); err != nil {
		return 0, false
	}
	return float64(stats.Blocks) * float64(stats.Bsize), true
}
