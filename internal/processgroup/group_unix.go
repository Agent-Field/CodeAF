//go:build !windows

package processgroup

import "syscall"

// groupSignalFn sends one signal to a recorded process group. It is a variable
// so the gate can be exercised with a recording sink.
var groupSignalFn = func(pid int, signal syscall.Signal) error {
	return groupSignal(pid, signal)
}

// owned reports whether the leader is still the process recorded at launch,
// and names why not when it is not. The answer is false — and the reason is
// recorded — the moment identity cannot be proven, because the safe default is
// to signal nothing rather than risk a recycled process group.
func (g Group) owned() (bool, string) {
	switch {
	case g.pid <= 0:
		return false, "no leader pid"
	case !g.known:
		return false, "no identity was recorded at launch"
	}
	now, ok := processStart(g.pid)
	if !ok {
		return false, "the leader is gone"
	}
	if now != g.start {
		return false, "the leader's identity no longer matches"
	}
	return true, ""
}

// Terminate asks a recorded process group to stop, and sends nothing when the
// recorded identity no longer matches the live process.
func (g Group) Terminate() error {
	if ok, reason := g.owned(); !ok {
		refuse(g.pid, reason)
		return nil
	}
	return groupSignalFn(g.pid, syscall.SIGTERM)
}

// Kill ends a recorded process group, and sends nothing when the recorded
// identity no longer matches the live process.
func (g Group) Kill() error {
	if ok, reason := g.owned(); !ok {
		refuse(g.pid, reason)
		return nil
	}
	return groupSignalFn(g.pid, syscall.SIGKILL)
}

// Alive reports whether the recorded group is still there AND still this job's.
// A group that cannot be attributed is not alive for our purposes: there is
// nothing of ours left to wait on and nothing of ours to escalate against, and
// neither is a group whose only remaining member is a zombie (members_linux.go
// says why that case is asked about at all). It records no refusal because it
// is polled in a loop; the signals above, sent at most a few times, are what
// carry the reason.
func (g Group) Alive() bool {
	if ok, _ := g.owned(); !ok {
		return false
	}
	return groupHasLiveMember(g.pid)
}
