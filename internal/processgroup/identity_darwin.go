//go:build darwin

package processgroup

import "golang.org/x/sys/unix"

// A kernel start timestamp distinguishes recycled pids without launching ps.
// Keep its microseconds: lstart's seconds can identify two different processes
// as the same one if the pid is reused within that second.
func readProcessStart(pid int) (uint64, bool) {
	if pid <= 0 {
		return 0, false
	}
	p, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || p == nil || int(p.Proc.P_pid) != pid {
		return 0, false
	}
	t := p.Proc.P_starttime
	if t.Sec <= 0 || t.Usec < 0 || t.Usec >= 1_000_000 {
		return 0, false
	}
	return uint64(t.Sec)*1_000_000 + uint64(t.Usec), true
}
