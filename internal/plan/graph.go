package plan

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/aforge-v2/internal/provider"
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

	// Undivided is why this node was left whole, written at the moment the
	// refusal was made rather than inferred afterwards from the shape that
	// resulted — the shape is the thing being explained. It follows the pattern
	// the scale gate already set for the job-level reading (see
	// store.RecordScaleGate): diagnosis, not control. Nothing reads it back to
	// decide anything, and losing it costs an explanation and nothing else.
	//
	// It answers the question a finished graph otherwise cannot: a node that
	// stayed a leaf because nobody could name two pieces and a node that stayed
	// a leaf because the pieces would have run one after another look identical
	// once the run is over, and the reading that separated them is a model's and
	// does not repeat. Empty means no split was ever considered for this node.
	Undivided string `json:"undivided,omitempty"`

	// Bundle marks the synthesis of declared-independent requests. Its merge
	// is assembly unless something mid-flight said otherwise, and the executor
	// reads this to skip the model when there is nothing to reconcile —
	// measured: a sink re-typing a 1,863-word part spent 121 of a 212-second
	// job saying what the parts had already said.
	Bundle bool `json:"-"`

	Needs []int  `json:"needs"`
	Size  Size   `json:"size,omitempty"`
	State State  `json:"state"`
	Kind  Kind   `json:"kind"`
	Brief string `json:"brief,omitempty"`

	// Subharness names the worker that takes this node whole. It is decided
	// where size is decided — a node oversized for one agent working alone can
	// be one job for a specialist — and it travels with the node from that
	// judgment to the executor that runs it, through the file the graph is
	// persisted to and through the splice that admits it to the store.
	//
	// "linear" is the generalist, chosen, and is what nearly every judged node
	// carries. Empty is the different fact that nobody judged this node at all,
	// and only that fact lets a reader downstream supply an answer of its own.
	Subharness string `json:"subharness,omitempty"`

	// Contract is the working method for this leaf: how an agent should work
	// this particular kind of job, as distinct from the Brief, which says what
	// the job is. A generic loop with a per-task contract is what lets one
	// executor match a specialised harness on any given leaf without the
	// harness itself changing.
	Contract string `json:"contract,omitempty"`

	// Spec is the same two facts as an object, plus the one nothing carried
	// before: the criterion this node's work is judged finished against.
	//
	// Brief and Contract stay the source of truth for one release and Spec is
	// written beside them — dual-write, single read — so rolling the wave back
	// is a one-line swap at each reader rather than a migration. What the
	// object buys is the retry path: a replacement node inherits Done verbatim
	// instead of re-authoring a spec from failure context, which is how the
	// module name, the filename and the acceptance check used to disappear the
	// moment a leaf was re-aimed at a different worker.
	Spec Spec `json:"spec,omitzero"`

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

	// Verdict is how the leaf ended, as distinct from State. State answers "may
	// its dependents run", and StateDone answers yes to a leaf that stopped
	// halfway because it ran out of budget — correctly, since the dependents
	// still need whatever it produced. Verdict answers the other question, the
	// one nothing could ask before: was that a success. Anything that learns
	// from a run reads this field and never State.
	Verdict provider.Verdict `json:"verdict,omitempty"`

	// Calibration is what the worker said about its own fit for this node, and
	// EscalatedFrom names the worker that tried it first and could not finish.
	// Both are carried for one reader: the profile record this node becomes when
	// the run lands, and through it the call that rewrites the ruler. Empty on
	// every node the generalist takes first and finishes, which is nearly all of
	// them and every one of them in a build with no specialist.
	Calibration   []string `json:"calibration,omitempty"`
	EscalatedFrom string   `json:"escalated_from,omitempty"`

	// Checked is what this node's own worker ran to check itself, and what each
	// one found — one clause, already composed by whoever observed it.
	//
	// It is here for exactly one reader: the pass that looks at a job's
	// remainder and decides whether anything more is worth adding. A node that
	// changed files and whose suite came back green is finished, and that fact
	// existed only inside the worker — so the reviser, seeing a title and a
	// state, kept proposing children to run the tests again and re-investigate
	// what was already proved. Between 48% and 57% of a run's measured cost went
	// there. This is not a rule telling the reviser what to conclude; it is the
	// evidence it was reasoning without.
	Checked string `json:"checked,omitempty"`

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

	// Evidence is the standard of support the goal warrants — reading and
	// citing, running and measuring, or building and demonstrating. It is
	// settled with the scope and for the same reason: left unsaid, each subtree
	// picks its own and the expensive answer wins, which is how a short written
	// report became a benchmarking project.
	Evidence string `json:"evidence,omitempty"`

	// FileShaped says the ask names its own deliverable — a file or document by
	// name, or a change to material that already exists — which is the one case
	// where filing the result is delivery rather than an evasion of it. It rides
	// the graph because it is one fact about the goal and three passes need the
	// same answer: the instruction the deliverable owner receives, the working
	// method written for it, and anything later that judges what came back.
	//
	// False is the safe default and the shape almost every ask has, so a caller
	// that has not made the judgment leaves every prompt exactly as it was.
	//
	// Intended wiring: the chat session sets Options.FileShaped on plan.Build, or
	// graph.FileShaped before plan.Briefs/plan.Contracts, from the same judgment
	// its delivery gate already makes about the request — the bit that decides
	// whether a leaf is offered a workspace path at all (leafOutputHint). Nothing
	// here matches a phrase against the goal.
	FileShaped bool `json:"file_shaped,omitempty"`

	// Terrain is the workspace this run stands on, drawn in code at build start
	// and frozen. It is persisted with the graph for the same reason the settled
	// points are: a graph read back off disk is revised against the premises it
	// was built from, and a reviser that lost the picture would be judging what
	// happened against a workspace it was told about but cannot see.
	Terrain string `json:"terrain,omitempty"`

	// ContextTokens is the window of the model that structures this job: the
	// one that planned it, and the ones that later revise it and judge whether
	// it is finished. Zero means unknown, which is the honest answer for a graph
	// nobody told and never a claim that the model is small.
	//
	// It is persisted for the same reason Terrain is. The passes that read this
	// document after the build — the revision sentinel, the growth gate — are
	// reached through signatures that carry the document and nothing else, and a
	// graph read back off disk that had lost the window would quietly go back to
	// showing a 200k-token reviser 4 KiB of what has happened.
	ContextTokens int `json:"context_tokens,omitempty"`

	// Invoice is the measured price list rendered by the caller before the build
	// starts, and it is what the three passes that judge division are given in
	// place of guessing what a split costs. See invoice.go.
	//
	// It rides the graph rather than the options because expansion and the
	// sizing pass inside it are reached through the document alone, and a
	// sub-planner that lost the prices would be weighing a division against
	// nothing — which is the state this whole block exists to end.
	//
	// Empty is a machine with nothing measured yet, and it is also the whole of
	// the compatibility story: every prompt below renders exactly the bytes it
	// rendered before invoices existed. It is deliberately NOT persisted: prices
	// move every time a leaf lands, and a graph read back off disk a day later
	// carrying yesterday's prices would be quoting a measurement as a fact when
	// the measurement has since changed.
	Invoice string `json:"-"`

	Stages []Stage `json:"stages"`
	Nodes  []Node  `json:"nodes"`
	NextID int     `json:"next_id"`
	Usage  Usage   `json:"usage"`
}

// Window is the graph's context window, asked safely of a graph that may not be
// there. A caller reaching for a budget usually holds a *Graph that is nil on
// every path where the job was never planned, and nil is the same answer as
// unknown: use the fallback.
func (g *Graph) Window() int {
	if g == nil {
		return 0
	}
	return g.ContextTokens
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
	// Only the new edge can close a cycle, and it closes one exactly when the
	// node being depended on can already reach the node depending on it. So the
	// question is asked of those two nodes rather than of the whole graph: this
	// runs a hundred and fifty times during a build, and each run was sweeping
	// every node and allocating a fresh mark table to re-establish what it had
	// established on the previous call. It rests on the graph being acyclic
	// beforehand, which generation guarantees, every edit here preserves, and
	// Load checks for anything that arrives from a file.
	if g.reaches(need, id) {
		return fmt.Errorf("edge %d → %d would create a cycle", need, id)
	}
	node.Needs = mergeNeeds(node.Needs, []int{need})
	return nil
}

// reaches reports whether one node can arrive at another by following
// dependencies. It walks only what is actually reachable from the start, which
// in a plan is a handful of nodes rather than the graph.
func (g *Graph) reaches(from, target int) bool {
	seen := map[int]bool{}
	var walk func(id int) bool
	walk = func(id int) bool {
		if id == target {
			return true
		}
		if seen[id] {
			return false
		}
		seen[id] = true
		node := g.Node(id)
		if node == nil {
			return false
		}
		for _, need := range node.Needs {
			if walk(need) {
				return true
			}
		}
		return false
	}
	return walk(from)
}

// setNeeds replaces a node's dependency list wholesale during generation.
//
// Edges into an earlier stage are acyclic by construction and go straight in. A
// same-stage edge is not — it is the mutation ordering, the one case where two
// simultaneous parts have to be sequenced because one of them changes what the
// other works on — so those are added through AddNeed, which drops the second
// edge of any pair that would close a cycle. Edges into a later stage are still
// impossible and are discarded.
func (g *Graph) setNeeds(id int, needs []int) {
	node := g.Node(id)
	if node == nil || node.State.Frozen() {
		return
	}
	kept := make([]int, 0, len(needs))
	seen := map[int]bool{}
	var siblings []int
	for _, need := range needs {
		source := g.Node(need)
		if source == nil || need == id || seen[need] || source.Stage > node.Stage {
			continue
		}
		seen[need] = true
		if source.Stage == node.Stage {
			siblings = append(siblings, need)
			continue
		}
		kept = append(kept, need)
	}
	sort.Ints(kept)
	node.Needs = kept
	for _, sibling := range siblings {
		_ = g.AddNeed(id, sibling)
	}
}

// hasCycle answers the question of the whole graph, and it is the entry check
// rather than the per-edge one. Generation cannot produce a cycle — edges may
// point only at earlier stages — and AddNeed refuses any edit that would close
// one, so the invariant holds for as long as a graph stays in memory. A graph
// read back off disk has had none of that applied to it, so Load pays for one
// full sweep to establish the premise the cheap per-edge check then relies on.
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

// deliverableSink names the node that holds the finished whole: the synthesis
// the harness appends once nothing else gathers the plan.
//
// It is a leaf in every operational sense — it is dispatched like one, and what
// it produces is the whole of what the person who asked will read — but its
// kind keeps it out of Leaves(), so for a long time the one node in a plan whose
// job is to BE the deliverable was the one node with no instruction and no
// working method. Everything that judges a finished job judges this node.
//
// Zero means there is none to write for: a one-node plan is already its own
// answer, and an unfinished graph has not gathered yet.
func (g *Graph) deliverableSink() int {
	sinks := g.Sinks()
	if len(sinks) != 1 {
		return 0
	}
	node := g.Node(sinks[0])
	if node == nil || node.Kind != KindSynthesis {
		return 0
	}
	return node.ID
}

// writtenLeaves are the nodes the instruction and working-method passes write
// for: every work leaf, plus the deliverable owner when the plan has one. It is
// the honest denominator for those passes too — counting Leaves() while writing
// one more than that is how progress reads "6/5".
func (g *Graph) writtenLeaves() []int {
	ids := g.Leaves()
	if sink := g.deliverableSink(); sink != 0 {
		ids = append(ids, sink)
	}
	return ids
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

// anchorLateStarts is the structural backstop behind bind and audit.
//
// Both of those passes are judgment calls, and both can legitimately leave a
// late node with no inputs: bind is written to under-connect, and audit asks
// only whether a node is finishable — which a synthesis-shaped node technically
// is, by redoing everything upstream itself. One run did exactly that: the node
// meant to write the final review ended binding with an empty list, launched at
// t=0 alongside the work it existed to consume, and exhausted its whole budget
// reproducing the plan single-handed while the deliverable never appeared.
//
// The contradiction is structural, so the repair is too. The spine placed a
// late node late for a reason; a stage>1 node that ends up needing nothing is
// wired to the frontier — every earlier-stage node whose output nothing else
// consumes. Needs is also the context routing table, and for a gathering node
// the unconsumed frontier is exactly the right input. If the frontier is empty
// (everything earlier already consumed), it falls back to the nearest earlier
// stage that has nodes. Nodes are anchored in stage-then-ID order and the
// frontier is recomputed after each, so a second loose node chains behind the
// first deterministically. Edges go through AddNeed, so nothing here can close
// a cycle. Stage-1 nodes are untouched: needing nothing is their normal state.
// It returns how many edges it forced.
func (g *Graph) anchorLateStarts() int {
	var loose []int
	for _, node := range g.Nodes {
		if node.Stage > 1 && len(node.Needs) == 0 && !node.State.Frozen() {
			loose = append(loose, node.ID)
		}
	}
	sort.Slice(loose, func(i, j int) bool {
		left, right := g.Node(loose[i]), g.Node(loose[j])
		if left.Stage != right.Stage {
			return left.Stage < right.Stage
		}
		return left.ID < right.ID
	})

	forced := 0
	for _, id := range loose {
		node := g.Node(id)
		if node == nil || len(node.Needs) > 0 {
			continue
		}
		consumed := make(map[int]bool, len(g.Nodes))
		for _, other := range g.Nodes {
			for _, need := range other.Needs {
				consumed[need] = true
			}
		}
		var sources []int
		for _, other := range g.Nodes {
			if other.ID != id && other.Stage < node.Stage && !consumed[other.ID] {
				sources = append(sources, other.ID)
			}
		}
		if len(sources) == 0 {
			nearest := 0
			for _, other := range g.Nodes {
				if other.ID != id && other.Stage < node.Stage && other.Stage > nearest {
					nearest = other.Stage
				}
			}
			for _, other := range g.Nodes {
				if other.ID != id && other.Stage == nearest && nearest > 0 {
					sources = append(sources, other.ID)
				}
			}
		}
		sort.Ints(sources)
		for _, source := range sources {
			if g.AddNeed(id, source) == nil {
				forced++
			}
		}
	}
	return forced
}

// deliverableOwner names the single node that produces whatever final
// deliverable the goal asks for. Everything else contributes material to it.
//
// The goal text reaches every agent, so without an owner every agent reads
// "produce REVIEW.md" as its own instruction — one run had five nodes writing
// that file over the top of each other. Ownership is structural rather than
// asked for: it is the one node nothing else consumes.
//
// A zero id means the owner does not exist yet. Several sinks means the
// synthesis appended at the end of planning will gather them, and briefs for the
// rest of the graph are written before that node is created, so it is named by
// role instead of by number.
func (g *Graph) deliverableOwner() (int, string) {
	if sinks := g.Sinks(); len(sinks) == 1 {
		if node := g.Node(sinks[0]); node != nil {
			return node.ID, fmt.Sprintf("node %d, %q", node.ID, node.Title)
		}
	}
	return 0, "the final step that assembles every result"
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

// planBlock is the whole shared prefix of the passes that look at the entire
// graph — bind, size and audit. It is one render because it is one string: the
// three of them are deliberately given the identical premise, and rendering it
// per pass spent the same bytes three times over for a block that is the same
// every time. Whoever holds a render is responsible for knowing whether the
// graph has moved underneath it; see the reuse in Build.
func (g *Graph) planBlock() string {
	return g.context() + "\nEvery node in the plan:\n" + g.catalog()
}

// stateBlock renders the graph for the reviser, which unlike every other call
// has to know what has already happened and what it is therefore not allowed to
// touch.
func (g *Graph) stateBlock() string {
	var block strings.Builder
	// Read once, at the top, and spent down from there: the two numbers decide
	// what the reviser is shown, so they must be the same two for every node of
	// one render.
	budget, perNode := g.stateResultBudget()
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
		// What a settled node actually produced, which is the only thing a
		// contradiction can be found in. Without it the sentinel was asked to
		// judge whether a result contradicts an assumption while seeing neither:
		// the plan as designed, and one node's title with a state beside it.
		// Bounded per node and per block, because this is a structuring call
		// whose whole value is that it is short — a plan with thirty landed
		// leaves must not turn one revision into a full transcript replay.
		if written := writeStateResult(&block, node, budget, perNode); written > 0 {
			budget -= written
		}
	}
	return block.String()
}

// stateResultBytes is what one settled node may contribute of its own result,
// and stateResultsBytes is what all of them may contribute together. The first
// keeps a single verbose leaf from crowding out its siblings; the second keeps
// a large graph from crowding out the plan.
//
// They are the fallback pair now, not the pair: a graph that knows the window of
// the model revising it sizes both from it. See stateResultBudget.
const (
	stateResultBytes  = 600
	stateResultsBytes = 4 << 10
)

// stateResultsShare of statePromptShares is how much of the reviser's prompt the
// landed results may take. The rest is the block they are hung on — one line per
// node for the whole graph — plus the goal's context and the event being judged,
// and the reviser's whole job is to read the second against the first.
const (
	stateResultsShare = 1
	statePromptShares = 2

	// stateFloorTokens is the reviser's turn before any of this: the revise
	// prompt, the schema, the context block.
	stateFloorTokens = 4 << 10

	// stateNodeDivisor is how many nodes' worth of result the whole pot is cut
	// into. It is what makes the per-node clip a share rather than a second
	// absolute ceiling; the fallback pair already stood in roughly this ratio
	// (4096/600 is under seven), so an unknown window keeps its old numbers.
	stateNodeDivisor = 8
)

// stateResultBudget is what the reviser may be shown of what has happened: the
// whole pot, and the most any single node may take of it.
//
// An unknown window returns exactly the pair this file carried before ctxbudget
// existed, which is the whole of the rollback.
func (g *Graph) stateResultBudget() (pot, perNode int) {
	pot = ctxbudget.For(g.Window()).WithFloor(stateFloorTokens).
		Share(stateResultsShare, statePromptShares, stateResultsBytes)
	perNode = pot / stateNodeDivisor
	if perNode < stateResultBytes {
		perNode = stateResultBytes
	}
	return pot, perNode
}

// writeStateResult renders one settled node's outcome and reports what it
// spent. A failure is rendered in preference to a result because a failure is
// the sharper signal: it says the plan's next steps may have nothing to consume.
func writeStateResult(block *strings.Builder, node Node, budget, perNode int) int {
	if budget <= 0 {
		return 0
	}
	body := strings.TrimSpace(node.Failure)
	label := "failed"
	if body == "" {
		body = strings.TrimSpace(node.Result)
		label = "produced"
	}
	checked := strings.TrimSpace(node.Checked)
	if body == "" && len(node.Artifacts) == 0 && checked == "" {
		return 0
	}
	room := budget
	if room > perNode {
		room = perNode
	}
	line := fmt.Sprintf("      %s: %s", label, firstParagraph(clipRunes(body, room)))
	if len(node.Artifacts) > 0 {
		line += "\n      files: " + strings.Join(node.Artifacts, ", ")
	}
	// What the node's own worker checked, on its own line, in the worker's
	// words. It is the one thing here the reviser cannot infer from anything
	// else it is shown: a result reads the same whether or not it was proved.
	if checked != "" {
		line += "\n      checked: " + firstParagraph(clipRunes(checked, perNode))
	}
	line += "\n"
	block.WriteString(line)
	return len(line)
}

// clipRunes cuts to a byte ceiling without splitting a character. Every
// truncation in this tree backs off to a rune boundary; a mangled character
// here would ride the sentinel's whole prompt.
func clipRunes(body string, limit int) string {
	if len(body) <= limit {
		return body
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(body[cut]) {
		cut--
	}
	return strings.TrimSpace(body[:cut]) + "…"
}

// firstParagraph keeps the render one node per block by folding newlines. The
// state block's shape is one indented line per node, and a result that brings
// its own line breaks would read as several unnumbered nodes.
func firstParagraph(body string) string {
	return strings.Join(strings.Fields(body), " ")
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
	// Nothing that produced this file can be trusted to have been us. A cyclic
	// graph is not merely wrong, it is unschedulable — every node in the loop
	// waits forever on another one — and every pass downstream of here assumes
	// it is acyclic, so it is refused at the door rather than diagnosed later.
	if graph.hasCycle() {
		return nil, errors.New("load graph: needs form a dependency cycle")
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
