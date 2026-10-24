package lease

import (
	"errors"
	"fmt"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ReconcileReport holds the claims desk across the item's result/receipt write.
// A replacement lease is an absolute fence, even on the same machine. The old
// holder, when still present, is released only after the item committed; retrying
// a lost response then observes the item's receipt before applying anything.
func (d *Desk) ReconcileReport(id int, worker Worker, originalLease, digest string, additionalCost float64, change func(*factory.Item, *factory.WorkerReportReceipt) error) (factory.Item, bool, error) {
	var saved factory.Item
	already := false
	known := errors.New("retained result already acknowledged")
	err := d.change(func(doc *document, _ time.Time) (bool, error) {
		held := find(doc, id)
		if err := d.store.Update(id, func(it *factory.Item) error {
			if worker.Machine == "" || it.WorktreeMachine != worker.Machine {
				return fmt.Errorf("retained results belong to another worktree machine")
			}
			if owner := it.Person(); owner == "" || owner != factory.NormalizeHandle(worker.Handle) {
				return ErrPerson
			}
			for i := range it.ReportReceipts {
				receipt := &it.ReportReceipts[i]
				if receipt.Digest == digest {
					already = true
					if additionalCost <= receipt.AdditionalCost {
						saved = *it
						return known
					}
					// Late usage is accounting only. It neither applies the frozen
					// result twice nor alters a later worker's execution state.
					if err := change(it, receipt); err != nil {
						return err
					}
					saved = *it
					return nil
				}
			}
			if held != nil && (held.ID != originalLease || held.Machine != worker.Machine || factory.NormalizeHandle(held.Handle) != factory.NormalizeHandle(worker.Handle)) {
				return ErrLeased
			}
			if err := change(it, nil); err != nil {
				return err
			}
			saved = *it
			return nil
		}); err != nil && !errors.Is(err, known) {
			return false, err
		}
		if held == nil || held.ID != originalLease {
			return false, nil
		}
		// Publication/merge cannot enter reconciliation, so no carried approval
		// may survive release and turn this result acknowledgment into an action.
		release(doc, id)
		return true, nil
	})
	return saved, already, err
}
