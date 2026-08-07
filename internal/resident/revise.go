// Result-driven revision closes the loop that separates a workflow engine
// from a problem-solver: plan, execute, observe, replan. When a landed result
// contradicts what an unstarted node was built to assume, the sentinel edits
// only that unstarted work — the store's own rules refuse everything else.
package resident

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// ApplyRevision mirrors the sentinel's applied plan-graph operations onto the
// durable store: adds splice under the job's root, removals cancel pending
// nodes, rewires replace dependency edges, retitles amend brief and title.
// Refusals and store-side rejections are returned as notes rather than
// failing the batch — a revision is advice, and the store is the law.
func ApplyRevision(graph *store.Store, planGraph *plan.Graph, prefix, jobRoot string, operations []plan.Operation) (int, []string) {
	applied := 0
	var notes []string
	id := func(planID int) string { return fmt.Sprintf("%s-n%d", prefix, planID) }
	exists := func(storeID string) bool {
		_, ok, err := graph.Node(storeID)
		return err == nil && ok
	}

	for _, operation := range operations {
		if !operation.Applied {
			if strings.TrimSpace(operation.Refused) != "" {
				notes = append(notes, fmt.Sprintf("%s %s: %s", operation.Op, id(operation.Node), operation.Refused))
			}
			continue
		}
		switch operation.Op {
		case "add":
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
			}
			for _, need := range node.Needs {
				if exists(id(need)) {
					spec.Needs = append(spec.Needs, store.Need{NodeID: id(need), Kind: store.FeedsInto})
				}
			}
			err := graph.Splice(jobRoot, store.Subtree{Nodes: []store.NodeSpec{spec}}, store.Provenance{
				Origin: store.OriginSelf,
				Intent: "revision: " + operation.Reason,
			})
			if err != nil {
				notes = append(notes, fmt.Sprintf("add %s: %v", spec.ID, err))
				continue
			}
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
	return applied, notes
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
