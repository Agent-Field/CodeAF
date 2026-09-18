//go:build linux

package exec

import "golang.org/x/sys/unix"

// reapShell waits for the job's shell to exit and, when the job is not
// persistent, cleans up whatever it detached into its process group.
//
// ON LINUX THE SWEEP COMES BEFORE THE REAP. waitid(WNOWAIT) blocks until the
// shell has exited without reaping it, so the shell is still a zombie — its
// identity still readable from /proc, its pid still held, so the pgid cannot
// have been handed out again — when the detached sweep decides whether to
// signal. That is what lets the sweep refuse a recycled process group without
// simply abandoning every detached child. The shell is left a zombie for
// cmd.Wait, which follows immediately and reaps it.
func (r *jobRegistry) reapShell(job *backgroundJob) {
	var info unix.Siginfo
	_ = unix.Waitid(unix.P_PID, job.cmd.Process.Pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
	sweep := !r.markWaited(job)
	if sweep {
		terminateDetachedGroup(job.group)
	}
	_ = job.cmd.Wait()
}
