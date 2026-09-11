package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE SLASH WORDS ARE THE SAME LENSES THE KEYS CYCLE. An unknown word must not
// silently land on rhythm — `/spend opus` is a refuse, not a no-op.
func TestParseSpendLens(t *testing.T) {
	tests := []struct {
		word string
		want spendLens
		ok   bool
	}{
		{"", spendLensRhythm, true},
		{"rhythm", spendLensRhythm, true},
		{"models", spendLensModels, true},
		{"model", spendLensModels, true},
		{"days", spendLensDays, true},
		{"day", spendLensDays, true},
		{"daily", spendLensDays, true},
		{"year", spendLensYear, true},
		{"stats", spendLensYear, true},
		{"heatmap", spendLensYear, true},
		{"MODELS", spendLensModels, true},
		{"opus", 0, false},
		{"cost", 0, false},
	}
	for _, tt := range tests {
		got, ok := parseSpendLens(tt.word)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Fatalf("parseSpendLens(%q) = %v, %v; want %v, %v", tt.word, got, ok, tt.want, tt.ok)
		}
	}
}

// `[` AND `]` WALK THE SAME ORDER AND WRAP. The cycle is the contract the foot
// names — one axis, both directions — so the ends must meet each other.
func TestSpendLensCycle(t *testing.T) {
	order := []spendLens{spendLensRhythm, spendLensModels, spendLensDays, spendLensYear}
	for i, at := range order {
		if got := at.next(); got != order[(i+1)%len(order)] {
			t.Fatalf("%s.next() = %s, want %s", at.word(), got.word(), order[(i+1)%len(order)].word())
		}
		if got := at.prev(); got != order[(i-1+len(order))%len(order)] {
			t.Fatalf("%s.prev() = %s, want %s", at.word(), got.word(), order[(i-1+len(order))%len(order)].word())
		}
	}
	if spendLensRhythm.prev() != spendLensYear {
		t.Fatal("rhythm.prev does not wrap onto year")
	}
	if spendLensYear.next() != spendLensRhythm {
		t.Fatal("year.next does not wrap onto rhythm")
	}
}

// SORT BY TOKENS IS NOT SORT BY COST. The fixture's sonnet ran more tokens than
// opus while costing less — tokens order must put the busier model first.
func TestModelsLensRowsSortByTokens(t *testing.T) {
	r := spendReading{
		now: spendTestNow,
		models: []session.ModelSpend{
			{Model: "dear-few", Tokens: 100, USD: 9},
			{Model: "cheap-many", Tokens: 900, USD: 2},
			{Model: "mid", Tokens: 400, USD: 4},
		},
	}
	byCost := r.modelsLensRows(spendGroupModel, spendSortCost)
	if len(byCost) != 3 || byCost[0].name != "dear-few" {
		t.Fatalf("cost sort = %#v, want dear-few first", byCost)
	}
	byTokens := r.modelsLensRows(spendGroupModel, spendSortTokens)
	if len(byTokens) != 3 || byTokens[0].name != "cheap-many" {
		t.Fatalf("tokens sort = %#v, want cheap-many first", byTokens)
	}
	if byTokens[0].tokens < byTokens[1].tokens || byTokens[1].tokens < byTokens[2].tokens {
		t.Fatalf("tokens sort is not descending: %#v", byTokens)
	}
}

// ENTER ON A DAYS ROW DRILLS INTO THAT DAY'S RHYTHM. The lens returns to rhythm
// and the window collapses to the one bucket the row named.
func TestDaysLensEnterDrillsToRhythm(t *testing.T) {
	a := spendLab(t, spendFixture())
	a.setSpendLens(spendLensDays)
	if a.spend.lens != spendLensDays {
		t.Fatalf("lens is %s, want days", a.spend.lens.word())
	}
	dayAt := -1
	var day session.DaySpend
	for i, stop := range a.spend.stops {
		if stop.ok && stop.day.USD > 0 && !stop.day.At.IsZero() {
			dayAt = i
			day = stop.day
			break
		}
	}
	if dayAt < 0 {
		t.Fatalf("the days lens has no day door:\n%s", placeFrameText(a))
	}
	a.spend.cursor = dayAt
	if _, ok := a.openSpendRow(); !ok {
		t.Fatal("enter on a day row opened nothing")
	}
	if a.spend.lens != spendLensRhythm {
		t.Fatalf("after the drill the lens is %s, want rhythm", a.spend.lens.word())
	}
	win := a.spend.win
	if win.Grain != session.GrainDay {
		t.Fatalf("grain is %q, want day", win.Grain)
	}
	if !win.From.Equal(day.At) || !win.To.Equal(day.At) {
		t.Fatalf("window is %v–%v, want the day %v", win.From, win.To, day.At)
	}
	if win.Buckets() != 1 {
		t.Fatalf("window has %d buckets, want one day", win.Buckets())
	}
}

// ESC AFTER A DAY DRILL RETURNS TO DAYS ON THAT DAY. The window and lens come
// back, and the cursor lands on the day that was opened — not on the loudest
// day of the restored window.
func TestDaysLensEscReturnsToTheDay(t *testing.T) {
	a := spendLab(t, spendFixture())
	a.setSpendLens(spendLensDays)
	dayAt := -1
	var day session.DaySpend
	for i, stop := range a.spend.stops {
		if stop.ok && stop.day.USD > 0 && !stop.day.At.IsZero() {
			dayAt = i
			day = stop.day
			break
		}
	}
	if dayAt < 0 {
		t.Fatal("no priced day door")
	}
	a.spend.cursor = dayAt
	if _, ok := a.openSpendRow(); !ok {
		t.Fatal("drill failed")
	}
	drive(t, a, key("esc"))
	if a.spend.lens != spendLensDays {
		t.Fatalf("esc left the lens on %s, want days", a.spend.lens.word())
	}
	if a.spend.drilled {
		t.Fatal("esc left the drill flag set")
	}
	stop := a.spendStopAt(a.spend.cursor)
	if !stop.ok || stop.day.At.IsZero() || !sameSpendBucket(stop.day.At, day.At, session.GrainDay) {
		t.Fatalf("cursor is on %#v, want the day %v", stop.day, day.At)
	}
}

// A QUIET DAY IS STILL A DOOR. Emptiness draws no figure; enter still drills
// into that day's rhythm window.
func TestQuietDayStillDrills(t *testing.T) {
	a := spendLab(t, spendFixture())
	a.setSpendLens(spendLensDays)
	quietAt := -1
	var quiet session.DaySpend
	for i, stop := range a.spend.stops {
		if stop.ok && !stop.day.At.IsZero() && !(stop.day.USD > 0) {
			quietAt = i
			quiet = stop.day
			break
		}
	}
	if quietAt < 0 {
		t.Fatalf("the days lens has no quiet day:\n%s", placeFrameText(a))
	}
	a.spend.cursor = quietAt
	if _, ok := a.openSpendRow(); !ok {
		t.Fatal("enter on a quiet day opened nothing")
	}
	if a.spend.lens != spendLensRhythm {
		t.Fatalf("after the quiet drill the lens is %s", a.spend.lens.word())
	}
	if !a.spend.win.From.Equal(quiet.At) {
		t.Fatalf("window is %v, want quiet day %v", a.spend.win.From, quiet.At)
	}
}

// THE HEAD ROW NAMES THE LENS. Foot keys stay `[ ] lenses` without repeating
// the asker's word — said once, on the head.
func TestSpendLensNamedOnTheHead(t *testing.T) {
	a := spendLab(t, spendFixture())
	for _, lens := range []spendLens{spendLensRhythm, spendLensModels, spendLensDays, spendLensYear} {
		a.setSpendLens(lens)
		rows, _ := a.spend.reading.paintLens(lens, spendGroupModel, spendSortCost, 120, newPalette(tokens.NoColor, false), nil)
		if len(rows) < 2 {
			t.Fatalf("%s painted %d rows", lens.word(), len(rows))
		}
		head := plain(rows[1])
		if !strings.Contains(head, lens.word()+" · ") {
			t.Fatalf("%s head does not name the lens: %q", lens.word(), head)
		}
		foot := (placeSpend{}).hint(a)
		if strings.Contains(foot, lens.word()+" · "+spendLensWord) {
			t.Fatalf("%s foot still repeats the lens word: %q", lens.word(), foot)
		}
		if !strings.Contains(foot, spendLensWord) {
			t.Fatalf("%s foot dropped the cycle keys: %q", lens.word(), foot)
		}
	}
}

// `/spend month` IS THE CURRENT CALENDAR MONTH, not the last thirty days.
func TestSpendMonthIsTheCalendarMonth(t *testing.T) {
	a := spendLab(t, spendFixture())
	now := a.now().Local()
	a.runSpendCommand("month")
	win := a.spend.win
	if win.Grain != session.GrainMonth {
		t.Fatalf("grain is %q, want month", win.Grain)
	}
	if win.From.Year() != now.Year() || win.From.Month() != now.Month() || win.From.Day() != 1 {
		t.Fatalf("month window starts %v, want the 1st of %v", win.From, now.Month())
	}
	if !win.From.Equal(win.To) {
		t.Fatalf("a one-month window has From≠To: %v–%v", win.From, win.To)
	}
	if win.Buckets() != 1 {
		t.Fatalf("month window has %d buckets, want 1", win.Buckets())
	}
	// Holds covers the whole calendar month even though To is the 1st.
	mid := time.Date(now.Year(), now.Month(), 15, 12, 0, 0, 0, now.Location())
	if !win.Holds(mid) {
		t.Fatalf("month window does not hold mid-month %v", mid)
	}
}

// AN UNKNOWN `/spend` ARG IS ONE REFUSE LINE, and the place is not opened as a
// second editor.
func TestSpendUnknownArgRefuses(t *testing.T) {
	a := spendLab(t, spendFixture())
	page := a.page
	a.runSpendCommand("opus")
	if a.page != page {
		t.Fatalf("unknown /spend opened %v", a.page)
	}
	got := lastNote(t, a)
	if !strings.Contains(got, "usage: /spend") {
		t.Fatalf("refuse did not name the accepted args: %q", got)
	}
}

// THE YEAR LENS DRAWS A HEATMAP OR NAMES ITS ACTIVE DAYS. A multi-day fixture
// must leave either the "active days" fact or the cell glyphs on the body —
// never an empty year that looks like an empty machine.
func TestYearLensDrawsHeatmap(t *testing.T) {
	lines := []session.UsageLine{
		{At: time.Date(2026, time.August, 10, 12, 0, 0, 0, time.Local), Model: "opus 4.1",
			Calls: 2, Input: 1000, Output: 200, USD: 1.5},
		{At: time.Date(2026, time.August, 18, 12, 0, 0, 0, time.Local), Model: "sonnet 4.5",
			Calls: 4, Input: 2000, Output: 400, USD: 2.25},
		{At: time.Date(2026, time.August, 25, 12, 0, 0, 0, time.Local), Model: "haiku 4.5",
			Calls: 1, Input: 500, Output: 50, USD: 0.4},
	}
	a := spendLab(t, lines)
	a.setSpendLens(spendLensYear)
	if a.spend.lens != spendLensYear {
		t.Fatalf("lens is %s, want year", a.spend.lens.word())
	}
	text := placeFrameText(a)
	rows, _ := a.spend.reading.paintLens(spendLensYear, spendGroupModel, spendSortCost, 120, newPalette(tokens.NoColor, false), nil)
	body := strings.Join(plainSpendRows(rows), "\n")
	hasActive := strings.Contains(text, "active days") || strings.Contains(body, "active days")
	hasCells := strings.ContainsAny(ansi.Strip(body), "░▒▓█")
	if !hasActive && !hasCells {
		t.Fatalf("year lens shows neither active days nor heatmap cells:\n%s\n---\n%s", text, body)
	}
}
