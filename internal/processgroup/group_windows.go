//go:build windows

package processgroup

// Group retains the launch's Job Object after its leader exits.
type Group struct {
	pid   int
	start uint64
	known bool
	job   *jobObject
}

func capturedGroup(pid int, start uint64, known bool) Group {
	return Group{pid: pid, start: start, known: known, job: launchJob(pid)}
}

// Persisted group identities still have no Windows boot identity. Captured
// groups retain their Job Object directly; services use native StartedAt.
func readProcessStart(int) (uint64, bool) { return 0, false }

func (g Group) Terminate() error {
	if g.job != nil {
		return g.job.terminate(g.pid)
	}
	return Terminate(g.pid)
}

func (g Group) Kill() error {
	if g.job != nil {
		return g.job.kill()
	}
	return Kill(g.pid)
}

func (g Group) Alive() bool {
	if g.job != nil {
		return g.job.alive()
	}
	return Alive(g.pid)
}
