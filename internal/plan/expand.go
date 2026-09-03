package plan

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// expandScope is the context a sub-planner is given. It is deliberately thin.
//
// The node being expanded needs to know the goal it ultimately serves, where it
// sits, and what its neighbours are called so it does not wander into them. It
// does not need their summaries, their sources, or anything from another
// subtree — that is context pollution with extra steps, and it would also break
// the frozen prefix that every expansion at this level shares.
type expandScope struct {
	Goal     string
	Ancestry []string
	Siblings []string
	Inputs   []string
	// Invoice is the measured price list, carried from the graph so the
	// sub-planner weighs its division against the same numbers the sizing pass
	// did. Empty on a machine with nothing measured, which renders nothing.
	Invoice string
	// Claim is what only a claim-time caller knows: what this node's
	// dependencies actually produced, and what the node is judged finished
	// against. Empty at build time, which is why the build's bytes are
	// unchanged.
	Claim ClaimContext
}

// ClaimContext is what a node's expansion is worth more for having, and what
// only exists once the node is claimed rather than merely planned.
//
// At build time a node is decomposed against the *titles* of the work feeding
// it, because nothing has run yet. At claim time every one of those pieces has
// landed and said what it produced, so the same question can be asked against
// what is really there. The criterion travels with it for the other half of the
// same correction: a division whose parts do not together cover what the node
// is judged on is a division that loses the node's own answer.
type ClaimContext struct {
	// Landed is what each finished dependency produced, in admission order.
	Landed []Landed
	// Criterion is what must be true of this node when it is finished.
	Criterion Done
}

// Empty reports whether a claim-time caller supplied anything at all. The zero
// value is the build, and renders the prompt the build has always rendered.
func (c ClaimContext) Empty() bool { return len(c.Landed) == 0 && c.Criterion.Empty() }

// criterionLimit bounds the criterion inside an expansion goal, for the same
// reason the remainder path bounds it: the goal already carries the node, its
// ancestry and its inputs, and the criterion is the smallest of the four.
const criterionLimit = 1200

// render lays the scope out stable-first, because every expansion in a job
// shares its head and only its tail is the node.
//
// The order is the whole of this call's cache discipline (L7): the goal is
// identical for every expansion of the job, ancestry and siblings are stable per
// parent, the burden is a constant, and the two claim-time blocks — what landed,
// and what this node is judged on — are the delta, so they come last. Putting
// the landed digests where the input titles used to sit would have buried a
// per-node payload in the middle of a prefix a whole job shares.
func (s expandScope) render(node *Node) string {
	var block strings.Builder
	fmt.Fprintf(&block, "This is part of a larger goal:\n%s\n\n", s.Goal)
	if len(s.Ancestry) > 0 {
		fmt.Fprintf(&block, "It sits under: %s\n\n", strings.Join(s.Ancestry, " → "))
	}
	if len(s.Siblings) > 0 {
		fmt.Fprintf(&block, "Other work happening alongside it, which it must not duplicate or stray into:\n  %s\n\n",
			strings.Join(s.Siblings, "\n  "))
	}
	// The titles are the answer only while there is no better one. Once the
	// work has actually run, naming what it was called instead of what it
	// produced would be describing the node's inputs to a planner that could
	// have read them.
	if len(s.Inputs) > 0 && len(s.Claim.Landed) == 0 {
		fmt.Fprintf(&block, "It will receive the results of:\n  %s\n\n", strings.Join(s.Inputs, "\n  "))
	}
	block.WriteString(expandBurden)
	block.WriteString("\n\n")
	if len(s.Claim.Landed) > 0 {
		block.WriteString(landedPreamble)
		block.WriteString("\n")
		for _, landed := range s.Claim.Landed {
			title := strings.TrimSpace(landed.Title)
			result := strings.TrimSpace(landed.Result)
			if title == "" && result == "" {
				continue
			}
			fmt.Fprintf(&block, "\n%s\n%s\n", title, result)
		}
		block.WriteString("\n")
	}
	if !s.Claim.Criterion.Empty() {
		fmt.Fprintf(&block, "%s\n%s\n%s\n\n", criterionPreamble,
			strings.TrimSpace(Spec{Done: s.Claim.Criterion}.Render(criterionLimit)), criterionCoverage)
	}
	fmt.Fprintf(&block, "Break down only this piece of it:\n%s — %s", node.Title, node.Summary)
	if len(node.Sources) > 0 {
		fmt.Fprintf(&block, "\nIt must touch: %s", strings.Join(node.Sources, "; "))
	}
	// Last, behind the node itself, which is already the most volatile thing in
	// this block. The prices move whenever a leaf lands; the goal, the ancestry,
	// the siblings and the burden do not, and they are the prefix every
	// expansion in this job shares.
	return withInvoice(block.String(), s.Invoice)
}

// landedPreamble introduces the results of the work this node consumes.
//
// Its second sentence is the whole reason the block exists. A sub-planner handed
// finished results still plans against the assignment it was given unless it is
// told which of the two is the ground truth, and the assignment is the older
// document: it was written before anything ran and it says what the inputs were
// expected to be, not what they are.
//
// It names no domain, no file and no artifact, because what "produced" means is
// the caller's business and stating it here would put a shape in a prompt every
// subtree of every job passes through.
const landedPreamble = "The work below has already finished, and this is what each piece actually " +
	"produced. Break this piece down against what is really there, not against what was " +
	"expected of it before anything ran."

// criterionPreamble states the coverage rule the parts have to satisfy
// together. It is the positive stopping condition doing its second job: it
// bounds a division from both sides at once — every part has to earn its place
// against the criterion, and the parts together have to reach all of it — which
// is the one thing the burden of proof above cannot say, because the burden is
// about whether to divide and this is about where the division may land.
const criterionPreamble = "When this piece is finished, this must be true of it:"

// criterionCoverage is the rule the criterion is quoted in order to state.
const criterionCoverage = "Every part you name has to contribute to that, and the parts together have to " +
	"cover all of it — no more and no less."

// expandBurden states the split's burden of proof where the sub-planner will
// read it, which is the one place in the expansion path that is written by this
// package rather than inherited from the fan-out prompt.
//
// It is here and not in that prompt because the two are asking different
// questions. The fan-out prompt divides a stage that has already been read as
// having structure; expansion re-opens a node that a sizing pass already judged,
// and re-opening is exactly where nodes get made for their own sake — something
// was asked to decompose, so something comes back decomposed. So the null
// hypothesis is restated at the point of the second question, in the same terms
// the sizing pass used, and the two things that discharge it are named before
// the node is.
//
// It carries no examples deliberately. An example here would be a domain, and a
// domain named in a prompt that every subtree of every job passes through is a
// shape the model will find again whether or not it is there.
const expandBurden = `This piece is one job unless dividing it proves what it buys. Return it as a
single part unless the division is bought on all three of what is being spent at
once: the time the person waits, what the work costs to run, and how good the
answer comes back.

Only two things buy it. Either the parts genuinely run at the same time, none of
them waiting on another, so the wait becomes the longest part instead of the sum
of all of them; or this piece cannot be carried to an end inside what one worker
can hold, so it would be stopped and resumed however it is written. Steps that
follow one another buy neither: they land no sooner than the undivided piece and
they add a briefing apiece and a reassembly at the end that it never paid for.

Every part must be sayable more precisely than this piece is — a deliverable of
its own and its own condition for being finished, both writable without
reference to the other parts. Narrower wording of the same job is not a part,
and two parts that would finish on the same condition are one part with the
answer bought twice. Where you cannot carry that, one part is the right answer
and a common one.`

// expansion is one node's attempt at being decomposed, before we decide whether
// to keep it.
type expansion struct {
	nodeID int
	sub    *Graph
	usage  Usage
	err    error
}

// ExpandLevel decomposes every node worth decomposing, all at the same time,
// and returns how many were actually spliced in.
//
// The parallelism is the point. Each expansion is a complete four-pass build,
// so a level costs four call-rounds no matter how many nodes expand — the cost
// of recursion is measured in depth, never in width. Depth is the only thing
// here that is genuinely serial, which is why the depth cap is the guard that
// matters most.
func ExpandLevel(ctx context.Context, client Completer, graph *Graph, options Options) (int, Usage, error) {
	candidates := selectForExpansion(graph, options)
	if len(candidates) == 0 {
		return 0, Usage{}, nil
	}

	results := make([]expansion, len(candidates))
	var group sync.WaitGroup
	for index, nodeID := range candidates {
		group.Add(1)
		go func(index, nodeID int) {
			defer group.Done()
			// A faulted expansion is a node that did not split. Nothing is
			// spliced, the level reports the failure, and the node runs whole —
			// which is what a failed sub-plan already means here.
			defer func() {
				if recovered := recover(); recovered != nil {
					results[index] = expansion{nodeID: nodeID, err: guard.Note(fmt.Sprintf("plan/expand node %d", nodeID), recovered)}
				}
			}()
			results[index] = expandOne(ctx, client, graph, nodeID, options)
		}(index, nodeID)
	}
	group.Wait()

	var usage Usage
	var failures []error
	spliced := 0
	for _, result := range results {
		usage.merge(result.usage)
		if result.err != nil {
			failures = append(failures, result.err)
			continue
		}
		if keep, reason := worthKeeping(result); !keep {
			JournalRefusal(graph.Node(result.nodeID), reason)
			continue
		}
		// The budget is enforced here rather than at selection, because until a
		// node has actually been expanded nobody knows how many children it
		// produced. Estimating beforehand overshot by a third on the first real
		// run; counting at the splice cannot.
		incoming := workNodes(result.sub)
		if len(graph.Nodes)+incoming > options.NodeBudget {
			JournalRefusal(graph.Node(result.nodeID), RefusalNoRoom)
			continue
		}
		if err := graph.Splice(result.nodeID, result.sub); err != nil {
			failures = append(failures, err)
			continue
		}
		// The node was divided after all, so whatever refusal an earlier round
		// or an earlier gate wrote onto it is no longer true of it. The field
		// describes the shape the graph has, never the shapes it considered.
		if parent := graph.Node(result.nodeID); parent != nil {
			parent.Undivided = ""
		}
		spliced++
	}
	return spliced, usage, joinErrors(failures)
}

// SplitVerdict is the answer to one question asked of one node: may this node be
// divided, and if the answer is no, in what words.
//
// Reason is filled on refusal and empty on admission, because a node that is
// about to be divided has nothing to explain — the shape it ends up with is the
// explanation. It is prose rather than a code because the only reader is a
// person asking why a graph looks the way it does.
type SplitVerdict struct {
	Divide bool
	Reason string
}

// The refusals, in the words they are journaled in. They are constants for the
// same reason the scale gate's routes are: a reader counting them across a week
// of runs must not have to guess whether two spellings mean one thing.
//
// Each names a way the burden of proof went unmet, and none of them names a
// domain — the judgment is about the shape of work and never about what the work
// is about.
const (
	// RefusalUnnamed is the ordinary one and the one the whole burden is built
	// around: nobody could name two pieces, so there is no split to weigh.
	RefusalUnnamed = "no two pieces could be named for it"
	// RefusalWithinReach is the null hypothesis holding: the node is already
	// inside what one worker carries, so division buys no wait it does not
	// already have and costs a briefing and a reassembly it does not pay now.
	RefusalWithinReach = "it is already within one worker's reach"
	// RefusalDepth and RefusalNoRoom are the two arithmetic refusals. They are
	// not judgments about the work and say so, so that a reader does not read
	// the graph's shape as a reading of the material when it was a ceiling.
	RefusalDepth  = "the depth ceiling was reached before it was weighed"
	RefusalNoRoom = "no room was left to carry its pieces"
	// RefusalSameAnswer is the information-gain rule failing: the pieces came
	// back returning the same thing, which is one answer bought twice.
	RefusalSameAnswer = "its pieces would each return the same thing"
	// RefusalOnePiece and RefusalNoSmaller are the restatement failures — a
	// division that gave back the node in different words, or in the same size.
	RefusalOnePiece  = "the division gave back one piece, which is the node again"
	RefusalNoSmaller = "its pieces came back no smaller than it is"
	// RefusalNotPaying is the EV-lookahead's verdict: the node could divide,
	// but the measured history says its parts do not buy back the fixed cost a
	// second worker pays before it produces. It is a measured refusal, not a
	// structural one.
	RefusalNotPaying = "the measured history says the split does not pay for itself"
)

// JudgeSplit is the burden of proof as a predicate over one node.
//
// Atomic-and-run is the null hypothesis: this returns a refusal unless something
// about the node actually argues for dividing it, and the arguments are exactly
// the two the sizing prompt names — pieces that could genuinely run at the same
// time, which is what a named part list is evidence of, or a node that does not
// fit inside what one worker can carry, which is what a size beyond atomic says.
// Everything else is a refusal with its reason attached.
//
// Measured capacity sharpens only the last boundary. When enough journaled
// leaves show that atomic-sized work often overruns, an atomic node that has
// already cleared every refusal above and named independent parts may divide.
// This does not rescue an unnamed split or invert the null hypothesis. Zero
// measurements — including every non-swarm caller — therefore take the old
// branches byte for byte.
//
// It is a free function over a node and the options rather than a step inside
// the level loop, and that shape is the point: the same question has to be
// asked again later, when a worker claims a leaf and the graph has moved on
// since planning. A claim-time caller reads the node it is about to hand out,
// asks this, and either divides it or records the refusal exactly as the level
// loop does. That wiring is deliberately not done here — this package still
// decides nothing at claim time — but the predicate is written so that doing it
// is a call and not a refactor.
//
// The budget is not asked about here. It is a fact about the whole graph rather
// than about this node, and folding it in would make a per-node question answer
// differently depending on who else is in the graph.
func JudgeSplit(node *Node, options Options) SplitVerdict {
	if node == nil || node.Kind != KindWork {
		return SplitVerdict{}
	}
	if node.Depth >= options.MaxDepth {
		return SplitVerdict{Reason: RefusalDepth}
	}
	// The pre-check. Sizing already named the parts this node would split into,
	// at no extra cost, and a node that could not name two of them is a node
	// whose expansion the acceptance check is going to throw away anyway.
	// Skipping it here saves the whole fan-out — one run burned 17,000 output
	// tokens producing splits that were all rejected.
	//
	// An oversized node is the one exception, and the sizing prompt is why: it
	// is told to name the ordered stages of a node whose inside is a sequence,
	// and a node it has ALSO put beyond one worker's reach with nothing named is
	// a node it could not name a division of at all. Neither is a reason to leave
	// it whole — the burden's second discharge stands either way — so it goes to
	// the stage question, which costs one call and no fan-out. Everything else
	// keeps the pre-check: atomic-and-run is still the null hypothesis, and a
	// node within one worker's reach is never staged.
	//
	// AND A NODE WHOSE OWN WORDS NAME TWO OR MORE PIECES IS NOT ONE SITTING
	// UNTIL THE STAGE QUESTION SAYS SO. The pre-check reads what the ruler
	// named; this reads what the node itself says, which is the evidence that
	// was on the node all along and that nothing was looking at. A stage the
	// spine wrote as "North: …; South: …; East: …" and the ruler then called
	// atomic with no parts was refused here as unnamed and run whole — three
	// disjoint lanes over one 144 KB file, in one sitting, on two draws of
	// three. Reading the enumeration is free (enumerated.go, no call), and it
	// decides nothing: it only stops the refusal, and the division is then
	// asked for and either drawn or refused as it always was.
	//
	// It is asked of a fresh plan and never of a remainder, which is the whole
	// of what admitsEnumeratedPieces adds to the reading: a remainder's list is
	// one worker's assignment, so a remainder is admitted on the ruler's size
	// and on nothing else.
	enumerated := admitsEnumeratedPieces(node, options)
	if len(node.Parts) < 2 && node.Size != SizeOversized && !enumerated {
		return SplitVerdict{Reason: RefusalUnnamed}
	}
	switch node.Size {
	case SizeOversized, SizeBorderline:
		return SplitVerdict{Divide: true}
	case SizeAtomic:
		// The atomic verdict is the ruler's reading of a title and a summary,
		// and where those same words enumerate their own pieces the two
		// readings disagree. The disagreement is not settled here — this
		// admits the node to the question, and the question (sequence.go) is
		// free to answer with one piece, which is the refusal that gets
		// journaled.
		if enumerated {
			return SplitVerdict{Divide: true}
		}
		if options.CapacitySamples >= capacityEvidenceFloor &&
			options.CapacityOverrunRate > capacityOverrunThreshold {
			return SplitVerdict{Divide: true}
		}
	}
	return SplitVerdict{Reason: RefusalWithinReach}
}

const (
	capacityEvidenceFloor    = 8
	capacityOverrunThreshold = 0.25
)

// JournalRefusal writes why a node was left whole, on the node. It is diagnosis
// and never control: nothing reads the field back to decide anything, and a
// refusal recorded on a node that is later divided anyway is cleared at the
// splice, so the field always describes the shape the graph actually has.
//
// Frozen nodes are left alone. Their shape is settled and something else owns
// them now, so a note written onto them would be both useless and a write into
// another party's node.
func JournalRefusal(node *Node, reason string) {
	if node == nil || node.State.Frozen() || reason == "" {
		return
	}
	node.Undivided = reason
}

// selectForExpansion decides who gets to expand, and it is where the model's
// judgment meets the budget. Oversized nodes always qualify; borderline ones
// only when there is room left, so the budget is spent on the nodes most likely
// to be hiding serial work rather than on ties.
//
// The per-node half of the decision is JudgeSplit and nothing else; what is left
// here is the arithmetic — who fits. Every refusal on either side is journaled
// where it was made, so a finished graph can say why each of its leaves is one.
func selectForExpansion(graph *Graph, options Options) []int {
	remaining := options.NodeBudget - len(graph.Nodes)
	var oversized, borderline []int
	for index := range graph.Nodes {
		node := &graph.Nodes[index]
		if node.Kind != KindWork || node.State.Frozen() {
			continue
		}
		verdict := JudgeSplit(node, options)
		if !verdict.Divide {
			JournalRefusal(node, verdict.Reason)
			continue
		}
		// EVERY ADMISSION LANDS IN A LIST. An oversized node is the strong claim
		// and always qualifies; everything else JudgeSplit admits — a borderline
		// node, and an atomic one admitted on measured capacity because the
		// journal says work this size overruns — is the weaker claim and queues
		// where the ties queue, behind the oversized and only while there is
		// room. Switching on the size alone dropped the measured admission on the
		// floor: the node was neither divided nor refused, so nothing divided it
		// and nothing could say why.
		if node.Size == SizeOversized {
			oversized = append(oversized, node.ID)
			continue
		}
		borderline = append(borderline, node.ID)
	}

	// Each expansion adds roughly a handful of nodes; budgeting at four keeps
	// the estimate honest without needing to know the answer in advance.
	const nodesPerExpansion = 4
	selected := oversized
	if len(selected)*nodesPerExpansion > remaining {
		affordable := remaining / nodesPerExpansion
		if affordable < 0 {
			affordable = 0
		}
		if affordable < len(selected) {
			journalRefusals(graph, selected[affordable:], RefusalNoRoom)
			journalRefusals(graph, borderline, RefusalNoRoom)
			return selected[:affordable]
		}
	}
	room := remaining - len(selected)*nodesPerExpansion
	for position, id := range borderline {
		if room < nodesPerExpansion {
			journalRefusals(graph, borderline[position:], RefusalNoRoom)
			break
		}
		selected = append(selected, id)
		room -= nodesPerExpansion
	}
	return selected
}

// journalRefusals records one reason against a run of node ids.
func journalRefusals(graph *Graph, ids []int, reason string) {
	for _, id := range ids {
		JournalRefusal(graph.Node(id), reason)
	}
}

// ExpandOne is one node's decomposition, asked for by a caller that is not a
// build level.
//
// The build asks this question of every candidate at once, at t=0, against the
// titles of work that has not started. A scheduler asks it of one node, at the
// moment that node is claimed, when its dependencies have landed and can be
// read — which is the same two calls against a strictly better picture. What
// comes back is a sub-graph and nothing else: the caller decides whether to
// keep it (WorthKeeping), where to put it (Graph.Splice), and what a refusal is
// called (JournalRefusal), because those are the three things a claim-time
// caller must do differently from a level loop and the only three.
func ExpandOne(ctx context.Context, client Completer, graph *Graph, nodeID int, options Options, claim ClaimContext) (*Graph, Usage, error) {
	result := expandScoped(ctx, client, graph, nodeID, options, claim)
	return result.sub, result.usage, result.err
}

func expandOne(ctx context.Context, client Completer, graph *Graph, nodeID int, options Options) expansion {
	return expandScoped(ctx, client, graph, nodeID, options, ClaimContext{})
}

func expandScoped(ctx context.Context, client Completer, graph *Graph, nodeID int, options Options, claim ClaimContext) expansion {
	node := graph.Node(nodeID)
	if node == nil {
		return expansion{nodeID: nodeID, err: fmt.Errorf("expand: node %d does not exist", nodeID)}
	}
	scope := scopeFor(graph, node)
	scope.Claim = claim
	goal := scope.render(node)

	// An expansion reuses the fan-out and sizing passes, but it is not doing what
	// they do at the top of a plan: the premise is one node rather than the whole
	// goal, the catalog is short, and the question is narrower. Ability on the two
	// is not the same measurement, so the class the inner calls would name for
	// themselves is overridden for the whole subtree.
	ctx = provider.WithCallClass(ctx, provider.ClassPlanExpand)
	// WHICH NODE IS BEING SPLIT. The class above already says these calls are an
	// expansion rather than a top-level fan-out; the node says whose, which is
	// the fact a person reading a run with several expansions in it needs.
	ctx = provider.WithCallNode(ctx, callNodeKey(nodeID))

	// A sub-decomposition is one fan-out, no spine, no binding. Running a full
	// staged build inside each node was the first instinct and it was wrong —
	// every subtree contributed its own internal depth, and two levels of
	// recursion turned a graph with a critical path of 3 into one with a
	// critical path of 12. Depth multiplies where width adds. The expansion
	// costs two calls instead of eight, and that is the discipline the second
	// move below is written to keep.
	//
	// What the restriction may NOT do is decide that a node whose inside is a
	// sequence stays whole. That was the reading here for a long time — split to
	// find work that can happen at once, and where there is none, leave it — and
	// it is only half of the burden the sizing pass states: a node that cannot be
	// brought to an end inside what one worker can hold is divided whether or not
	// anything in it runs at the same time. The fan-out asks the first question;
	// sequence.go asks the second, of the nodes the first one could not answer.
	// The subtree inherits the settled points verbatim, the evidence standard
	// included. Without this a sub-planner rebinds the goal's free variables for
	// itself, which is exactly how one expansion produced Berlin, Paris and
	// Madrid while another produced Amsterdam — each answer defensible, the pair
	// useless — and how a subtree of a written report decides on its own that the
	// comparison needs a benchmark harness first. The terrain travels with them:
	// a sub-planner is the part of the build most likely to invent material that
	// is not there, and it is the one place a fresh render would be forbidden
	// anyway, since by now workers are writing into that directory.
	sub := &Graph{
		Goal:     goal,
		Settled:  graph.Settled,
		Open:     graph.Open,
		Evidence: graph.Evidence,
		Terrain:  graph.Terrain,
		// The prices travel with them. The sizing pass inside this expansion is
		// the same judgment made one level down, and a sub-graph that lost the
		// invoice would be the one place in the system that still weighs a
		// division against nothing.
		Invoice: graph.Invoice,
		Stages:  []Stage{{Title: node.Title, Summary: node.Summary}},
		NextID:  1,
	}
	// AN OVERSIZED NODE WITH NO SIMULTANEOUS PIECES NAMED IS EVIDENCE OF A
	// SEQUENCE, NOT A REASON TO LEAVE IT WHOLE. The ruler has already been asked
	// which pieces of this node could run at the same time and has answered with
	// none, so buying a fan-out to ask a second time is buying the answer twice.
	// The stage question is asked directly, and the whole expansion is one call.
	if node.Size == SizeOversized && len(node.Parts) < 2 {
		return expandAsStages(ctx, client, sub, node, goal, Usage{})
	}

	nodes, fanUsage, err := FanOutWith(ctx, client, sub.context(), sub.Stages, sub.Settled)
	usage := fanUsage
	if len(nodes) == 0 {
		return expansion{nodeID: nodeID, usage: usage, err: fmt.Errorf("expand %q: %w", node.Title, err)}
	}
	// THE SECOND MOVE. The fan-out has answered the simultaneity question with
	// the node itself, which for a node past one worker's reach is the answer
	// that leaves the burden's other discharge standing: it cannot be carried to
	// an end in one sitting, so it is divided in time instead. Sizing the single
	// restated part would buy a verdict nobody can act on — the node's own size
	// is already on the node — so that call is spent on the stages instead.
	if len(nodes) == 1 && dividesInTime(node, options) {
		return expandAsStages(ctx, client, sub, node, goal, usage)
	}
	for _, child := range nodes {
		sub.Add(child)
	}
	// A failed sizing pass leaves every child unjudged, which sizeApply reads as
	// atomic, so the division stands as it was drawn. It is deliberately not
	// carried out as this expansion's error: an expansion that returns one would
	// be thrown away whole, and a ruler that could not answer is no reason to
	// discard a division that has already been paid for.
	sizeUsage, _ := SizeNodes(ctx, client, sub)
	usage.merge(sizeUsage)
	// AND EVERY CHILD IS MINTED INSIDE ITS PARENT'S SPEC. It is the last thing
	// done here, below every pass that adds a node, so a branch that mints
	// children another way still passes through it. See Graph.mintedInside for
	// what each field of the spec inherits and why.
	sub.mintedInside(node.Spec)
	sub.Goal = node.Title
	return expansion{nodeID: nodeID, sub: sub, usage: usage}
}

// scopeFor assembles what the sub-planner is allowed to see.
func scopeFor(graph *Graph, node *Node) expandScope {
	scope := expandScope{Goal: graph.Goal, Invoice: graph.Invoice}
	for ancestor := node.Parent; ancestor != 0; {
		parent := graph.Node(ancestor)
		if parent == nil {
			break
		}
		scope.Ancestry = append([]string{parent.Title}, scope.Ancestry...)
		ancestor = parent.Parent
	}
	for _, other := range graph.Nodes {
		if other.ID == node.ID || other.Kind == KindSynthesis || other.Parent != node.Parent {
			continue
		}
		scope.Siblings = append(scope.Siblings, other.Title)
	}
	for _, need := range node.Needs {
		if source := graph.Node(need); source != nil {
			scope.Inputs = append(scope.Inputs, source.Title)
		}
	}
	return scope
}

// worthKeeping is the guard against decomposition that only restates, and it is
// where the burden of proof is actually collected rather than merely asked for.
//
// A model asked to break something down will always produce something, and the
// cheapest way to comply is to rewrite the parent as a list of near-copies of
// itself. That looks like progress and costs a full round of calls while
// leaving every part exactly as serial as before. Three symptoms give it away: a
// split into one thing, children that size out no smaller than the parent did,
// and children that do not name deliverables apart from each other. Each means
// the split bought nothing, so the parent stays a leaf and says why.
//
// The third is the structural half of the information-gain rule and it is
// deliberately conservative. Whether two pieces are really the same work is a
// semantic question and the prompt is where it is asked; what can be checked
// here without a model is whether the split even claims a difference, and a
// split that hands two children the same deliverable is not claiming one. Only
// an exact collision after normalisation refuses, so a split that is merely
// similar is admitted and judged by the shrinkage rule beside it.
//
// It returns the reason alongside the verdict because a refusal that leaves no
// trace is a graph shape nobody can explain afterwards.
func worthKeeping(result expansion) (bool, string) {
	return WorthKeeping(result.sub)
}

// WorthKeeping is the acceptance check as a claim-time caller needs it: over a
// sub-graph alone, because that is all a caller holding one expansion has. The
// level loop's own call goes through it unchanged.
func WorthKeeping(sub *Graph) (bool, string) {
	if sub == nil {
		return false, RefusalOnePiece
	}
	var children []Node
	stillOversized := 0
	for _, child := range sub.Nodes {
		if child.Kind == KindSynthesis {
			continue
		}
		children = append(children, child)
		if child.Size == SizeOversized {
			stillOversized++
		}
	}
	if len(children) < 2 {
		return false, RefusalOnePiece
	}
	if !distinctDeliverables(children) {
		return false, RefusalSameAnswer
	}
	// Most of the children have to have actually shrunk. Requiring merely that
	// one did was too weak in practice — a split where five of eight children
	// came back still oversized passed the check and bought almost nothing,
	// while costing a full round of calls and eight more nodes.
	if stillOversized*2 >= len(children) {
		return false, RefusalNoSmaller
	}
	return true, ""
}

// distinctDeliverables reports whether every child claims to return something no
// sibling returns.
//
// The key is read from the strongest statement a child has made about what it
// returns and never mixes two strengths against each other: a child that names
// its produced outputs is compared with the others that name theirs, a child
// that carries a written instruction with the others that carry one, and a bare
// child — which is what a fresh expansion produces, since instructions are
// written long after this — with the others by its subject. Comparing a named
// output against a subject line would refuse splits for being described at
// different stages of the build, which is not a difference in the work.
func distinctDeliverables(children []Node) bool {
	seen := make(map[string]bool, len(children))
	for _, child := range children {
		key := deliverableKey(child)
		if seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

// deliverableKey is what a child claims to hand back, normalised for comparison.
func deliverableKey(node Node) string {
	if produces := squashAll(node.Spec.Done.Produces); produces != "" {
		return "produces:" + produces
	}
	if instruction := squash(node.Spec.Instruction); instruction != "" {
		return "instruction:" + instruction
	}
	return "subject:" + squash(node.Title+" "+node.Summary)
}

// squash reduces text to the thing being said: case and spacing carry no claim
// about what a piece returns, so two children differing only in those are not
// two children.
func squash(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

// squashAll is squash over a set, order-insensitive: two children that produce
// the same outputs listed in a different order produce the same outputs.
func squashAll(values []string) string {
	kept := make([]string, 0, len(values))
	for _, value := range values {
		if squashed := squash(value); squashed != "" {
			kept = append(kept, squashed)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	sort.Strings(kept)
	return strings.Join(kept, "\x00")
}

// workNodes counts what a sub-graph would really add, ignoring the synthesis
// node that the parent absorbs.
func workNodes(sub *Graph) int {
	count := 0
	for _, node := range sub.Nodes {
		if node.Kind == KindWork {
			count++
		}
	}
	return count
}
