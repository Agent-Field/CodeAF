package tui3

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// ── THE SPENDING TAB ────────────────────────────────────────────────────────
//
// Money has ONE EDITOR and MANY DOORS (docs/design/spending/DESIGN.md). The
// editor is this tab: the registry's four money rows, in the order a person
// worries about them, with the day's own bill above them and — between them —
// the two rails this build enforces somewhere a settings row cannot reach.
//
//	 today             $3.42 of $500 · resets at midnight
//	 per day           $500
//	 per conversation  no limit                          this one $0.41
//	 per plan          asks first above $100
//	 per task          no limit of its own    it spends against the day and this conversation
//	 per standing run  $5 a firing                       each order may name its own
//	 practice          $50 of the day
//
// Three of those seven rows are READINGS and not settings, and the difference
// is the whole of what keeps this tab honest:
//
//   - `today` is where the eye lands, and it answers "what is it costing" before
//     anybody edits anything. It is a receipt: the cursor steps over it, and
//     before the first call of the day it is not there at all (the emptiness
//     law — a `$0.00 of $500` on a fresh morning is a claim nobody made).
//   - `per task` and `per standing run` are rails this build HAS and does not
//     keep a settings row for. A v3 task carries no dollar cap of its own —
//     docs/LIMITS.md says so in those words — and a standing order's per-firing
//     rail is written per item on the `stand` tool. The design asked for seven
//     rows and the honest way to have seven is to SAY what those two rails are,
//     not to grow two knobs that write nowhere. A row that pretended to edit a
//     rail nothing reads would be worse than the absence it was covering.
//
// EVERY VALUE ON THIS TAB IS A SENTENCE FRAGMENT COMPLETING "it may spend…",
// and every one of them degrades through rowfit rather than being cut: the
// label is the identity and stays whole, the value gives up its longest
// spelling first, and the receipt is the first thing off the row (rowfit.go's
// law 3, the design's "Narrow widths").

// railReading is one row of this tab that is a fact rather than a knob: what it
// is called, what it says, and the dim fact beside that. Both are [rowField]s
// because every one of them has a shorter spelling for a narrower frame.
type railReading struct {
	name    string
	value   rowField
	receipt rowField
}

// spendingOrder is the four registry rows of this tab, in READING order: the
// day (the bill), this conversation (the window in front of you), the plan (the
// question), and aforge's own slice. Not alphabetical, not registry order, not
// by key — by how often a person worries about each one.
var spendingOrder = []string{
	config.KeyDailyBudget,
	config.KeySpendRail,
	config.KeyPlanConsent,
	config.KeyPracticeBudget,
}

// spendingItems is the whole Spending tab, readings and rows interleaved.
//
// A ROW NOBODY ORDERED IS STILL DRAWN, at the foot, in registry order — the same
// contract [sheet.tabRows] keeps for every other tab, so a fifth money row lands
// on this tab reachable before anybody has thought about where it goes.
func (s *sheet) spendingItems() []sheetItem {
	mine := map[string]config.Setting{}
	var order []string
	for _, row := range s.rows {
		if meta, ok := settingMetaFor(row); ok && meta.tab == tabSpending {
			mine[row.Key] = row
			order = append(order, row.Key)
		}
	}
	items := make([]sheetItem, 0, len(order)+3)
	add := func(key string) {
		row, ok := mine[key]
		if !ok {
			return
		}
		delete(mine, key)
		meta, _ := settingMetaFor(row)
		items = append(items, sheetItem{row: row, meta: meta})
	}
	if s.today != nil {
		items = append(items, sheetItem{read: s.today})
	}
	add(config.KeyDailyBudget)
	add(config.KeySpendRail)
	add(config.KeyPlanConsent)
	items = append(items, sheetItem{read: taskReading()}, sheetItem{read: standingReading()})
	add(config.KeyPracticeBudget)
	for _, key := range order {
		if _, left := mine[key]; left {
			add(key)
		}
	}
	return items
}

// todayReading is the first row: what the day has cost, against what it is
// allowed, and when the count starts again.
//
// IT IS NIL BEFORE THE FIRST CALL OF THE DAY. A machine that has not spent
// anything has not spent zero — it has not spent — and `$0.00 of $500` on a
// quiet morning is exactly the reading the emptiness law exists to prevent.
func (a *app) todayReading() *railReading {
	spent, counted := a.spentTodayUSD()
	if !counted || spent <= 0 {
		return nil
	}
	figure := dollars(spent)
	rail, err := config.DailyBudgetUSDAt(a.profileDir)
	if err != nil || rail <= 0 {
		// No rail is not a missing denominator to be drawn as a gap: the row
		// says the day's figure and the word that says nothing bounds it.
		return &railReading{name: spendTodayWord,
			value: rowSay(figure+" · "+config.NoLimitWord, figure)}
	}
	of := figure + " of " + railFigure(rail)
	return &railReading{name: spendTodayWord,
		value: rowSay(of+" · resets at midnight", of, figure)}
}

// spendTodayWord is the first row's name, and it is the same word every door
// that lands here uses for it.
const spendTodayWord = "today"

// railFigure is how a LIMIT is written on this surface: whole dollars when the
// figure is whole, and cents when it is not.
//
// IT IS NOT [dollars], and the difference is the point. That function writes
// what something COST — a measurement, always to the cent, `$0.00` on the status
// line so its segments do not jump sideways. A limit is a figure somebody TYPED,
// and `$500.00` is that figure with two cells of noise on the end: nobody sets a
// daily limit of five hundred dollars and no cents. The day's own spend on the
// `today` row keeps [dollars], because that half of the line is a measurement.
// railSpell is that spelling applied to a figure the registry has already
// formatted. internal/config writes the shortest form that is still the same
// number ("$4.1"), which is right for a config file and one cell short of what a
// column of money reads as — this tab's own law is whole dollars when whole and
// cents otherwise, and this is the one place it is applied.
func railSpell(value string) string {
	figure, err := strconv.ParseFloat(strings.TrimPrefix(value, "$"), 64)
	if err != nil || !strings.HasPrefix(value, "$") {
		return value
	}
	return railFigure(figure)
}

func railFigure(usd float64) string {
	switch {
	case usd == float64(int64(usd)):
		return "$" + strconv.FormatInt(int64(usd), 10)
	case usd < 0.01:
		// AND A SUB-CENT LIMIT IS STILL A LIMIT. Two decimals turn a tenth of a
		// cent into `$0.00`, which is the one reading this tab exists to never
		// give: the figure a person typed rendered as its own opposite.
		return fmt.Sprintf("$%.4f", usd)
	}
	return fmt.Sprintf("$%.2f", usd)
}

// taskReading is the rail a task actually runs under, said plainly.
//
// A TASK HAS NO DOLLAR CAP OF ITS OWN (docs/LIMITS.md). Its bounds are steps and
// time; its money bound is whatever the conversation that started it carries,
// which is the row two above this one and the day's row above that. The design
// asked for a `per task` row and this is the true one: a person who wanted to
// know what a task may spend now knows, and knows where to go and change it.
func taskReading() *railReading {
	return &railReading{name: "per task",
		value:   rowSay("no limit of its own", "no limit"),
		receipt: rowSay("it spends against the day and this conversation", "against the day and this chat")}
}

// standingReading is what one firing of a standing order may spend when the
// order did not name its own figure ([standing.DefaultPerRunUSD]).
//
// IT IS A READING BECAUSE THE RAIL IS PER ITEM. Every standing order carries its
// own `per_run_usd`, set where the order is written, so there is no single
// number a settings row could hold — what this row can honestly say is the
// figure an order that named nothing runs under, and where the other answer
// lives.
func standingReading() *railReading {
	return &railReading{name: "per standing run",
		value:   rowSay(railFigure(standing.DefaultPerRunUSD)+" a firing", railFigure(standing.DefaultPerRunUSD)),
		receipt: rowSay("each order may name its own", "set per order")}
}

// ── what a money row says ───────────────────────────────────────────────────

// spendValue is the VALUE of one money row in up to three spellings.
//
// It is the registry's own reading — the figure, or the row's word for zero
// ([config.Setting.EmptyLabel], which is where `no limit`, `never asks` and
// `practice off` are written down once) — wrapped in the fragment that completes
// "it may spend…" for the two rows that have one. `asks first above $100`
// becomes `asks > $100` becomes `$100`, and the sentence survives three widths
// further down than a string that could only be cut.
func spendValue(row config.Setting) rowField {
	value := row.Value()
	if value == "" {
		return rowSay()
	}
	// A row holding nothing reads its own word for that and takes no fragment:
	// "asks first above never asks" is not a sentence.
	if value == row.EmptyLabel {
		return rowSay(value)
	}
	value = railSpell(value)
	switch row.Key {
	case config.KeyPlanConsent:
		return rowSay("asks first above "+value, "asks > "+value, value)
	case config.KeyPracticeBudget:
		return rowSay(value+" of the day", value)
	}
	return rowSay(value)
}

// spendNote is the whole right-hand side of a money row: the value, the pin that
// froze it, and the receipt — ranked, and fitted to the cells this row's label
// leaves ([overlayNoteRoom]).
//
// THE RECEIPT IS LAST AND THEREFORE FIRST TO GO. That is rowfit's law 3 read
// forwards: facts are added in priority order and the first that will not fit
// ends the tail, so a sixty-cell frame keeps `no limit` and drops `this one
// $0.41` rather than keeping half of each.
func (s *sheet) spendNote(item sheetItem, width int, ascii bool) string {
	fields := []rowField{s.markedValue(item, spendValue(item.row), ascii)}
	if name, pinned := item.row.PinnedBy(); pinned {
		fields = append(fields, rowSay("set by "+name, name))
	}
	if receipt := item.row.Receipt(); receipt != "" {
		fields = append(fields, rowSay(receipt))
	}
	return rowTail(fields, overlayNoteRoom(item.meta.label, width))
}

// readingNote is the same two ranked facts for a row that is a reading.
func (s *sheet) readingNote(item sheetItem, width int) string {
	fields := []rowField{item.read.value}
	if item.read.receipt.known() {
		fields = append(fields, item.read.receipt)
	}
	return rowTail(fields, overlayNoteRoom(item.read.name, width))
}

// markedValue puts the changed mark in front of every spelling of a value, so a
// row a person chose is marked at every width rather than only at the widest.
func (s *sheet) markedValue(item sheetItem, value rowField, ascii bool) rowField {
	if !s.changed(item) {
		return value
	}
	mark := changedMark
	if ascii {
		mark = "*"
	}
	lead := func(spelling string) string {
		if spelling == "" {
			return ""
		}
		return mark + " " + spelling
	}
	return rowField{full: lead(value.full), short: lead(value.short), tiny: lead(value.tiny)}
}

// ── the doors ───────────────────────────────────────────────────────────────

// openSpending opens the settings panel on the Spending tab, with the cursor on
// the row the door came from.
//
// EVERY DOOR ONTO MONEY COMES THROUGH HERE. The status line's money segment, the
// spend place's pointer line, `/budget`, and a rail that has just tripped are
// four ways of asking one question, and a second door that opened a second
// editor would be a second answer to what the limit is. A key that is not a row
// of this tab lands on the first row a cursor may rest on, which is what `today`
// being a receipt means in practice.
func (a *app) openSpending(key string) tea.Cmd {
	cmd := a.showPage(pageSettings)
	a.sheet.tab = spendingTabIndex()
	a.sheet.query.reset()
	a.sheet.build()
	a.sheet.cursorTo(key)
	a.touch()
	return cmd
}

// spendingTabIndex is where the Spending tab sits in the bar. It is looked up
// rather than written down, so a tab inserted before it moves the doors with it.
func spendingTabIndex() int {
	for at, title := range settingTabs {
		if title == tabSpending {
			return at
		}
	}
	return 0
}

// cursorTo puts the cursor on the registry row with this key, and leaves it on
// the first row it may rest on when nothing on the tab has that key.
func (s *sheet) cursorTo(key string) {
	for at, item := range s.items {
		if item.restful() && item.row.Key == key {
			s.cursor = at
			s.top = 0
			return
		}
	}
	s.cursor = s.clampCursor(0)
	s.top = 0
}

// ── the day's own figure ────────────────────────────────────────────────────

// spentTodayUSD is what this MACHINE has spent since midnight, read off the
// usage ledger every model call writes a line to.
//
// IT IS READ ON THE WAY INTO THE PANEL AND NOT ON A DRAW. The ledger grows by a
// line per call and this walk is over one day of it, which is cheap once and
// wrong to do sixty times a second — [app.raiseSettings] takes the reading, and
// every row that quotes it quotes the same one, so the tab cannot disagree with
// itself while somebody is reading it.
//
// The bool is the seam's own distinction and the reason the row can be absent: a
// door with no ledger behind it, or a day with nothing on it, has NOT COUNTED
// ZERO — it has not counted.
func (a *app) spentTodayUSD() (float64, bool) { return a.dayCost, a.dayCosted }

// readDayCost takes that reading.
func (a *app) readDayCost() {
	a.dayCost, a.dayCosted = 0, false
	if strings.TrimSpace(a.usageLedger) == "" {
		return
	}
	now := a.now()
	if now.IsZero() {
		now = time.Now()
	}
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	lines, err := session.ReadUsage(a.usageLedger, day)
	if err != nil {
		return
	}
	total := 0.0
	for _, line := range lines {
		total += line.USD
	}
	a.dayCost, a.dayCosted = total, true
}

// spentThisSessionUSD is what the conversation in front of the person has spent,
// for the receipt beside its own ceiling. Nothing spent is nothing said.
func (a *app) spentThisSessionUSD() (float64, bool) { return a.cost, a.cost > 0 }
