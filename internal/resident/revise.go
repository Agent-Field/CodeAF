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

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

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
	id := func(planID int) string { return fmt.Sprintf("%s-n%d", prefix, planID) }
	exists := func(storeID string) bool {
		_, ok, err := graph.Node(storeID)
		return err == nil && ok
	}

	// One verdict for the batch, asked once with the whole count, because the
	// batch is what the sentinel decided: adds admitted one at a time would let
	// a batch of twenty walk through a ceiling that had room for one.
	adds := 0
	for _, operation := range operations {
		if operation.Op == "add" && operation.Applied {
			adds++
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
			notes = append(notes, fmt.Sprintf("add: %v", err))
		} else {
			verdict = decided
		}
	}
	spliced := 0

	for _, operation := range operations {
		if !operation.Applied {
			if strings.TrimSpace(operation.Refused) != "" {
				notes = append(notes, fmt.Sprintf("%s %s: %s", operation.Op, id(operation.Node), operation.Refused))
			}
			continue
		}
		switch operation.Op {
		case "add":
			if !verdict.Allow {
				// The governor has already said this in the person's own words
				// on the job's record; the note is the same refusal in the
				// sentinel's vocabulary, for whoever is reading the batch.
				notes = append(notes, fmt.Sprintf("add %s: %s", id(operation.Node), growthRefusalNote(verdict)))
				continue
			}
			node := planGraph.Node(operation.Node)
			if node == nil {
				notes = append(notes, fmt.Sprintf("add %d: vanished from the plan", operation.Node))
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
				if exists(id(need)) {
					spec.Needs = append(spec.Needs, store.Need{NodeID: id(need), Kind: store.FeedsInto})
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
			err := graph.Splice(jobRoot, store.Subtree{Nodes: []store.NodeSpec{spec}}, store.Provenance{
				Origin:    store.OriginSelf,
				SessionID: session,
				Intent:    "revision: " + operation.Reason,
			})
			if err != nil {
				notes = append(notes, fmt.Sprintf("add %s: %v", spec.ID, err))
				continue
			}
			spliced++
			applied++

		case "remove":
			if err := graph.CancelPending(id(operation.Node), "revision: "+operation.Reason); err != nil {
				notes = append(notes, fmt.Sprintf("remove %s: %v", id(operation.Node), err))
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
				notes = append(notes, fmt.Sprintf("rewire %s: %v", target, err))
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
					notes = append(notes, fmt.Sprintf("rewire %s: %v", target, err))
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
			for from := range wanted {
				if err := graph.AddEdge(from, target, store.FeedsInto); err != nil {
					notes = append(notes, fmt.Sprintf("rewire %s: %v", target, err))
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
				notes = append(notes, fmt.Sprintf("retitle %d: vanished from the plan", operation.Node))
				continue
			}
			if err := graph.AmendPending(id(node.ID), nodeBrief(*node), strings.TrimSpace(node.Title)); err != nil {
				notes = append(notes, fmt.Sprintf("retitle %s: %v", id(node.ID), err))
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
func RevisionEvent(node store.Node, summary string, artifacts []string, failure string) string {
	label := strings.TrimSpace(node.Title)
	if label == "" {
		label = firstLine(node.Brief)
	}
	ending := "finished"
	if failure = strings.TrimSpace(failure); failure != "" {
		ending = "FAILED: " + clipEventBytes(firstLine(failure), revisionFailureBytes)
	}
	event := fmt.Sprintf("Node %q %s. Its result:\n%s", label, ending,
		clipEventBytes(strings.TrimSpace(summary), revisionResultBytes))
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
func CancelledRevisionEvent(node store.Node, partial, reason string) string {
	label := strings.TrimSpace(node.Title)
	if label == "" {
		label = firstLine(node.Brief)
	}
	if reason = strings.TrimSpace(reason); reason == "" {
		reason = "no reason given"
	}
	event := fmt.Sprintf("Node %q was CANCELLED by the user: %s.", label,
		clipEventBytes(firstLine(reason), revisionFailureBytes))
	if partial = strings.TrimSpace(partial); partial != "" {
		event += "\n\nWhat it had written when they stopped it:\n" +
			clipEventBytes(partial, revisionResultBytes)
	}
	return event + "\n\nThe user stopped this on purpose; it is not a failure and it is not " +
		"waiting to be finished. Reconsider only the unstarted remainder: a step that can no " +
		"longer get what it needed from this one may need rewiring, retitling or removing. " +
		"Never add a node that redoes, finishes, resumes or verifies the cancelled work, and " +
		"never treat what it left behind as something to be repaired."
}

const (
	revisionResultBytes  = 1200
	revisionFailureBytes = 300
)

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
