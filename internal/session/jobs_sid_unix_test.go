//go:build !windows

package session

import (
	"testing"

	"golang.org/x/sys/unix"
)

// A JOB RUNS IN ITS OWN SESSION, WITH NO CONTROLLING TERMINAL. The group-kill
// contract is untouched — a session leader leads its own process group — and
// what the detachment buys is the person's screen: a job's child that opens
// /dev/tty (a CLI that is itself a screen, a prompt that insists on the
// keyboard) is refused instead of painting over the running frame. Two review
// CLIs run as jobs did exactly that, drawing their output across the top of a
// live conversation. The proof here is the session id itself: the job's differs
// from ours, which under the old Setpgid it never did.
//
// Unix-only, because the proof reads session ids with the kernel directly;
// Windows answers ConfigureDetached with CREATE_NEW_PROCESS_GROUP instead.
func TestAJobLeadsItsOwnSessionAwayFromTheTerminal(t *testing.T) {
	agent, _ := jobsAgent(t)

	id := startJob(t, agent, "sleep 30")
	job := agent.jobs.find(id)
	if job == nil || job.cmd == nil || job.cmd.Process == nil {
		t.Fatal("the sleeper has no process to ask about")
	}
	theirs, err := unix.Getsid(job.cmd.Process.Pid)
	if err != nil {
		t.Fatalf("could not read the job's session id: %v", err)
	}
	ours, err := unix.Getsid(0)
	if err != nil {
		t.Fatalf("could not read our own session id: %v", err)
	}
	if theirs == ours {
		t.Fatalf("the job shares our session (sid %d): a child of it could open /dev/tty and draw on the frame", ours)
	}
	// The agent's Close (t.Cleanup) is what ends the sleeper, exactly as the
	// return-immediately test above leaves it.
}
