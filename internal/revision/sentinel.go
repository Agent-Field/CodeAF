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
	applied, notes := resident.ApplyRevision(graph, planGraph, prefix, root, operations)
	if applied > 0 && journal != nil {
		// The journaled structure is now behind the graph in memory. Re-writing
		// it here rather than on every landing is the whole economy of the
		// arrangement: a revision is rare and changes the document, a landing is
		// constant and changes only what the store already records.
		journal()
	}
	if len(notes) > 0 {
		_, _ = thread.Post(graph, store.Message{
			SessionID: node.Provenance.SessionID,
			Role:      store.RoleSystem,
			NodeID:    node.ID,
			Body:      "revision sentinel refusals after " + fmt.Sprintf("%q", firstLine(nodeDisplay(node))) + ":\n" + strings.Join(notes, "\n"),
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
	_, _ = thread.Post(graph, store.Message{
		SessionID: node.Provenance.SessionID,
		Role:      store.RoleSystem,
		NodeID:    node.ID,
		Body:      body,
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
	for _, operation := range operations {
		id := fmt.Sprintf("%s-n%d", job.ID, operation.Node)
		if operation.Op == "remove" {
			if node, found, err := graph.Node(id); err == nil && found &&
				(node.Status == store.Running || node.Status == store.Claimed) {
				redirection.RunningRemovals = append(redirection.RunningRemovals, id)
				continue
			}
		}
		editable = append(editable, operation)
	}
	applied, notes := resident.ApplyRevision(graph, planGraph, job.ID, root, editable)
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
