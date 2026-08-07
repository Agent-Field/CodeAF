package resident

import (
	"fmt"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// reconcileCharterOutcomes is deliberately derived from durable graph state,
// not only from the resident's event cursor. A process restart between subtree
// settlement and review therefore cannot lose promotion or demotion evidence.
func (r *Reconciler) reconcileCharterOutcomes() error {
	nodes, err := r.store.Nodes()
	if err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, node := range nodes {
		if node.Provenance.Origin != store.OriginTrigger || node.Provenance.CharterID == "" {
			continue
		}
		assessment, decided, err := r.store.AssessCharterFiring(node.ID)
		if err != nil {
			return fmt.Errorf("assess charter firing %s: %w", node.ID, err)
		}
		if !decided || seen[assessment.JobID] {
			continue
		}
		seen[assessment.JobID] = true
		if _, err := r.store.RecordCharterFiringOutcome(assessment, tenureAfter()); err != nil {
			return fmt.Errorf("review charter firing %s: %w", assessment.JobID, err)
		}
	}
	return nil
}
