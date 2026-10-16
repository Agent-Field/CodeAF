//go:build linux

package service

import "golang.org/x/sys/unix"

// waitExited blocks until pid has exited WITHOUT reaping it. The leader stays a
// zombie that still holds its pid and its start identity, so its group can be
// swept through the identity gate before anything lets the number go.
func waitExited(pid int) {
	var info unix.Siginfo
	for {
		err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if err != unix.EINTR {
			return
		}
	}
}
