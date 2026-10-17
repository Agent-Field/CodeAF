package main

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// PageOrdersMadeIn answers orders.made_in: one bounded page of the orders whose
// RECORDED creation team is exactly ask.Crew, and how many orders the store has
// no recorded team for (see [appwire.OrdersMadeResult]).
//
// THE CREW ONLY HAS TO EXIST: a closed crew reads like an open one, because
// what was made in it is history. Nothing about who is in the crew now, who
// manages it, or its workspace is consulted, and [appwire.Order.Crew] (where an
// order belongs) is echoed as stored and never matched. Rows carry no last_run
// and no asking: the store is asked for the page and one count, never a query
// per row.
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
	page, more, err := store.MadeInTeam(ctx, ask.Crew, ask.Cursor, limit)
	if err != nil {
		return nil, err
	}
	unknown, err := store.TeamUnknownCount(ctx)
	if err != nil {
		return nil, err
	}
	now := b.now()
	res := &appwire.OrdersMadeResult{Orders: make([]appwire.Order, 0, len(page)), Unknown: unknown}
	for _, a := range page {
		res.Orders = append(res.Orders, orderView(nil, a, now))
	}
	if more && len(page) > 0 {
		res.Next = page[len(page)-1].ID
	}
	return res, nil
}
