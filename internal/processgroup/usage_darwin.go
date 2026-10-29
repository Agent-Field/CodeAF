//go:build darwin

package processgroup

import "github.com/Agent-Field/codeaf/internal/darwinproc"

// Usage reports the recorded leader's whole subtree, or says it cannot say.
// Identity is checked around the walk because a reading of a reused pid could
// cause the bound to cut somebody else's work.
func (g Group) Usage() (Usage, bool) {
	if ok, _ := g.owned(); !ok {
		return Usage{}, false
	}
	root, err := darwinproc.Task(g.pid)
	if err != nil || root.PID != g.pid || root.Status == darwinproc.StatusZombie {
		return Usage{}, false
	}
	usage := Usage{}
	seen := map[int]bool{g.pid: true}
	queue := []darwinproc.TaskInfo{root}
	for next := 0; next < len(queue); next++ {
		parent := queue[next]
		usage.Processes++
		usage.CPUSeconds += parent.CPUSeconds
		// Follow descendants even after a child changes process groups, because
		// the bound covers the whole job rather than only its original group.
		children, err := darwinproc.Children(parent.PID)
		if err != nil {
			continue
		}
		for _, pid := range children {
			if pid <= 0 || seen[pid] {
				continue
			}
			seen[pid] = true
			child, err := darwinproc.Task(pid)
			// A child can exit, be reparented or have its pid reused between reads.
			// Skip that snapshot rather than charging an unrelated process to us.
			if err != nil || child.PID != pid || child.PPID != parent.PID || child.Status == darwinproc.StatusZombie {
				continue
			}
			queue = append(queue, child)
		}
	}
	if ok, _ := g.owned(); !ok {
		return Usage{}, false
	}
	return usage, true
}
