package workgraph

import (
	"time"
	"unsafe"
)

// WHAT A SNAPSHOT HOLDS, IN BYTES, counted conservatively.
//
// A cache bounded by node count alone is a cache with no bound: a node may carry
// a hundred paths and a thousand-rune brief. So a snapshot prices itself when it
// is built, field by field — the fixed size of each struct (unsafe.Sizeof, which
// includes its pointers and headers), every string at its length, every slice at
// its capacity, every pointed-to value, and every map entry at its key and value
// plus a fixed overhead for the table — and the cache is held to a budget of
// those bytes.
//
// IT COUNTS THE STRINGS EVEN WHEN THEY ARE SHARED. A snapshot's strings are
// usually the very bytes the producer's facts hold (Go strings are immutable and
// copying a struct copies the header), so they stay alive for as long as the
// snapshot does even after the producer's own cache has dropped them. Counting
// them here means that retention is paid for by the snapshot that causes it and
// the budget cannot understate what is kept; counting them twice, once here and
// once in the producer, only makes the combined budget safer.
//
// A snapshot keeps no reference to its Input: the nodes are copies, the edges
// are built, and nothing in the struct points back at the producer's facts or its
// resolver. The tests measure the heap to prove it.

const (
	// mapEntryOverhead is what a Go map spends per entry beyond key and value:
	// bucket tophash, overflow pointers and load-factor slack, rounded up.
	mapEntryOverhead = 48
	// fudge is the safety factor on the whole estimate, in percent.
	fudge = 125
)

func strBytes(s string) int64 { return int64(len(s)) }

func timeBytes(t *time.Time) int64 {
	if t == nil {
		return 0
	}
	return int64(unsafe.Sizeof(*t))
}

func textBytes(t *GraphText) int64 {
	if t == nil {
		return 0
	}
	n := int64(unsafe.Sizeof(*t)) + strBytes(t.Source) + strBytes(t.Text)
	if t.Anchor != nil {
		n += int64(unsafe.Sizeof(*t.Anchor)) + strBytes(t.Anchor.Agent)
	}
	return n
}

func nodeBytes(n *GraphNode) int64 {
	b := int64(unsafe.Sizeof(*n))
	b += strBytes(n.ID) + strBytes(n.Ref.Owner) + strBytes(n.Ref.Task) + strBytes(n.Kind) + strBytes(n.Title)
	b += strBytes(n.State) + strBytes(n.StateDetail) + strBytes(n.Ending) + strBytes(n.Parent) + strBytes(n.ParentState) + strBytes(n.Model) + strBytes(n.Links)
	if n.Group != nil {
		b += int64(unsafe.Sizeof(*n.Group)) + strBytes(n.Group.ID) + strBytes(n.Group.Title)
	}
	if n.Crew != nil {
		b += int64(unsafe.Sizeof(*n.Crew)) + strBytes(n.Crew.Crew)
		for _, p := range n.Crew.Path {
			b += 16 + strBytes(p)
		}
		for _, p := range n.Crew.AlsoIn {
			b += 16 + strBytes(p)
		}
	}
	for _, t := range []*time.Time{n.Clock.AdmittedAt, n.Clock.StartedAt, n.Clock.StateAt, n.Clock.EndedAt} {
		b += timeBytes(t)
	}
	b += textBytes(n.Instructions)
	for _, r := range n.Route {
		b += int64(unsafe.Sizeof(r)) + strBytes(r.Role) + strBytes(r.Model)
		if r.Price != nil {
			b += int64(unsafe.Sizeof(*r.Price))
		}
	}
	if n.Landing != nil {
		b += int64(unsafe.Sizeof(*n.Landing)) + strBytes(n.Landing.Outcome) + timeBytes(n.Landing.At)
	}
	for _, p := range n.Files.Paths {
		b += 16 + strBytes(p)
	}
	b += strBytes(n.Files.Source)
	if n.Spend != nil {
		b += int64(unsafe.Sizeof(*n.Spend)) + strBytes(n.Spend.Basis)
	}
	if n.ElapsedMS != nil {
		b += 8
	}
	if n.Checks != nil {
		b += int64(unsafe.Sizeof(*n.Checks))
	}
	return b
}

func edgeBytes(e *GraphEdge) int64 {
	return int64(unsafe.Sizeof(*e)) + strBytes(e.From) + strBytes(e.To) + strBytes(e.ToRef.Owner) + strBytes(e.ToRef.Task) +
		strBytes(e.Via) + strBytes(e.Target) + strBytes(e.ToState)
}

// estimateBytes prices the whole snapshot.
func (s *Snapshot) estimateBytes() int64 {
	b := int64(unsafe.Sizeof(*s))
	for i := range s.nodes {
		b += nodeBytes(&s.nodes[i])
	}
	b += int64(len(s.keys)) * int64(unsafe.Sizeof(orderKey{}))
	for i := range s.keys {
		b += strBytes(s.keys[i].owner) + strBytes(s.keys[i].task)
	}
	for id := range s.idx {
		b += mapEntryOverhead + 16 + strBytes(id) + 4
	}
	for i := range s.edges {
		b += edgeBytes(&s.edges[i])
	}
	b += int64(len(s.edgeTo)+len(s.edgeStart)+len(s.revStart)+len(s.revList)) * 4
	for _, c := range s.clusters {
		b += int64(unsafe.Sizeof(c)) + strBytes(c.ID) + strBytes(c.Title) + strBytes(c.Parent) + strBytes(c.Kind)
		b += int64(len(c.ByState)) * (mapEntryOverhead + 24)
	}
	for k, list := range s.members {
		b += mapEntryOverhead + 16 + strBytes(k) + 24 + int64(cap(list))*4
	}
	for _, c := range s.crews {
		b += int64(unsafe.Sizeof(c)) + strBytes(c.ID) + strBytes(c.Name) + strBytes(c.Parent) + strBytes(c.Manager) + strBytes(c.State)
	}
	for _, m := range s.people {
		b += int64(unsafe.Sizeof(m)) + strBytes(m.Agent) + strBytes(m.Handle) + strBytes(m.Title) + strBytes(m.Crew) + strBytes(m.ReportsTo) + strBytes(m.Leads)
		b += int64(len(m.Tasks.ByState)) * (mapEntryOverhead + 24)
	}
	for _, c := range s.diag.Cycles {
		b += int64(unsafe.Sizeof(c))
		for _, t := range c.Tasks {
			b += 16 + strBytes(t)
		}
	}
	b += int64(len(s.totals.ByState)) * (mapEntryOverhead + 24)
	for _, u := range s.coverage.Unread {
		b += int64(unsafe.Sizeof(u)) + strBytes(u.Owner) + strBytes(u.Reason)
	}
	return b * fudge / 100
}

// Bytes is the conservative size of the snapshot the cache budgets it at.
func (s *Snapshot) Bytes() int64 { return s.bytes }

// FactBytes is the conservative size of one fact as its producer holds it: the
// node as [nodeBytes] counts it plus its waits. A producer budgets what it
// accumulates with this, so the figure it bounds is the one the builder is later
// charged for.
func FactBytes(f *Fact) int64 {
	b := nodeBytes(&f.Node)
	for _, w := range f.Waits {
		b += int64(unsafe.Sizeof(w)) + strBytes(w.On.Owner) + strBytes(w.On.Task) + strBytes(w.Via)
	}
	return b * fudge / 100
}

// WHAT BUILDING COSTS. A build allocates several times what its input holds:
// it copies every node, builds every edge (a dense graph's edges dominate), and
// indexes both ways. The multipliers below are not guesses made in the abstract:
// TestBuildWeightIsNeverBelowWhatBuildAllocates measures a normal, a dense and a
// long-text ten-thousand-task input against them, and fails if the weight falls
// under the bytes really allocated.
const (
	// buildFixedFactor multiplies the structural bytes of a fact (its struct
	// and pointers); strings are shared and cost one.
	buildFixedFactor = 6
	// buildEdgeBytes is what one stored wait costs to build into the graph.
	buildEdgeBytes = 1400
)

// BuildWeight is the conservative peak allocation of building this fact into a
// snapshot, in bytes.
func BuildWeight(f *Fact) int64 {
	str := factStrings(f)
	fixed := FactBytes(f) - str
	return fixed*buildFixedFactor + str + int64(len(f.Waits))*buildEdgeBytes
}

// factStrings is the bytes of the strings a fact holds, counted as FactBytes
// counts them, so the structural remainder can be told from them.
func factStrings(f *Fact) int64 {
	n := &f.Node
	b := strBytes(n.ID) + strBytes(n.Ref.Owner) + strBytes(n.Ref.Task) + strBytes(n.Kind) + strBytes(n.Title) +
		strBytes(n.State) + strBytes(n.StateDetail) + strBytes(n.Ending) + strBytes(n.Parent) + strBytes(n.ParentState) + strBytes(n.Model) + strBytes(n.Links)
	if n.Instructions != nil {
		b += strBytes(n.Instructions.Source) + strBytes(n.Instructions.Text)
	}
	for _, p := range n.Files.Paths {
		b += strBytes(p)
	}
	for _, w := range f.Waits {
		b += strBytes(w.On.Owner) + strBytes(w.On.Task) + strBytes(w.Via)
	}
	return b
}
