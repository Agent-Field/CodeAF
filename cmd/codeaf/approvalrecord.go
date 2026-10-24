package main

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/pair"
)

// withApprovalRecord moves the terminal's own-record write ahead of granting
// the request. The new device can read the grant immediately, before this
// terminal prints its receipt, so a write after approval was already too late.
func (d pairDoor) withApprovalRecord() pairDoor {
	makeApprover, record := d.approver, d.ownRecord
	d.approver = func(route pair.Mailbox) (pair.Approver, error) {
		who, err := makeApprover(route)
		if err == nil && record != nil {
			who.BeforeApprove = func(ctx context.Context) error { return record(ctx, route) }
		}
		return who, err
	}
	d.ownRecord = nil
	return d
}
