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
