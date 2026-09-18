//go:build !linux

package exec

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// reapShell waits for the job's shell to exit and, when the job is not
// persistent, cleans up whatever it detached into its process group.
//
// OFF LINUX THERE IS NO WAIT-WITHOUT-REAP, so the shell is reaped first and the
// detached sweep has no identity to check. It keeps its old, unverified shape
// rather than leak every detached child; Linux — where the recycled-pgid
// teardown was observed — takes the checked path above.
func (r *jobRegistry) reapShell(job *backgroundJob) {
	_ = job.cmd.Wait()
	if !r.markWaited(job) {
		terminateLegacyDetachedGroup(job.cmd.Process.Pid)
	}
}

// terminateLegacyDetachedGroup is the pre-identity sweep: the shell is already
// reaped, so there is nothing to check the group against, and it is signalled
// exactly as it always was.
func terminateLegacyDetachedGroup(pid int) {
	if !processgroup.Alive(pid) {
		return
	}
	_ = processgroup.Terminate(pid)
	deadline := time.Now().Add(jobTerminateGrace)
	for processgroup.Alive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if processgroup.Alive(pid) {
		_ = processgroup.Kill(pid)
	}
}
