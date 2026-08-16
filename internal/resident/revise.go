// Result-driven revision closes the loop that separates a workflow engine
// from a problem-solver: plan, execute, observe, replan. When a landed result
// contradicts what an unstarted node was built to assume, the sentinel edits
// only that unstarted work — the store's own rules refuse everything else.
package resident

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// WithdrawnByPlan prefixes the reason on a node the job's own second thought
// dropped, and it is the one thing that tells a step withdrawn from a step
// lost.
//
// Both end as a cancelled node, and until this had a name every reader
// downstream had to guess which it was — so a delivery whose plan had correctly
// dropped a step because another node had already done that work announced
// itself as "Not all of this landed… much of it is missing", about a job that
// had in fact landed whole. The prefix is a contract between the pass that
// withdraws work and the composition that has to describe it, and the reason
// after it is the reviser's own words about why, which is usually the name of
// the node that covered it.
const WithdrawnByPlan = "revision: "

// ApplyRevision mirrors the sentinel's applied plan-graph operations onto the
// durable store, governed as an overrun replan is.
//
// It is the compatibility shape: every caller that has nothing to say about why
// it is growing the job, and no reader to ask whether the job still needs
// anything, gets the caps and the journal and no paid question.
func ApplyRevision(graph *store.Store, planGraph *plan.Graph, prefix, jobRoot string, operations []plan.Operation) (int, []string) {
	return ApplyRevisionGoverned(context.Background(), Growth{Reason: GrowRevision},
		graph, planGraph, prefix, jobRoot, operations)
}

// ApplyRevisionGoverned mirrors the sentinel's applied plan-graph operations
// onto the durable store: adds splice under the job's root, removals cancel
// pending nodes, rewires replace dependency edges, retitles amend brief and
// title. Refusals and store-side rejections are returned as notes rather than
// failing the batch — a revision is advice, and the store is the law.
//
// The adds now pass the growth governor first, which they never did. This path
// spliced straight into the job root with no ceiling, no round counter and no
// rail check, which was invisible while the sentinel was a rare second thought
// and is the whole story once anything fires it often: a job could be grown
// without limit by the one mechanism nobody was counting. A refused batch
// becomes a note, exactly as a store rejection already does — the removals,
// rewires and retitles in the same batch still apply, because none of them
// grows anything.
func ApplyRevisionGoverned(ctx context.Context, growth Growth, graph *store.Store, planGraph *plan.Graph, prefix, jobRoot string, operations []plan.Operation) (int, []string) {
	applied := 0
	var notes []string
	// The batch's own additions, named before anything is mirrored, because they
	// are the one thing the naming law must not see: a node added this round
	// cannot be the root of a subtree that was minted before it existed.
	grown := make(map[int]bool)
	for _, operation := range operations {
		if operation.Op == "add" && operation.Applied {
			grown[operation.Node] = true
		}
	}
	// The store's name for a plan node, from the one place that knows it. This
	// file used to spell it out itself, as "<prefix>-n<id>" for every node
	// including the root — which the splice names with the bare prefix. Every
	// edit aimed at a job's deliverable therefore addressed a node that has never
	// existed, and was refused as unknown while the plan document recorded it as
	// applied.
	id := PlanStoreIDs(planGraph, prefix, grown)
	// A note is read by a PERSON: the redirection receipt prints these lines
	// straight under its own (redirect.go). So a note names the step by what the
	// step is for, and never by the op's verb or the store's id for the row —
	// "retitle task-8-n4: node 4 is running" is three pieces of machinery in the
	// room, in a line nobody asked for, saying nothing the reader can act on.
	step := func(planID int) string {
		if node := planGraph.Node(planID); node != nil {
			if title := strings.TrimSpace(node.Title); title != "" {
				return title
			}
		}
		return fmt.Sprintf("step %d", planID)
	}
	note := func(planID int, reason string) {
		if reason = strings.TrimSpace(reason); reason != "" {
			notes = append(notes, step(planID)+": "+reason)
		}
	}
	exists := func(storeID string) bool {
		_, ok, err := graph.Node(storeID)
		return err == nil && ok
	}

	// One verdict for the batch, asked once with the whole count, because the
	// batch is what the sentinel decided: adds admitted one at a time would let
	// a batch of twenty walk through a ceiling that had room for one.
	adds := len(grown)

	// What the additions must be able to see, and who will read what they
	// produce. Both are facts about the result this revision reacted to, and
	// both are wiring the overrun splice has always performed around the node it
	// repairs: entry nodes consume that node and the parts it was assembled from
	// (repairSources, attachNeeds), and the consumers that were waiting on it
	// come to wait on the repair instead.
	//
	// This path performed neither. An addition arrived with whatever inputs a
	// model had named by integer — no edge to the failed work it was standing in
	// for, no evidence of the job it belongs to — and it was parented under a
	// deliverable that was not waiting for it, so its result reached the person
	// through no channel at all. Both reads happen only when something is
	// actually being added; a batch of removals and rewires costs what it always
	// did.
	var (
		sources    []string
		candidates []string
		edges      []store.Edge
		readByAdd  = make(map[int]bool, adds)
	)
	if adds > 0 {
		if strings.TrimSpace(growth.After.ID) != "" {
			sources = repairSources(graph, growth.After)
		}
		edges, _ = graph.ActiveEdges()
		candidates = revisionReaders(graph, jobRoot, growth.After, edges)
		// An addition that another addition in the same batch consumes is
		// already read, and wiring it to the deliverable as well would widen the
		// sink's fan-in with a result that arrives through its consumer anyway.
		for _, operation := range operations {
			if operation.Op != "add" || !operation.Applied {
				continue
			}
			if node := planGraph.Node(operation.Node); node != nil {
				for _, need := range node.Needs {
					if grown[need] {
						readByAdd[need] = true
					}
				}
			}
		}
	}
	request := GrowRequest{JobRoot: jobRoot, Lineage: jobRoot, Reason: growth.reason(),
		Adding: adds, Ungated: growth.Ungated}
	if root, ok, err := graph.Node(jobRoot); err == nil && ok {
		request.Node = root
	}
	verdict := GrowVerdict{Allow: true}
	if adds > 0 {
		decided, err := growJob(ctx, graph, growth.Ask, request)
		if err != nil {
			notes = append(notes, "the plan could not be grown: "+err.Error())
		} else {
			verdict = decided
		}
	}
	spliced := 0

	for _, operation := range operations {
		if !operation.Applied {
			if strings.TrimSpace(operation.Refused) != "" {
				note(operation.Node, operation.Refused)
			}
			continue
		}
		switch operation.Op {
		case "add":
			if !verdict.Allow {
				// The governor has already said this in the person's own words
				// on the job's record; the note repeats it against the step it
				// stopped, for whoever is reading the batch.
				note(operation.Node, growthRefusalNote(verdict))
				continue
			}
			node := planGraph.Node(operation.Node)
			if node == nil {
				note(operation.Node, "it vanished from the plan before it could be added")
				continue
			}
			spec := store.NodeSpec{
				ID:    id(node.ID),
				Brief: nodeBrief(*node),
				Title: strings.TrimSpace(node.Title),
				Stage: node.Stage,
				// A node the sentinel added is a spec like any other. It is
				// usually empty — the sentinel authors a title and a summary and
				// nothing else — and it is not empty in the one case that
				// matters: a node standing in for work that failed, which
				// inherits the failed node's criterion before it ever reaches
				// here (revision.RetargetAdds).
				Spec: EncodeSpec(node.Spec),
			}
			for _, need := range node.Needs {
				target := id(need)
				if !exists(target) {
					// Surfaced rather than dropped. An input named by an integer
					// that resolves to no store node is the sentinel wiring this
					// node to something expanded away at admission, refused
					// earlier in this same batch, or never there at all — and the
					// addition still lands, one input short, with nothing
					// anywhere to say which. Silence here is how a replacement
					// ends up reading none of the work it was replacing.
					note(operation.Node, "it was added without one of its inputs — "+
						step(need)+" is not part of the running job")
					continue
				}
				spec.Needs = append(spec.Needs, store.Need{NodeID: target, Kind: store.FeedsInto})
			}
			// The evidence edge, on the same terms the overrun splice wires it:
			// an entry — an addition depending on nothing else this batch adds —
			// reads the result that convened the revision and the parts that
			// result was assembled from.
			entry := true
			for _, need := range node.Needs {
				if grown[need] {
					entry = false
					break
				}
			}
			if entry {
				spec.Needs = entryNeeds(spec.Needs, sources)
			}
			// Where the addition belongs is decided by who will read it, which is
			// the same law overrun.go states for a repair: work whose result
			// nobody is waiting for is not a part of a job, it is a deliverable,
			// and a deliverable parented inside a job that is no longer gathering
			// finishes correctly and is delivered to nobody. The delivery gate
			// and the announcer both look at exactly one place — the nodes
			// standing on the spine — so that is where such an addition stands.
			readers := []string(nil)
			parent := jobRoot
			if !readByAdd[operation.Node] {
				readers = readersOf(candidates, spec, edges)
				if len(readers) == 0 {
					parent = store.RootID
				}
			}
			// The job's session rides along so a failure of this node can
			// interrupt the person whose work it revises — a session-less
			// child is one whose bad news nobody hears (overrun.go carries
			// it for the same reason). The announce path also walks to the
			// root, but provenance should not need rescuing to be read.
			session := ""
			if root, ok, err := graph.Node(jobRoot); err == nil && ok {
				session = root.Provenance.SessionID
			}
			err := graph.Splice(parent, store.Subtree{Nodes: []store.NodeSpec{spec}}, store.Provenance{
				Origin:    store.OriginSelf,
				SessionID: session,
				Intent:    "revision: " + operation.Reason,
			})
			if err != nil {
				note(operation.Node, "it could not be added: "+err.Error())
				continue
			}
			for _, reader := range readers {
				// Idempotent, and refused by the store the moment a reader has
				// started — rewriting what a running transcript was built from is
				// not on offer, here or on the overrun path.
				if err := graph.AddEdge(spec.ID, reader, store.FeedsInto); err == nil {
					edges = append(edges, store.Edge{From: spec.ID, To: reader, Kind: store.FeedsInto})
				}
			}
			// The batch's own edges join the picture the next addition is read
			// against: one addition consuming another must not then be wired into
			// anything that addition already waits for.
			for _, need := range spec.Needs {
				edges = append(edges, store.Edge{From: need.NodeID, To: spec.ID, Kind: need.Kind})
			}
			spliced++
			applied++

		case "remove":
			if err := graph.CancelPending(id(operation.Node), WithdrawnByPlan+operation.Reason); err != nil {
				note(operation.Node, "it could not be dropped: "+err.Error())
				continue
			}
			applied++

		case "rewire":
			target := id(operation.Node)
			wanted := make(map[string]bool, len(operation.Needs))
			for _, need := range operation.Needs {
				if exists(id(need)) {
					wanted[id(need)] = true
				}
			}
			edges, err := graph.ActiveEdges()
			if err != nil {
				note(operation.Node, "its inputs could not be moved: "+err.Error())
				continue
			}
			ok := true
			for _, edge := range edges {
				if edge.To != target || edge.Kind == store.Suggests {
					continue
				}
				if wanted[edge.From] {
					delete(wanted, edge.From)
					continue
				}
				if err := graph.RemoveEdge(edge.From, target, edge.Kind); err != nil {
					note(operation.Node, "its inputs could not be moved: "+err.Error())
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
			for from := range wanted {
				if err := graph.AddEdge(from, target, store.FeedsInto); err != nil {
					note(operation.Node, "its inputs could not be moved: "+err.Error())
					ok = false
					break
				}
			}
			if ok {
				applied++
			}

		case "retitle":
			node := planGraph.Node(operation.Node)
			if node == nil {
				note(operation.Node, "it vanished from the plan before it could be renamed")
				continue
			}
			if err := graph.AmendPending(id(node.ID), nodeBrief(*node), strings.TrimSpace(node.Title)); err != nil {
				note(operation.Node, "it could not be renamed: "+err.Error())
				continue
			}
			applied++
		}
	}
	// The round is spent by what landed, not by what was proposed: a batch whose
	// every add was rejected by the store grew nothing, and a round nobody spent
	// is not one to charge.
	admitGrowth(graph, request, verdict, spliced)
	return applied, notes
}

// revisionReaders names the pending work that could read an addition's result:
// whatever was waiting on the node this revision reacted to, and the job's own
// deliverable, which gathers everything the job does.
//
// It is the overrun splice's closing move (the consumer rewiring at the end of
// replanOverrun) asked one step earlier, because on this path the answer decides
// where the addition is parented and not only what it feeds. Only pending
// readers are named: a consumer that has already started built its transcript
// from what it had, and the store refuses the edge anyway.
func revisionReaders(graph *store.Store, jobRoot string, after store.Node, edges []store.Edge) []string {
	seen := make(map[string]bool, len(edges)+1)
	var readers []string
	consider := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		if node, ok, err := graph.Node(id); err == nil && ok && node.Status == store.Pending {
			readers = append(readers, id)
		}
	}
	if strings.TrimSpace(after.ID) != "" {
		for _, edge := range edges {
			if edge.From == after.ID && edge.Kind != store.Suggests {
				consider(edge.To)
			}
		}
	}
	consider(jobRoot)
	return readers
}

// readersOf narrows those candidates to the ones this particular addition may
// legally feed: everything it does not already, transitively, wait for.
//
// The exclusion is not fastidiousness. The store's AddEdge checks that a
// consumer is pending and nothing else, so an edge from an addition to its own
// dependency is accepted and the job is deadlocked from that instant — neither
// end can ever be ready. The one shape that produces it is real and already in
// the tests: a sentinel that adds a check reading the finished deliverable, and
// a deliverable that would then be wired to wait for its own checker.
func readersOf(candidates []string, spec store.NodeSpec, edges []store.Edge) []string {
	waiting := make(map[string]bool, len(spec.Needs))
	frontier := make([]string, 0, len(spec.Needs))
	for _, need := range spec.Needs {
		if need.Kind == store.Suggests || waiting[need.NodeID] {
			continue
		}
		waiting[need.NodeID] = true
		frontier = append(frontier, need.NodeID)
	}
	for len(frontier) > 0 {
		current := frontier[len(frontier)-1]
		frontier = frontier[:len(frontier)-1]
		for _, edge := range edges {
			if edge.To != current || edge.Kind == store.Suggests || waiting[edge.From] {
				continue
			}
			waiting[edge.From] = true
			frontier = append(frontier, edge.From)
		}
	}
	readers := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == spec.ID || waiting[candidate] {
			continue
		}
		readers = append(readers, candidate)
	}
	return readers
}

// growthRefusalNote is the refusal in the batch's own vocabulary. A rail pause
// carries no words of its own here — the durable question it raised is the
// sentence, and this batch is simply not the place it is asked.
func growthRefusalNote(verdict GrowVerdict) string {
	if words := strings.TrimSpace(verdict.Refused); words != "" {
		return words
	}
	return "the daily rail is reached; nothing new can be started until it is raised"
}

// RevisionEvent phrases what just happened for the sentinel: which node
// landed, how, what it reported, and what it left on disk. Bounded — the
// sentinel judges whether a result contradicts the plan, not the result's full
// content.
//
// The failure arrives as its own words rather than as a boolean. "FAILED" tells
// the sentinel that the plan's next steps have nothing to consume; "FAILED: the
// API returns 410 Gone for every v2 endpoint" tells it which assumption died,
// and that is the entire question it was convened to answer. The artifact list
// is here for the same reason it is in OverrunGoal: a leaf that says "wrote the
// notes to api-notes.md" has reported its whole finding in a filename, and a
// sentinel that cannot see the file at least learns one exists.
//
// contextTokens is the window of the model that will read the event. Zero is
// unknown and keeps the two literals this function was written with.
func RevisionEvent(node store.Node, summary string, artifacts []string, failure string, contextTokens int) string {
	label := strings.TrimSpace(node.Title)
	if label == "" {
		label = firstLine(node.Brief)
	}
	resultRoom, failureRoom := revisionBytes(contextTokens)
	ending := "finished"
	if failure = strings.TrimSpace(failure); failure != "" {
		ending = "FAILED: " + clipEventBytes(firstLine(failure), failureRoom)
	}
	event := fmt.Sprintf("Node %q %s. Its result:\n%s", label, ending,
		clipEventBytes(strings.TrimSpace(summary), resultRoom))
	if len(artifacts) > 0 {
		event += "\n\nFiles it left in the workspace:\n" + strings.Join(artifacts, "\n")
	}
	return event
}

// CancelledRevisionEvent is RevisionEvent's third flavor, and the one that had
// no channel at all until now. A failure tells the sentinel that an assumption
// died; a redirection tells it the owner changed their mind about the goal. A
// cancellation says something narrower than either: this particular piece of
// work is not wanted, and nothing about the goal has changed.
//
// So the licence is narrow to match. The remaining plan may need to stop
// depending on what was withdrawn — that is a real contradiction, and it is the
// only one here. What it must never do is treat the cancellation as a failure
// to repair: adding a node to redo the cancelled work, or to check what it left
// behind, spends the user's money undoing the decision they just made. The
// prompt refuses it and this says it again at the event, because the event is
// what the sentinel reads last.
func CancelledRevisionEvent(node store.Node, partial, reason string, contextTokens int) string {
	label := strings.TrimSpace(node.Title)
	if label == "" {
		label = firstLine(node.Brief)
	}
	if reason = strings.TrimSpace(reason); reason == "" {
		reason = "no reason given"
	}
	resultRoom, failureRoom := revisionBytes(contextTokens)
	event := fmt.Sprintf("Node %q was CANCELLED by the user: %s.", label,
		clipEventBytes(firstLine(reason), failureRoom))
	if partial = strings.TrimSpace(partial); partial != "" {
		event += "\n\nWhat it had written when they stopped it:\n" +
			clipEventBytes(partial, resultRoom)
	}
	return event + "\n\nThe user stopped this on purpose; it is not a failure and it is not " +
		"waiting to be finished. Reconsider only the unstarted remainder: a step that can no " +
		"longer get what it needed from this one may need rewiring, retitling or removing. " +
		"Never add a node that redoes, finishes, resumes or verifies the cancelled work, and " +
		"never treat what it left behind as something to be repaired."
}

// What an event may carry, for a sentinel whose window nobody could name. Every
// other case is a share of the real thing; see revisionBytes.
const (
	revisionResultBytes  = 1200
	revisionFailureBytes = 300
)

// The event's share of the sentinel's prompt. The plan and its landed results
// are the larger half and are budgeted where they are rendered (see
// plan.Graph.stateBlock); this is the one thing that has just happened, and it
// is what the rest is being read against.
const (
	revisionEventShare = 1
	revisionShares     = 4

	// revisionFloorTokens is the revise prompt, its schema and the goal's
	// context block — everything the event is added to.
	revisionFloorTokens = 4 << 10

	// revisionFailureDivisor keeps a failure line the sharp fraction of a result
	// it has always been: 1200 and 300 stood in this ratio, and the pair scales
	// together rather than one of them being left behind at a literal.
	revisionFailureDivisor = 4
)

// revisionBytes is how much of a landed result, and of the line saying why one
// failed, the event may carry — sized from the window of the model that reads
// it. An unknown window returns exactly the old pair.
func revisionBytes(contextTokens int) (result, failure int) {
	result = ctxbudget.For(contextTokens).WithFloor(revisionFloorTokens).
		Share(revisionEventShare, revisionShares, revisionResultBytes)
	failure = result / revisionFailureDivisor
	if failure < revisionFailureBytes {
		failure = revisionFailureBytes
	}
	return result, failure
}

// clipEventBytes bounds prompt-bound text at a rune boundary. A byte cut
// through a character produces a replacement glyph that rides the whole
// sentinel prompt, which is the one place in this file where the exact text is
// what is being reasoned about.
func clipEventBytes(body string, limit int) string {
	if limit <= 0 || len(body) <= limit {
		return body
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(body[cut]) {
		cut--
	}
	return strings.TrimSpace(body[:cut]) + "…"
}
