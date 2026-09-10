package tui3

import (
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// spendPanel is `spend`: what today has cost against the day's allowance, on
// the heading and as a thin bar under it, and the fortnight behind it as one
// row of spark cells with its total and the model most of it went to. Every row
// is a door into the spend place.
type spendPanel struct{ homePanelBase }

// homeSpendDays is the stretch the panel draws, and homeSpendBarCells the
// longest the day's bar is drawn.
const (
	homeSpendDays     = 14
	homeSpendBarCells = 20
)

// homeSpendReading is everything the panel draws, taken on home's beat and
// never on a draw ([app.readHomeSpend]).
type homeSpendReading struct {
	// today is what the day has cost and ceiling the day's allowance — zero for
	// a machine that has none.
	today, ceiling float64
	// days is the fortnight's dollars, a day to a value, and total their sum.
	days  []float64
	total float64
	// top is the model most of the fortnight went to, and share its part.
	top   string
	share float64
}

// readHomeSpend is the pure half: the ledger's lines, a clock and an allowance
// in, the panel's figures out. The fortnight is the spend place's own reading
// of the same lines ([readSpend]), so the two surfaces cannot disagree.
//
// OWED: lane E — today's figure moves to session.SpendToday (DESIGN §3 E4) once
// it lands; it is the pulse's own sum until then ([spendDayTotal]).
func readHomeSpend(lines []session.UsageLine, now time.Time, ceiling float64) homeSpendReading {
	week := readSpend(lines, session.LastDays(now, homeSpendDays), now)
	out := homeSpendReading{today: spendDayTotal(lines, now), ceiling: ceiling,
		days: week.dayValues(), total: week.totals.USD}
	if len(week.models) > 0 && out.total > 0 {
		out.top, out.share = week.modelName(week.models[0].Model), week.models[0].USD/out.total
	}
	return out
}

// readHomeSpend takes the fortnight off the usage ledger, on the beat.
func (a *app) readHomeSpend() {
	now := a.now()
	lines, known := a.usageSince(session.LastDays(now, homeSpendDays).From)
	if !known {
		a.home.spend = homeSpendReading{}
		return
	}
	a.home.spend = readHomeSpend(lines, now, a.machineAllowance())
}

func (spendPanel) rows(in *homeGridInput) homePanelRows {
	s := in.spend
	var out homePanelRows
	if s.today > 0 {
		out.right = "today " + dollars(s.today)
		if s.ceiling > 0 {
			out.right += " of " + dollars(s.ceiling)
			out.lines = append(out.lines, spendLine("\x00bar", &homeCell{kind: cellBar, share: s.today / s.ceiling}))
		}
	}
	if s.total > 0 {
		out.lines = append(out.lines, spendLine("\x00days", &homeCell{kind: cellSpark, spark: s.days, right: s.fortnightWords()}))
	}
	return out
}

// spendLine is one of the panel's rows: a door into the spend place, told apart
// from its neighbour by key ([homeLine.sameRow]).
func spendLine(key string, cell *homeCell) homeLine {
	cell.panel = panelSpend
	return homeLine{kind: homeLedger, project: pageSpend.word(), dir: key, cell: cell}
}

// fortnightWords is `14 days $34.10 · opus 63%`.
func (s homeSpendReading) fortnightWords() string {
	words := fmt.Sprintf("%d days %s", homeSpendDays, spendMoneyWord(s.total))
	if s.top != "" && s.share > 0 {
		words += rowSep + fmt.Sprintf("%s %d%%", s.top, int(s.share*100+0.5))
	}
	return words
}

// homeSpendBar is the day against its allowance: the spent part in the reading
// tier and the rest dim, both in the vocabulary's own gauge cells.
func homeSpendBar(share float64, width int, pal palette) string {
	cells := min(homeSpendBarCells, width)
	if cells < 1 {
		return ""
	}
	filled := min(cells, max(1, int(share*float64(cells)+0.5)))
	return pal.muted(strings.Repeat(tokens.Gauge(1), filled)) + pal.dim(strings.Repeat(tokens.Gauge(0), cells-filled))
}

// homeSpendSpark is the fortnight: a spark cell a day, and the words after it
// when the column has room for them.
func homeSpendSpark(cell *homeCell, width int, pal palette) string {
	chart := sparkline(cell.spark, min(len(cell.spark), width))
	line := pal.muted(chart)
	used := len([]rune(chart))
	if room := width - used - len(homeCellGap); cell.right != "" && room > 0 {
		line += homeCellGap + pal.dim(fit(cell.right, room))
	}
	return line
}
