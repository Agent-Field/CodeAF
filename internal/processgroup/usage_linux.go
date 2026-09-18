//go:build linux

package processgroup

import (
	"os"
	"strconv"
	"strings"
)

// userHZ is Linux's USER_HZ: the ticks per second the utime and stime fields of
// /proc/<pid>/stat are counted in. It is 100 on every architecture this product
// ships to, x86-64 and arm64 included, so it is written rather than asked — a
// host read would return the same 100 everywhere a job can run.
const userHZ = 100

// Usage reports what the recorded process group's whole subtree is using, or
// says it cannot say.
//
// IT REFUSES WHEN THE IDENTITY NO LONGER MATCHES, for [Group.owned]'s reason:
// the pid may have been reused, and a reading of somebody else's process group
// is worse than no reading — it could cut a subtree that is not ours. A group
// that cannot be attributed is one this bound must leave alone.
func (g Group) Usage() (Usage, bool) {
	if ok, _ := g.owned(); !ok {
		return Usage{}, false
	}
	processes, ticks, ok := subtreeTicks(g.pid)
	if !ok {
		return Usage{}, false
	}
	return Usage{Processes: processes, CPUSeconds: float64(ticks) / userHZ}, true
}

// subtreeTicks walks the process tree below root and sums its accumulated CPU
// ticks and its live process count.
//
// THE WALK FOLLOWS THE TREE DOWN, through each process's threads' `children`
// files, rather than reading every process on the machine and sorting out whose
// is whose: what it reads scales with what the job started, not with how busy
// the box is. This is session's task_pressure.go's walk with a CPU figure added;
// the kernel promises `children` exactly only for a stopped tree, so a process
// born or reaped mid-walk can be missed for one reading, which is the right
// precision for a figure re-read on every tick.
//
// `ok` is false only when the root itself could not be read: every other unread
// child is a process that went away between two reads, and one reading that
// missed it is corrected by the next.
func subtreeTicks(root int) (processes int, ticks int64, ok bool) {
	seen := map[int]bool{}
	for queue := []int{root}; len(queue) > 0; {
		pid := queue[0]
		queue = queue[1:]
		if seen[pid] {
			continue
		}
		seen[pid] = true
		dir := "/proc/" + strconv.Itoa(pid)
		if raw, err := os.ReadFile(dir + "/stat"); err == nil {
			if utime, stime, read := processTicks(string(raw)); read {
				ticks += utime + stime
				processes++
				ok = true
			}
		}
		threads, err := os.ReadDir(dir + "/task")
		if err != nil {
			continue
		}
		for _, thread := range threads {
			raw, err := os.ReadFile(dir + "/task/" + thread.Name() + "/children")
			if err != nil {
				continue
			}
			for _, field := range strings.Fields(string(raw)) {
				if child, err := strconv.Atoi(field); err == nil {
					queue = append(queue, child)
				}
			}
		}
	}
	return processes, ticks, ok
}

// processTicks reads a process's utime and stime (fields 14 and 15) out of one
// /proc/<pid>/stat line.
//
// comm (field 2) is parenthesised and may itself contain spaces and
// parentheses, so the fields are counted from after its LAST ')': the state
// (field 3) is then index 0, utime is index 11 and stime is index 12. This is
// the same off-by-the-parens counting identity_linux.go already does for the
// start time.
func processTicks(raw string) (utime, stime int64, ok bool) {
	close := strings.LastIndexByte(raw, ')')
	if close < 0 {
		return 0, 0, false
	}
	fields := strings.Fields(raw[close+1:])
	if len(fields) <= 12 {
		return 0, 0, false
	}
	user, err := strconv.ParseInt(fields[11], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	system, err := strconv.ParseInt(fields[12], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return user, system, true
}
