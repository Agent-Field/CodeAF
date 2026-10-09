package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
)

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
	// factoryTitleMinW is the narrowest a title column is made while the
	// facts beside it still hold more than the state fact: THE TITLE IS THE
	// ROW'S SUBJECT, so a narrow floor drops whole fact columns, the rightmost
	// first, before it cuts a title under this ([app.factoryGridAt]).
	factoryTitleMinW = 28
	// factoryStateMinW is the narrowest the state fact's column is kept
	// once it is the only fact left: under it the state fact drops as a
	// column too, because `shippe…` says less than the row's lead mark, and
	// the peek and the item page say the state whole.
	factoryStateMinW = 12
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
	// factoryStripCellMax the widest one cell is made: every cell of one
	// strip shares the width of its longest name up to this, and a longer
	// name is cut to it with an ellipsis ([factoryStripCellW]).
	factoryStripGap     = 2
	factoryStripCellMax = 16
	// factoryProseW is the peek's measure: sixty cells is a line read without
	// losing the start of the next, and a wider peek spends the rest as air.
	factoryProseW = 60
	// factoryPageProseW is the item page's measure for the whole issue.
	factoryPageProseW = 72
	// factoryRailW is the item page's stage rail, the rule beside it not
	// included.
	factoryRailW = 20
	// factoryStageRoundsW is a stage row's rounds on the item page's rail,
	// `×2` before the stage runs and `1/2` once it has, right-aligned so the
	// numbers stand under one another, and factoryStageGlyphW the one glyph
	// after it: `+` for a stage someone other than the recipe added or
	// changed, `?` for one that asks you. A COLUMN NO STAGE HAS ANYTHING IN
	// IS NOT DRAWN, and each stands one cell of air past what comes before.
	factoryStageRoundsW = 3
	factoryStageGlyphW  = 1
	// factoryRuleW is the divider's rule, and factoryDividerW the divider's
	// two cells: one of air beside the rows, then the rule.
	factoryRuleW    = 1
	factoryDividerW = 2
	// factoryHeadHours is the handover sparkline's cells, one an hour.
	factoryHeadHours = 24
	// factoryStampW is a log line's time at the margin, `12:04` and its
	// space, so every line's mark stands in one column whether it carries a
	// time or not.
	factoryStampW = 6
	// factoryMediumW is a proof row's medium chip at the right, wide enough
	// for `screenshot` and `transcript`, so the evidence beside it ends in
	// one column on every row of the sheet.
	factoryMediumW = 10
)

// THE REPO PICKER'S GRID (`R`, factory_settings.go). A row is its mark, the
// repository's full name, and the facts that choose, each in its own
// right-aligned column so a number stands under a number on every row:
//
//	│mark│owner/name …                    │gg│  open│gg│private│gg│              here│gg│     pushed│
//	   4                                     2     9    2     7    2                18   2        11
//
// A COLUMN NO VISIBLE ROW HAS ANYTHING IN IS NOT DRAWN, and while the name
// would be narrower than [factoryPickNameMinW] the columns drop in
// [factoryPickDrop]'s order, so every row drops the same fact at one width.
const (
	// factoryPickMarkW is the `[x]` or `[ ]` mark and its space.
	factoryPickMarkW = 4
	// factoryPickNameMinW is the narrowest the name is made before a fact
	// column gives way: THE NAME IS WHAT A PERSON IS LOOKING FOR.
	factoryPickNameMinW = 24
	// factoryPickOpenW is `12 open`, as wide as `999k open`.
	factoryPickOpenW = 9
	// factoryPickPrivateW is `private`.
	factoryPickPrivateW = 7
	// factoryPickHereW is `here`, checked out on this machine, as wide as
	// `clone on first run`, which a seam with a Clone door says instead.
	factoryPickHereW = 18
	// factoryPickPushedW is `pushed 3m`, as wide as `pushed 11mo`.
	factoryPickPushedW = 11
)

// THE VERTICAL RHYTHM.
const (
	// factoryBlockGap is the blank rows between two blocks, and above an
	// action line.
	factoryBlockGap = 1
	// factoryActionRows is an action line and the one blank row above it.
	factoryActionRows = factoryBlockGap + 1
)

// THE `?` SHEET'S COLUMNS (factory_keysheet.go): the key column, the widest
// a group's column is drawn, and the air between two group columns.
const (
	factorySheetKeyW   = 8
	factorySheetColW   = 34
	factorySheetColGap = 2
)

// factoryItemColW is the item page's left column, its margin included: room
// for a nested step's mark, a one-word name, its loop and `until` in one
// line (owner's layout, 2026-10-09).
const factoryItemColW = 30

// THE ITEM PAGE'S LEFT COLUMN IS THE ISSUE'S MAP (factory_item.go): one row
// per facet, the steps nested under `steps` by [factoryNestW].
// THE SETTINGS FACET is a table of three knobs and then one line per stage:
//
//	│label      │value       │key
//	│ask me at  │plan        │t
//	│1 plan     │on          │
//	      11          14
//
// The label column holds `ask me at` and its air, or a stage's number and
// name when a stage name is longer ([app.factorySettingsPane] widens it for
// every line at once, so the values start at one cell); the value column
// holds `pull request` whole and two cells of air before the key.
const (
	factoryNestW     = 2
	factorySetLabelW = 11
	factorySetValueW = 14
	factorySetNumW   = 2
)

// THE FRAME'S OWN LINES, which the place draws and the floor writes into.
const (
	// factoryHintInset is the cells the place's note and hint lines leave:
	// one at each edge.
	factoryHintInset = 2
)

// THE RIGHT MARGIN. EVERY REGION STOPS [factoryMargin] BEFORE THE FRAME'S
// RIGHT EDGE AT EVERY WIDTH (owner ruling, 2026-10-08, after the floor ended
// five cells short at 160 and flush with the edge at 120 and 100): the rows
// when they are the whole width, the peek, and the item page. The handover's
// hairline is the one thing drawn to the edge, because it is a rule and not
// words. factoryMargins is a region's two margins together.
const factoryMargins = factoryMargin * 2

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

// factoryGrid is one row's columns at a width: the ref's cells, the title's,
// the facts' (0 when the row carries none), and factsN, how many fact columns
// every row carries: 0 for as many as it has, -1 for none.
type factoryGrid struct {
	refW, titleW, factsW, factsN int
	columns                      bool
}

// factoryGridMemo is the last grid measured, and what it was measured for:
// the grid reads every item on the floor, and every row of a frame asks for
// it, so a frame measures it once ([app.factoryGridAt]).
type factoryGridMemo struct {
	width, items, refW int
	columns, comfy     bool
	grid               factoryGrid
	ok                 bool
}

// factoryGridAt lays the grid out for a row of width cells. Without columns
// the title takes everything after the ref. With them, THE TITLE AND THE FACTS
// SHARE WHAT THE FIXED COLUMNS LEAVE IN THIS ORDER OF CLAIM (owner ruling,
// 2026-10-08, after a floor at 120 cut every title to 18 cells while the
// facts kept their width):
//
//  1. the title, up to [factoryTitleMinW] or the floor's longest title when
//     that is shorter;
//  2. the facts, as many WHOLE COLUMNS as fit beside it, the rightmost
//     dropped first, so every row drops the same fact at the same width;
//  3. the title again, up to the floor's longest title;
//  4. the facts again, which may then carry one more column.
//
// When not even the state fact fits whole beside the title's floor, the title
// keeps its floor and the state fact is cut with an ellipsis in what is left,
// down to [factoryStateMinW]; under that the row carries no facts at all and
// the title takes the column. The comfortable density wraps its second title
// line at the same width.
func (a *app) factoryGridAt(width int) factoryGrid {
	m := &a.fp.gridMemo
	if m.ok && m.width == width && m.items == len(a.fp.snap.Items) && m.refW == a.fp.refW && m.columns == a.fp.columns && m.comfy == a.fp.comfy {
		return m.grid
	}
	g := a.factoryGridMeasure(width)
	*m = factoryGridMemo{width: width, items: len(a.fp.snap.Items), refW: a.fp.refW, columns: a.fp.columns, comfy: a.fp.comfy, grid: g, ok: true}
	return g
}

// factoryGridForget drops the measured grid, so the next row measures the
// floor it is drawn on: every body draw and every fold asks it.
func (a *app) factoryGridForget() { a.fp.gridMemo = factoryGridMemo{} }

func (a *app) factoryGridMeasure(width int) factoryGrid {
	g := factoryGrid{refW: max(a.fp.refW, factoryRefW), columns: a.fp.columns}
	head := factoryLeadW + factoryPriorityW + g.refW + 1
	if !g.columns {
		g.titleW = max(width-head, 0)
		return g
	}
	rest := max(width-head-factoryGutter-factoryRepoW-factoryGutter-factoryGutter-factoryAgeW, 0)
	need, longest := a.factoryFactsNeed()
	if len(need) == 0 {
		g.titleW = rest
		return g
	}
	want := min(min(longest, factoryTitleMinW), rest)
	cols := func(room int) int {
		n := 0
		for n < len(need) && need[n] <= room {
			n++
		}
		return n
	}
	n := cols(rest - want)
	factsW := 0
	switch {
	case n > 0:
		factsW = need[n-1]
	case rest-want >= factoryStateMinW:
		factsW = rest - want
	default:
		// NO FACT COLUMN AT ALL; a question's `[y/n]` still keeps its
		// cells before the age.
		keep := 0
		for _, it := range a.fp.snap.Items {
			if factoryOnFloor(it) && it.State == factory.StateNeedsYou {
				keep = ansi.StringWidth(factoryAnswerWord) + factoryGutter
				break
			}
		}
		g.titleW, g.factsW, g.factsN = rest-keep, keep, -1
		return g
	}
	g.titleW = min(rest-factsW, max(longest, want))
	g.factsW = rest - g.titleW
	g.factsN = max(cols(g.factsW), 1)
	// THE AIR A COLUMN THAT DID NOT FIT LEFT BEHIND GOES BACK TO THE TITLE,
	// so the facts end where their last whole column does.
	if g.factsN <= len(need) && need[g.factsN-1] <= g.factsW {
		g.titleW = min(rest-need[g.factsN-1], max(longest, want))
		g.factsW = rest - g.titleW
	}
	return g
}

// factoryFactsNeed is, for each count k of fact columns, the cells the widest
// row's first k facts take, never narrower for more columns; and the floor's
// longest title. A question is left out of the facts: it is a sentence, cut to
// whatever the column holds, and the peek and the item page say it whole.
func (a *app) factoryFactsNeed() ([]int, int) {
	var need []int
	longest := 0
	sepW := ansi.StringWidth(rowSep)
	for _, it := range a.fp.snap.Items {
		if !factoryOnFloor(it) {
			continue
		}
		longest = max(longest, ansi.StringWidth(it.Title))
		if it.State == factory.StateNeedsYou {
			continue
		}
		w := 0
		for k, p := range a.factoryRowFacts(it) {
			if k > 0 {
				w += sepW
			}
			w += ansi.StringWidth(p.plain)
			if k >= len(need) {
				need = append(need, 0)
			}
			need[k] = max(need[k], w)
		}
	}
	// THE FLIGHT FACT'S CELLS ARE RESERVED, not raised while a read is out
	// ([app.factoryFlightKeep]), so the grid does not move every time an
	// arrival is read: the second column is never narrower than the widest
	// at-rest state fact with `· refreshing…` behind it.
	if keep := a.factoryFlightKeep(); keep > 0 && len(need) > 0 {
		if len(need) == 1 {
			need = append(need, 0)
		}
		need[1] = max(need[1], keep)
	}
	for k := 1; k < len(need); k++ {
		need[k] = max(need[k], need[k-1])
	}
	return need, longest
}
