package plan

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// State is what has happened to a node. It exists to make the graph editable
// safely: an edit is legal or illegal depending on state, and nothing else.
type State string

const (
	StatePending State = "pending"
	StateRunning State = "running"
	StateDone    State = "done"
	StateFailed  State = "failed"

	// StateBlocked is a node whose input failed. It is distinct from failed
	// because nothing was wrong with it — it never got the chance to run — and
	// a report that conflates the two makes a single upstream failure look like
	// a collapse.
	StateBlocked State = "blocked"
)

// Frozen reports whether a node may still be changed. Once work has started,
// its output may already be someone else's input, so the past is not editable —
// a revision to a started node has to be expressed as new work appended after
// it, never as a rewrite of it.
func (s State) Frozen() bool { return s == StateRunning || s == StateDone }

// Kind separates work the plan asked for from work the harness owns.
type Kind string

const (
	KindWork      Kind = "work"
	KindSynthesis Kind = "synthesis"
)

// Stage is one position in the generation spine. Stages are scaffolding, not
// schedule: they exist so the graph can be produced cheaply and acyclically,
// and they gate nothing once the real edges are known.
type Stage struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

// Node is one unit of work.
//
// ID is assigned from a counter and never reused or renumbered. That matters
// more than it looks: the moment anything can insert a node, a positional
// identifier silently rewrites every dependency that referred to a later node.
// Stable IDs are what make the graph safe to revise at all.
//
// Needs carries both meanings the graph has at once. It is the schedule — this
// node waits for those — and it is the context routing table: the executing
// agent sees the goal plus exactly those outputs and nothing else. Keeping them
// one list is deliberate. A dependency that cannot justify a place in the
// context has not earned the right to delay the node either.
type Node struct {
	ID     int `json:"id"`
	Stage  int `json:"stage"`
	Depth  int `json:"depth"`
	Parent int `json:"parent,omitempty"`

	Title   string `json:"title"`
	Summary string `json:"summary"`

	// Sources are the distinct things this node must touch to be done — pages,
	// documents, datasets, vendors, decisions. They are collected because
	// enumeration is something a model does reliably and effort estimation is
	// not, so they stand in as evidence of size. They are dual-use: the same
	// list is a real hint to whatever eventually executes the node.
	Sources []string `json:"sources,omitempty"`

	// Parts are the pieces this node would break into that could genuinely run
	// at the same time. They are named during sizing, which costs nothing extra,
	// and they are the cheap pre-check on expansion: a node that cannot name two
	// parallel parts is not worth spending a fan-out call on, because whatever
	// comes back will be rejected for not shrinking anything. Naming is the test
	// — a split nobody can describe concretely is a split that does not exist.
	Parts []string `json:"parts,omitempty"`

	Needs []int  `json:"needs"`
	Size  Size   `json:"size,omitempty"`
	State State  `json:"state"`
	Kind  Kind   `json:"kind"`
	Brief string `json:"brief,omitempty"`

	// Contract is the working method for this leaf: how an agent should work
	// this particular kind of job, as distinct from the Brief, which says what
	// the job is. A generic loop with a per-task contract is what lets one
	// executor match a specialised harness on any given leaf without the
	// harness itself changing.
	Contract string `json:"contract,omitempty"`

	// Result is what this node produced and is what its dependents receive. It
	// is the deliverable itself rather than a report about it, so that routing
	// it downstream needs no further interpretation. Artifacts are referenced by
	// path instead of inlined: a large output would otherwise be pasted into
	// every dependent's context at once, which is the exact pollution the
	// dependency list exists to prevent.
	Result    string   `json:"result,omitempty"`
	Artifacts []string `json:"artifacts,omitempty"`
	Turns     int      `json:"turns,omitempty"`

	// Tokens and Cost are recorded per node because a run's total says nothing
	// about where it went. One leaf was 54% of a run's input tokens and that had
	// to be inferred from turn counts afterwards rather than read off, which is
	// exactly the measurement the calibration loop needs.
	Tokens int     `json:"tokens,omitempty"`
	Cost   float64 `json:"cost,omitempty"`
	Stop   string  `json:"stop,omitempty"`

	Failure string `json:"failure,omitempty"`
}

// Size is how a node measures against a linear harness: whether one agent with
// tools, running serially, is the right thing to hand it to.
type Size string

const (
	SizeUnknown    Size = ""
	SizeAtomic     Size = "atomic"
	SizeBorderline Size = "borderline"
	SizeOversized  Size = "oversized"
)

// Graph is the whole plan, and the unit that is persisted between a planning
// run and any later revision of it.
type Graph struct {
	Goal string `json:"goal"`

	// Settled are the goal's free variables, bound once so that every parallel
	// call works from the same premise. Open are the ones that cannot be bound
	// in advance because they are the answer to the work — they exist to tell
	// the binder what must become a real dependency rather than an assumption.
	Settled []string `json:"settled,omitempty"`
	Open    []string `json:"open,omitempty"`

	Stages []Stage `json:"stages"`
	Nodes  []Node  `json:"nodes"`
	NextID int     `json:"next_id"`
	Usage  Usage   `json:"usage"`
}

// Node returns the node with the given stable ID.
func (g *Graph) Node(id int) *Node {
	for index := range g.Nodes {
		if g.Nodes[index].ID == id {
			return &g.Nodes[index]
		}
	}
	return nil
}

// Add appends a node, assigning it the next stable ID.
func (g *Graph) Add(node Node) int {
	if g.NextID == 0 {
		g.NextID = 1
	}
	node.ID = g.NextID
	g.NextID++
	if node.State == "" {
		node.State = StatePending
	}
	if node.Kind == "" {
		node.Kind = KindWork
	}
	g.Nodes = append(g.Nodes, node)
	return node.ID
}

// Remove deletes a pending node and rewires anything that depended on it onto
// that node's own dependencies. Inheriting the needs rather than dropping them
// is what keeps the removal from silently freeing a downstream node to run
// before its real inputs exist.
func (g *Graph) Remove(id int) error {
	node := g.Node(id)
	if node == nil {
		return fmt.Errorf("node %d does not exist", id)
	}
	if node.State.Frozen() {
		return fmt.Errorf("node %d is %s and cannot be removed", id, node.State)
	}
	inherited := append([]int(nil), node.Needs...)
	kept := make([]Node, 0, len(g.Nodes)-1)
	for _, candidate := range g.Nodes {
		if candidate.ID == id {
			continue
		}
		if contains(candidate.Needs, id) {
			candidate.Needs = mergeNeeds(without(candidate.Needs, id), inherited)
		}
		kept = append(kept, candidate)
	}
	g.Nodes = kept
	return nil
}

// Retarget replaces every reference to one node with a reference to another,
// then removes the original. It is how a duplicate is folded into the node it
// duplicates.
func (g *Graph) Retarget(from, to int) error {
	if from == to {
		return nil
	}
	source, target := g.Node(from), g.Node(to)
	if source == nil || target == nil {
		return fmt.Errorf("cannot fold %d into %d: unknown node", from, to)
	}
	if source.State.Frozen() {
		return fmt.Errorf("node %d is %s and cannot be folded away", from, source.State)
	}
	for index := range g.Nodes {
		if contains(g.Nodes[index].Needs, from) {
			g.Nodes[index].Needs = mergeNeeds(without(g.Nodes[index].Needs, from), []int{to})
		}
	}
	return g.Remove(from)
}

// AddNeed records a dependency, refusing anything that would break the graph.
// Self-reference, unknown nodes, and edges into a frozen node's inputs are all
// rejected, and so is any edge that would close a cycle.
func (g *Graph) AddNeed(id, need int) error {
	node := g.Node(id)
	if node == nil || g.Node(need) == nil {
		return fmt.Errorf("cannot add %d → %d: unknown node", need, id)
	}
	if id == need {
		return fmt.Errorf("node %d cannot depend on itself", id)
	}
	if node.State.Frozen() {
		return fmt.Errorf("node %d is %s and cannot take new inputs", id, node.State)
	}
	if contains(node.Needs, need) {
		return nil
	}
	node.Needs = mergeNeeds(node.Needs, []int{need})
	if g.hasCycle() {
		node.Needs = without(node.Needs, need)
		return fmt.Errorf("edge %d → %d would create a cycle", need, id)
	}
	return nil
}

// setNeeds replaces a node's dependency list wholesale during generation, where
// the backward-stage rule already guarantees acyclicity.
func (g *Graph) setNeeds(id int, needs []int) {
	node := g.Node(id)
	if node == nil || node.State.Frozen() {
		return
	}
	kept := make([]int, 0, len(needs))
	seen := map[int]bool{}
	for _, need := range needs {
		source := g.Node(need)
		if source == nil || need == id || seen[need] || source.Stage >= node.Stage {
			continue
		}
		seen[need] = true
		kept = append(kept, need)
	}
	sort.Ints(kept)
	node.Needs = kept
}

// hasCycle is only needed on the revision path. Generation cannot produce a
// cycle — edges may point only at earlier stages — but an edit can, so every
// mutation that adds an edge is checked.
func (g *Graph) hasCycle() bool {
	const (
		unvisited = 0
		active    = 1
		finished  = 2
	)
	mark := make(map[int]int, len(g.Nodes))
	var walk func(id int) bool
	walk = func(id int) bool {
		switch mark[id] {
		case active:
			return true
		case finished:
			return false
		}
		mark[id] = active
		if node := g.Node(id); node != nil {
			for _, need := range node.Needs {
				if walk(need) {
					return true
				}
			}
		}
		mark[id] = finished
		return false
	}
	for _, node := range g.Nodes {
		if walk(node.ID) {
			return true
		}
	}
	return false
}

// Prune drops references to nodes that no longer exist. Revision can delete a
// node that something else still names, and a dangling need would otherwise
// stall that node forever.
func (g *Graph) Prune() {
	alive := make(map[int]bool, len(g.Nodes))
	for _, node := range g.Nodes {
		alive[node.ID] = true
	}
	for index := range g.Nodes {
		kept := g.Nodes[index].Needs[:0]
		for _, need := range g.Nodes[index].Needs {
			if alive[need] && need != g.Nodes[index].ID {
				kept = append(kept, need)
			}
		}
		g.Nodes[index].Needs = kept
	}
}

// Leaves are the nodes that represent real work — everything the harness would
// actually hand to an executing agent, excluding the synthesis nodes it owns.
func (g *Graph) Leaves() []int {
	var leaves []int
	for _, node := range g.Nodes {
		if node.Kind == KindWork {
			leaves = append(leaves, node.ID)
		}
	}
	return leaves
}

// Unresolved counts leaves that are still judged too big for one agent. They
// are shipped anyway — a leaf that is too large still gets done, only slowly —
// but the count is the honest measure of where decomposition ran out of depth
// or budget, and it should never be silently swallowed.
func (g *Graph) Unresolved() int {
	count := 0
	for _, node := range g.Nodes {
		if node.Kind == KindWork && node.Size == SizeOversized {
			count++
		}
	}
	return count
}

// WorkDepth is the critical path counting only real work. Splicing leaves a
// synthesis node behind at every level, and those hops are near-free — they
// assemble results that already exist rather than going and getting anything.
// Counting them makes a graph look more serial than it will actually run, which
// matters because the critical path is the number we are trying to minimise.
func (g *Graph) WorkDepth() int {
	depth := make(map[int]int, len(g.Nodes))
	var resolve func(id int) int
	resolve = func(id int) int {
		if known, ok := depth[id]; ok {
			return known
		}
		depth[id] = 0
		node := g.Node(id)
		if node == nil {
			return 0
		}
		deepest := 0
		for _, need := range node.Needs {
			if level := resolve(need); level > deepest {
				deepest = level
			}
		}
		if node.Kind == KindWork {
			deepest++
		}
		depth[id] = deepest
		return deepest
	}
	longest := 0
	for _, node := range g.Nodes {
		if level := resolve(node.ID); level > longest {
			longest = level
		}
	}
	return longest
}

// Depth reports how many levels of decomposition the graph has.
func (g *Graph) Depth() int {
	deepest := 0
	for _, node := range g.Nodes {
		if node.Depth > deepest {
			deepest = node.Depth
		}
	}
	return deepest
}

// Splice replaces a node with its own decomposition, in place.
//
// The node does not go away — it becomes the synthesis of the subtree that
// replaced it. That is what makes recursion safe: the parent keeps its ID, its
// inbound edges, and its position, so nothing in the outer graph is rewired and
// nothing that pointed at it has to learn that it was expanded. Expansion is
// therefore a purely local operation no matter how deep it goes.
//
//	before:  A ──▶ X ──▶ B
//	after:   A ──▶ x1 ┐
//	         A ──▶ x2 ├──▶ X ──▶ B
//	         A ──▶ x3 ┘
//
// Children inherit the parent's inputs, because a child cannot know which of
// its parent's inputs it actually needs and inheriting is the answer that
// cannot strand it. A sub-binding pass may narrow that later; guessing narrow
// here would silently starve a node of data it was promised.
func (g *Graph) Splice(parentID int, sub *Graph) error {
	parent := g.Node(parentID)
	if parent == nil {
		return fmt.Errorf("splice: node %d does not exist", parentID)
	}
	if parent.State.Frozen() {
		return fmt.Errorf("splice: node %d is %s", parentID, parent.State)
	}
	// Everything needed from the parent is copied out by value before the first
	// child is added. Node returns a pointer into g.Nodes, and Add appends to
	// it, so any append can move the backing array and leave that pointer aimed
	// at the abandoned one. Writes through it are then silently lost — which is
	// exactly how a spliced parent stayed marked as unexpanded work and got
	// expanded a second time on the next level, duplicating its whole subtree.
	inherited := append([]int(nil), parent.Needs...)
	parentDepth, parentStage := parent.Depth, parent.Stage
	parent = nil

	// The sub-graph's own synthesis node is dropped: the parent is already
	// playing that role, and keeping both would add a hop that does nothing.
	var sinks []int
	remap := make(map[int]int, len(sub.Nodes))
	for _, node := range sub.Nodes {
		if node.Kind == KindSynthesis {
			sinks = append(sinks, node.Needs...)
			continue
		}
		child := node
		child.Depth = parentDepth + 1
		child.Parent = parentID
		child.Stage = parentStage
		child.Needs = nil
		child.State = StatePending
		child.Kind = KindWork
		remap[node.ID] = g.Add(child)
	}
	if len(remap) == 0 {
		return fmt.Errorf("splice: node %d expanded to nothing", parentID)
	}

	for _, node := range sub.Nodes {
		childID, ok := remap[node.ID]
		if !ok {
			continue
		}
		internal := 0
		for _, need := range node.Needs {
			if mapped, ok := remap[need]; ok {
				if g.AddNeed(childID, mapped) == nil {
					internal++
				}
			}
		}
		// A child with no upstream inside the subtree is a root of it, so it
		// takes the parent's inputs. A child that already has internal inputs
		// reaches the parent's inputs transitively through them.
		if internal == 0 {
			for _, need := range inherited {
				_ = g.AddNeed(childID, need)
			}
		}
	}

	// Re-fetch now that every append is done and the backing array is stable.
	parent = g.Node(parentID)
	if parent == nil {
		return fmt.Errorf("splice: node %d vanished", parentID)
	}
	parent.Needs = nil
	for _, sink := range sinks {
		if mapped, ok := remap[sink]; ok {
			_ = g.AddNeed(parentID, mapped)
		}
	}
	if parent = g.Node(parentID); len(parent.Needs) == 0 {
		// No sink survived the remap; fall back to every child so the parent
		// still gathers the whole subtree rather than becoming unreachable.
		for _, childID := range remap {
			_ = g.AddNeed(parentID, childID)
		}
	}
	parent.Kind = KindSynthesis
	parent.Size = SizeUnknown
	return nil
}

// Sinks are the nodes nothing else consumes — what the synthesis node reads.
func (g *Graph) Sinks() []int {
	consumed := make(map[int]bool, len(g.Nodes))
	for _, node := range g.Nodes {
		for _, need := range node.Needs {
			consumed[need] = true
		}
	}
	var sinks []int
	for _, node := range g.Nodes {
		if !consumed[node.ID] {
			sinks = append(sinks, node.ID)
		}
	}
	return sinks
}

// Waves groups nodes by earliest possible start. A node's wave is one past the
// deepest wave it depends on, so a node needing nothing starts in wave 0 no
// matter which stage produced it. This is the whole point of binding: stage
// membership gates nothing, only real data flow does.
func (g *Graph) Waves() [][]int {
	depth := make(map[int]int, len(g.Nodes))
	var resolve func(id int) int
	resolve = func(id int) int {
		if known, ok := depth[id]; ok {
			return known
		}
		depth[id] = 0
		node := g.Node(id)
		if node == nil {
			return 0
		}
		deepest := -1
		for _, need := range node.Needs {
			if level := resolve(need); level > deepest {
				deepest = level
			}
		}
		depth[id] = deepest + 1
		return depth[id]
	}
	var waves [][]int
	for _, node := range g.Nodes {
		level := resolve(node.ID)
		for len(waves) <= level {
			waves = append(waves, nil)
		}
		waves[level] = append(waves[level], node.ID)
	}
	for _, wave := range waves {
		sort.Ints(wave)
	}
	return waves
}

// StageWaves is what execution would look like if stages were barriers — the
// naive schedule this design exists to beat. It is computed only so the
// difference can be shown rather than claimed.
func (g *Graph) StageWaves() [][]int {
	if len(g.Stages) == 0 {
		return nil
	}
	waves := make([][]int, len(g.Stages))
	for _, node := range g.Nodes {
		stage := node.Stage
		if stage < 1 {
			stage = 1
		}
		if stage > len(waves) {
			stage = len(waves)
		}
		waves[stage-1] = append(waves[stage-1], node.ID)
	}
	return waves
}

// Roots counts nodes that can start immediately.
func (g *Graph) Roots() int {
	count := 0
	for _, node := range g.Nodes {
		if len(node.Needs) == 0 {
			count++
		}
	}
	return count
}

// Edges counts declared dependencies.
func (g *Graph) Edges() int {
	count := 0
	for _, node := range g.Nodes {
		count += len(node.Needs)
	}
	return count
}

// addSynthesis appends the terminal node the harness owns rather than asks for.
// Asking a model to omit a merge step and then hoping is unreliable — it leaked
// one into an early run despite an explicit instruction. Constructing it here
// makes the sink a structural fact instead of a negotiation, and it is skipped
// when there is only one node, which is already its own answer.
func (g *Graph) addSynthesis() {
	if len(g.Nodes) < 2 {
		return
	}
	sinks := g.Sinks()
	if len(sinks) == 0 {
		return
	}
	stage := 0
	for _, node := range g.Nodes {
		if node.Stage > stage {
			stage = node.Stage
		}
	}
	g.Add(Node{
		Stage:   stage + 1,
		Title:   "Synthesis",
		Summary: "Assemble the finished answer to the goal from every result.",
		Needs:   sinks,
		Kind:    KindSynthesis,
	})
}

// catalog renders every node the same way for every call that needs the whole
// picture. Stable ordering and stable formatting are not cosmetic here: this
// block is the frozen shared prefix across a fan-out, and a byte of drift costs
// every cache hit behind it.
func (g *Graph) catalog() string {
	var block strings.Builder
	for stageIndex, stage := range g.Stages {
		fmt.Fprintf(&block, "Stage %d — %s: %s\n", stageIndex+1, stage.Title, stage.Summary)
		for _, node := range g.Nodes {
			if node.Stage == stageIndex+1 {
				fmt.Fprintf(&block, "  %d. %s — %s\n", node.ID, node.Title, node.Summary)
			}
		}
	}
	return block.String()
}

// stateBlock renders the graph for the reviser, which unlike every other call
// has to know what has already happened and what it is therefore not allowed to
// touch.
func (g *Graph) stateBlock() string {
	var block strings.Builder
	children := map[int][]int{}
	for _, node := range g.Nodes {
		if node.Parent != 0 {
			children[node.Parent] = append(children[node.Parent], node.ID)
		}
	}
	for _, node := range g.Nodes {
		inputs := "none"
		if len(node.Needs) > 0 {
			inputs = joinInts(node.Needs)
		}
		// The reviser is the only caller that has to understand the hierarchy,
		// because it is the only one allowed to change it. A node with children
		// is not work any more — it is where its children's results come back
		// together — and an edit aimed at it usually belongs on a child.
		role := "work"
		if kids := children[node.ID]; len(kids) > 0 {
			role = "gathers " + joinInts(kids)
		} else if node.Kind == KindSynthesis {
			role = "gathers results"
		}
		lock := ""
		if node.State.Frozen() {
			lock = "  [locked]"
		}
		fmt.Fprintf(&block, "  %d.%s %s — %s (%s, inputs: %s, %s)%s\n",
			node.ID, strings.Repeat("  ", node.Depth), node.Title, node.Summary,
			role, inputs, node.State, lock)
	}
	return block.String()
}

// MarshalJSON is provided through a plain method so a graph round-trips to disk
// between a planning run and a later revision.
func (g *Graph) JSON() ([]byte, error) { return json.MarshalIndent(g, "", "  ") }

// Load reads a persisted graph.
func Load(data []byte) (*Graph, error) {
	var graph Graph
	if err := json.Unmarshal(data, &graph); err != nil {
		return nil, fmt.Errorf("load graph: %w", err)
	}
	if graph.NextID == 0 {
		for _, node := range graph.Nodes {
			if node.ID >= graph.NextID {
				graph.NextID = node.ID + 1
			}
		}
	}
	if len(graph.Nodes) == 0 {
		return nil, errors.New("load graph: no nodes")
	}
	return &graph, nil
}

func contains(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func without(values []int, target int) []int {
	kept := make([]int, 0, len(values))
	for _, value := range values {
		if value != target {
			kept = append(kept, value)
		}
	}
	return kept
}

func mergeNeeds(values, extra []int) []int {
	seen := make(map[int]bool, len(values)+len(extra))
	merged := make([]int, 0, len(values)+len(extra))
	for _, group := range [][]int{values, extra} {
		for _, value := range group {
			if !seen[value] {
				seen[value] = true
				merged = append(merged, value)
			}
		}
	}
	sort.Ints(merged)
	return merged
}

func joinInts(values []int) string {
	parts := make([]string, len(values))
	for index, value := range values {
		parts[index] = fmt.Sprint(value)
	}
	return strings.Join(parts, ",")
}
