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
//
// AND AN ORDER IS APPLIED, NOT KEPT (owner ruling, 2026-10-08, after a demo
// floor shuffled under the cursor on every read). The rows are sorted when
// the floor is first read and when `O` is pressed; after that a row keeps its
// place across every re-read, whatever the read changed about it. Two things
// move a row: an ARRIVAL, which slides in at the top of its section, and a
// CHANGE OF STATE, which moves it to the top of its new one ([app.factoryPlace]).
type factoryOrder int

const (
	// factoryOrderPriority is the default: the cheap read's priority, 1
	// first and an item with none after every item with one, then the forge
	// number descending, the newest issue first (the floor's own id for work
	// a chat or a terminal made, which has no forge number).
	factoryOrderPriority factoryOrder = iota
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
var factoryOrderWords = [factoryOrders]string{"priority", "first", "age", "cost"}

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

// factoryCycleOrder is `O`: the next order, applied now, with the cursor kept
// on the item it stood on.
func (a *app) factoryCycleOrder() {
	a.factoryRefocus(func() {
		a.fp.order = a.fp.order.next()
		a.factoryPlace(true)
	})
}

// factoryNumber is the number the default order counts down: the forge's
// number, and the floor's own id for an item that has none.
func factoryNumber(it factory.Item) int {
	if it.Num > 0 && (it.Origin == factory.OriginForge || it.Origin == "") {
		return it.Num
	}
	return it.ID
}

// factoryPlace hands each item on the floor its place inside its section. ON
// THE FIRST READ, OR WHEN all IS SET (`O`), every item is placed by the order
// the floor is in. Otherwise an item keeps the place it has, and only an item
// the floor has not placed yet, or one whose state changed, is placed again,
// above every place handed out so far; several arriving on one read stand in
// the order's own order among themselves.
func (a *app) factoryPlace(all bool) {
	snap := a.fp.snap
	if all || a.fp.pos == nil {
		a.fp.pos, a.fp.posState, a.fp.posTop = map[int]int{}, map[int]factory.State{}, 0
		for _, g := range factoryGroups {
			var in []int
			for i, it := range snap.Items {
				if factoryIn(it.State, g.states) {
					in = append(in, i)
				}
			}
			for _, i := range factoryOrdered(snap, in, a.fp.order) {
				it := snap.Items[i]
				a.fp.pos[it.ID] = len(a.fp.pos)
				a.fp.posState[it.ID] = it.State
			}
		}
		return
	}
	var fresh []int
	for i, it := range snap.Items {
		if st, ok := a.fp.posState[it.ID]; !ok || st != it.State {
			fresh = append(fresh, i)
		}
	}
	fresh = factoryOrdered(snap, fresh, a.fp.order)
	for n := len(fresh) - 1; n >= 0; n-- {
		it := snap.Items[fresh[n]]
		a.fp.posTop--
		a.fp.pos[it.ID] = a.fp.posTop
		a.fp.posState[it.ID] = it.State
	}
}

// factoryPlaced sorts one section's rows by the places the page handed out,
// stably, so a row with no place keeps the order's own position after every
// row that has one. A view with no places is the order alone.
func factoryPlaced(snap factory.Snapshot, in []int, pos map[int]int) []int {
	if len(pos) == 0 || len(in) < 2 {
		return in
	}
	at := func(i int) (int, bool) {
		p, ok := pos[snap.Items[i].ID]
		return p, ok
	}
	out := append([]int(nil), in...)
	sort.SliceStable(out, func(i, j int) bool {
		pi, oki := at(out[i])
		pj, okj := at(out[j])
		if oki != okj {
			return oki
		}
		return oki && pi < pj
	})
	return out
}

// factoryOrdered sorts one section's rows (indexes into snap.Items) by o. The
// sort is stable, so ties keep the store's order.
func factoryOrdered(snap factory.Snapshot, in []int, o factoryOrder) []int {
	if len(in) < 2 {
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
	case factoryOrderPriority:
		less = func(x, y factory.Item) bool {
			px, py := x.Triage.Priority, y.Triage.Priority
			if (px == 0) != (py == 0) {
				return px != 0
			}
			if px != py {
				return px < py
			}
			return factoryNumber(x) > factoryNumber(y)
		}
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
	w := ansi.StringWidth(r) + factoryGutter
	if w > room {
		return "", 0
	}
	return a.pal.dim(r) + factorySpaces(factoryGutter), w
}
