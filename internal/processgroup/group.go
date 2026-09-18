package processgroup

// group.go records WHAT a process group is, so that a later teardown can prove
// it is signalling the group it started.
//
// THE DEFECT THIS ANSWERS. A job is torn down by signalling its recorded
// process-GROUP id: kill(-pid, SIG). Every such signal reaches WHATEVER process
// group currently holds that pgid, not necessarily the job's own. Process ids
// are handed out again — under load, within the minute — so a recorded pgid can
// belong to somebody else entirely by the time teardown fires, and a SIGKILL to
// a recycled pgid destroys an unrelated, live process group. That was observed:
// a long-lived codeaf tearing several job groups down at once sent SIGKILL to a
// pgid that had become a tmux server's, taking a dozen running jobs with it.
//
// So a group is recorded with the identity its leader had at launch, and a
// signal is sent only while that identity still matches the live process. The
// safe default is one-sided on purpose: when identity cannot be verified — the
// leader is gone, the number is free, the read fails — NOTHING is signalled. A
// missed kill leaks one process; a wrong kill destroys someone else's work, and
// the second is never traded away to avoid the first.

// Group is a process group recorded when a job starts: the leader's pid and the
// identity that pid had then.
type Group struct {
	pid   int
	start uint64
	known bool
}

// CaptureGroup records a just-started process-group leader. It is called while
// the process is known to be this job's own, which is the only moment the
// identity is trustworthy.
func CaptureGroup(pid int) Group {
	start, ok := processStart(pid)
	return Group{pid: pid, start: start, known: ok}
}

// processStart reads the platform's identity for a live pid. It is a variable
// so the gate can be exercised against a recycled pid without racing the
// kernel.
var processStart = readProcessStart

// OnRefused, when non-nil, is told why a group signal was withheld. A refusal
// is the safe answer on its own and needs no record to be safe; a host that
// wants one hangs it here.
var OnRefused func(pid int, reason string)

func refuse(pid int, reason string) {
	if OnRefused != nil {
		OnRefused(pid, reason)
	}
}
