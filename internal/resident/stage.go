package resident

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
)

// The phases a pending command passes through before its work exists.
//
// These are the reconciler's half of a sequence the planner already writes the
// middle of. Everything between "reading the request" and "setting working
// standards" has posted a row for as long as plan progress has existed; the two
// ends never did. A splice is a compile, then a plan, then a title, then the
// graph write, and only the plan ever said anything — so a card that had been
// pulsing "creating task…" for ninety seconds was, as far as any surface could
// tell, doing nothing, whether it was mid-compile or wedged forever.
//
// The vocabulary is the planner's own (internal/plan/progress.go): a surface
// reads Phase and draws it, so a new word here would be a new word on the card.
// "reading the request" is deliberately the same phase the planner's grounding
// pass uses — it is the same phase from the person's side, and a surface that
// collapses consecutive identical rows is right to collapse them.
const (
	stageReading  = "reading the request"
	stagePlanning = "working out the plan"
	stageStarting = "starting the work"
	// stageStalled is its own phase because it is the only one that is not
	// forward movement. A person watching a card deserves to see that the
	// system noticed, rather than watching a phase line sit still.
	stageStalled = "taking another run at it"
)

// commandPlanAnchor is the root a splice will admit, which is also the node
// every row about that splice — the reconciler's stage rows and the planner's
// own progress — is recorded against. One expression, so the anchor a phase row
// names and the anchor the plan is built under cannot drift apart.
func commandPlanAnchor(command store.Command) string {
	return fmt.Sprintf("task-%d", command.Seq)
}

// spliceStageLatest says what landed, in the shape a person counts in. A step
// count is the honest answer for a plan and "1 step" would be a strange way to
// describe a single errand, so a lone leaf is named instead.
func spliceStageLatest(subtree store.Subtree) string {
	switch len(subtree.Nodes) {
	case 0:
		return ""
	case 1:
		title := strings.TrimSpace(subtree.Nodes[0].Title)
		if title == "" {
			title = firstLine(subtree.Nodes[0].Brief)
		}
		return title
	default:
		return fmt.Sprintf("%d steps", len(subtree.Nodes))
	}
}

// noteCommandStage journals one phase boundary of a pending command.
//
// It is the same message the contract pass has always posted and deliberately
// not a new mechanism: a system row, carrying the command seq and a progress
// payload, recorded against the root the splice is about to admit. Written
// through thread.Record, so it reaches the job's record and never the
// conversation — the head has already said the one sentence in the thread this
// command gets.
//
// A failed write is dropped exactly as the planner's poster drops one. A row
// about progress must never be the reason the work it describes fails.
func (r *Reconciler) noteCommandStage(command store.Command, phase, latest string) {
	if r == nil || r.store == nil {
		return
	}
	// Only a pending splice may name a root that does not exist yet
	// (store.pendingMessageAnchor). Every other kind would be refused, and a
	// reflex has no planning to report.
	if command.Kind != store.CommandSplice || command.Reflex {
		return
	}
	_, _ = thread.Record(r.store, store.Message{
		Role:       store.RoleSystem,
		Body:       phase,
		NodeID:     commandPlanAnchor(command),
		CommandSeq: command.Seq,
		Progress: &store.MessageProgress{
			Phase:  phase,
			Latest: clipLabel(strings.TrimSpace(latest), 200),
		},
	})
}
