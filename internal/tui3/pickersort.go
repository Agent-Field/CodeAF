package tui3

import (
	"sort"
	"strings"
)

// ── SORTING A TABLE ─────────────────────────────────────────────────────────
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
// way to ask for it, and that two words are two opinions about seven columns. A
// key that walks the columns you can see is the discoverable form of the same
// request, and it reaches every one of them.
//
// IT IS ONE ENGINE FOR BOTH TABLES, which is [colTable]'s own rule said about
// order instead of about width: the model list and the providers inside a fold are
// two column sets drawn by one thing, so they are two column sets ORDERED by one
// thing. Which column can be sorted, and which way it is read first, are written
// on the column itself ([tableColumn.sorts] and `up`) rather than in a second list
// here that would have to be kept in step with the first.

// tableSort is which column a table is ordered by and which way round.
//
// THE ZERO VALUE IS THE NAME COLUMN, ASCENDING, which is the first column of
// every table on this surface and the order a list of names is expected in. That
// is why `at` counts from one: a picker that has never been sorted is already
// sorted, so nothing has to remember to initialise this.
type tableSort struct {
	// at is 0 for the name column, and a column of the set is its index plus one.
	at int
	// back is the column read the other way round.
	back bool
}

// tableSortName is what [tableSort.column] answers for the name column, whose
// head is [colTableFit.name0] rather than one of the set's.
const tableSortName = -1

// column is the index into the column set this sort orders, and [tableSortName]
// for the name.
func (s tableSort) column() int { return s.at - 1 }

// arrow is the character the sorted column's head wears.
//
// IT IS ALWAYS ONE OR THE OTHER, because a table on this surface is always in some
// order and there is no such thing as an unsorted one. A heading with no arrow
// anywhere would leave the order a thing a person has to infer from the rows.
func (s tableSort) arrow() string {
	if s.back {
		return tasksSortUp
	}
	return tasksSortDown
}

// step walks to the next rung, or the previous one.
//
// ── EVERY COLUMN IS TWO RUNGS ───────────────────────────────────────────────
//
// The cycle is each column read its natural way and then turned round, before
// moving on: `model ↓`, `model ↑`, `in/M ↓`, `in/M ↑`, and so on back round to the
// name. So one key reaches every order this table has, and the useful direction of
// each column — cheapest, quickest, biggest, highest, A to Z
// ([tableColumn.up]) — is always the first of the pair, because that is the
// question people bring to that column.
//
// `can` is asked of each column before it is landed on, and it is how a column
// this list published nothing in is stepped over ([picker.sortableColumn]).
func (s tableSort) step(cols []tableColumn, back bool, can func(int) bool) tableSort {
	if back {
		// Backwards: the same column's other direction first, then the column
		// before it, arrived at turned round — so walking back retraces exactly
		// the rungs walking forward visited.
		if s.back {
			return tableSort{at: s.at}
		}
		return tableSort{at: sortStepColumn(cols, s.at, -1, can), back: true}
	}
	if !s.back {
		return tableSort{at: s.at, back: true}
	}
	return tableSort{at: sortStepColumn(cols, s.at, 1, can)}
}

// sortStepColumn is the next or previous column that can be sorted, wrapping
// through the name column — which is always one of them, because there is no such
// thing as a row without a name.
func sortStepColumn(cols []tableColumn, at, by int, can func(int) bool) int {
	// WALKING BACK OFF THE NAME COLUMN IS THE ONLY WRAP GOING THAT WAY, and it
	// lands on the LAST sortable column — where walking forward came from. Every
	// other step back stops at the name, which is the cycle's own start: reaching
	// zero while stepping down used to wrap here too, so walking back past the
	// first column jumped to the last one and the two directions stopped being
	// each other's opposite.
	if by < 0 && at == 0 {
		return sortEdgeColumn(cols, can)
	}
	for range len(cols) + 1 {
		at += by
		if at <= 0 || at > len(cols) {
			return 0
		}
		if col := cols[at-1]; col.sorts && can(at-1) {
			return at
		}
	}
	return 0
}

// sortEdgeColumn is the last column of the set that can be sorted.
func sortEdgeColumn(cols []tableColumn, can func(int) bool) int {
	for at := len(cols); at > 0; at-- {
		if col := cols[at-1]; col.sorts && can(at-1) {
			return at
		}
	}
	return 0
}

// ── THE ORDERING RULE ───────────────────────────────────────────────────────

// tableAheadBy is whether one figure sorts before another.
//
// WHAT NOBODY PUBLISHED SORTS LAST, IN BOTH DIRECTIONS. This is the emptiness
// law's own shape said about order instead of about drawing: a model with no
// published price is not the cheapest model, and turning the column round must not
// make it the most expensive one either — it is simply not in the comparison. The
// old `cheap` filter word needed the same rule and got it by calling an unknown
// price positive infinity, which was right in one direction only.
func tableAheadBy(back, up bool, first, second float64) bool {
	if (first == 0) != (second == 0) {
		return second == 0
	}
	if first == second {
		return false
	}
	if back {
		up = !up
	}
	if up {
		return first < second
	}
	return first > second
}

// tableAheadName is the same for the name column, where there is no such thing as
// an unpublished value.
func tableAheadName(back bool, first, second string) bool {
	if back {
		return first > second
	}
	return first < second
}

// ── THE MODEL LIST ──────────────────────────────────────────────────────────

// pickerRank is one model's sortable values, read ONCE per sort rather than once
// per comparison.
//
// THE FIGURES ARE THE NUMBERS AND NEVER THE CELLS. The table's cells are
// formatted strings — `$1.3`, `$10`, `262k`, `1M` — and sorting those as text puts
// `$10` under `$1.3` and a million tokens under two hundred thousand. The cell is
// for reading; this is for ordering, and they are different jobs on the same fact.
//
// ZERO IS "NOBODY PUBLISHED IT" in every field, which is the catalog's own
// convention (models.go) and the ledger's, and [tableAheadBy] is what makes that
// safe to sort on.
type pickerRank struct {
	name string
	// group is the row's service in the order those services are held
	// ([Model.GroupOrder]), and it is the OUTER key of every sort — see the
	// comparator in [picker.sortHits] for why a column may not reorder services.
	group int
	// score is how well this row matched what was typed ([fuzzy.Score], HIGHER
	// is better — the one convention the whole repo's matcher keeps) and is
	// meaningless with an empty box, where every row scores zero.
	score  int
	via    string
	first  float64
	in     float64
	out    float64
	window float64
	rate   float64
	elo    float64
}

// pickerRankOf reads one model's sortable values.
//
// THE MEASURED THREE COME FROM THE ROW'S OWN READING OF THE LEDGER
// ([modelLaneReading]) AND NOT FROM [bestLane]. Those are different questions:
// `bestLane` is the quickest-feeling lane the ledger holds, while the row draws the
// lane it NAMES — the pin, or the chooser's answer, or nothing at all under a
// routing row where codeaf does not choose. Asking the wrong one handed rows with a
// blank `via`, `first` and `t/s` real numbers to be sorted by, so the blanks did not
// land together and the order was one the screen could not explain.
func pickerRankOf(model Model, pin, routing string) pickerRank {
	rank := pickerRank{
		name:   strings.ToLower(model.ID),
		group:  model.GroupOrder,
		window: float64(model.ContextLength),
		elo:    model.ArenaElo,
	}
	// BOTH HALVES OR NEITHER, the same reading [modelFactsOf] makes of a price:
	// a catalog row that published one side of it draws NO price at all, and a
	// sort that took the half it had ordered the list by a figure that is not on
	// the screen — the row would sit among the cheap ones with a blank cell.
	if model.PromptPrice > 0 && model.CompletionPrice > 0 {
		rank.in, rank.out = model.PromptPrice, model.CompletionPrice
	}
	via, best, known := modelLaneReading(model, pin, routing)
	rank.via = via
	if known {
		rank.first, rank.rate = best.TTFT, best.Rate
	}
	return rank
}

// modelSortsBy is one model's value in the column at `at`, looked up by that
// column's own HEAD so the spelling lives in [modelColumns] and nowhere else.
func modelSortsBy(rank pickerRank, at int) (float64, string) {
	switch modelColumns[at].head {
	case "via":
		return 0, rank.via
	case "first":
		return rank.first, ""
	case "in/M":
		return rank.in, ""
	case "out/M":
		return rank.out, ""
	case "window":
		return rank.window, ""
	case "t/s":
		return rank.rate, ""
	case "elo":
		return rank.elo, ""
	}
	return 0, ""
}

// sortableColumn reports whether this list has anything to order by in a column.
//
// ── A COLUMN NOBODY PUBLISHED IS NOT A RUNG ─────────────────────────────────
//
// This is law 4 said about the cycle instead of about the heading. A catalog with
// no measurements draws no `via`, no `first` and no `t/s` ([colTable.fit] drops
// them), and a rung for a column that is not there is a press that moves no row,
// turns no arrow and changes nothing a person can see — which is
// indistinguishable from the key being broken. It was: on a real catalog with
// nothing measured, a press of the key did exactly nothing.
//
// IT IS ASKED OF THE MEASUREMENT AND NOT OF THE FIT, so the answer is about the
// DATA rather than about the frame. A narrow window that had no room for `elo`
// still shows the score in the ranked tail, and a column the frame dropped is
// still one the list can be ordered by; a column the catalog never filled is not.
func (p *picker) sortableColumn(at int) bool {
	p.measure()
	if p.columns == nil || at < 0 || at >= len(p.columns.wide) {
		// NOTHING MEASURED MEANS NOTHING RULED OUT. Refusing a sort we cannot
		// prove is empty would be worse than allowing one that moves no row.
		return true
	}
	return p.columns.wide[at] > 0
}

// sortHits puts the filter's hits in the order the sort asks for.
//
// IT IS STABLE AND IT SORTS INSIDE EACH SERVICE. Stable so that rows a column
// cannot tell apart — every model with no elo, every model at the same price —
// keep the order they already had, which is the name ranking's and is the only
// order a person has any expectation about. Inside each service because the
// headings are the shape of the page rather than a column of it; the comparator
// says why, and on the ordinary door with one service there is no difference to
// see.
func (p *picker) sortHits(ranked bool) {
	if len(p.hits) < 2 {
		return
	}
	// THE PIN IS THIS CONVERSATION'S ROW ALONE, which is [picker.tableFit]'s own
	// rule said about order instead of about width: only the row in use draws a
	// pinned `via`, so only that row may be ordered by one.
	pin := p.pinnedLane()
	ranks := make(map[int]pickerRank, len(p.hits))
	for _, at := range p.hits {
		held := ""
		if p.all[at].ID == p.current {
			held = pin
		}
		rank := pickerRankOf(p.all[at], held, p.routing)
		if ranked {
			rank.score = p.score[at]
		}
		ranks[at] = rank
	}
	order, col := p.sort, p.sort.column()
	sort.SliceStable(p.hits, func(i, j int) bool {
		first, second := ranks[p.hits[i]], ranks[p.hits[j]]
		// ── A SERVICE IS THE SPINE AND NOT A COLUMN ──────────────────────────
		//
		// The list is drawn under one heading per service, written where the
		// service changes from the row before ([picker.groupBefore]) — so the
		// service order is not a preference this table may express, it is the
		// shape of the page. A sort that ignored it moved a lonely connection
		// above `openrouter` because its one row had no price, and interleaving
		// two services' rows would have drawn the same heading twice.
		//
		// SO EVERY COLUMN ORDERS ROWS INSIDE A SERVICE, never the services, and
		// the arrow on a heading-less list (one service, which is nearly every
		// door) means exactly what it says.
		if first.group != second.group {
			return first.group < second.group
		}
		if col == tableSortName {
			// ── THE NAME COLUMN IS RELEVANCE FIRST WHILE SOMETHING IS TYPED ───
			//
			// The name column's order and the search's order are both orders of
			// the same column, and with a query in the box the search's is what
			// was asked for: `gpt` has to put `gpt-5-classic` above
			// `anthropic/claude-gpt-echo`, which the shared matcher does (a word
			// found whole, early, on a boundary beats letters scattered through a
			// name) and plain alphabetical does not. Sorting by name alone put the
			// fuzzy hit first, which is the search itself going wrong.
			//
			// SO RELEVANCE LEADS AND THE ARROW DECIDES THE TIE — the BIGGER score
			// first, which is [fuzzy.Score]'s direction and the repo's. Inside one
			// band the rows are alphabetical, forwards or back, and with an empty
			// box every row scores zero, so the whole column is plainly
			// alphabetical and the arrow means all of it.
			if first.score != second.score {
				return first.score > second.score
			}
			return tableAheadName(order.back, first.name, second.name)
		}
		leftNum, leftWord := modelSortsBy(first, col)
		rightNum, rightWord := modelSortsBy(second, col)
		if modelColumns[col].words {
			// A COLUMN OF NAMES IS ORDERED AS NAMES, and an empty one is a row that
			// published none — last, the same as an absent figure. It is asked of the
			// COLUMN and not of the two values, because a pair that both published
			// nothing used to fall through and be compared as ZEROES — one rule for
			// the column and a different one for some of its pairs.
			if (leftWord == "") != (rightWord == "") {
				return rightWord == ""
			}
			if leftWord != rightWord {
				return tableAheadName(order.back, leftWord, rightWord)
			}
		} else if leftNum != rightNum {
			return tableAheadBy(order.back, modelColumns[col].up, leftNum, rightNum)
		}
		// ── AND THE NAME BREAKS EVERY TIE ───────────────────────────────────
		//
		// A sparse column leaves a BLOCK of rows it cannot tell apart — every model
		// with no elo, every model nobody has measured — and a stable sort leaves
		// that block in whatever order the rung before it happened to produce. So
		// the same press gave a different arrangement of the same blanks depending
		// on how you had walked to it, which reads as the sort being arbitrary at
		// exactly the place a person is least sure of it. The name is the one order
		// every row has, so the blanks come back alphabetically and the same press
		// draws the same screen twice.
		return first.name < second.name
	})
}

// ── THE PROVIDERS INSIDE A FOLD ─────────────────────────────────────────────

// laneSortsBy is one machine's value in the providers table's column at `at`.
//
// `note` AND `last 8` ARE NOT SORTABLE and say so on the column
// ([tableColumn.sorts]). A note is whichever ONE thing is worth saying about a
// lane past its figures (lanes.go's [laneNote]) and ordering by the text of it
// would rank "bad replies" against "tail 3s" alphabetically, which is an order
// about spelling rather than about anything true; `last 8` is a picture of the
// recent answers and has no single value to compare.
func laneSortsBy(view laneView, at int) float64 {
	switch laneColumns[at].head {
	case "first":
		return view.TTFT
	case "t/s":
		return view.Rate
	case "$/M":
		return view.PriceOut * 1_000_000
	case "up":
		return view.Uptime
	}
	return 0
}

// sortLanes puts one model's machines in the order the fold's own sort asks for.
// It is what the fold opens in, so the default — the name column, ascending — is
// the alphabetical order the machines have always been drawn in.
func (p *picker) sortLanes() {
	if len(p.lanes) < 2 {
		return
	}
	order, col := p.laneSort, p.laneSort.column()
	sort.SliceStable(p.lanes, func(i, j int) bool {
		first, second := p.lanes[i], p.lanes[j]
		if col == tableSortName {
			return tableAheadName(order.back,
				strings.ToLower(first.Name), strings.ToLower(second.Name))
		}
		return tableAheadBy(order.back, laneColumns[col].up,
			laneSortsBy(first, col), laneSortsBy(second, col))
	})
}

// sortableLane is [picker.sortableColumn] for the providers' own table, asked of
// the machines currently in the fold — which is the whole population that table
// will ever draw, so an empty column here is empty for certain.
func (p *picker) sortableLane(at int) bool {
	if at < 0 || at >= len(laneColumns) {
		return false
	}
	for _, view := range p.lanes {
		if cells := laneCells(view); at < len(cells) && cells[at] != "" {
			return true
		}
	}
	return false
}

// ── THE KEY ─────────────────────────────────────────────────────────────────

// sortNext is the sort key pressed, forwards or back.
//
// WHICH TABLE IT ORDERS IS WHICHEVER ONE THE CURSOR IS IN, which is the rule the
// fold's other keys already follow: typing inside an open fold filters the
// machines ([picker.narrowFold]) and `←` there closes the machines rather than the
// model's own block. A key that ordered the models while a person was reading a
// provider table would be the one gesture in this list that ignores where the
// cursor is.
func (p *picker) sortNext(back bool) {
	if p.cursorInMachines() {
		p.laneSort = p.laneSort.step(laneColumns, back, p.sortableLane)
		p.sortLanes()
		p.relist()
		// THE TABLE IS RE-FITTED because the arrow takes cells in the column it
		// lands on ([colTableFit.mark]) — and the providers' fit is cached against
		// the fold it was built for, so the width is forced to disagree.
		p.lanesFitAt = 0
		p.cursorToMachine()
		p.revealFold(pickerRows)
		return
	}
	p.sort = p.sort.step(modelColumns, back, p.sortableColumn)
	p.fitAt = 0
	p.rank()
	// THE CURSOR GOES TO THE TOP BECAUSE THE TOP IS THE ANSWER. Somebody who
	// ordered by price asked which model is cheapest, and that is row one; going
	// back to the model in use — which is what [picker.rank] does for an empty box
	// — shows them their own row's neighbourhood instead, and made turning a
	// column round look like it had done nothing.
	p.cursor, p.top = 0, 0
}

// cursorInMachines reports whether the cursor is standing inside an open provider
// table: on a machine, on the `default` row beside them, or on the `openrouter`
// row that holds them.
func (p *picker) cursorInMachines() bool {
	if !p.machines || p.cursor < 0 || p.cursor >= len(p.list) {
		return false
	}
	row := p.list[p.cursor]
	return row.lane >= 0 || row.lane == laneDefaultAt || row.lane == laneRoutAt
}
