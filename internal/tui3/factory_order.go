package tui3

import (
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/charmbracelet/x/ansi"
)

// factoryOrder is how the floor orders the rows WITHIN each of its sections,
// cycled by `O`. The sections themselves never move: what waits on the person
// stays first whichever order is chosen, because an order is a way to read a
// section, not a way to bury a question.
//
// THE CHOICE LASTS THE SESSION. The density toggle (`z`) is not saved either,
// and the two are the same kind of thing: how a person likes to look at the
// floor right now, not a setting to manage.
type factoryOrder int

const (
	// factoryOrderObligation is today's order: the store's, newest first.
	factoryOrderObligation factoryOrder = iota
	// factoryOrderFirst is the cheap read's guess at what to take first:
	// priority 1 before 5, then the older item first, and an item with no
	// priority after every item with one. Rows draw the read's reason.
	factoryOrderFirst
	// factoryOrderAge is the oldest arrival first.
	factoryOrderAge
	// factoryOrderCost is the cheapest first, by what the item has spent
	// when it has spent anything and by its estimate otherwise; an item with
	// neither comes last.
	factoryOrderCost
	factoryOrders
)

// factoryOrderWords is each order as the hint line names it.
var factoryOrderWords = [factoryOrders]string{"by obligation", "first", "age", "cost"}

func (o factoryOrder) word() string {
	if o < 0 || o >= factoryOrders {
		return factoryOrderWords[0]
	}
	return factoryOrderWords[o]
}

func (o factoryOrder) next() factoryOrder { return (o + 1) % factoryOrders }

// factoryOrderHint is the hint line's clause for `O`, naming the order the
// floor is in now: `O order · first`.
func (a *app) factoryOrderHint() string { return "O order · " + a.fp.order.word() }

// factoryCycleOrder is `O`: the next order, with the cursor kept on the item
// it stood on.
func (a *app) factoryCycleOrder() {
	a.factoryRefocus(func() { a.fp.order = a.fp.order.next() })
}

// factoryOrdered sorts one section's rows (indexes into snap.Items) by o. The
// sort is stable, so ties keep the store's order, and the default order is
// the rows untouched.
func factoryOrdered(snap factory.Snapshot, in []int, o factoryOrder) []int {
	if o == factoryOrderObligation || len(in) < 2 {
		return in
	}
	items := snap.Items
	older := func(x, y factory.Item) bool {
		switch {
		case x.Created.IsZero() != y.Created.IsZero():
			return !x.Created.IsZero()
		}
		return x.Created.Before(y.Created)
	}
	var less func(x, y factory.Item) bool
	switch o {
	case factoryOrderFirst:
		less = func(x, y factory.Item) bool {
			px, py := x.Triage.Priority, y.Triage.Priority
			if (px == 0) != (py == 0) {
				return px != 0
			}
			if px != py {
				return px < py
			}
			return older(x, y)
		}
	case factoryOrderAge:
		less = older
	case factoryOrderCost:
		less = func(x, y factory.Item) bool {
			cx, cy := factoryCost(x), factoryCost(y)
			if (cx == 0) != (cy == 0) {
				return cx != 0
			}
			return cx < cy
		}
	default:
		return in
	}
	out := append([]int(nil), in...)
	sort.SliceStable(out, func(i, j int) bool { return less(items[out[i]], items[out[j]]) })
	return out
}

// factoryCost is what an item costs for the `cost` order: its spend once it
// has spent anything, and its estimate before, the same two figures the row's
// money fact draws.
func factoryCost(it factory.Item) float64 {
	if s := it.Stream; s != nil && s.Spent > 0 {
		return s.Spent
	}
	return it.Triage.Est
}

// factoryOrderReason is the read's reason as a row draws it in the `first`
// order: dim, at the far right before the age, and "" in every other order,
// on an item with none, or when room (the cells the facts leave free) cannot
// hold it with its two-cell gap. IT IS THE FIRST THING A NARROW ROW DROPS,
// before any fact, because it explains an order and the facts are the item.
func (a *app) factoryOrderReason(it factory.Item, room int) (string, int) {
	if a.fp.order != factoryOrderFirst {
		return "", 0
	}
	r := strings.TrimSpace(it.Triage.Reason)
	if r == "" {
		return "", 0
	}
	w := ansi.StringWidth(r) + 2
	if w > room {
		return "", 0
	}
	return a.pal.dim(r) + "  ", w
}
