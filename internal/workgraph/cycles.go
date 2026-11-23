package workgraph

import (
	"sort"
	"time"
)

func copyTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	c := *t
	return &c
}

// tarjan is the strongly connected components of the waits-on graph, found
// without recursion: a loop of ten thousand tasks must not be ten thousand
// stack frames. It looks at its context as it goes.
type tarjan struct {
	b     *builder
	order []int32 // discovery number + 1, 0 = unseen
	low   []int32
	on    []bool
	stack []int32
	comp  []int32 // component id, -1 when the task is on no cycle
	next  int32
	steps int
	// cycles are the components that are cycles, as indexes in creation order.
	cycles [][]int32
	err    error
}

type callFrame struct {
	v  int32
	ei int32
}

func (b *builder) findCycles() error {
	n := len(b.snap.nodes)
	t := &tarjan{b: b, order: make([]int32, n), low: make([]int32, n), on: make([]bool, n), comp: make([]int32, n)}
	for i := range t.comp {
		t.comp[i] = -1
	}
	for v := 0; v < n && t.err == nil; v++ {
		if t.order[v] == 0 {
			t.run(int32(v))
		}
	}
	if t.err != nil {
		return t.err
	}
	b.markCycles(t)
	return nil
}

// run walks everything reachable from root with an explicit call stack.
func (t *tarjan) run(root int32) {
	s := t.b.snap
	calls := []callFrame{{v: root, ei: s.edgeStart[root]}}
	t.discover(root)
	for len(calls) > 0 {
		if t.steps++; t.steps%tickEvery == 0 {
			if t.err = t.b.ctx.Err(); t.err != nil {
				return
			}
		}
		top := &calls[len(calls)-1]
		if top.ei >= s.edgeStart[top.v+1] {
			calls = calls[:len(calls)-1]
			t.finish(top.v, calls)
			continue
		}
		to := s.edgeTo[top.ei]
		top.ei++
		if to < 0 {
			continue
		}
		if t.order[to] == 0 {
			t.discover(to)
			calls = append(calls, callFrame{v: to, ei: s.edgeStart[to]})
		} else if t.on[to] && t.order[to] < t.low[top.v] {
			t.low[top.v] = t.order[to]
		}
	}
}

func (t *tarjan) discover(v int32) {
	t.next++
	t.order[v], t.low[v] = t.next, t.next
	t.stack = append(t.stack, v)
	t.on[v] = true
}

// finish closes a task's visit: it is the head of a component when nothing
// below it reached further back, and it lends its low-link to its caller.
func (t *tarjan) finish(v int32, calls []callFrame) {
	if t.low[v] == t.order[v] {
		t.pop(v)
	}
	if len(calls) > 0 {
		parent := calls[len(calls)-1].v
		if t.low[v] < t.low[parent] {
			t.low[parent] = t.low[v]
		}
	}
}

// pop takes one component off the stack, and keeps it when it is a cycle: more
// than one task, or one task that waits on itself.
func (t *tarjan) pop(head int32) {
	var members []int32
	for {
		w := t.stack[len(t.stack)-1]
		t.stack = t.stack[:len(t.stack)-1]
		t.on[w] = false
		members = append(members, w)
		if w == head {
			break
		}
	}
	if len(members) == 1 && !t.selfLoop(head) {
		return
	}
	sort.Slice(members, func(i, j int) bool { return members[i] < members[j] })
	id := int32(len(t.cycles))
	for _, m := range members {
		t.comp[m] = id
	}
	t.cycles = append(t.cycles, members)
}

func (t *tarjan) selfLoop(v int32) bool {
	s := t.b.snap
	for e := s.edgeStart[v]; e < s.edgeStart[v+1]; e++ {
		if s.edgeTo[e] == v {
			return true
		}
	}
	return false
}

// markCycles flags the tasks and edges on cycles and names the first few.
func (b *builder) markCycles(t *tarjan) {
	s := b.snap
	for v := range s.nodes {
		if t.comp[v] >= 0 {
			s.nodes[v].InCycle = true
		}
		for e := s.edgeStart[v]; e < s.edgeStart[v+1]; e++ {
			if to := s.edgeTo[e]; to >= 0 && t.comp[v] >= 0 && t.comp[v] == t.comp[to] {
				s.edges[e].InCycle = true
			}
		}
	}
	sort.Slice(t.cycles, func(i, j int) bool { return t.cycles[i][0] < t.cycles[j][0] })
	b.diag.CyclesTotal = len(t.cycles)
	for _, members := range t.cycles {
		if len(b.diag.Cycles) == MaxCycles {
			break
		}
		cycle := Cycle{Total: len(members)}
		for _, m := range members {
			if len(cycle.Tasks) == MaxCycleNodes {
				break
			}
			cycle.Tasks = append(cycle.Tasks, s.nodes[m].ID)
		}
		b.diag.Cycles = append(b.diag.Cycles, cycle)
	}
}
