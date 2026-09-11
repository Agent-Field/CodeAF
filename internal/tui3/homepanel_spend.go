package tui3

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// spendPanel is `spend`: a small HUD, THREE LINES THAT EACH SAY ONE THING. The
// heading carries what today has cost against the day's allowance, and — once
// the day has spent enough of it to see — one thin meter says how much of it is
// gone. Then the fortnight, one block cell a day with today at the right, its
// total and its loudest day. Then who it went to and what for: the two models
// most of it bought, and how many chats and tasks today has seen. Every row is
// a door into the spend place.
//
// IT WAS TWO CHARTS (owner, 2026-09-10). A gauge-cell bar under the heading
// read as a second sparkline, the braille fortnight beside it was noise at a
// column's width, and the allowance said `$500.00` under a pulse saying `$500`.
// The meter is a line now and not a chart, the fortnight is block cells, and the
// allowance is spelled the pulse's way ([railFigure]).
type spendPanel struct{ homePanelBase }

const (
	// homeSpendDays is the stretch the panel draws, a cell a day.
	homeSpendDays = 14
	// homeSpendMeterCells is the meter's length: the fortnight's under it, so
	// the two drawings stand one width.
	homeSpendMeterCells = homeSpendDays
	// homeSpendMeterFloor is the least share of the allowance the meter is
	// drawn for. UNDER A TWENTIETH A METER READS AS BROKEN — a one-cell run on a
	// fourteen-cell line looks like a bar that failed to draw — and the heading
	// already says the figure.
	homeSpendMeterFloor = 1.0 / 20
	// homeSpendModels is how many models the last line names.
	homeSpendModels = 2
)

// The words and marks the panel is drawn with.
const (
	// homeSpendLoudWord leads the fortnight's costliest day: `loudest sun $88.10`.
	homeSpendLoudWord = "loudest "
	// homeSpendGroupSep stands between the last line's two groups — the models,
	// and what the day was spent on — wider than the `·` inside each, so the
	// line reads as two facts and not as four.
	homeSpendGroupSep = homeCellGap + "·" + homeCellGap
	// homeSpendRun and homeSpendRest are the meter's spent run and the rest of
	// the allowance; the ASCII pair is the floor for a terminal refused box
	// drawing, as the task meter's is ([app.progress]).
	homeSpendRun       = "━"
	homeSpendRest      = "╌"
	homeSpendRunASCII  = "="
	homeSpendRestASCII = "-"
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
	// loud is the fortnight's costliest day said as a day — `sun`, or `today` —
	// and loudUSD what it cost.
	loud    string
	loudUSD float64
	// models are the models most of the fortnight went to, costliest first.
	models []homeSpendModel
}

// homeSpendModel is one model's share of the fortnight.
type homeSpendModel struct {
	name  string
	share float64
}

// readHomeSpend is the pure half: the ledger's lines, a clock and an allowance
// in, the panel's figures out. The fortnight, its loudest day and its models
// are the spend place's own reading of the same lines ([readSpend]), so the two
// surfaces cannot disagree; today's figure is the engine's one reading of the
// day ([session.SpendToday], DESIGN §3 E4).
func readHomeSpend(lines []session.UsageLine, now time.Time, ceiling float64) homeSpendReading {
	week := readSpend(lines, session.LastDays(now, homeSpendDays), now)
	out := homeSpendReading{today: session.SpendToday(lines, now), ceiling: ceiling,
		days: week.dayValues(), total: week.totals.USD}
	if week.loudest.USD > 0 {
		out.loud, out.loudUSD = spendDayWord(week.loudest.At, now), week.loudest.USD
	}
	for _, model := range week.models {
		if len(out.models) == homeSpendModels || out.total <= 0 {
			break
		}
		out.models = append(out.models, homeSpendModel{name: week.modelName(model.Model), share: model.USD / out.total})
	}
	return out
}

// spendDayWord is a day as the panel says it: `today` for today, which is what
// the rest of this surface calls it, and its lowercase weekday otherwise — the
// fortnight is two weeks, so a weekday is never a guess about which.
func spendDayWord(at, now time.Time) string {
	if sameSpendBucket(at, now, session.GrainDay) {
		return spendTodayWord
	}
	return strings.ToLower(at.Format("Mon"))
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
		out.right, out.money = s.todayWords(), dollars(s.today)
	}
	if share := s.used(); share >= homeSpendMeterFloor {
		out.lines = append(out.lines, spendLine("\x00bar", &homeCell{kind: cellBar, share: share}))
	}
	if s.total <= 0 {
		return out
	}
	out.lines = append(out.lines, spendLine("\x00days", &homeCell{kind: cellSpark, spark: s.days, title: s.fortnightWords(), right: s.loudWords()}))
	if models, today := s.modelWords(), spendActivity(in.world, in.now); models != "" || today != "" {
		out.lines = append(out.lines, spendLine("\x00what", &homeCell{kind: cellFacts, title: models, note: today}))
	}
	return out
}

// spendLine is one of the panel's rows: a door into the spend place, told apart
// from its neighbours by key ([homeLine.sameRow]).
func spendLine(key string, cell *homeCell) homeLine {
	cell.panel = panelSpend
	return homeLine{kind: homeLedger, project: pageSpend.word(), dir: key, cell: cell}
}

// used is how much of the day's allowance is gone, and nothing for a machine
// that has none ([session.SpendShare]).
func (s homeSpendReading) used() float64 { return session.SpendShare(s.today, s.ceiling) }

// todayWords is the heading's clause: `today $6.51 of $500`, the allowance
// spelled the way the pulse spells a figure somebody typed ([railFigure]).
func (s homeSpendReading) todayWords() string {
	words := spendTodayWord + " " + dollars(s.today)
	if s.ceiling > 0 {
		words += " of " + railFigure(s.ceiling)
	}
	return words
}

// fortnightWords is `14 days $204.36`.
func (s homeSpendReading) fortnightWords() string {
	return fmt.Sprintf("%d days %s", homeSpendDays, spendMoneyWord(s.total))
}

// loudWords is `loudest sun $88.10`, and nothing for a fortnight with no day.
func (s homeSpendReading) loudWords() string {
	if s.loud == "" || s.loudUSD <= 0 {
		return ""
	}
	return homeSpendLoudWord + s.loud + " " + spendMoneyWord(s.loudUSD)
}

// modelWords is `glm-5.3 55% · opus 31%`, each model whose share rounds to a
// whole percent.
func (s homeSpendReading) modelWords() string {
	var parts []string
	for _, model := range s.models {
		if percent := int(model.share*100 + 0.5); model.name != "" && percent > 0 {
			parts = append(parts, fmt.Sprintf("%s %d%%", model.name, percent))
		}
	}
	return strings.Join(parts, rowSep)
}

// spendActivity is what the day was spent on: `3 chats and 1 task today` —
// the conversations somebody spoke in since midnight and the pieces of work
// that started or ended since then, one per piece and never its parts. It is
// read off the world the beat already holds, and says nothing for a day with
// neither.
func spendActivity(world session.World, now time.Time) string {
	day := machineDayStart(now)
	if day.IsZero() {
		return ""
	}
	chats, tasks := 0, 0
	for _, project := range world.Projects {
		for _, row := range project.Sessions {
			if !row.At.Before(day) {
				chats++
			}
			for _, entry := range row.Tasks.Rows {
				if entry.Parent == "" && (!entry.StartedAt.Before(day) || !entry.EndedAt.Before(day)) {
					tasks++
				}
			}
		}
	}
	var parts []string
	if chats > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", chats, switcherPlural(chats, "chat", "chats")))
	}
	if tasks > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", tasks, switcherPlural(tasks, "task", "tasks")))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " and ") + " " + spendTodayWord
}

// ── the paint ───────────────────────────────────────────────────────────────

// homeSpendMeter is the day against its allowance: `━━━╌╌╌╌╌╌╌╌╌╌╌ 34%`, the
// spent run in the money ink and the rest dim, as one line and not a chart.
func homeSpendMeter(share float64, width int, pal palette) string {
	percent := fmt.Sprintf("%d%%", int(share*100+0.5))
	cells := min(homeSpendMeterCells, width-1-len(percent))
	if cells < 1 {
		return pal.dim(fit(percent, width))
	}
	run, rest := homeSpendRun, homeSpendRest
	if pal.ascii {
		run, rest = homeSpendRunASCII, homeSpendRestASCII
	}
	spent := min(cells, max(1, int(share*float64(cells)+0.5)))
	return placeMoneyInk(pal)(strings.Repeat(run, spent)) + pal.dim(strings.Repeat(rest, cells-spent)) +
		" " + pal.dim(percent)
}

// homeSpendSpark is the fortnight: a block cell a day, muted with today's cell
// in ink, and after it the total and the loudest day — each whole or not at all,
// the loudest day giving way first ([rowTail]).
func homeSpendSpark(cell *homeCell, width int, pal palette) string {
	steps := homeSparkCells(cell.spark)
	if len(steps) > width {
		steps = steps[len(steps)-width:]
	}
	if len(steps) == 0 {
		return ""
	}
	line := pal.muted(strings.Join(steps[:len(steps)-1], "")) + pal.ink(steps[len(steps)-1])
	room := width - len(steps) - len(homeCellGap)
	if words := rowTail([]rowField{rowSay(cell.title), rowSay(cell.right)}, room); words != "" {
		line += homeCellGap + pal.dim(words)
	}
	return line
}

// homeSpendFacts is the last line: the models, then what the day was spent on
// after the wider separator. A column too narrow for both drops the second
// model before it drops the day, and the day before the first model.
func homeSpendFacts(cell *homeCell, width int, pal palette) string {
	if cell.title == "" {
		return pal.dim(rowTail([]rowField{rowSay(cell.note)}, width))
	}
	models := strings.Split(cell.title, rowSep)
	for n := len(models); n > 0; n-- {
		line := strings.Join(models[:n], rowSep)
		if cell.note != "" {
			line += homeSpendGroupSep + cell.note
		}
		if ansi.StringWidth(line) <= width {
			return pal.dim(line)
		}
	}
	return pal.dim(rowTail([]rowField{rowSay(models[0])}, width))
}
