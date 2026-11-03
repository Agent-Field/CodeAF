package lease

import (
	"errors"
	"fmt"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ReconcileReport holds the claims desk across the item's result/receipt write.
// A replacement lease fences result changes. Archived receipts admit only
// cumulative usage from their recorded lease, machine and person. The old
// holder, when still present, is released only after the item committed; retrying
// a lost response then observes the item's receipt before applying anything.
func (d *Desk) ReconcileReport(id int, worker Worker, originalLease, digest string, additionalCost float64, change func(*factory.Item, *factory.WorkerReportReceipt) error) (factory.Item, bool, error) {
	var saved factory.Item
	already := false
	known := errors.New("retained result already acknowledged")
	err := d.change(func(doc *document, _ time.Time) (bool, error) {
		held := find(doc, id)
		if err := d.store.Update(id, func(it *factory.Item) error {
			var receipt *factory.WorkerReportReceipt
			for i := range it.ReportReceipts {
				if it.ReportReceipts[i].Digest == digest {
					receipt = &it.ReportReceipts[i]
					break
				}
			}
			if receipt != nil && receipt.Archived {
				if receipt.Lease != originalLease || receipt.Machine == "" || receipt.Machine != worker.Machine || receipt.Handle == "" || receipt.Handle != factory.NormalizeHandle(worker.Handle) {
					return ErrPerson
				}
			} else {
				if worker.Machine == "" || it.WorktreeMachine != worker.Machine {
					return fmt.Errorf("retained results belong to another worktree machine")
				}
				if owner := it.Person(); owner == "" || owner != factory.NormalizeHandle(worker.Handle) {
					return ErrPerson
				}
			}
			if receipt != nil {
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
