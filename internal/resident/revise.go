// Result-driven revision closes the loop that separates a workflow engine
// from a problem-solver: plan, execute, observe, replan. When a landed result
// contradicts what an unstarted node was built to assume, the sentinel edits
// only that unstarted work — the store's own rules refuse everything else.
package resident

import (
	"fmt"
	"strings"

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
// landed, how, and what it reported. Bounded — the sentinel judges whether a
// result contradicts the plan, not the result's full content.
func RevisionEvent(node store.Node, summary string, failed bool) string {
	label := strings.TrimSpace(node.Title)
	if label == "" {
		label = firstLine(node.Brief)
	}
	ending := "finished"
	if failed {
		ending = "FAILED"
	}
	summary = strings.TrimSpace(summary)
	if len(summary) > 1200 {
		summary = summary[:1200] + "…"
	}
	return fmt.Sprintf("Node %q %s. Its result:\n%s", label, ending, summary)
}
