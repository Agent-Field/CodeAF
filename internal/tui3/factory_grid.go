package tui3

import "strings"

// ── THE FLOOR'S GRID ────────────────────────────────────────────────────────
//
// EVERY HORIZONTAL POSITION ON THE FACTORY FLOOR, THE PEEK AND THE ITEM PAGE
// COMES FROM A NAME IN THIS FILE (owner ruling, 2026-10-08: padding,
// positioning and alignment are deliberate, never incidental). A width spelled
// as a number anywhere else in factory_*.go is a column nobody chose, and
// factory_align_test.go fails the build on one, walking the sources with
// go/ast; 0 and 1 stay free for index arithmetic.
//
// The floor is three regions, each standing [factoryMargin] in from its own
// left edge:
//
//	│mm│lead│pr│   ref│ title …                 │gg│repo       │gg│facts …  │gg│ age│
//	  2   2   2     6                              2     11        2           2    4
//
// The rows, then the divider's two cells (one of air, then the rule), then the
// peek, whose prose wraps at [factoryProseW]; the item page's issue wraps at
// [factoryPageProseW] beside its stage rail of [factoryRailW]. Vertically, one
// blank row ([factoryBlockGap]) stands between every two blocks and nowhere
// else, and an action line has exactly that one blank above it.
const (
	// factoryMargin is every region's left margin: the handover, the rows,
	// the peek, the item page, the recipe page.
	factoryMargin = 2
	// factoryLeadW is a row's state mark and its space.
	factoryLeadW = 2
	// factoryPriorityW is the priority column's one cell and its space.
	factoryPriorityW = 2
	// factoryRefW is the ref, `#` included, right-aligned so the numbers line
	// up by digit; wider when the floor holds a longer ref.
	factoryRefW = 6
	// factoryGutter is the air between two columns, and the least air
	// between a row's words and its right-aligned meta.
	factoryGutter = 2
	// factoryRepoW is the repo's short name.
	factoryRepoW = 11
	// factoryAgeW is the age, right-aligned at the row's end.
	factoryAgeW = 4
	// factoryTitleMin is the narrowest a title column is ever made.
	factoryTitleMin = 24
	// factoryTitleFloor is how far a title column narrows on a tight floor so
	// the widest state fact stands whole beside it ([app.factoryGridAt]).
	factoryTitleFloor = 18
	// factoryFactsKeep is the fewest cells the facts give up to a long title
	// ([app.factoryGridAt]), and never less than the widest state fact.
	factoryFactsKeep = 22
	// factoryFactGap is the air between two facts, and between two chips, on
	// one row: wide enough that three short phrases read as three things with
	// no separator mark between them.
	factoryFactGap = 6
	// factoryLabelGap is the air between a chip's label and its value.
	factoryLabelGap = 2
	// factoryChipValueW is a chip's value cell, so the next chip's label
	// starts at one cell on every item whatever this one says, and
	// factoryChipGap the air after it, before the next chip's label.
	factoryChipValueW = 6
	factoryChipGap    = 2
	// factoryStripGap is the air between two cells of the stage strip, and
	// factoryStripClass the step a cell's width is rounded up to, so a strip
	// of short and long stage names is regular rather than ragged.
	factoryStripGap   = 2
	factoryStripClass = 6
	// factoryProseW is the peek's measure: sixty cells is a line read without
	// losing the start of the next, and a wider peek spends the rest as air.
	factoryProseW = 60
	// factoryPageProseW is the item page's measure for the whole issue.
	factoryPageProseW = 72
	// factoryRailW is the item page's stage rail, the rule beside it not
	// included.
	factoryRailW = 20
	// factoryRuleW is the divider's rule, and factoryDividerW the divider's
	// two cells: one of air beside the rows, then the rule.
	factoryRuleW    = 1
	factoryDividerW = 2
	// factoryHeadHours is the handover sparkline's cells, one an hour.
	factoryHeadHours = 24
)

// THE VERTICAL RHYTHM.
const (
	// factoryBlockGap is the blank rows between two blocks, and above an
	// action line.
	factoryBlockGap = 1
	// factoryActionRows is an action line and the one blank row above it.
	factoryActionRows = factoryBlockGap + 1
)

// THE FRAME'S OWN LINES, which the place draws and the floor writes into.
const (
	// factoryHintInset is the cells the place's note and hint lines leave:
	// one at each edge.
	factoryHintInset = 2
	// factoryTitleShare is the title's share of what a row's fixed columns
	// leave, as a divisor: the title takes half, rounded up.
	factoryTitleShare = 2
)

// THE GEOMETRIES: the widths at which the floor changes shape.
const (
	// factoryPaneFloor is the narrowest terminal that draws the peek.
	factoryPaneFloor = 120
	// factoryFactsFloor is the narrowest whose rows carry repo, facts, age.
	factoryFactsFloor = 90
	// factoryRowsShare is the rows' share of the width in percent until the
	// divider is moved, factoryRowsMin the fewest columns they keep, and
	// factoryPeekMin the fewest the peek keeps.
	factoryRowsShare = 58
	factoryRowsMin   = 70
	factoryPeekMin   = 40
	// factorySplitStep is how far `{` and `}` move the divider.
	factorySplitStep = 4
	// factoryStageFloor is the narrowest that draws the item page's rail
	// beside its pane; under it the rail is one line above.
	factoryStageFloor = 72
	// factoryHeadSparkFloor is the narrowest handover row that still draws
	// the day's sparkline.
	factoryHeadSparkFloor = 60
	// factoryLedMin is the narrowest a hanging-indent paragraph is wrapped.
	factoryLedMin = 4
)

// factorySpaces is n cells of air. IT IS THE ONE WAY A FACTORY FILE SPELLS
// AIR, so every run of spaces on the floor is a named width.
func factorySpaces(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}

// factoryMarginPad is a region's left margin.
func factoryMarginPad() string { return factorySpaces(factoryMargin) }
