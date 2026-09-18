//go:build windows

package processgroup

// readProcessStart has no answer on Windows: there is no start-time identity to
// read here, so the Windows group operations below keep their old, ungated
// shape and the platform's own taskkill sits underneath them exactly as before.
func readProcessStart(pid int) (uint64, bool) { return 0, false }

func (g Group) Terminate() error { return Terminate(g.pid) }

func (g Group) Kill() error { return Kill(g.pid) }

func (g Group) Alive() bool { return Alive(g.pid) }
