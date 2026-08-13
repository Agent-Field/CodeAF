// Decomposition at claim time: the depth loop, moved out of the build and into
// the scheduler.
//
// A plan built at t=0 decides every division it will ever make against titles,
// because at t=0 nothing has run and titles are all there are. The measured
// consequence was a graph that was the same shape in every run of the battery
// — two levels, decided before the first leaf started, never revisited — and a
// node that turned out to hold four workers' worth of work could only be
// discovered by spending a whole leaf budget failing at it and paying for a
// replan afterwards.
//
// So the question moves to the one moment it can be answered well: a node is
// claimed, every piece feeding it has landed and said what it produced, and the
// same two calls the build would have spent are spent here instead — against
// what is really there. The node either divides, and its parts run at the same
// time under it, or it does not, and the worker that is already holding it runs
// it whole. Refusing is the null hypothesis and it is free: the question that
// decides costs no call at all (plan.JudgeSplit reads the node), so a job whose
// every node is atomic pays exactly what it paid before this file existed.
//
// Everything that grows a running job goes through growJob, and this is no
// exception — it is the fourth caller, and the one whose rounds are counted per
// node rather than per job, because two siblings that each divide once have
// divided once each.
package resident

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// jitDigestBytes bounds what an expansion is shown of its landed inputs, for an
// expander that could not be told what window it plans through. It is the same
// order as a leaf's own dependency fallback and for the same reason: the
// sub-planner is reading results to decide a shape, not to do the work, so it
// needs to know what is there and not to hold all of it.
const jitDigestBytes = 4096

// The sub-planner's share of its own prompt. The landed inputs are half of what
// ExpandOne reads; the other half is the plan document it is dividing a node of.
const (
	jitLandedShare = 1
	jitPlanShares  = 2

	// jitFloorTokens is the expansion prompt and its schema, before either half.
	jitFloorTokens = 4 << 10
)

// digestPot is what one expansion may be shown of everything that landed into
// the node it is dividing, sized from the window of the planning model.
//
// It is arithmetic over a field fixed at construction, so both reads below —
// the landed context and the inputs the children inherit — get the same number
// for the same division, and every division of a job gets the same number as
// the one before it.
func (e JITExpander) digestPot() int {
	return ctxbudget.For(e.ContextTokens).WithFloor(jitFloorTokens).
		Share(jitLandedShare, jitPlanShares, jitDigestBytes)
}

// JITDepthCeiling is the arithmetic backstop under the atomicity judgment, and
// it is deliberately not the policy.
//
// The policy is the judgment: a claimed node divides while a reading of it says
// it is not yet one worker's job, and stops when that reading says it is. What
// actually stops a runaway is the pair that cannot be argued with — the
// per-job node ceiling in growJob, and the shrinkage guard that refuses a
// division whose parts came back no smaller — and both bind long before this
// number does. It exists so that a judgment that has gone wrong in a way
// nobody anticipated still terminates, and it is set well above any depth a
// real job has reached so that it is never the thing making the decision.
const JITDepthCeiling = 8

// JITTarget is one job's plan document, and the four facts needed to find one
// node inside it and put children under that node afterwards.
//
// It is resolved by the caller rather than read from the journal here, because
// the live document is the one a revision sentinel is also editing: reading a
// second copy out of the store and writing it back would silently drop whatever
// the sentinel had just decided.
type JITTarget struct {
	// Plan is the job's live plan document.
	Plan *plan.Graph
	// Lock guards it. Nil is legal and means the caller has no concurrency to
	// protect against, which is every test and no production surface.
	Lock sync.Locker
	// Prefix is the id namespace the job's store nodes are minted under, so a
	// child of plan node N is "<prefix>-nN" — the same arithmetic the reconciler
	// used at admission, which is what lets the child be looked up and judged
	// again when it is itself claimed.
	Prefix string
	// PlanNode is the claimed node's id inside Plan.
	PlanNode int
	// Client plans the expansion. Nil refuses it — a job whose planning client
	// did not survive a restart runs its nodes whole, which is what it did
	// before this file existed.
	Client plan.Completer
	// Options carries the ceilings the job was planned under.
	Options plan.Options
	// Journal persists the document after it has grown. Nil skips it, at the
	// cost of a restart not seeing the deeper shape.
	Journal func()
}

// JITExpander is the claim-time half of decomposition.
type JITExpander struct {
	Graph          *store.Store
	DailyBudgetUSD float64
	// ContextTokens is the window of the planning model this expander divides
	// through — the same model the build asked, and not the leaf's. Zero is
	// unknown and falls back to jitDigestBytes, which is what every expansion
	// took before the window was threaded here.
	ContextTokens int
	// Resolve finds the job a claimed node belongs to. Not ok means "this node
	// has no plan" — a one-leaf job, a craft node, a rehydration miss — and is
	// the ordinary answer for most of what a resident claims.
	Resolve func(node store.Node) (JITTarget, bool)
	// Ask overrides the process-wide satisfaction gate. Nil uses it.
	Ask Satisfier
}

// Expand is the Runner's hook: consulted after a node is claimed and before a
// worker is given it.
//
// It reports whether the node was turned into structure. When it was, the claim
// has already been released and the node must not be executed — its children
// are in the graph, ready, and the next pass dispatches them. When it was not,
// nothing at all has happened to the node and the caller runs it exactly as it
// would have.
func (e JITExpander) Expand(ctx context.Context, node store.Node) (int, bool) {
	spliced, err := e.expand(ctx, node)
	if err != nil {
		// A failed expansion is a node that did not divide, which is the same
		// answer a failed sub-plan has always been. It is logged rather than
		// returned because the caller's only remaining move is the one it was
		// going to make anyway: run the node.
		log.Printf("note: could not divide %s at claim time: %v", node.ID, err)
	}
	return spliced, spliced > 0
}

func (e JITExpander) expand(ctx context.Context, node store.Node) (int, error) {
	if e.Graph == nil || e.Resolve == nil {
		return 0, nil
	}
	// The claim is the licence. A node this expander cannot release is a node
	// somebody else is running, and turning it into a container underneath its
	// own worker would leave that worker writing a result into structure.
	if strings.TrimSpace(node.Owner) == "" {
		return 0, nil
	}
	target, ok := e.Resolve(node)
	if !ok || target.Plan == nil || target.Client == nil {
		return 0, nil
	}
	options := claimOptions(target.Options)

	// 1. The free question, and the whole of the common path. Nothing below
	//    this line runs for a node the plan already judged atomic, so a job of
	//    atomic nodes costs one predicate per claim and not one call.
	judged, verdict := judgeAtClaim(target, options)
	if judged == nil || !verdict.Divide {
		return 0, nil
	}

	criterion := DecodeSpec(node.Spec).Done
	request := GrowRequest{
		JobRoot: jobRootID(e.Graph, node), Node: node,
		// A node's lineage is its own. Two siblings that each divide once have
		// each divided once — counting them against one job-wide allowance
		// would make the third node of a wide job undividable for a reason
		// that has nothing to do with it.
		Lineage: node.ID, Reason: GrowJIT,
		Criterion: criterion, DailyBudgetUSD: e.DailyBudgetUSD,
		// A refusal here is not a handover: the node runs whole, now, on the
		// worker already holding it.
		Quiet: true,
	}
	growth, err := growJob(ctx, e.Graph, e.Ask, request)
	if err != nil {
		return 0, fmt.Errorf("weigh the growth: %w", err)
	}
	if !growth.Allow {
		// The governor's answer is the graph's shape, so it is written where
		// every other refusal to divide is written.
		underLock(target.Lock, func() {
			if live := target.Plan.Node(target.PlanNode); live != nil {
				plan.JournalRefusal(live, growthRefusal(growth.Cause))
			}
		})
		return 0, nil
	}

	// 2. What the build could not have known: what the work feeding this node
	//    actually produced.
	inputs, err := e.Graph.DependencyAccounts(node.ID, e.digestPot())
	if err != nil {
		return 0, fmt.Errorf("read what landed: %w", err)
	}
	scope := plan.ClaimContext{Landed: landedFrom(inputs), Criterion: criterion}

	// 3. The two calls, made against a copy so the document is not held while a
	//    model thinks. Every leaf of this job resolves itself through the same
	//    lock, and a round-trip held under it stops all of them.
	snapshot, err := copyPlan(target)
	if err != nil {
		return 0, err
	}
	sub, _, err := plan.ExpandOne(ctx, target.Client, snapshot, target.PlanNode, options, scope)
	if err != nil {
		return 0, err
	}
	if keep, reason := plan.WorthKeeping(sub); !keep {
		underLock(target.Lock, func() {
			if live := target.Plan.Node(target.PlanNode); live != nil {
				plan.JournalRefusal(live, reason)
			}
		})
		return 0, nil
	}

	// 4. The exact ceiling, now that the pieces exist and can be counted. The
	//    free caps again and never the paid question, which was asked on the
	//    way in and whose answer has not changed.
	exact := request
	exact.Adding = planWorkNodes(sub)
	exact.Rechecking = true
	exact.DailyBudgetUSD = 0
	if recheck, err := growJob(ctx, e.Graph, nil, exact); err == nil && !recheck.Allow {
		underLock(target.Lock, func() {
			if live := target.Plan.Node(target.PlanNode); live != nil {
				plan.JournalRefusal(live, growthRefusal(recheck.Cause))
			}
		})
		return 0, nil
	}

	children, sinks, err := growPlan(target, sub)
	if err != nil {
		return 0, err
	}
	spliced, err := e.admit(node, target, children, sinks)
	if spliced > 0 {
		admitGrowth(e.Graph, request, growth, spliced)
		if target.Journal != nil {
			target.Journal()
		}
	}
	return spliced, err
}

// admit puts the children in the graph, hands the node back, and turns it into
// the gathering node its own plan document now says it is.
//
// The order is the whole of its safety. The children land while the node is
// still claimed, so nothing can pick the node up in the middle; the claim goes
// back only once they exist, and by then the node has children the ready loop
// will not claim past; the edges that make their results its inputs are added
// after that, because an edge may only be added into a node that is pending.
func (e JITExpander) admit(node store.Node, target JITTarget, children []plan.Node, sinks map[int]bool) (int, error) {
	inputs, err := e.parentNeeds(node.ID)
	if err != nil {
		return 0, err
	}
	provenance := store.Provenance{
		Origin:      store.OriginSelf,
		SessionID:   node.Provenance.SessionID,
		Intent:      spliceIntent(node),
		Attachments: append([]string(nil), node.Provenance.Attachments...),
	}
	minted := make(map[int]string, len(children))
	for _, child := range children {
		minted[child.ID] = fmt.Sprintf("%s-n%d", target.Prefix, child.ID)
	}
	spliced := 0
	for _, child := range order(children) {
		spec := store.NodeSpec{
			ID:         minted[child.ID],
			Brief:      nodeBrief(child),
			Title:      strings.TrimSpace(child.Title),
			Group:      childGroup(node),
			Stage:      child.Stage,
			Subharness: strings.TrimSpace(child.Subharness),
			Spec:       EncodeSpec(childSpec(node, child)),
		}
		internal := false
		for _, need := range child.Needs {
			if sibling, ok := minted[need]; ok {
				internal = true
				spec.Needs = append(spec.Needs, store.Need{NodeID: sibling, Kind: store.FeedsInto})
			}
		}
		// A child with no upstream among its siblings is an entry of the
		// division, so it consumes what the node itself consumed. The rest
		// reach the same inputs through it.
		if !internal {
			for _, input := range inputs {
				spec.Needs = append(spec.Needs, store.Need{NodeID: input, Kind: store.FeedsInto})
			}
		}
		if err := e.Graph.Splice(node.ID, store.Subtree{Nodes: []store.NodeSpec{spec}}, provenance); err != nil {
			// Whatever landed is real work under a node that now has children,
			// so the expansion continues to its end rather than being half
			// abandoned: the node gathers what its pieces produced either way.
			if spliced == 0 {
				return 0, fmt.Errorf("admit %s: %w", spec.ID, err)
			}
			log.Printf("note: could not admit %s under %s: %v", spec.ID, node.ID, err)
			continue
		}
		spliced++
	}
	if spliced == 0 {
		return 0, nil
	}
	if err := e.Graph.Release(store.Claim{ID: node.ID, Owner: node.Owner, Token: node.ClaimToken}); err != nil {
		return spliced, fmt.Errorf("hand back %s: %w", node.ID, err)
	}
	for _, child := range children {
		if !sinks[child.ID] {
			continue
		}
		if err := e.Graph.AddEdge(minted[child.ID], node.ID, store.FeedsInto); err != nil {
			log.Printf("note: could not feed %s into %s: %v", minted[child.ID], node.ID, err)
		}
	}
	if err := e.Graph.AmendPending(node.ID, gatheringBrief(node), ""); err != nil {
		log.Printf("note: could not restate %s as a gathering step: %v", node.ID, err)
	}
	return spliced, nil
}

// parentNeeds is what the node was about to be run with: the ids of the work
// whose results are its inputs. They become the entry children's inputs, which
// is what makes the division see what the node would have seen.
func (e JITExpander) parentNeeds(id string) ([]string, error) {
	inputs, err := e.Graph.DependencyAccounts(id, e.digestPot())
	if err != nil {
		return nil, fmt.Errorf("read the inputs of %s: %w", id, err)
	}
	needs := make([]string, 0, len(inputs))
	for _, input := range inputs {
		// The overflow notice is a sentence, not a dependency.
		if strings.TrimSpace(input.NodeID) == "" {
			continue
		}
		needs = append(needs, input.NodeID)
	}
	return needs, nil
}

// judgeAtClaim asks the free question under the document lock and hands back a
// copy. The copy matters: everything after this point may run for seconds, and
// a pointer into a slice the sentinel can append to is a pointer that moves.
func judgeAtClaim(target JITTarget, options plan.Options) (*plan.Node, plan.SplitVerdict) {
	var claimed *plan.Node
	var verdict plan.SplitVerdict
	underLock(target.Lock, func() {
		live := target.Plan.Node(target.PlanNode)
		if live == nil {
			return
		}
		verdict = plan.JudgeSplit(live, options)
		if !verdict.Divide {
			plan.JournalRefusal(live, verdict.Reason)
		}
		copied := *live
		claimed = &copied
	})
	return claimed, verdict
}

// growPlan splices the accepted division into the live document and reports
// what it added: the children, and which of them the node now gathers from.
//
// The plan package does the structural half on its own — the parent becomes a
// synthesis node whose needs are the division's sinks — so this reads the answer
// back rather than deciding it a second time in different words.
func growPlan(target JITTarget, sub *plan.Graph) ([]plan.Node, map[int]bool, error) {
	var children []plan.Node
	sinks := map[int]bool{}
	var err error
	underLock(target.Lock, func() {
		before := len(target.Plan.Nodes)
		if err = target.Plan.Splice(target.PlanNode, sub); err != nil {
			return
		}
		for _, child := range target.Plan.Nodes[before:] {
			children = append(children, child)
		}
		if parent := target.Plan.Node(target.PlanNode); parent != nil {
			for _, need := range parent.Needs {
				sinks[need] = true
			}
		}
	})
	if err != nil {
		return nil, nil, fmt.Errorf("grow the plan: %w", err)
	}
	if len(children) == 0 {
		return nil, nil, fmt.Errorf("grow the plan: node %d expanded to nothing", target.PlanNode)
	}
	return children, sinks, nil
}

// order sorts children so a child is admitted after the siblings it consumes.
// The store checks that a declared dependency exists, and a division is a small
// DAG, so the cheapest correct answer is repeated passes over what is left.
func order(children []plan.Node) []plan.Node {
	inside := make(map[int]bool, len(children))
	for _, child := range children {
		inside[child.ID] = true
	}
	admitted := make(map[int]bool, len(children))
	sorted := make([]plan.Node, 0, len(children))
	for len(sorted) < len(children) {
		progressed := false
		for _, child := range children {
			if admitted[child.ID] {
				continue
			}
			ready := true
			for _, need := range child.Needs {
				if inside[need] && !admitted[need] {
					ready = false
					break
				}
			}
			if !ready {
				continue
			}
			admitted[child.ID] = true
			sorted = append(sorted, child)
			progressed = true
		}
		if !progressed {
			// A cycle the plan package should have refused. Admitting the rest
			// in their own order loses an edge and never the work.
			for _, child := range children {
				if !admitted[child.ID] {
					sorted = append(sorted, child)
				}
			}
			break
		}
	}
	return sorted
}

// claimOptions is the ceiling a claim-time division is judged under.
//
// It is the job's own, with depth raised: the build's ceiling is a statement
// about how deep a graph should be *decided in advance*, and this is the other
// question. Recursion here continues while the reading of each claimed node
// says it is not yet one worker's job, and what stops it is the node ceiling in
// growJob, the shrinkage guard, and — never, in any job measured — the backstop.
func claimOptions(options plan.Options) plan.Options {
	if options.MaxDepth < JITDepthCeiling {
		options.MaxDepth = JITDepthCeiling
	}
	options.BuildDepth = 0
	return options
}

// childSpec is what the division's parts are handed. The method travels down
// because how this kind of work is done well is a fact about the work and not
// about how finely it was cut; the criterion does not, because the criterion
// names the whole deliverable and that is the node's own, still, and it is what
// the node will be judged on when it gathers.
func childSpec(node store.Node, child plan.Node) plan.Spec {
	spec := plan.Spec{Instruction: nodeBrief(child), Sources: child.Sources}
	if method := strings.TrimSpace(DecodeSpec(node.Spec).Method); method != "" {
		spec.Method = method
	}
	return spec
}

// childGroup keeps the rail's nesting readable: a part of a divided step reads
// under the step it came out of.
func childGroup(node store.Node) string {
	title := strings.TrimSpace(node.Title)
	if title == "" {
		return strings.TrimSpace(node.Group)
	}
	if group := strings.TrimSpace(node.Group); group != "" {
		return group + " › " + title
	}
	return title
}

// gatheringPreamble is what a divided node is asked to do instead of the work
// its parts are now doing.
//
// Without it the node keeps the assignment it was given and runs it again with
// its own parts' results as reading material, which is the whole assignment
// bought twice. It names no domain and no artifact: what "the finished thing"
// is was decided when the node was written, and it is still written on the node
// underneath this.
const gatheringPreamble = "This work was divided into parts, and those parts have run. What each of them " +
	"produced arrives below as an input. Put them together into the finished thing this work was asked " +
	"for, out of what they produced — without redoing their work, and without planning more of it.\n\n" +
	"What this work was asked for:\n"

func gatheringBrief(node store.Node) string {
	return gatheringPreamble + strings.TrimSpace(node.Brief)
}

// growthRefusal turns the governor's machine-readable cause into the words a
// refusal to divide is journaled in. A ceiling is arithmetic and says so — the
// existing refusals already draw that line and this one stays on the same side
// of it.
func growthRefusal(cause string) string {
	switch cause {
	case CauseCeiling:
		return plan.RefusalNoRoom
	case CauseRounds:
		return plan.RefusalDepth
	case CauseCovered:
		return plan.RefusalWithinReach
	}
	return plan.RefusalNoRoom
}

// landedFrom turns what the store hands a consumer into what a sub-planner
// reads. The files travel with the digest because they are the one part a
// division has to be able to name: a part that does not know what already
// exists is a part that recreates it.
//
// The handle travels for the same reason, one step further out. A digest says
// what a step found; the handle is the whole of what it said, and a division
// that has to decide whether a part is already covered may need the whole of it.
// It rides as a path beside the artifacts because it is a file like any other —
// the planner does not open it, but the part the planner writes is handed the
// same list and can.
func landedFrom(inputs []store.DependencyInput) []plan.Landed {
	landed := make([]plan.Landed, 0, len(inputs))
	for _, input := range inputs {
		result := strings.TrimSpace(input.Digest)
		files := input.Artifacts
		if input.Handle != "" {
			files = append(append([]string(nil), files...), input.Handle)
		}
		if len(files) > 0 {
			result += "\nFiles it left behind: " + strings.Join(files, "; ")
		}
		if result == "" {
			continue
		}
		title := strings.TrimSpace(input.NodeID)
		if title == "" {
			title = "Also finished"
		}
		landed = append(landed, plan.Landed{Title: title, Result: result})
	}
	return landed
}

// spliceIntent is the verbatim ask a subtree is admitted under. The store
// requires one, and a divided node's parts belong to whatever the node itself
// belonged to.
func spliceIntent(node store.Node) string {
	if intent := strings.TrimSpace(node.Provenance.Intent); intent != "" {
		return intent
	}
	if title := strings.TrimSpace(node.Title); title != "" {
		return title
	}
	return strings.TrimSpace(node.Brief)
}

// planWorkNodes counts what a division would really add.
func planWorkNodes(sub *plan.Graph) int {
	if sub == nil {
		return 0
	}
	count := 0
	for _, node := range sub.Nodes {
		if node.Kind != plan.KindSynthesis {
			count++
		}
	}
	return count
}

// copyPlan takes the document out from under its lock, so the two calls that
// follow are made against a picture that cannot move and a lock that is not
// held.
func copyPlan(target JITTarget) (*plan.Graph, error) {
	var encoded []byte
	var err error
	underLock(target.Lock, func() {
		encoded, err = target.Plan.JSON()
	})
	if err != nil {
		return nil, fmt.Errorf("read the plan: %w", err)
	}
	copied, err := plan.Load(encoded)
	if err != nil {
		return nil, fmt.Errorf("read the plan: %w", err)
	}
	return copied, nil
}

func underLock(lock sync.Locker, do func()) {
	if lock != nil {
		lock.Lock()
		defer lock.Unlock()
	}
	do()
}
