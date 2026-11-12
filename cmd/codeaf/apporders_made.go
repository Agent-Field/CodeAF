package main

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/automation"
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
//
// THE ANSWER IS BOUNDED IN THE BYTES IT ENCODES TO, not estimated from what is
// stored: each order is encoded exactly as the wire will encode it — with HTML
// escaping on, so a "<" counts the six bytes it becomes — and the page ends
// BEFORE the order that would take it past [appwire.OrdersMadeBytesMax]. The
// store projects only the fields an order's view reads and copies out at most
// that budget of projections, so a watch's large memo never reaches here.
// The page then continues from the last row it covered and counts unknown only
// up to there; an order that cannot fit even alone is refused by name, and the
// walk never moves past it.
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
	page, err := store.MadeInTeam(ctx, ask.Crew, ask.Cursor, limit, appwire.OrdersMadeBytesMax)
	if err != nil {
		return nil, ordersMadeError(err)
	}
	now := b.now()
	res := &appwire.OrdersMadeResult{Orders: []appwire.Order{}}
	used, last, cut, stopID := 0, "", page.Stopped, page.StopID
	for _, entry := range page.Entries {
		// AN ENTRY IS COVERED ONLY WHEN A CURSOR CAN CONTINUE AFTER IT, so an id
		// too long for one ends the page before it rather than being passed over.
		if _, err := page.CursorAt(entry.ID); err != nil {
			cut, stopID = true, ""
			break
		}
		if entry.Row != nil {
			order := orderView(nil, *entry.Row, now)
			encoded, err := json.Marshal(order)
			if err != nil {
				return nil, appwire.Errorf(appwire.CodeEngine, "the crew's orders could not be read")
			}
			// One byte more for the comma between this order and the last.
			if used+len(encoded)+1 > appwire.OrdersMadeBytesMax {
				cut, stopID = true, entry.ID
				break
			}
			used += len(encoded) + 1
			res.Orders = append(res.Orders, order)
		}
		if entry.Unknown {
			res.Unknown++
		}
		last = entry.ID
	}
	if last == "" && cut {
		// The store refuses a page that covers nothing, so this is the app's own
		// cut: the first order alone encodes past the budget. Its id was read
		// within the store's bound, so it may be named.
		if stopID == "" {
			return nil, appwire.Errorf(appwire.CodeTooLarge, "the next order is too large to read here")
		}
		return nil, appwire.Errorf(appwire.CodeTooLarge, "order %s is too large to read here", stopID)
	}
	// NEXT IS ABSENT ONLY AT THE STORE'S END WITH EVERY ENTRY COVERED: a page
	// this method cut short is never the end, whatever follows it.
	if !page.End || cut || last != lastEntry(page) {
		if last == "" {
			// The store answers no page that is neither the end nor covers a row;
			// one that did would hand out a cursor going nowhere.
			return nil, appwire.Errorf(appwire.CodeEngine, "the crew's orders could not be read")
		}
		if res.Next, err = page.CursorAt(last); err != nil {
			return nil, appwire.Errorf(appwire.CodeEngine, "the crew's orders could not be read")
		}
	}
	return res, nil
}

// lastEntry is the id of a page's last entry, "" for none.
func lastEntry(page automation.MadeInPage) string {
	if len(page.Entries) == 0 {
		return ""
	}
	return page.Entries[len(page.Entries)-1].ID
}

// ordersMadeError is the wire's word for a store refusal: a walk that must
// start again, an order too large to carry, a cursor no page of this crew's
// walk gave, a store that cannot be read honestly; any other error is the
// engine's, and the method says it plainly. Every message the store writes
// names an id only when it is short enough to read.
func ordersMadeError(err error) error {
	switch {
	case errors.Is(err, automation.ErrMadeInStale):
		return appwire.Errorf(appwire.CodeStaleCursor, "the orders changed while they were being read; start again from the first page")
	case errors.Is(err, automation.ErrMadeInTooLarge):
		return appwire.Errorf(appwire.CodeTooLarge, "%s", err.Error())
	case errors.Is(err, automation.ErrMadeInCursor):
		return appwire.Errorf(appwire.CodeBadParams, "orders.made_in cursor is not one this crew's walk gave")
	case errors.Is(err, automation.ErrMadeInDamaged):
		return appwire.Errorf(appwire.CodeEngine, "%s", err.Error())
	}
	return err
}
