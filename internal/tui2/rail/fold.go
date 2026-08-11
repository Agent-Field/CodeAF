package rail

import (
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
)

// The overflow policy (8.1.7).
//
// The law has three clauses: a LIVE list folds from the top so the running rows
// stay visible; a FINALIZED list gives its slots to the failed rows first; and
// the fold line carries the breakdown (`… 21 more (18 pending · 3 done)`).
// internal/tui2/blocks implements exactly that for lists of equal-height items
// with no cursor, and the HUD rendering uses it directly ([blocks.Folder]).
//
// The rail and the full-pane list need two things blocks.Folder cannot know
// about, so they fold here instead:
//
//   - ROWS HAVE HEIGHTS. A task card is three lines, a focused one more, a
//     worker row one. A budget counted in rows would overflow a budget measured
//     in lines.
//   - THE CURSOR MUST SURVIVE. A fold that hides the selected row makes the
//     rail lie about where the user is — the one thing a map may never do.
//
// So this is the same law with the selection pinned and the budget in lines:
// rows claim slots in priority order, and are then DRAWN IN THEIR ORIGINAL
// ORDER, because a fold may hide a row but must never re-sort one (7.2).

// Rank order for slot claiming. Lower claims first.
//
// The middle two SWAP with the policy, and that swap is the whole of 8.1.7's
// two clauses: a live list is being read to watch work happen, so its running
// rows outrank its wreckage; a finalized list is being read to find out what
// went wrong, so its failures outrank everything that merely finished.
const (
	rankSelected = iota // the cursor never folds away
	rankQuestion        // a blocked human outranks everything else (5.9, 10.5.22)
	rankFirst           // running in a live list; failed in a finalized one
	rankSecond
	rankRest
)

// plan is a fold decision in lines.
type plan struct {
	// shown are row indices in ascending display order.
	shown []int
	// hidden is how many rows the fold line stands for.
	hidden int
	// fold is the fold line, empty when nothing was hidden.
	fold string
	// atTop places the fold line above the rows: a live list folds from the
	// top, so the live edge sits where the reader is looking.
	atTop bool
}

// folder carries the buffers so a rail that re-folds on every frame allocates
// nothing once warm, including the fold line — which is rebuilt only when the
// hidden breakdown actually changes.
type folder struct {
	shown  []int
	order  []int
	counts map[blocks.ItemState]int
	last   [6]int
	fold   string
	built  bool
}

// fit picks which rows fit in a line budget.
//
// rows, heights and the selection index are all indexed the same way. sel < 0
// means no selection in this list (the HUD has no cursor). The returned plan
// aliases the folder's buffer and is valid until the next fit.
func (f *folder) fit(rows []Row, heights []int, budget, sel int, live bool) plan {
	f.shown = f.shown[:0]
	n := len(rows)
	if n == 0 {
		return plan{}
	}
	total := 0
	for i := 0; i < n; i++ {
		total += heights[i]
	}
	if budget <= 0 {
		return f.line(rows, nil, n, live)
	}
	if total <= budget {
		for i := 0; i < n; i++ {
			f.shown = append(f.shown, i)
		}
		return plan{shown: f.shown}
	}

	// With one line and a cursor to keep, the cursor wins: at that size the
	// choice is between showing the user where they are and showing them a
	// count, and a map answers the first question first.
	if budget == 1 && sel >= 0 && sel < n {
		f.shown = append(f.shown, sel)
		return plan{shown: f.shown, hidden: n - 1}
	}

	// One line goes to the fold line itself; it is the price of honesty about
	// what is not on screen.
	room := budget - 1
	f.rank(rows, n, sel, live)
	for _, i := range f.order {
		switch {
		case heights[i] <= room:
			f.shown = append(f.shown, i)
			room -= heights[i]
		case i == sel:
			// The cursor is shown even when it does not fit: a rail that hid
			// the row the user is standing on would be a map lying about where
			// they are. The row is drawn clipped instead, and the height limit
			// in the renderer is what makes the clip safe.
			f.shown = append(f.shown, i)
			room = 0
		}
		if room == 0 {
			break
		}
	}
	sortInts(f.shown)
	return f.line(rows, f.shown, n-len(f.shown), live)
}

// rank fills f.order with row indices in slot-claiming order.
//
// It is an insertion sort over a reused slice: n is a terminal's worth of rows,
// insertion sort is allocation-free, and it is stable — which is what keeps the
// tie-break below meaning what it says.
func (f *folder) rank(rows []Row, n, sel int, live bool) {
	f.order = f.order[:0]
	if cap(f.order) < n {
		f.order = make([]int, 0, n)
	}
	for i := 0; i < n; i++ {
		f.order = append(f.order, i)
	}
	broken, running := rankFirst, rankSecond
	if live {
		broken, running = rankSecond, rankFirst
	}
	key := func(i int) int {
		if i == sel {
			return rankSelected
		}
		switch rows[i].Attention() {
		case AttnQuestion:
			return rankQuestion
		case AttnFailed, AttnCancelled:
			return broken
		case AttnWorking, AttnWaitsOn:
			return running
		}
		return rankRest
	}
	for i := 1; i < len(f.order); i++ {
		v := f.order[i]
		kv := key(v)
		j := i - 1
		for j >= 0 && worseThan(key(f.order[j]), f.order[j], kv, v, live) {
			f.order[j+1] = f.order[j]
			j--
		}
		f.order[j+1] = v
	}
}

// worseThan reports whether row a should yield its slot to row b. Ranks decide
// it; position breaks the tie, and the tie-break is the other half of 8.1.7 —
// a live list keeps the tail (the live edge), a finalized one keeps the head
// (where the reader starts).
func worseThan(ka, ia, kb, ib int, live bool) bool {
	if ka != kb {
		return ka > kb
	}
	if live {
		return ia < ib
	}
	return ia > ib
}

// line builds the fold line for whatever was left out.
func (f *folder) line(rows []Row, shown []int, hidden int, live bool) plan {
	if hidden <= 0 {
		return plan{shown: shown}
	}
	if f.counts == nil {
		f.counts = make(map[blocks.ItemState]int, 6)
	}
	var counts [6]int
	k := 0
	for i := range rows {
		if k < len(shown) && shown[k] == i {
			k++
			continue
		}
		if s := rows[i].itemState(); int(s) < len(counts) {
			counts[s]++
		}
	}
	if !f.built || counts != f.last {
		for key := range f.counts {
			delete(f.counts, key)
		}
		for s, c := range counts {
			if c > 0 {
				f.counts[blocks.ItemState(s)] = c
			}
		}
		f.fold = blocks.FoldLine(hidden, f.counts)
		f.last = counts
		f.built = true
	}
	return plan{shown: shown, hidden: hidden, fold: f.fold, atTop: live}
}

// sortInts is an insertion sort on an already-nearly-sorted, terminal-sized
// slice. sort.Ints would allocate an interface and reach for reflection to do
// less work than this.
func sortInts(v []int) {
	for i := 1; i < len(v); i++ {
		x := v[i]
		j := i - 1
		for j >= 0 && v[j] > x {
			v[j+1] = v[j]
			j--
		}
		v[j+1] = x
	}
}
