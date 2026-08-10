package blocks

import "strconv"

// ItemState is what a row in a foldable list is doing. It drives both which
// rows survive a fold and what the fold line says about the ones that did not.
type ItemState uint8

const (
	// ItemPending is queued: ○.
	ItemPending ItemState = iota
	// ItemRunning is working: ◐.
	ItemRunning
	// ItemWaiting is blocked on a human or a sibling: ? or ⚑.
	ItemWaiting
	// ItemDone is settled: ✓.
	ItemDone
	// ItemFailed is broken: ✕.
	ItemFailed
	// ItemAborted is cancelled.
	ItemAborted
)

const itemStates = 6

// itemWords is the fold line's vocabulary, in the order counts are reported.
var itemWords = [itemStates]string{
	ItemPending: "pending",
	ItemRunning: "running",
	ItemWaiting: "waiting",
	ItemDone:    "done",
	ItemFailed:  "failed",
	ItemAborted: "aborted",
}

// Plan is a fold decision: which rows to draw, how many were hidden, and the
// one line that accounts for them.
type Plan struct {
	// Shown are indices into the caller's item list, in original order. It
	// aliases the [Folder]'s buffer and is valid until the next fold.
	Shown []int
	// Hidden is how many items the fold line stands for.
	Hidden int
	// Fold is the fold line, e.g. "… 21 more (18 pending · 3 done)". Empty
	// when nothing was hidden.
	Fold string
	// FoldAtTop places the fold line above the shown rows. Live lists fold
	// from the top so the running edge stays at the bottom, where the reader
	// is looking.
	FoldAtTop bool
}

// Folder applies the collapse policies of 8.1.7. It carries its own buffers so
// a LIVE list — which re-folds on every frame — costs no allocation once warm,
// including the fold line itself, which is rebuilt only when the hidden
// breakdown actually changes.
//
// The two policies:
//
//   - [Folder.Live] folds from the TOP. Running rows are at the live edge and
//     the live edge is what the reader came for.
//   - [Folder.Finalized] gives the slots to failed and aborted rows FIRST. A
//     settled list is read to find out what went wrong.
//
// A Folder is not safe for concurrent use.
type Folder struct {
	shown  []int
	counts [itemStates]int
	last   [itemStates]int
	fold   string
	folded bool
}

// Live keeps the last budget rows (minus one for the fold line, if folding),
// so the live edge survives.
func (f *Folder) Live(states []ItemState, budget int) Plan {
	n := len(states)
	f.shown = f.shown[:0]
	if budget <= 0 {
		return f.plan(states, nil, n, true)
	}
	if n <= budget {
		for i := range states {
			f.shown = append(f.shown, i)
		}
		return Plan{Shown: f.shown}
	}
	keep := budget - 1 // the fold line costs a row
	if keep < 0 {
		keep = 0
	}
	from := n - keep
	for i := from; i < n; i++ {
		f.shown = append(f.shown, i)
	}
	return f.plan(states, f.shown, n-keep, true)
}

// Finalized fills the budget with failed and aborted rows first, then the rest
// in original order, and draws the shown rows in original order regardless.
func (f *Folder) Finalized(states []ItemState, budget int) Plan {
	n := len(states)
	f.shown = f.shown[:0]
	if budget <= 0 {
		return f.plan(states, nil, n, false)
	}
	if n <= budget {
		for i := range states {
			f.shown = append(f.shown, i)
		}
		return Plan{Shown: f.shown}
	}
	keep := budget - 1
	if keep < 0 {
		keep = 0
	}
	// Two passes over the list, no sort, no allocation: broken rows claim
	// their slots, then the remainder fills what is left, both in order.
	taken := 0
	for i, s := range states {
		if taken == keep {
			break
		}
		if s == ItemFailed || s == ItemAborted {
			f.shown = append(f.shown, i)
			taken++
		}
	}
	brokenCount := taken
	if taken < keep {
		for i, s := range states {
			if taken == keep {
				break
			}
			if s != ItemFailed && s != ItemAborted {
				f.shown = append(f.shown, i)
				taken++
			}
		}
	}
	// Merge the two ordered runs back into one ascending run in place.
	mergeRuns(f.shown, brokenCount)
	return f.plan(states, f.shown, n-taken, false)
}

// mergeRuns merges shown[:split] and shown[split:] — both ascending — into one
// ascending run. Insertion of the second run into the first is O(n·k) in the
// worst case but both runs are bounded by the row budget of a terminal.
func mergeRuns(shown []int, split int) {
	for i := split; i < len(shown); i++ {
		v := shown[i]
		j := i - 1
		for j >= 0 && shown[j] > v {
			shown[j+1] = shown[j]
			j--
		}
		shown[j+1] = v
	}
}

// plan builds the fold line for whatever the selection left out.
func (f *Folder) plan(states []ItemState, shown []int, hidden int, atTop bool) Plan {
	if hidden <= 0 {
		return Plan{Shown: shown}
	}
	for i := range f.counts {
		f.counts[i] = 0
	}
	if len(shown) == 0 {
		for _, s := range states {
			if int(s) < itemStates {
				f.counts[s]++
			}
		}
	} else {
		// shown is ascending; walk both lists once.
		k := 0
		for i, s := range states {
			if k < len(shown) && shown[k] == i {
				k++
				continue
			}
			if int(s) < itemStates {
				f.counts[s]++
			}
		}
	}
	if !f.folded || f.counts != f.last {
		f.fold = foldLine(hidden, f.counts)
		f.last = f.counts
		f.folded = true
	}
	return Plan{Shown: shown, Hidden: hidden, Fold: f.fold, FoldAtTop: atTop}
}

// FoldLine is the shared fold-line grammar: "… 21 more (18 pending · 3 done)".
// The breakdown is what makes a fold honest — a bare "… 21 more" hides whether
// any of them failed.
func FoldLine(hidden int, counts map[ItemState]int) string {
	var c [itemStates]int
	for state, n := range counts {
		if int(state) < itemStates {
			c[state] = n
		}
	}
	return foldLine(hidden, c)
}

func foldLine(hidden int, counts [itemStates]int) string {
	if hidden <= 0 {
		return ""
	}
	var buf [96]byte
	out := append(buf[:0], "… "...)
	out = strconv.AppendInt(out, int64(hidden), 10)
	out = append(out, " more"...)
	first := true
	for state := 0; state < itemStates; state++ {
		n := counts[state]
		if n == 0 {
			continue
		}
		if first {
			out = append(out, " ("...)
			first = false
		} else {
			out = append(out, " · "...)
		}
		out = strconv.AppendInt(out, int64(n), 10)
		out = append(out, ' ')
		out = append(out, itemWords[state]...)
	}
	if !first {
		out = append(out, ')')
	}
	return string(out)
}
