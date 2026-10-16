//go:build darwin

package service

import (
	"time"

	"golang.org/x/sys/unix"
)

// sZomb is Darwin's p_stat for an exited, unreaped process.
const sZomb = 5

// waitExited blocks until pid has exited WITHOUT reaping it, through the
// kernel's exit note: Darwin has no waitid(WNOWAIT) that Go can reach, and
// kqueue reports the exit while the leader is still an unreaped zombie holding
// its pid and its start identity. A leader that exited before the note was
// registered answers ESRCH — it is a zombie already, and nobody but this
// process reaps it, so the number is still held.
//
// IT NEVER RETURNS WHILE THE LEADER RUNS, because its caller reaps under the
// lock every signal takes. A kqueue that cannot be had at all — descriptors
// exhausted — falls back to looking at the process's state until it is a
// zombie, which is slower and still never reaps.
func waitExited(pid int) {
	if kq, err := unix.Kqueue(); err == nil {
		defer unix.Close(kq)
		var change unix.Kevent_t
		unix.SetKevent(&change, pid, unix.EVFILT_PROC, unix.EV_ADD|unix.EV_ONESHOT)
		change.Fflags = unix.NOTE_EXIT
		events := make([]unix.Kevent_t, 1)
		for {
			_, err := unix.Kevent(kq, []unix.Kevent_t{change}, events, nil)
			if err == nil || err == unix.ESRCH {
				return
			}
			if err != unix.EINTR {
				break
			}
		}
	}
	for {
		p, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
		if err != nil || p == nil || int(p.Proc.P_pid) != pid || p.Proc.P_stat == sZomb {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}
