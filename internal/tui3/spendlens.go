package tui3

// SPEND LENSES — ways of READING the same ledger, never a second editor.
//
// The spend place answers "what did it cost". That answer has more than one
// shape a person wants: the rhythm of the window, a model table they can
// compare down, a day ledger they can open, and a year heatmap that says when
// they burn. Each shape is a LENS over the same lines [spendPage] already
// holds. The Spending tab remains the only place money is edited
// (docs/design/spending/DESIGN.md); a lens that wrote a rail would be a second
// answer to what the ceiling is.
//
// THE KEYS ARE `[` AND `]`. They cycle the lens the way `shift+↑↓` cycles the
// grain — one axis, both directions, named on the foot only while this place
// owns the keyboard. Slash doors (`/spend models`, `/spend days`, `/spend year`)
// land on the same field so a command and a key never disagree about which
// reading is up.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// spendLens is which reading the spend place is showing.
type spendLens int

const (
	// spendLensRhythm is today's page: spark, models, subjects — the reading
	// this place opened with before lenses existed.
	spendLensRhythm spendLens = iota
	// spendLensModels is the full model table for the window.
	spendLensModels
	// spendLensDays is one row per bucket, Enter drills into that day.
	spendLensDays
	// spendLensYear is the Stats-lite heatmap over the last year.
	spendLensYear
)

// spendLensOrder is the cycle `[` / `]` walk, and the order slash names land.
var spendLensOrder = []spendLens{
	spendLensRhythm, spendLensModels, spendLensDays, spendLensYear,
}

func (l spendLens) word() string {
	switch l {
	case spendLensModels:
		return "models"
	case spendLensDays:
		return "days"
	case spendLensYear:
		return "year"
	default:
		return "rhythm"
	}
}

func (l spendLens) next() spendLens {
	for i, at := range spendLensOrder {
		if at == l {
			return spendLensOrder[(i+1)%len(spendLensOrder)]
		}
	}
	return spendLensRhythm
}

func (l spendLens) prev() spendLens {
	for i, at := range spendLensOrder {
		if at == l {
			return spendLensOrder[(i-1+len(spendLensOrder))%len(spendLensOrder)]
		}
	}
	return spendLensRhythm
}

// parseSpendLens reads a slash argument word into a lens. Unknown words answer
// false so `/spend opus` is not a silent no-op.
func parseSpendLens(word string) (spendLens, bool) {
	switch strings.ToLower(strings.TrimSpace(word)) {
	case "", "rhythm":
		return spendLensRhythm, true
	case "models", "model":
		return spendLensModels, true
	case "days", "day", "daily":
		return spendLensDays, true
	case "year", "stats", "heatmap":
		return spendLensYear, true
	}
	return 0, false
}

// ── the lens chip strip ─────────────────────────────────────────────────────
//
// THE FOUR READINGS USED TO LIVE ONLY ON `[` `]` AND A HEAD CLAUSE. People
// landed on rhythm (today's familiar bill), never pressed a bracket, and never
// found models / days / year. A chip strip under the rails — the same object
// the settings panel and the place tab bar already are — names every lens at
// once and takes a press. The keys stay; the strip is how a stranger discovers
// them (docs/design/spend-lenses/DESIGN.md).

// spendLensBar is the chip row under the rails: one padded word per lens, the
// active one filled. It is drawn at the place's full width so a press's column
// and the paint agree (settings.go's [sheetTabBar] is the same bargain).
func spendLensBar(width int, active spendLens, pal palette) string {
	line := strings.Repeat(" ", tabLead)
	for i, lens := range spendLensOrder {
		if i > 0 {
			line += strings.Repeat(" ", tabGap)
		}
		title := lens.word()
		chip := tabPad + title + tabPad
		if lens == active {
			line += pal.selected(pal.bold(pal.ink(chip)), tabChipCols(title))
		} else {
			line += pal.dim(chip)
		}
	}
	return fit(line, width)
}

// spendLensAtColumn is which chip a press landed in. The gap between chips
// belongs to none of them — same miss rule as the place tab bar.
func spendLensAtColumn(x int) (spendLens, bool) {
	at := tabLead
	for i, lens := range spendLensOrder {
		if i > 0 {
			at += tabGap
		}
		cols := tabChipCols(lens.word())
		if x >= at && x < at+cols {
			return lens, true
		}
		at += cols
	}
	return 0, false
}

// withSpendLensBar inserts the chip strip immediately under the rails (row 0)
// and shifts every stop under it by one, marking the strip itself so a press
// can switch lenses without walking the cursor onto chrome.
func withSpendLensBar(out []string, stops []spendStop, active spendLens, width int, pal palette) ([]string, []spendStop) {
	if len(out) == 0 {
		return out, stops
	}
	nextOut := make([]string, 0, len(out)+1)
	nextOut = append(nextOut, out[0], spendLensBar(width, active, pal))
	nextOut = append(nextOut, out[1:]...)
	nextStops := make([]spendStop, len(nextOut))
	if len(stops) > 0 {
		nextStops[0] = stops[0]
	}
	nextStops[1] = spendStop{lensBar: true}
	for i := 1; i < len(stops); i++ {
		nextStops[i+1] = stops[i]
	}
	return nextOut, nextStops
}

// spendGroup is how the Models lens aggregates rows.
type spendGroup int

const (
	spendGroupModel spendGroup = iota
	spendGroupRole
	spendGroupSubject
)

func (g spendGroup) word() string {
	switch g {
	case spendGroupRole:
		return "role"
	case spendGroupSubject:
		return "subject"
	default:
		return "model"
	}
}

func (g spendGroup) next() spendGroup {
	return spendGroup((int(g) + 1) % 3)
}

// spendSort is the Models / Days sort key. Models cycles every column the table
// draws (DESIGN.md §3 — "sorts by each column"); Days keeps cost ↔ tokens.
type spendSort int

const (
	spendSortCost spendSort = iota
	spendSortTokens
	spendSortModel
	spendSortRole
	spendSortRate // realized $/M
	spendSortCount
)

func (s spendSort) word() string {
	switch s {
	case spendSortTokens:
		return "tokens"
	case spendSortModel:
		return "model"
	case spendSortRole:
		return "role"
	case spendSortRate:
		return "$/M"
	default:
		return "cost"
	}
}

// next advances the Models-lens sort through every column.
func (s spendSort) next() spendSort {
	return spendSort((int(s) + 1) % int(spendSortCount))
}

// nextDay is the Days-lens two-step: cost ↔ tokens only.
func (s spendSort) nextDay() spendSort {
	if s == spendSortCost {
		return spendSortTokens
	}
	return spendSortCost
}

// spendLensWord is the head control's lens clause: `models · [ ] lenses`.
const (
	spendLensKeysWord   = "[ ] lenses"
	spendGroupKeyWord   = "g group"
	spendSortKeyWord    = "c/t sort"
	spendSortHeadWord   = "c/t sort · enter on a head cell"
	spendModelsLensWord = "what ran it"
	spendDaysLensWord   = "by the day"
	spendYearLensWord   = "this year"
	spendYearDays       = 365
)

// spendStop extensions for day drill live beside the existing door flags.
// day set means Enter narrows the window onto that bucket and returns to Rhythm.

// yearWindow is the stretch the Year lens reads: the last year ending today.
func yearWindow(now time.Time) session.UsageWindow {
	return session.LastDays(now, spendYearDays)
}

// paintLens is [spendReading.paint] branched by the place's lens. Rhythm keeps
// the original body; the other three replace the spark/models/subjects block
// while the rails pointer and window head stay — one bill, one ceiling, many
// readings of the same money.
func (r spendReading) paintLens(lens spendLens, group spendGroup, sort spendSort, width int, pal palette, lit func(int) bool) ([]string, []spendStop) {
	switch lens {
	case spendLensModels:
		return r.paintModelsLens(group, sort, width, pal, lit)
	case spendLensDays:
		return r.paintDaysLens(sort, width, pal, lit)
	case spendLensYear:
		return r.paintYearLens(width, pal, lit)
	default:
		return r.paintRhythm(width, pal, lit)
	}
}

// paintRhythm is the original spend body, renamed so the lens switch has one
// word for "what this place used to be". The chip strip under the rails names
// which reading is up; the window head stays the window alone.
func (r spendReading) paintRhythm(width int, pal palette, lit func(int) bool) ([]string, []spendStop) {
	rows, stops := r.paint(width, pal, lit)
	return withSpendLensBar(rows, stops, spendLensRhythm, width, pal)
}

// windowHeaderRowFor is the window head on a lens body. The active lens is
// named on the chip strip above it, never again on this row — one source of
// truth for "which reading is up".
func (r spendReading) windowHeaderRowFor(_ spendLens, width int, pal palette) string {
	return r.windowHeaderRow(width, pal)
}

func (r spendReading) paintModelsLens(group spendGroup, sort spendSort, width int, pal palette, lit func(int) bool) ([]string, []spendStop) {
	if width < 1 || r.empty() {
		return nil, nil
	}
	on := func(at int) bool { return lit != nil && lit(at) }
	inner := width - len(placeLead)
	var out []string
	rails := len(out)
	out = append(out, placeLead+r.railsRowIn(inner, placeFactInk(on(rails), pal)))
	out = append(out, r.windowHeaderRowFor(spendLensModels, width, pal))
	caption := spendModelsLensWord + " · by " + group.word() + " · " + sort.word()
	out = appendPlaceSection(out, placeLead+placeHeading(fit(caption, inner), pal))
	// THE HEAD CELLS ARE THE SORT DOORS (DESIGN.md §3). One row names every
	// column; the active sort is ink, the rest stay dim; enter / a press on a
	// cell sets that column.
	headAt := len(out)
	out = append(out, placeLead+modelsSortHead(inner, sort, on(headAt), pal))
	rows := r.modelsLensRows(group, sort)
	for _, row := range rows {
		out = append(out, placeLead+r.modelsLensRow(row, inner, pal))
	}
	if len(rows) == 0 {
		out = append(out, placeLead+pal.dim(fit(spendModelsQuietWord, inner)))
	}
	stops := make([]spendStop, len(out))
	stops[rails] = spendStop{ok: true, rails: true}
	stops[headAt] = spendStop{ok: true, sortHead: true}
	return withSpendLensBar(out, stops, spendLensModels, width, pal)
}

// modelsSortHead is the Models-lens column row. Cells left→right match the
// design table: model · in · out · role · $/M · spend.
func modelsSortHead(width int, sort spendSort, lit bool, pal palette) string {
	cells := []struct {
		key  spendSort
		word string
	}{
		{spendSortModel, "model"},
		{spendSortTokens, "in · out"},
		{spendSortRole, "role"},
		{spendSortRate, "$/M"},
		{spendSortCost, "spend"},
	}
	parts := make([]string, 0, len(cells))
	for _, cell := range cells {
		word := cell.word
		if cell.key == sort {
			word = "▸ " + word
			if lit {
				parts = append(parts, pal.data(word))
			} else {
				parts = append(parts, pal.muted(word))
			}
			continue
		}
		parts = append(parts, pal.dim(word))
	}
	line := strings.Join(parts, pal.dim("  "))
	return fit(line, width)
}

// modelsLensRow is one aggregated Models-lens line.
type modelsLensRow struct {
	name   string
	role   string
	calls  int
	input  int
	output int
	tokens int
	usd    float64
}

func (r spendReading) modelsLensRows(group spendGroup, sort spendSort) []modelsLensRow {
	switch group {
	case spendGroupRole:
		return r.modelsByRole(sort)
	case spendGroupSubject:
		return r.modelsBySubject(sort)
	default:
		out := make([]modelsLensRow, 0, len(r.models))
		for _, model := range r.models {
			out = append(out, modelsLensRow{
				name:   r.modelName(model.Model),
				role:   strings.TrimSpace(r.modelRole(model.Model)),
				calls:  model.Calls,
				input:  model.Input,
				output: model.Output,
				tokens: model.Tokens,
				usd:    model.USD,
			})
		}
		sortModelsLens(out, sort)
		return out
	}
}

func (r spendReading) modelsByRole(sort spendSort) []modelsLensRow {
	totals := map[string]*modelsLensRow{}
	order := []string{}
	for _, model := range r.models {
		role := strings.TrimSpace(r.modelRole(model.Model))
		if role == "" {
			role = "unbound"
		}
		row := totals[role]
		if row == nil {
			row = &modelsLensRow{name: role}
			totals[role] = row
			order = append(order, role)
		}
		row.calls += model.Calls
		row.input += model.Input
		row.output += model.Output
		row.tokens += model.Tokens
		row.usd += model.USD
	}
	out := make([]modelsLensRow, 0, len(order))
	for _, id := range order {
		out = append(out, *totals[id])
	}
	sortModelsLens(out, sort)
	return out
}

func (r spendReading) modelsBySubject(sort spendSort) []modelsLensRow {
	out := make([]modelsLensRow, 0, len(r.subjects))
	for _, subject := range r.subjects {
		out = append(out, modelsLensRow{
			name:   r.name(subject),
			role:   subject.Kind,
			calls:  subject.Calls,
			tokens: subject.Tokens,
			usd:    subject.USD,
		})
	}
	sortModelsLens(out, sort)
	return out
}

func sortModelsLens(rows []modelsLensRow, key spendSort) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		switch key {
		case spendSortTokens:
			if a.tokens != b.tokens {
				return a.tokens > b.tokens
			}
		case spendSortModel:
			if a.name != b.name {
				return strings.ToLower(a.name) < strings.ToLower(b.name)
			}
		case spendSortRole:
			if a.role != b.role {
				return strings.ToLower(a.role) < strings.ToLower(b.role)
			}
		case spendSortRate:
			ra, rb := modelsLensRate(a), modelsLensRate(b)
			if ra != rb {
				return ra > rb
			}
		default: // spendSortCost
			if a.usd != b.usd {
				return a.usd > b.usd
			}
		}
		if a.usd != b.usd {
			return a.usd > b.usd
		}
		return a.tokens > b.tokens
	})
}

func modelsLensRate(row modelsLensRow) float64 {
	if row.usd <= 0 || row.tokens <= 0 {
		return 0
	}
	return row.usd / (float64(row.tokens) / 1_000_000)
}

func sortDaysLens(days []session.DaySpend, key spendSort) {
	sort.SliceStable(days, func(i, j int) bool {
		// Days only answer cost ↔ tokens; other keys fall through to cost.
		if key == spendSortTokens {
			if days[i].Tokens != days[j].Tokens {
				return days[i].Tokens > days[j].Tokens
			}
			return days[i].USD > days[j].USD
		}
		if days[i].USD != days[j].USD {
			return days[i].USD > days[j].USD
		}
		return days[i].Tokens > days[j].Tokens
	})
}

func (r spendReading) modelsLensRow(row modelsLensRow, width int, pal palette) string {
	left := tokens.GlyphProseBullet + " " + row.name
	if row.role != "" && row.role != row.name {
		left += " · " + row.role
	}
	if width >= 80 {
		if bar := spendBar(row.usd/r.modelsLensTop(row), spendModelBarCap); bar != "" {
			left += " " + bar
		}
		if stats := spendLensStats(row); stats != "" {
			left += " " + stats
		}
	}
	return spendSides(width, left, spendMoneyWord(row.usd), func(s string) string {
		return pal.dim(s)
	}, placeMoneyInk(pal))
}

func (r spendReading) modelsLensTop(row modelsLensRow) float64 {
	if len(r.models) > 0 && r.models[0].USD > 0 {
		return r.models[0].USD
	}
	if row.usd > 0 {
		return row.usd
	}
	return 1
}

func spendLensStats(row modelsLensRow) string {
	var parts []string
	if row.calls > 0 {
		parts = append(parts, fmt.Sprintf("%s calls", groupedInt(row.calls)))
	}
	if row.input > 0 || row.output > 0 {
		parts = append(parts, tokenWord(row.input)+" in · "+tokenWord(row.output)+" out")
	} else if row.tokens > 0 {
		parts = append(parts, tokenWord(row.tokens))
	}
	if row.usd > 0 && row.tokens > 0 {
		perM := row.usd / (float64(row.tokens) / 1_000_000)
		if perM > 0 {
			parts = append(parts, "$"+spendTrimFloat(perM)+"/M")
		}
	}
	return strings.Join(parts, " · ")
}

// spendTrimFloat spells a realized $/M figure: fewer places as the number grows,
// so a failure message and a page row both read the same magnitude.
func spendTrimFloat(v float64) string {
	if v >= 100 {
		return fmt.Sprintf("%.0f", v)
	}
	if v >= 10 {
		return fmt.Sprintf("%.1f", v)
	}
	return fmt.Sprintf("%.2f", v)
}

func (r spendReading) paintDaysLens(sort spendSort, width int, pal palette, lit func(int) bool) ([]string, []spendStop) {
	if width < 1 || r.empty() {
		return nil, nil
	}
	on := func(at int) bool { return lit != nil && lit(at) }
	inner := width - len(placeLead)
	var out []string
	rails := len(out)
	out = append(out, placeLead+r.railsRowIn(inner, placeFactInk(on(rails), pal)))
	out = append(out, r.windowHeaderRowFor(spendLensDays, width, pal))
	out = appendPlaceSection(out, placeLead+placeHeading(fit(spendDaysLensWord+" · "+sort.word(), inner), pal))
	days := append([]session.DaySpend(nil), r.days...)
	sortDaysLens(days, sort)
	doors := map[int]session.DaySpend{}
	for _, day := range days {
		// QUIET DAYS STAY ON THE LIST. UsageByDay already fills empty buckets;
		// skipping them here would compress the fortnight and leave enter with
		// nowhere to land on a named quiet day (DESIGN.md §2).
		at := len(out)
		doors[at] = day
		out = append(out, placeLead+spendDayRow(day, inner, on(at), pal))
	}
	stops := make([]spendStop, len(out))
	for at, day := range doors {
		stops[at] = spendStop{ok: true, day: day}
	}
	stops[rails] = spendStop{ok: true, rails: true}
	return withSpendLensBar(out, stops, spendLensDays, width, pal)
}

func spendDayRow(day session.DaySpend, width int, lit bool, pal palette) string {
	left := tokens.GlyphProseBullet + " " + day.Label
	if width >= 72 {
		left += " " + tokenWord(day.Input) + " in · " + tokenWord(day.Output) + " out"
	} else if day.Tokens > 0 {
		left += " " + tokenWord(day.Tokens)
	}
	ink := placeFactInk(lit, pal)
	return spendSides(width, left, spendMoneyWord(day.USD), func(s string) string {
		return ink(s)
	}, placeMoneyInk(pal))
}

func (r spendReading) paintYearLens(width int, pal palette, lit func(int) bool) ([]string, []spendStop) {
	if width < 1 {
		return nil, nil
	}
	on := func(at int) bool { return lit != nil && lit(at) }
	inner := width - len(placeLead)
	var out []string
	rails := len(out)
	out = append(out, placeLead+r.railsRowIn(inner, placeFactInk(on(rails), pal)))
	out = append(out, r.windowHeaderRowFor(spendLensYear, width, pal))
	out = appendPlaceSection(out, placeLead+placeHeading(fit(spendYearLensWord, inner), pal))

	year := yearWindow(r.now)
	days := session.UsageByDay(r.yearLines(), year)
	heat := spendHeatmap(days, inner, pal)
	if heat != "" {
		for _, line := range strings.Split(heat, "\n") {
			out = append(out, placeLead+line)
		}
	}
	facts := r.yearFacts(days)
	for _, fact := range facts {
		out = append(out, placeLead+pal.dim(fit(fact, inner)))
	}
	// NO FACTS YET: keep a dim guide under the plane (or in its place) so a year
	// that has not priced a day is not a blank under the heading.
	if len(facts) == 0 {
		out = append(out, placeLead+pal.dim(fit(spendYearQuietWord, inner)))
	}
	stops := make([]spendStop, len(out))
	stops[rails] = spendStop{ok: true, rails: true}
	return withSpendLensBar(out, stops, spendLensYear, width, pal)
}

// yearLines is every priced line the reading was built from. The Year lens
// ignores the fortnight window on purpose: a heatmap of fourteen days is a
// sparkline with a different glyph, and the year is the question this lens asks.
func (r spendReading) yearLines() []session.UsageLine {
	return r.source
}

func (r spendReading) yearFacts(days []session.DaySpend) []string {
	var facts []string
	active := 0
	var loud session.DaySpend
	for _, day := range days {
		if day.USD > 0 {
			active++
			if day.USD > loud.USD {
				loud = day
			}
		}
	}
	if active > 0 {
		facts = append(facts, fmt.Sprintf("%d active days", active))
	}
	if loud.USD > 0 {
		facts = append(facts, loud.Label+" was the loudest day — "+spendMoneyWord(loud.USD))
	}
	if streak := session.ActiveStreak(r.source, r.now); streak > 0 {
		facts = append(facts, fmt.Sprintf("%d-day streak", streak))
	}
	if hour, ok := session.PeakHour(r.source); ok {
		facts = append(facts, fmt.Sprintf("peak hour %s", hourWord(hour)))
	}
	// FAVORITE IS THE YEAR'S DEAREST MODEL, not the fortnight window's. The Year
	// lens asks about the year; handing it r.models would answer a different
	// question whenever the window and the year disagreed.
	year := yearWindow(r.now)
	var yearPriced []session.UsageLine
	for _, line := range r.yearLines() {
		if line.USD > 0 && year.Holds(session.UsageLineDay(line)) {
			yearPriced = append(yearPriced, line)
		}
	}
	if models := session.UsageByModel(yearPriced); len(models) > 0 && models[0].Model != "" {
		facts = append(facts, "favorite · "+r.modelName(models[0].Model))
	}
	if r.totals.USD > 0 || active > 0 {
		// Prefer the year total when we have year days; otherwise the window.
		total := 0.0
		for _, day := range days {
			total += day.USD
		}
		if total > 0 {
			facts = append(facts, "this year "+spendMoneyWord(total))
		}
	}
	return facts
}

func hourWord(hour int) string {
	switch {
	case hour == 0:
		return "12am"
	case hour < 12:
		return fmt.Sprintf("%dam", hour)
	case hour == 12:
		return "12pm"
	default:
		return fmt.Sprintf("%dpm", hour-12)
	}
}

// spendHeatmap is a GitHub-style week×day grid over days, intensity from USD.
// No borders: rows of cells with a muted weekday gutter, months along the top
// when they fit. Empty days are a dim floor cell — present, not `$0.00`.
func spendHeatmap(days []session.DaySpend, width int, pal palette) string {
	if len(days) == 0 || width < 20 {
		return ""
	}
	max := 0.0
	for _, day := range days {
		if day.USD > max {
			max = day.USD
		}
	}
	// Lay days into weeks starting Monday.
	first := days[0].At
	for first.Weekday() != time.Monday {
		first = first.AddDate(0, 0, -1)
	}
	last := days[len(days)-1].At
	byDay := map[string]float64{}
	for _, day := range days {
		byDay[day.At.Format("2006-01-02")] = day.USD
	}
	weeks := 0
	for at := first; !at.After(last); at = at.AddDate(0, 0, 7) {
		weeks++
	}
	cellW := 1
	gutter := 4
	if gutter+weeks*cellW > width {
		// Keep the most recent weeks that fit.
		fit := (width - gutter) / cellW
		if fit < 1 {
			return ""
		}
		drop := weeks - fit
		first = first.AddDate(0, 0, drop*7)
		weeks = fit
	}
	steps := []string{"·", "░", "▒", "▓", "█"}
	cell := func(usd float64) string {
		if !(usd > 0) || max <= 0 {
			return pal.dim(steps[0])
		}
		n := int(usd/max*float64(len(steps)-1) + 0.5)
		if n < 1 {
			n = 1
		}
		if n >= len(steps) {
			n = len(steps) - 1
		}
		if n >= len(steps)/2 {
			return pal.data(steps[n])
		}
		return pal.dim(steps[n])
	}
	var lines []string
	// Month labels on a header row when there is room.
	if width >= gutter+weeks {
		var head strings.Builder
		head.WriteString(strings.Repeat(" ", gutter))
		prev := time.Month(0)
		for w := 0; w < weeks; w++ {
			at := first.AddDate(0, 0, w*7)
			if at.Month() != prev {
				head.WriteString(strings.ToLower(at.Format("Jan"))[:1])
				prev = at.Month()
			} else {
				head.WriteString(" ")
			}
		}
		lines = append(lines, pal.dim(head.String()))
	}
	weekdays := []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}
	for d := 0; d < 7; d++ {
		var row strings.Builder
		label := weekdays[d]
		if d%2 == 1 {
			label = "   "
		}
		row.WriteString(pal.dim(fit(label, gutter)))
		for w := 0; w < weeks; w++ {
			at := first.AddDate(0, 0, w*7+d)
			if at.Before(days[0].At) || at.After(last) {
				row.WriteString(" ")
				continue
			}
			row.WriteString(cell(byDay[at.Format("2006-01-02")]))
		}
		lines = append(lines, row.String())
	}
	return strings.Join(lines, "\n")
}
