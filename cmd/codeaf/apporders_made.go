package main

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// PageOrdersMadeIn answers orders.made_in: one bounded page of the orders whose
// RECORDED creation team is exactly ask.Crew, and how many orders in the same
// stretch of the store have no recorded team, from one read of the store
// (see [appwire.OrdersMadeResult] and the store's MadeInTeam).
//
// THE CREW ONLY HAS TO EXIST: a closed crew reads like an open one, because
// what was made in it is history. Nothing about who is in the crew now, who
// manages it, or its workspace is consulted, and [appwire.Order.Crew] (where an
// order belongs) is echoed as stored and never matched. Rows carry no last_run
// and no asking: the store is read once per page, never a query per row.
func (b *appBackend) PageOrdersMadeIn(ctx context.Context, ask appwire.OrdersMadeAsk) (*appwire.OrdersMadeResult, error) {
	limit, err := appwire.CheckOrdersMadeAsk(ask)
	if err != nil {
		return nil, err
	}
	if _, _, err := b.crewTeam(ask.Crew); err != nil {
		return nil, err
	}
	store, err := b.ordersOf()
	if err != nil {
		return nil, err
	}
	page, err := store.MadeInTeam(ctx, ask.Crew, ask.Cursor, limit)
	if err != nil {
		return nil, err
	}
	now := b.now()
	res := &appwire.OrdersMadeResult{Orders: make([]appwire.Order, 0, len(page.Rows)), Unknown: page.Unknown, Next: page.Next}
	for _, a := range page.Rows {
		res.Orders = append(res.Orders, orderView(nil, a, now))
	}
	return res, nil
}
