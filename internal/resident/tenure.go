package resident

import "fmt"

// reconcileCharterOutcomes is deliberately derived from durable graph state,
// not only from the resident's event cursor. A process restart between subtree
// settlement and review therefore cannot lose promotion or demotion evidence.
//
// What it must not do is re-derive the whole history of firings twice a second.
// Assessing one costs a node read, a walk up the parent chain, and two
// unindexed json_extract queries — all of it before the short-circuit that
// says the ladder already recorded this job. The query now excludes the firings
// that are settled business by construction, and this pass carries a watermark
// over the rest: the outcome is journaled, so a firing that has been reviewed
// never needs assessing again.
func (r *Reconciler) reconcileCharterOutcomes() error {
	nodes, err := r.store.CharterFiredNodes(r.charterOutcomeSeq)
	if err != nil {
		return err
	}
	seen := make(map[string]bool)
	settled, waiting := r.charterOutcomeSeq, int64(-1)
	for _, node := range nodes {
		assessment, decided, err := r.store.AssessCharterFiring(node.ID)
		if err != nil {
			return fmt.Errorf("assess charter firing %s: %w", node.ID, err)
		}
		if !decided {
			// The firing is either still running or already on the ladder, and
			// from out here the two look the same. Both hold the watermark: one
			// because a later tick has to reach this node again, the other
			// because it costs nothing to keep asking a question that is about
			// to leave the set anyway when the job folds.
			if waiting < 0 {
				waiting = node.CreatedSeq
			}
			continue
		}
		if !seen[assessment.JobID] {
			seen[assessment.JobID] = true
			if _, err := r.store.RecordCharterFiringOutcome(assessment, tenureAfter()); err != nil {
				return fmt.Errorf("review charter firing %s: %w", assessment.JobID, err)
			}
		}
		if waiting < 0 {
			settled = node.CreatedSeq
		}
	}
	// Nodes admitted by one splice share a created_seq, so the watermark stops
	// below anything still waiting rather than at the last thing finished — a
	// sibling must never be skipped for having been born in the same breath.
	if waiting >= 0 && settled >= waiting {
		settled = waiting - 1
	}
	r.charterOutcomeSeq = settled
	return nil
}
