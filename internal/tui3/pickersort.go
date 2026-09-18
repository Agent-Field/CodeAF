package tui3

import (
	"sort"
	"strings"
	"time"
)

// ── SORTING THE MODEL LIST ──────────────────────────────────────────────────
//
// The list is a TABLE, and the question in front of somebody reading one is
// almost never "what does this row say" — it is "which row is the cheapest",
// "which holds the most", "which answers fastest". A column makes those
// comparable by eye over a screenful. Over six hundred rows the eye is not
// enough, and ordering by the column is the only thing that is.
//
// This is also where the filter box's two ordering words went. `fast` and `cheap`
// sorted the list and were removed with the rest of that query language
// ([picker.rank] argues why), and the objection to them was never that sorting is
// the wrong idea — it was that a word typed into a name box is an undiscoverable
// way to ask for it, and that `fast` and `cheap` are two opinions about six
// columns. A key that walks the columns you can see is the discoverable form of
// the same request, and it can reach every one of them.

// pickerSortKey is which column the list is ordered by. The zero value is the
// list's own order, which is what the picker opens on and what the name search
// ranks into.
type pickerSortKey uint8

const (
	// pickerByList is NOT A COLUMN AND HAS TO BE IN THE CYCLE. It is the catalog's
	// order, grouped by service, and — the half that matters — it is the order the
	// name search puts its hits in. A cycle that could not come back to it would
	// be a sort you cannot undo: type `claude`, press the key once, and best-match
	// ordering is gone for the life of the list.
	pickerByList pickerSortKey = iota
	pickerByName
	pickerByFirst
	pickerByIn
	pickerByOut
	pickerByWindow
	pickerByRate
	pickerByElo
	pickerSortKeyCount
)

// pickerSort is the order the list is in: the column, and whether it has been
// turned round.
type pickerSort struct {
	key  pickerSortKey
	back bool
}

// on is the tasks table's own rule ([tasksSort.on]), which is the rule this
// surface already has for this gesture: a DIFFERENT column starts the way that
// column is naturally read, and the SAME column again turns it round.
func (s pickerSort) on(key pickerSortKey) pickerSort {
	if s.key == key {
		return pickerSort{key: key, back: !s.back}
	}
	return pickerSort{key: key}
}

// next walks to the next column, wrapping through [pickerByList].
func (k pickerSortKey) next() pickerSortKey { return (k + 1) % pickerSortKeyCount }

// column is which of [modelColumns] this key orders, and [colTableNameMark] for
// the name — whose head is not in that set — and [colTableNoMark] for the list's
// own order, which marks nothing because nothing is being ordered by.
func (k pickerSortKey) column() int {
	head := ""
	switch k {
	case pickerByList:
		return colTableNoMark
	case pickerByName:
		return colTableNameMark
	case pickerByFirst:
		head = "first"
	case pickerByIn:
		head = "in/M"
	case pickerByOut:
		head = "out/M"
	case pickerByWindow:
		head = "window"
	case pickerByRate:
		head = "t/s"
	case pickerByElo:
		head = "elo"
	}
	// THE HEAD IS LOOKED UP AND NOT WRITTEN TWICE. [modelColumns] owns the
	// spelling; a second copy here is a column that keeps its arrow after
	// somebody renames the heading (the one-source-of-truth rule).
	for at, col := range modelColumns {
		if col.head == head {
			return at
		}
	}
	return colTableNoMark
}

// column is the mark the table's heading wears: this sort's key, asked of the key
// itself. It exists so the fit is handed one question and not two.
func (s pickerSort) column() int { return s.key.column() }

// down is the arrow a column wears when it is read its natural way round, and up
// when it has been turned. They are the tasks table's own two characters
// ([tasksSortDown]) because this is the same gesture on a different list.
func (s pickerSort) arrow() string {
	if s.key == pickerByList {
		return ""
	}
	if s.back {
		return tasksSortUp
	}
	return tasksSortDown
}

// up reports whether this key is naturally read SMALLEST FIRST.
//
// EACH COLUMN HAS ONE OBVIOUS DIRECTION and it is the answer to the question
// people bring to that column. Nobody opens a price column to find the most
// expensive model or a window column to find the smallest, so the first press is
// the useful order every time and the second press is there for the rarer half.
func (k pickerSortKey) up() bool {
	switch k {
	case pickerByName, pickerByFirst, pickerByIn, pickerByOut:
		// A name reads A to Z; a wait and a price are better when they are
		// smaller.
		return true
	}
	// A window, a rate and a score are better when they are bigger.
	return false
}

// word is how the foot and the manual name this key. ONE SPELLING PER KEY, and
// for the six that are columns it is the column's own head, so the word in the
// foot is the word over the cells.
func (k pickerSortKey) word() string {
	switch k {
	case pickerByList:
		return "list"
	case pickerByName:
		return modelHead
	}
	if at := k.column(); at >= 0 {
		return modelColumns[at].head
	}
	return ""
}

// pickerRank is one model's sortable values, read ONCE per sort rather than once
// per comparison.
//
// THE FIGURES ARE THE NUMBERS AND NEVER THE CELLS. The table's cells are
// formatted strings — `$1.3`, `$10`, `262k`, `1M` — and sorting those as text
// puts `$10` under `$1.3` and a million tokens under two hundred thousand. The
// cell is for reading; this is for ordering, and they are different jobs on the
// same fact.
//
// ZERO IS "NOBODY PUBLISHED IT" in every field, which is the catalog's own
// convention (models.go) and the ledger's, and [pickerAhead] is what makes that
// safe to sort on.
type pickerRank struct {
	name   string
	first  float64
	in     float64
	out    float64
	window float64
	rate   float64
	elo    float64
}

// value is the one figure a key orders by.
func (r pickerRank) value(k pickerSortKey) float64 {
	switch k {
	case pickerByFirst:
		return r.first
	case pickerByIn:
		return r.in
	case pickerByOut:
		return r.out
	case pickerByWindow:
		return r.window
	case pickerByRate:
		return r.rate
	case pickerByElo:
		return r.elo
	}
	return 0
}

// pickerRankOf reads one model's sortable values. The two measured ones come from
// the lane the chooser would land on, which is the lane the `first` and `t/s`
// cells are drawn from ([modelFactsOf]) — so the column a person sorts by is the
// column they were looking at.
func pickerRankOf(model Model, now time.Time) pickerRank {
	rank := pickerRank{
		name:   strings.ToLower(model.ID),
		in:     model.PromptPrice,
		out:    model.CompletionPrice,
		window: float64(model.ContextLength),
		elo:    model.ArenaElo,
	}
	if best, known := bestLane(laneViews(model.ID, now)); known {
		rank.first, rank.rate = best.TTFT, best.Rate
	}
	return rank
}

// pickerAhead reports whether a sorts before b under this order.
//
// WHAT NOBODY PUBLISHED SORTS LAST, IN BOTH DIRECTIONS. This is the emptiness
// law's own shape said about order instead of about drawing: a model with no
// published price is not the cheapest model, and turning the column round must not
// make it the most expensive one either — it is simply not in the comparison. The
// old `cheap` filter word needed the same rule and got it by calling an unknown
// price positive infinity, which was right in one direction only.
func pickerAhead(s pickerSort, a, b pickerRank) bool {
	if s.key == pickerByName {
		if s.back {
			return a.name > b.name
		}
		return a.name < b.name
	}
	first, second := a.value(s.key), b.value(s.key)
	if (first == 0) != (second == 0) {
		return second == 0
	}
	if first == second {
		return false
	}
	up := s.key.up()
	if s.back {
		up = !up
	}
	if up {
		return first < second
	}
	return first > second
}

// sortHits puts the filter's hits in the order the sort asks for, and leaves them
// exactly as the ranking left them under [pickerByList].
//
// IT IS STABLE AND IT IGNORES THE SERVICE GROUPS. Stable so that rows a column
// cannot tell apart — every model with no elo, every model at the same price —
// keep the order they already had, which is the name ranking's or the catalog's
// and is the only order a person has any expectation about. And across groups
// because "the cheapest model" is a question about the catalog and not about one
// service: the group HEADINGS come off while a sort is on ([picker.groupBefore]),
// since a heading over rows that are no longer grouped is a claim about the
// structure that is not true any more.
func (p *picker) sortHits() {
	if p.sort.key == pickerByList || len(p.hits) < 2 {
		return
	}
	now := timeNow()
	ranks := make(map[int]pickerRank, len(p.hits))
	for _, at := range p.hits {
		// AN UNAVAILABLE SERVICE'S NOTICE IS NOT A MODEL and has no figures. It
		// ranks as a row that published nothing, which sorts it to the end
		// alongside the models that did the same.
		ranks[at] = pickerRankOf(p.all[at], now)
	}
	sort.SliceStable(p.hits, func(i, j int) bool {
		return pickerAhead(p.sort, ranks[p.hits[i]], ranks[p.hits[j]])
	})
}

// sortable reports whether this list has anything to order by that key.
//
// ── A COLUMN NOBODY PUBLISHED IS NOT A RUNG ─────────────────────────────────
//
// This is law 4 said about the cycle instead of about the heading. A catalog with
// no measurements draws no `first` and no `t/s` ([colTable.fit] drops them), and a
// rung for a column that is not there is a press that moves no row, paints no
// arrow and changes nothing a person can see — which is indistinguishable from the
// key being broken. It was: on a real catalog with nothing measured, the second
// press of `alt+s` did exactly nothing.
//
// IT IS ASKED OF THE MEASUREMENT AND NOT OF THE FIT, so the answer is about the
// DATA rather than about the frame. A narrow window that had no room for `elo`
// still shows the score in the ranked tail, and a column the frame dropped is
// still a column the list can be ordered by; a column the catalog never filled is
// not.
func (p *picker) sortable(key pickerSortKey) bool {
	switch key {
	case pickerByList, pickerByName:
		// The list's own order and the names are always there — there is no such
		// thing as a row without an id.
		return true
	}
	p.measure()
	at := key.column()
	if p.columns == nil || at < 0 || at >= len(p.columns.wide) {
		// NOTHING MEASURED YET MEANS NOTHING RULED OUT. A phone never builds this
		// measurement at all ([picker.tableFit] leaves before it), and refusing a
		// sort we cannot prove is empty would be worse than allowing one that
		// turns out to move nothing.
		return true
	}
	return p.columns.wide[at] > 0
}

// nextSort is the rung the key walks to: the next one this list can actually
// order by. The cap is the cycle's own length, so a list with nothing in any
// column comes back round to [pickerByList] rather than spinning.
func (p *picker) nextSort() pickerSortKey {
	key := p.sort.key
	for range pickerSortKeyCount {
		if key = key.next(); p.sortable(key) {
			return key
		}
	}
	return pickerByList
}

// sortBy is the key pressed: the order changes, the list is rebuilt in it, and the
// cursor goes to the top.
//
// THE CURSOR GOES TO THE TOP BECAUSE THE TOP IS THE ANSWER. Somebody who sorted by
// price asked which model is cheapest, and the answer is row one — leaving the
// cursor on row nine of the old order would put it on a model the new order has
// nothing to say about. It is [picker.rank]'s own rule after a changed query, for
// the same reason.
func (p *picker) sortBy(key pickerSortKey) {
	p.sort = p.sort.on(key)
	// THE TABLE IS RE-FITTED because the arrow takes cells in the column it lands
	// on ([colTableFit.mark]), and a fit cached under the old mark would draw the
	// arrow in a column measured without room for it.
	p.fitAt = 0
	p.hits = p.hits[:0]
	p.rank()
}
