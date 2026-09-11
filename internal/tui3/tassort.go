package tui3

import (
	"strings"
	"time"
)

// tassort.go is WHAT THE LIST IS ORDERED BY, AND IT IS ALSO THE COLUMN THE LIST
// DRAWS. The two are one fact: THE SORT KEY IS THE COLUMN YOU SEE.
//
// The table has exactly two fact columns and both are always filled. `state` is
// the one word every row has. The second is the sort key — age by default, and
// cost or files only while the list is sorted by one of them — so a blank cell
// is now a TRUE ANSWER (this row cost nothing anybody recorded) rather than a
// missing one. Before it, the row's tail offered six facts and most rows could
// say two of them, so the right edge was holes, and a blank meant either
// "nothing to say" or "the frame ran out" with nothing on screen telling the two
// apart.
//
// SORTING IS INSIDE EACH LEVEL OF THE TREE AND THE TREE NEVER FLATTENS.
// Conversations order by their aggregate inside their section, the work under a
// conversation orders by its own value, and a family's children order among
// themselves. [tasksTreeOf] applies ONE comparison at each level it builds, so
// there is one answer to "which of these two rows comes first" wherever the
// question is asked.

// tasksSortKey is one of the five things this list can be ordered by.
type tasksSortKey uint8

const (
	// tasksByAge is the default, and it is the reason this page exists: a record
	// is read newest first.
	tasksByAge tasksSortKey = iota
	tasksByName
	tasksByState
	tasksByFiles
	tasksByCost
	tasksSortKeyCount
)

// word is the label the control row draws, the word the foot names and the word
// the manual quotes. ONE SPELLING PER KEY.
func (k tasksSortKey) word() string {
	switch k {
	case tasksByName:
		return "name"
	case tasksByState:
		return "state"
	case tasksByFiles:
		return "files"
	case tasksByCost:
		return "cost"
	}
	return "age"
}

// next is the cycle the sort key walks, and it wraps.
func (k tasksSortKey) next() tasksSortKey { return (k + 1) % tasksSortKeyCount }

// column is WHICH KEY'S VALUE THE SECOND COLUMN SHOWS.
//
// Three of the five have a value a cell can hold. The other two order the list
// by something a person can already see — the row's own name, and the `state`
// column standing beside this one — so putting either in the second column would
// be one fact in two places. The column falls back to the AGE, which is what a
// record is read by when nothing else is asked for, and which is why every
// section on this page is named by time.
func (k tasksSortKey) column() tasksSortKey {
	switch k {
	case tasksByFiles, tasksByCost:
		return k
	}
	return tasksByAge
}

// cells is how wide that column has to be to say its key's longest answer WHOLE.
//
// IT IS SIZED BY WHAT IT HOLDS AND NOT BY ONE NUMBER THAT SUITS THREE OF THE
// FIVE. A figure with its end cut off is a wrong number, which is the one thing
// rowfit.go's law forbids anywhere on this surface: the widest money this page
// draws needs six cells and `12 files` needs eight, so a single width would
// either cut a price or leave four cells of air down every frame sorted by age.
func (k tasksSortKey) cells() int {
	switch k.column() {
	case tasksByFiles:
		return len("12 files")
	case tasksByCost:
		return len("$60.11")
	}
	return len("11h")
}

// tasksRank is ONE ROW'S ANSWER TO EVERY SORT KEY AT ONCE, gathered once while
// the tree is built.
//
// IT IS ONE STRUCT AND NOT FIVE ACCESSORS for two reasons. The comparison is
// asked O(n log n) times at every level, and each field is a walk or a format
// away from the record. And a CONVERSATION's answer is an AGGREGATE of the work
// under it ([tasksRank.fold]) rather than a field of anything — so a group and a
// row can only be compared at all through one shape.
type tasksRank struct {
	// at is when this row IS, for an age to be drawn from, and the zero time
	// wherever nobody knows one ([tasksEntryStamp] holds that judgement and says
	// why it is not the stamp the time window sorts by).
	at   time.Time
	name string
	// state is the row's urgency as this page files it, which is THE SAME ANSWER
	// that decides which section it stands under ([tasksSectionOf]). Sorting by
	// state off a second table would be two orders for one word.
	state tasksSection
	files int
	cost  float64
}

// tasksRankOf is one piece of work's answer.
func tasksRankOf(item tasksItem, now time.Time) tasksRank {
	return tasksRank{
		at:    tasksEntryStamp(item, now),
		name:  tasksLabel(item.entry),
		state: item.section,
		files: item.entry.FilesChanged,
		cost:  item.entry.Cost,
	}
}

// tasksRankZero is the empty aggregate a fold starts from: nothing known about
// any key, and the LEAST urgent state, so the first row folded in decides it.
func tasksRankZero() tasksRank { return tasksRank{state: tasksSectionCount} }

// fold folds one row's answer into a group's: the newest age, the total spend,
// the total files, and the most urgent state.
//
// THE AGGREGATE IS THE SAME QUESTION ASKED OF A GROUP. A person sorting by cost
// wants to know which CONVERSATION cost the most; a root showing one child's
// figure would be answering a question nobody asked. It is also what a shut fold
// must say, because a shut fold is standing in for everything behind it.
func (r tasksRank) fold(kid tasksRank) tasksRank {
	if kid.at.After(r.at) {
		r.at = kid.at
	}
	if r.name == "" {
		r.name = kid.name
	}
	if kid.state < r.state {
		r.state = kid.state
	}
	r.files += kid.files
	r.cost += kid.cost
	return r
}

// known reports whether this row has anything to say about one key. A row that
// has nothing draws NOTHING in the column — the emptiness law — and sinks to the
// bottom of its group whichever way the column is pointing.
func (k tasksSortKey) known(r tasksRank) bool {
	switch k {
	case tasksByName:
		return strings.TrimSpace(r.name) != ""
	case tasksByState:
		// EVERY ROW HAS A STATE, which is the whole reason it is the first of the
		// two columns and the reason this arm cannot answer false.
		return r.state < tasksSectionCount
	case tasksByFiles:
		return r.files > 0
	case tasksByCost:
		return r.cost > 0
	}
	return !r.at.IsZero()
}

// less is the ONE comparison this page sorts by, at every level of the tree.
func (k tasksSortKey) less(a, b tasksRank, back bool) bool {
	ak, bk := k.known(a), k.known(b)
	if ak != bk {
		// THE EMPTINESS LAW'S OWN ANSWER SINKS, WHICHEVER WAY THE COLUMN POINTS.
		// A blank cell is a row with nothing to say about this key, and reversing
		// the order must never promote silence to the top of the page.
		return ak
	}
	if ak {
		switch {
		case k.ahead(a, b):
			return !back
		case k.ahead(b, a):
			return back
		}
	}
	// A TIE IN THE KEY IS BROKEN BY THIS PAGE'S OWN DEFAULT ORDER AND THEN BY THE
	// NAME, so one reading drawn twice is the same list twice. A sort that left
	// ties to the order rows happened to arrive in would reshuffle under somebody
	// reading it every time a node landed.
	if !a.at.Equal(b.at) {
		return a.at.After(b.at)
	}
	return strings.ToLower(a.name) < strings.ToLower(b.name)
}

// ahead is the key's own order with no direction applied: which of the two a
// person who asked for this column would expect to read first.
func (k tasksSortKey) ahead(a, b tasksRank) bool {
	switch k {
	case tasksByName:
		return strings.ToLower(a.name) < strings.ToLower(b.name)
	case tasksByState:
		// MOST URGENT FIRST, which is the order the sections themselves are in
		// ([tasksSectionOrder]) rather than a second ranking of the same words.
		return a.state < b.state
	case tasksByFiles:
		return a.files > b.files
	case tasksByCost:
		return a.cost > b.cost
	}
	// NEWEST FIRST. Every section here is named by time, and the age is what a
	// person scans down.
	return a.at.After(b.at)
}

// tasksKeyField is the second column's cell for one row, in the spellings the
// fitter chooses between ([rowSay]).
//
// IT IS THE COLUMN'S VALUE AND NEVER THE KEY'S. Sorting by name or by state puts
// the AGE in this cell ([tasksSortKey.column] says why), so the cell is asked of
// the column rather than of the key somebody pressed.
func tasksKeyField(k tasksSortKey, r tasksRank, now time.Time) rowField {
	switch k.column() {
	case tasksByFiles:
		if r.files <= 0 {
			return rowSay()
		}
		return rowSay(itoa(r.files)+plural(" file", r.files), itoa(r.files))
	case tasksByCost:
		if r.cost <= 0 {
			return rowSay()
		}
		return rowSay(dollars(r.cost))
	}
	return rowSay(sinceAt(r.at, now))
}

// tasksKeyInk is the hue that cell is said in. Money is the only fact on this
// page with an ink of its own and it keeps it here, because the ink is part of
// the fact (placeprose.go's [placeMoneyInk]).
func tasksKeyInk(k tasksSortKey, lit bool, pal palette) func(string) string {
	if k.column() == tasksByCost {
		return placeMoneyInk(pal)
	}
	return placeFactInk(lit, pal)
}
