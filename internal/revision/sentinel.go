package revision

import (
	"context"
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/provider/pool"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/router"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
)

// Sentinel is the pass that reads one landed result against a job's remaining
// plan and edits the plan only where the result contradicts a specific
// assumption in a specific node. Its default is no change; the plan package
// refuses everything else, and the store refuses the same edits again on its
// own authority.
//
// It does not own the locks. The plan document belongs to whoever retained it,
// and the arrangement that matters — hold the lock while the plan is rendered
// and again while the answer is applied, hand it back for the round-trip in
// between — is expressed by the client the caller passes in. Everything here
// runs inside whatever the caller is already holding.
//
// journal is called exactly where the caller used to call it: after the edits
// land and before anything is said about them, because the journaled structure
// is behind the in-memory graph the moment a revision applies. It returns how
// many operations landed, so a caller that would rather journal for itself can.
func Sentinel(ctx context.Context, settings config.Config, client plan.Completer,
	graph *store.Store, node store.Node, prefix, root string, planGraph *plan.Graph,
	event, workerModel string, journal func()) int {
	// The sentinel is this job's own second thought about its own remainder,
	// so its spend belongs to this job.
	judgeCtx := pool.WithSpendNode(router.WithAvoidModel(ctx, workerModel), node.ID)
	// terrain wiring lands here (world-grounded-planning handoff)
	operations, _, err := plan.Revise(settings.Context(judgeCtx, planGraph.Goal), client, planGraph, event)
	if err != nil || len(operations) == 0 {
		return 0
	}
	// A node the sentinel adds after a failure is a replacement, and a
	// replacement inherits rather than re-authors. This is the one place that
	// distinction can be drawn — the failed node and the additions made in its
	// name are both in hand here, and one step later the additions are store
	// nodes with nothing to inherit from.
	if node.Status == store.Failed {
		if failedNode := PlanNodeFor(planGraph, prefix, node.ID); failedNode != nil {
			RetargetAdds(planGraph, failedNode, operations)
		}
	}
	// The sentinel's own client answers the growth gate's question, on the
	// job's spend node like every other second thought this job has about
	// itself. It is asked only after the free caps pass, and only when the job
	// carries a criterion to be judged against.
	applied, notes := resident.ApplyRevisionGoverned(judgeCtx,
		// The gate reads through the same window the plan was structured
		// through, which the document itself records.
		//
		// The landed node travels with it because the additions are a reaction
		// to that node and to nothing else: it is the evidence they must be able
		// to read, and its consumers are who they stand in front of. Without it
		// this pass admitted a replacement for a failed leaf that was wired to
		// neither.
		resident.Growth{Reason: resident.GrowRevision, After: node,
			Ask: resident.SatisfierFor(client, planGraph.Window())},
		graph, planGraph, prefix, root, operations)
	if applied > 0 && journal != nil {
		// The journaled structure is now behind the graph in memory. Re-writing
		// it here rather than on every landing is the whole economy of the
		// arrangement: a revision is rare and changes the document, a landing is
		// constant and changes only what the store already records.
		journal()
	}
	if len(notes) > 0 {
		// The record, never the conversation (13.18): a refused revision
		// operation is the sentinel arguing with itself in the sentinel's own
		// vocabulary, and the plan the person cares about did not move.
		_, _ = thread.Record(graph, store.Message{
			Role:   store.RoleSystem,
			NodeID: node.ID,
			Body:   "revision sentinel refusals after " + fmt.Sprintf("%q", firstLine(nodeDisplay(node))) + ":\n" + strings.Join(notes, "\n"),
		})
	}
	if applied == 0 {
		return 0
	}
	reasons := make([]string, 0, len(operations))
	for _, operation := range operations {
		if operation.Applied && strings.TrimSpace(operation.Reason) != "" {
			reasons = append(reasons, operation.Op+": "+firstLine(operation.Reason))
		}
	}
	body := fmt.Sprintf("revised the remaining plan after %q — %d change(s)", firstLine(nodeDisplay(node)), applied)
	if len(reasons) > 0 {
		body += "\n" + strings.Join(reasons, "\n")
	}
	// How the remaining plan changed is progress, and progress lives on the
	// job's own record where the plan it describes is drawn. The person hears
	// about the plan when it delivers, or when they ask.
	_, _ = thread.Record(graph, store.Message{
		Role:   store.RoleSystem,
		NodeID: node.ID,
		Body:   body,
	})
	return applied
}

// ForUser is Sentinel's twin for the other event source. It differs in exactly
// two places: the event is the user speaking with authority, and it never
// declines for want of pending work — the leaves already running still have to
// be told, and that broadcast is the reconciler's next move.
//
// A removal aimed at something already claimed or running is not applied and
// not silently dropped either: it comes back on the receipt as a running
// removal, which is the one thing the caller has to say out loud.
func ForUser(ctx context.Context, settings config.Config, client plan.Completer,
	graph *store.Store, job store.Node, planGraph *plan.Graph, root, message string,
	flavor resident.RevisionFlavor) (resident.Redirection, int, error) {
	// terrain wiring lands here (world-grounded-planning handoff)
	operations, _, err := plan.Revise(settings.Context(pool.WithSpendNode(ctx, job.ID), planGraph.Goal),
		client, planGraph, resident.UserRevisionEvent(message, flavor))
	if err != nil {
		return resident.Redirection{}, 0, err
	}

	var redirection resident.Redirection
	editable := make([]plan.Operation, 0, len(operations))
	// The same naming law the mirror below applies, from the same place, so this
	// pre-check and the edit it guards cannot disagree about which store node an
	// operation means. Spelled out here, a removal aimed at the job's own
	// deliverable looked at "<job>-n<root>", found nothing, and let the removal
	// through as though the node were not running.
	grown := make(map[int]bool)
	for _, operation := range operations {
		if operation.Op == "add" && operation.Applied {
			grown[operation.Node] = true
		}
	}
	storeID := resident.PlanStoreIDs(planGraph, job.ID, grown)
	for _, operation := range operations {
		id := storeID(operation.Node)
		if operation.Op == "remove" {
			if node, found, err := graph.Node(id); err == nil && found &&
				(node.Status == store.Running || node.Status == store.Claimed) {
				redirection.RunningRemovals = append(redirection.RunningRemovals, id)
				continue
			}
		}
		editable = append(editable, operation)
	}
	// Ungated, and only here: the person has just said what they want, so the
	// question "is the goal already covered" is answered by them and not by a
	// reader of the plan. The caps and the journal still hold — a redirect can
	// sprawl a job exactly as anything else can.
	applied, notes := resident.ApplyRevisionGoverned(ctx,
		resident.Growth{Reason: resident.GrowRedirect, Ungated: true},
		graph, planGraph, job.ID, root, editable)
	redirection.Notes = notes
	for _, operation := range editable {
		if !operation.Applied {
			continue
		}
		switch operation.Op {
		case "add":
			redirection.Added++
		case "remove":
			redirection.Dropped++
		case "rewire", "retitle":
			redirection.Amended++
		}
	}
	return redirection, applied, nil
}
