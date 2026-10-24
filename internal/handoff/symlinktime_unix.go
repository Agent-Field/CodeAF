//go:build !windows

package handoff

import (
	"time"

	"golang.org/x/sys/unix"
)

// setSymlinkTime preserves the link itself without touching its target.
func setSymlinkTime(path string, at time.Time) bool {
	ts := unix.NsecToTimespec(at.UnixNano())
	return unix.UtimesNanoAt(unix.AT_FDCWD, path, []unix.Timespec{ts, ts}, unix.AT_SYMLINK_NOFOLLOW) == nil
}
