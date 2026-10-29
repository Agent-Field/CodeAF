//go:build darwin

package processgroup

import (
	"errors"

	"github.com/Agent-Field/codeaf/internal/darwinproc"
	"golang.org/x/sys/unix"
)

// usageWalkLimit is how many processes one reading looks at most. Past it the
// process count is a lower bound and the CPU sum is marked partial, so a tree
// that keeps growing cannot make every two-second sample a longer walk. A storm
// that size normally still counts far over the job bound's 128-process ceiling
// and is cut on the count; a lower count can only hold a cut back.
const usageWalkLimit = 1024

// usageReader is the kernel's answer for one process and for its children. It
// is a variable so the walk's limits and refusals can be proven on a scripted
// tree instead of real load.
var usageReader = struct {
	task     func(pid int) (darwinproc.TaskInfo, error)
	children func(ppid int) ([]int, error)
}{darwinproc.Task, darwinproc.Children}

// Usage reports the recorded leader's whole subtree, or says it cannot say.
// Identity is checked around the walk because a reading of a reused pid could
// cause the bound to cut somebody else's work.
func (g Group) Usage() (Usage, bool) {
	if ok, _ := g.owned(); !ok {
		return Usage{}, false
	}
	usage, ok := subtreeUsage(g.pid)
	if !ok {
		return Usage{}, false
	}
	if ok, _ := g.owned(); !ok {
		return Usage{}, false
	}
	return usage, true
}

// subtreeUsage walks the tree below root through each process's children, so
// a child that changed process groups is still the job's.
//
// A PARENT IS RE-READ AFTER ITS CHILDREN ARE LISTED. A queued process can exit
// and its pid be handed to another process before its children are asked for;
// the children of that stranger would pass the parent-pid check. Comparing the
// parent's start time before and after the listing keeps them out.
//
// A listing that filled even the largest buffer, a walk that reached
// [usageWalkLimit], and a process that could not be read for any reason other
// than having exited are counted as far as they got, with the CPU sum marked
// partial ([Usage.CPUPartial]): the count is a lower bound and no rate is taken
// from the sum. A process that exited between two reads is skipped, as the
// Linux reading skips it. A listing that fails outright makes the whole
// reading unknown.
func subtreeUsage(root int) (Usage, bool) {
	top, err := usageReader.task(root)
	if err != nil || top.PID != root || top.Status == darwinproc.StatusZombie {
		return Usage{}, false
	}
	usage := Usage{}
	seen := map[int]bool{root: true}
	queue := []darwinproc.TaskInfo{top}
	for next := 0; next < len(queue); next++ {
		parent := queue[next]
		usage.Processes++
		usage.CPUSeconds += parent.CPUSeconds
		if len(seen) >= usageWalkLimit {
			usage.CPUPartial = true
			continue
		}
		children, err := usageReader.children(parent.PID)
		switch {
		case err == nil:
		case errors.Is(err, unix.ENOMEM):
			usage.CPUPartial = true
		case errors.Is(err, unix.ESRCH):
			continue
		default:
			return Usage{}, false
		}
		again, err := usageReader.task(parent.PID)
		if err != nil || again.Start != parent.Start || again.PID != parent.PID {
			if next == 0 {
				return Usage{}, false
			}
			continue
		}
		for _, pid := range children {
			if len(seen) >= usageWalkLimit {
				usage.CPUPartial = true
				break
			}
			if pid <= 0 || seen[pid] {
				continue
			}
			seen[pid] = true
			child, err := usageReader.task(pid)
			if err != nil && !errors.Is(err, unix.ESRCH) {
				usage.CPUPartial = true
				continue
			}
			// A child can exit, be reparented or have its pid reused between reads.
			// Skip that snapshot rather than charging an unrelated process to us.
			if err != nil || child.PID != pid || child.PPID != parent.PID || child.Status == darwinproc.StatusZombie {
				continue
			}
			queue = append(queue, child)
		}
	}
	return usage, true
}
