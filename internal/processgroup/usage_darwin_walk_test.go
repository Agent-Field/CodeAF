//go:build darwin

package processgroup

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/darwinproc"
	"golang.org/x/sys/unix"
)

// scriptedTree answers the walk from a fixed table, so its limits and refusals
// are proven on a tree that exists only as numbers and never as real load.
type scriptedTree struct {
	tasks    map[int]darwinproc.TaskInfo
	children map[int][]int
	listErr  map[int]error
	// restart gives a pid a new start time after its children are listed: the
	// pid was handed to another process in between.
	restart  map[int]bool
	listed   map[int]bool
	taskRead int
}

func (s *scriptedTree) use(t *testing.T) {
	t.Helper()
	saved := usageReader
	t.Cleanup(func() { usageReader = saved })
	s.listed = map[int]bool{}
	usageReader.task = func(pid int) (darwinproc.TaskInfo, error) {
		s.taskRead++
		task, ok := s.tasks[pid]
		if !ok {
			return darwinproc.TaskInfo{}, unix.ESRCH
		}
		if s.restart[pid] && s.listed[pid] {
			task.Start++
		}
		return task, nil
	}
	usageReader.children = func(ppid int) ([]int, error) {
		s.listed[ppid] = true
		return s.children[ppid], s.listErr[ppid]
	}
}

func task(pid, ppid, pgid int, cpu float64) darwinproc.TaskInfo {
	return darwinproc.TaskInfo{PID: pid, PPID: ppid, PGID: pgid, Start: uint64(pid) * 1000, CPUSeconds: cpu, Status: 2}
}

// The job's tree is its children's children too, including one that moved to
// a process group of its own; each live process is counted once.
func TestDarwinUsageCountsTheWholeTreeOnce(t *testing.T) {
	tree := &scriptedTree{
		tasks: map[int]darwinproc.TaskInfo{
			10: task(10, 1, 10, 1), 11: task(11, 10, 10, 2), 12: task(12, 10, 99, 3), 13: task(13, 11, 10, 4),
		},
		children: map[int][]int{10: {11, 12, 11}, 11: {13}, 12: {13}},
	}
	tree.use(t)
	usage, ok := subtreeUsage(10)
	if !ok || usage.Processes != 4 || usage.CPUSeconds != 10 || usage.CPUPartial {
		t.Fatalf("usage = %+v, %v, want 4 processes and a complete 10 CPU seconds", usage, ok)
	}
}

// A queued process whose pid went to another process before its children were
// listed contributes itself but none of the stranger's children.
func TestDarwinUsageKeepsAReusedParentsChildrenOut(t *testing.T) {
	tree := &scriptedTree{
		tasks: map[int]darwinproc.TaskInfo{
			10: task(10, 1, 10, 1), 11: task(11, 10, 10, 1), 30: task(30, 11, 30, 50),
		},
		children: map[int][]int{10: {11}, 11: {30}},
		restart:  map[int]bool{11: true},
	}
	tree.use(t)
	usage, ok := subtreeUsage(10)
	if !ok || usage.Processes != 2 || usage.CPUSeconds != 2 {
		t.Fatalf("usage = %+v, %v, want the leader and its own child only", usage, ok)
	}
	tree.restart, tree.listed = map[int]bool{10: true}, map[int]bool{}
	if usage, ok := subtreeUsage(10); ok {
		t.Fatalf("a root whose pid changed hands mid-walk was read: %+v", usage)
	}
}

// A listing that filled the largest buffer is a lower bound and still counts;
// an unexpected failure makes the reading unknown rather than partial.
func TestDarwinUsageCountsATruncatedListingAndRefusesAFailedOne(t *testing.T) {
	tasks := map[int]darwinproc.TaskInfo{10: task(10, 1, 10, 0)}
	var kids []int
	for pid := 100; pid < 400; pid++ {
		tasks[pid] = task(pid, 10, 10, 0)
		kids = append(kids, pid)
	}
	tree := &scriptedTree{tasks: tasks, children: map[int][]int{10: kids}, listErr: map[int]error{10: unix.ENOMEM}}
	tree.use(t)
	if usage, ok := subtreeUsage(10); !ok || usage.Processes != 301 || !usage.CPUPartial {
		t.Fatalf("truncated listing: usage = %+v, %v, want the 301 it named and a partial CPU sum", usage, ok)
	}
	tree.listErr = map[int]error{10: unix.EPERM}
	if usage, ok := subtreeUsage(10); ok {
		t.Fatalf("a listing that failed was read as a partial tree: %+v", usage)
	}
}

// A tree past the walk limit is read as far as the limit, with a bounded
// number of kernel reads, and is still far over the job bound's ceiling.
func TestDarwinUsageStopsAtTheWalkLimit(t *testing.T) {
	tasks := map[int]darwinproc.TaskInfo{10: task(10, 1, 10, 0)}
	children := map[int][]int{}
	parent, next := 10, 11
	for i := 0; i < 5000; i++ {
		tasks[next] = task(next, parent, 10, 0)
		children[parent] = append(children[parent], next)
		if i%3 == 2 {
			parent = next
		}
		next++
	}
	tree := &scriptedTree{tasks: tasks, children: children}
	tree.use(t)
	usage, ok := subtreeUsage(10)
	if !ok || usage.Processes != usageWalkLimit || !usage.CPUPartial {
		t.Fatalf("usage = %+v, %v, want exactly the walk limit %d and a partial CPU sum", usage, ok, usageWalkLimit)
	}
	if tree.taskRead > 2*usageWalkLimit+1 {
		t.Fatalf("%d task reads for a walk limited to %d processes", tree.taskRead, usageWalkLimit)
	}
	if len(tree.listed) > usageWalkLimit {
		t.Fatalf("%d child listings for a walk limited to %d processes", len(tree.listed), usageWalkLimit)
	}
}

// A child that exited between two reads leaves the tree, as on Linux; one that
// could not be read for another reason makes the CPU sum partial.
func TestDarwinUsageMarksAnUnreadableChildPartial(t *testing.T) {
	tree := &scriptedTree{
		tasks:    map[int]darwinproc.TaskInfo{10: task(10, 1, 10, 1)},
		children: map[int][]int{10: {11}},
	}
	tree.use(t)
	if usage, ok := subtreeUsage(10); !ok || usage.Processes != 1 || usage.CPUPartial {
		t.Fatalf("an exited child: usage = %+v, %v, want the leader alone and a complete sum", usage, ok)
	}
	saved := usageReader.task
	usageReader.task = func(pid int) (darwinproc.TaskInfo, error) {
		if pid == 11 {
			return darwinproc.TaskInfo{}, unix.EPERM
		}
		return saved(pid)
	}
	if usage, ok := subtreeUsage(10); !ok || usage.Processes != 1 || !usage.CPUPartial {
		t.Fatalf("an unreadable child: usage = %+v, %v, want the leader and a partial sum", usage, ok)
	}
}

// A parent that read once and then could not be re-read leaves its children out
// of this reading only, so the sum is partial; one that exited does not.
func TestDarwinUsageMarksAnUnreadableParentPartial(t *testing.T) {
	tree := &scriptedTree{
		tasks:    map[int]darwinproc.TaskInfo{10: task(10, 1, 10, 1), 11: task(11, 10, 10, 1), 12: task(12, 11, 10, 9)},
		children: map[int][]int{10: {11}, 11: {12}},
	}
	tree.use(t)
	saved := usageReader.task
	reads := map[int]int{}
	failWith := unix.EPERM
	usageReader.task = func(pid int) (darwinproc.TaskInfo, error) {
		reads[pid]++
		if pid == 11 && reads[pid] == 2 {
			return darwinproc.TaskInfo{}, failWith
		}
		return saved(pid)
	}
	if usage, ok := subtreeUsage(10); !ok || usage.Processes != 2 || !usage.CPUPartial {
		t.Fatalf("a parent that could not be re-read: usage = %+v, %v, want 2 processes and a partial sum", usage, ok)
	}
	reads, failWith = map[int]int{}, unix.ESRCH
	if usage, ok := subtreeUsage(10); !ok || usage.Processes != 2 || usage.CPUPartial {
		t.Fatalf("a parent that exited: usage = %+v, %v, want 2 processes and a complete sum", usage, ok)
	}
}
