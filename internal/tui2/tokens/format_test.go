package tokens

import (
	"math"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// The formatting gate (8.2.20, 10.2.8, 5.21).
//
// Two properties are the whole point of this file and neither is checkable by
// reading the code:
//
//   - WIDTH-STABILITY. Every live cell must render at a fixed width so nothing
//     to the right of it ever dances. The tests below sweep decades of values
//     through every ladder and measure the result with the same ANSI-aware
//     ruler the renderer uses — not len(), which would miss that "—" is three
//     bytes and one cell.
//   - ALLOCATION-FREEDOM. A rail with twenty rows repaints on a timer. The
//     Append forms must write into a caller's buffer and allocate nothing, and
//     testing.AllocsPerRun proves it rather than a comment claiming it.

// cells measures a formatted cell the way a terminal will.
func cells(s string) int { return ansi.StringWidth(s) }

// TestLaddersMatchTheStatedExamples pins the exact forms 8.2.20 names. These
// are the strings the doc promises; if one changes, the doc changed.
func TestLaddersMatchTheStatedExamples(t *testing.T) {
	countCases := []struct {
		in   int64
		want string
	}{
		{0, "0"}, {7, "7"}, {999, "999"},
		{1000, "1K"}, {1500, "1.5K"}, {1549, "1.5K"}, {1550, "1.6K"},
		{9950, "10K"}, {25_000, "25K"}, {25_400, "25K"}, {25_500, "26K"},
		{999_000, "999K"}, {999_500, "1M"},
		{1_000_000, "1M"}, {1_200_000, "1.2M"}, {12_000_000, "12M"},
		{1_000_000_000, "1G"}, {1_000_000_000_000, "1T"},
		{-5, "0"}, // a negative count is not a count
	}
	for _, c := range countCases {
		if got := Count(c.in); got != c.want {
			t.Errorf("Count(%d) = %q, want %q", c.in, got, c.want)
		}
	}

	durationCases := []struct {
		in   time.Duration
		want string
	}{
		{0, "0s"},
		{4500 * time.Millisecond, "4.5s"},
		{9900 * time.Millisecond, "9.9s"},
		{10 * time.Second, "10s"},
		{45 * time.Second, "45s"},
		{3*time.Minute + 12*time.Second, "3m12s"},
		{59*time.Minute + 59*time.Second, "59m59s"},
		{2*time.Hour + 14*time.Minute, "2h14m"},
		{25 * time.Hour, "1d01h"},
		{-time.Second, "0s"},
	}
	for _, c := range durationCases {
		if got := Duration(c.in); got != c.want {
			t.Errorf("Duration(%v) = %q, want %q", c.in, got, c.want)
		}
	}

	if got := Context(51_000, 1_000_000); got != "5.1%/1M" {
		t.Errorf("Context(51000, 1M) = %q, want %q", got, "5.1%/1M")
	}
	if got := Context(0, 0); got != GlyphMissing {
		t.Errorf("Context with an unknown window = %q, want %q — unknown is not zero (10.2.8)",
			got, GlyphMissing)
	}
}

// TestCellsAreWidthStable is 5.21's law, swept. Every value a formatter can be
// handed in this product's lifetime must land inside its stated cell width.
func TestCellsAreWidthStable(t *testing.T) {
	// Counts: every decade, and the boundaries either side of each rung.
	for n := int64(0); n < 2_000_000_000_000; n = next(n) {
		for _, v := range []int64{n - 1, n, n + 1} {
			if w := cells(CountCell(v)); w != CountCellWidth {
				t.Fatalf("CountCell(%d) = %q is %d cells, want %d", v, CountCell(v), w, CountCellWidth)
			}
		}
	}
	// The extremes, where the clamp is the only thing holding the promise.
	for _, v := range []int64{countClamp - 1, countClamp, countClamp + 1, math.MaxInt64, math.MinInt64} {
		if w := cells(CountCell(v)); w != CountCellWidth {
			t.Fatalf("CountCell(%d) = %q is %d cells, want %d", v, CountCell(v), w, CountCellWidth)
		}
	}
	// Durations: milliseconds through years, on a geometric sweep plus the
	// exact rung boundaries where the granularity switches.
	boundaries := []time.Duration{
		0, time.Millisecond, 999 * time.Millisecond,
		9999 * time.Millisecond, 10 * time.Second,
		59*time.Second + 999*time.Millisecond, time.Minute,
		59*time.Minute + 59*time.Second, time.Hour,
		23*time.Hour + 59*time.Minute, 24 * time.Hour,
		99*24*time.Hour + 23*time.Hour, 100 * 24 * time.Hour,
		4000 * 24 * time.Hour,
	}
	for d := time.Millisecond; d < 4000*24*time.Hour; d = d*3/2 + time.Millisecond {
		boundaries = append(boundaries, d)
	}
	for _, d := range boundaries {
		for _, v := range []time.Duration{d - time.Millisecond, d, d + time.Millisecond} {
			if w := cells(DurationCell(v)); w != DurationCellWidth {
				t.Fatalf("DurationCell(%v) = %q is %d cells, want %d", v, DurationCell(v), w, DurationCellWidth)
			}
			if w := cells(ElapsedCell(v)); w != ElapsedCellWidth {
				t.Fatalf("ElapsedCell(%v) = %q is %d cells, want %d", v, ElapsedCell(v), w, ElapsedCellWidth)
			}
		}
	}
	// Percentages and the context form across the whole domain, including the
	// values that cannot be rendered honestly.
	windows := []int64{0, 1, 1000, 8192, 128_000, 200_000, 1_000_000, 10_000_000}
	for i := range 1001 {
		f := float64(i) / 1000
		if w := cells(PercentCell(f)); w != PercentCellWidth {
			t.Fatalf("PercentCell(%v) = %q is %d cells, want %d", f, PercentCell(f), w, PercentCellWidth)
		}
		for _, win := range windows {
			used := int64(f * float64(win))
			if w := cells(ContextCell(used, win)); w != ContextCellWidth {
				t.Fatalf("ContextCell(%d, %d) = %q is %d cells, want %d",
					used, win, ContextCell(used, win), w, ContextCellWidth)
			}
		}
	}
	for _, f := range []float64{-1, 2, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if w := cells(PercentCell(f)); w != PercentCellWidth {
			t.Fatalf("PercentCell(%v) = %q is %d cells, want %d", f, PercentCell(f), w, PercentCellWidth)
		}
	}
	// Money, from zero to the ladder.
	for _, usd := range []float64{0, 0.004, 0.005, 0.01, 8.65, 99.994, 999.99, 1000, 1234, 999_000, math.NaN()} {
		if w := cells(MoneyCell(usd)); w != MoneyCellWidth {
			t.Fatalf("MoneyCell(%v) = %q is %d cells, want %d", usd, MoneyCell(usd), w, MoneyCellWidth)
		}
	}
	// The missing mark fills whatever column it is asked for.
	for w := 1; w <= 16; w++ {
		if got := cells(MissingCell(w)); got != w {
			t.Fatalf("MissingCell(%d) is %d cells", w, got)
		}
	}
}

// next walks a decade sweep that lands on every rung boundary of the count
// ladder without iterating two trillion times.
func next(n int64) int64 {
	switch {
	case n == 0:
		return 1
	case n < 1000:
		return n * 2
	default:
		return n*3/2 + 1
	}
}

// TestElapsedNeverJitters is the specific promise of 5.21's "elapsed that
// ages": the unit granularity switches AT FIXED WIDTH. Walking one second at a
// time across every switch point proves the column never moves.
func TestElapsedNeverJitters(t *testing.T) {
	switches := []time.Duration{time.Minute, time.Hour, 24 * time.Hour}
	for _, s := range switches {
		for d := s - 3*time.Second; d <= s+3*time.Second; d += time.Second {
			if w := cells(ElapsedCell(d)); w != ElapsedCellWidth {
				t.Fatalf("ElapsedCell(%v) = %q is %d cells across the switch at %v",
					d, ElapsedCell(d), w, s)
			}
		}
	}
}

// TestDurationTruncatesAndSubMinuteRounds pins the deliberate asymmetry in
// AppendDuration: sub-minute readings round (a person judges "fast" at tenths),
// and readings above a minute truncate, because an elapsed number that rounds
// up has told a small lie about work that has not happened yet.
func TestDurationTruncatesAndSubMinuteRounds(t *testing.T) {
	if got := Duration(4449 * time.Millisecond); got != "4.4s" {
		t.Errorf("Duration(4.449s) = %q, want 4.4s", got)
	}
	if got := Duration(4450 * time.Millisecond); got != "4.5s" {
		t.Errorf("Duration(4.450s) = %q, want 4.5s (sub-minute rounds)", got)
	}
	if got := Duration(3*time.Minute + 12*time.Second + 999*time.Millisecond); got != "3m12s" {
		t.Errorf("Duration(3m12.999s) = %q, want 3m12s (above a minute truncates)", got)
	}
	if got := Duration(2*time.Hour + 14*time.Minute + 59*time.Second); got != "2h14m" {
		t.Errorf("Duration(2h14m59s) = %q, want 2h14m", got)
	}
}

// TestMissingIsNeverAnEstimate is 10.2.8 made mechanical: a value that does not
// exist renders "—". It is never zero (a number that has not arrived and a
// number that is zero are different facts) and never a guess.
func TestMissingIsNeverAnEstimate(t *testing.T) {
	if got := Percent(math.NaN()); got != GlyphMissing {
		t.Errorf("Percent(NaN) = %q, want %q", got, GlyphMissing)
	}
	if got := Money(math.NaN()); got != GlyphMissing {
		t.Errorf("Money(NaN) = %q, want %q", got, GlyphMissing)
	}
	if got := MoneyDime(math.NaN()); got != GlyphMissing {
		t.Errorf("MoneyDime(NaN) = %q, want %q", got, GlyphMissing)
	}
	if got := Context(500, 0); got != GlyphMissing {
		t.Errorf("Context with no window = %q, want %q", got, GlyphMissing)
	}
	if Percent(0) == GlyphMissing || Money(0) == GlyphMissing {
		t.Error("zero must render as zero; only absent data renders the missing mark")
	}
	// The honesty mark and its column allowance.
	if AppendEstimate(nil); string(AppendEstimate(nil)) != GlyphEstimate {
		t.Error("the estimate mark is ~ (10.2.8)")
	}
	if EstimateWidth(CountCellWidth) != CountCellWidth+1 {
		t.Error("an estimate column must reserve one more cell than a measurement column")
	}
}

// TestMoneyForms pins the two resolutions and the reason they differ (12.4.1):
// exact cents for anything a person reads, dimes for anything written into a
// model's context, where a figure may only be as precise as it is stable.
func TestMoneyForms(t *testing.T) {
	cases := []struct {
		usd         float64
		money, dime string
	}{
		{0, "$0.00", "$0.00"},
		{8.65, "$8.65", "$8.70"},
		{8.64, "$8.64", "$8.60"},
		{0.004, "$0.00", "$0.00"},
		{999.994, "$999.99", "$1000.00"},
		{1000, "$1K", "$1000.00"},
		{1234, "$1.2K", "$1234.00"},
	}
	for _, c := range cases {
		if got := Money(c.usd); got != c.money {
			t.Errorf("Money(%v) = %q, want %q", c.usd, got, c.money)
		}
		if got := MoneyDime(c.usd); got != c.dime {
			t.Errorf("MoneyDime(%v) = %q, want %q", c.usd, got, c.dime)
		}
	}
}

// TestElapsedToken is 5.21's ageing rule: dim inside the estimate, one tier
// brighter past it, and never amber — amber means a human is needed (5.16), and
// a slow worker is not asking for anything.
func TestElapsedToken(t *testing.T) {
	if got := ElapsedToken(30*time.Second, 0); got != TextTertiary {
		t.Errorf("with no estimate the reading stays chrome, got %s", got)
	}
	if got := ElapsedToken(30*time.Second, time.Minute); got != TextTertiary {
		t.Errorf("inside the estimate the reading stays chrome, got %s", got)
	}
	got := ElapsedToken(2*time.Minute, time.Minute)
	if got != Promote(TextTertiary) {
		t.Errorf("past the estimate the reading brightens one tier, got %s", got)
	}
	if got == Amber {
		t.Error("a slow worker must never turn amber")
	}
}

// TestAppendFormsDoNotAllocate is the hot-path contract. Token lookups and cell
// formatting sit inside the render loop; a formatter that allocates wakes the
// collector once per frame per row.
func TestAppendFormsDoNotAllocate(t *testing.T) {
	buf := make([]byte, 0, 64)
	checks := map[string]func(){
		"AppendCount":        func() { _ = AppendCount(buf[:0], 1_234_567) },
		"AppendCountCell":    func() { _ = AppendCountCell(buf[:0], 1_234_567) },
		"AppendDuration":     func() { _ = AppendDuration(buf[:0], 3*time.Minute+12*time.Second) },
		"AppendDurationCell": func() { _ = AppendDurationCell(buf[:0], 3*time.Minute+12*time.Second) },
		"AppendElapsedCell":  func() { _ = AppendElapsedCell(buf[:0], 90*time.Minute) },
		"AppendPercentCell":  func() { _ = AppendPercentCell(buf[:0], 0.051) },
		"AppendContextCell":  func() { _ = AppendContextCell(buf[:0], 51_000, 1_000_000) },
		"AppendMoneyCell":    func() { _ = AppendMoneyCell(buf[:0], 8.65) },
		"AppendMoneyDime":    func() { _ = AppendMoneyDime(buf[:0], 8.65) },
		"AppendMissingCell":  func() { _ = AppendMissingCell(buf[:0], 6) },
	}
	for name, fn := range checks {
		if n := testing.AllocsPerRun(200, fn); n != 0 {
			t.Errorf("%s allocates %.1f times per call; the Append forms must be allocation-free", name, n)
		}
	}
}

// TestTokenLookupsDoNotAllocate: the palette is a table computed once, so
// resolving a colour on the render path must cost nothing but a bounds check.
func TestTokenLookupsDoNotAllocate(t *testing.T) {
	checks := map[string]func(){
		"Token.Fg":     func() { _ = Amber.Fg(TrueColor, FocusNormal) },
		"Token.Bg":     func() { _ = Band.Bg(ANSI256, FocusDimmed) },
		"Token.Color":  func() { _ = Cyan.Color(FocusDimmed) },
		"Token.Index":  func() { _ = Green.Index(ANSI256, FocusNormal) },
		"ResolveToken": func() { _ = ResolveToken(HueBroken, StateLive) },
		"IdentityFor":  func() { _ = IdentityFor("wisp-parity") },
		"IdentityNext": func() { _ = IdentityNext("wisp-parity", Identity3) },
		"Gauge":        func() { _ = Gauge(0.62) },
		"Spinner":      func() { _ = Spinner(7) },
		"ContextToken": func() { _ = ContextToken(120_000, 200_000) },
	}
	for name, fn := range checks {
		if n := testing.AllocsPerRun(200, fn); n != 0 {
			t.Errorf("%s allocates %.1f times per call", name, n)
		}
	}
}

// TestElapsedFormsAgree: the string form is a convenience over the Append form
// and must never drift from it, since a caller picks between them on cost
// grounds and not on meaning.
func TestElapsedFormsAgree(t *testing.T) {
	for _, d := range []time.Duration{
		0, 45 * time.Second, 5 * time.Minute, 90 * time.Minute,
		25 * time.Hour, 120 * 24 * time.Hour, -time.Second,
	} {
		if got, want := Elapsed(d), string(AppendElapsed(nil, d)); got != want {
			t.Errorf("Elapsed(%v) = %q, AppendElapsed says %q", d, got, want)
		}
		if got, want := Duration(d), string(AppendDuration(nil, d)); got != want {
			t.Errorf("Duration(%v) = %q, AppendDuration says %q", d, got, want)
		}
		if got, want := ElapsedCell(d), string(AppendElapsedCell(nil, d)); got != want {
			t.Errorf("ElapsedCell(%v) = %q, the Append form says %q", d, got, want)
		}
	}
	// The aging form drops the trailing unit once the reading has two parts,
	// which is what keeps the granularity switch at a fixed width (5.21).
	if got := Elapsed(90 * time.Minute); got != "1h30" {
		t.Errorf("Elapsed(90m) = %q, want 1h30", got)
	}
	if got := Elapsed(30 * time.Second); got != "30s" {
		t.Errorf("Elapsed(30s) = %q, want 30s", got)
	}
	// A clamped reading is still a reading: 5.21's column must not grow for a
	// job that has been running for a year.
	if got := Elapsed(4000 * 24 * time.Hour); got != "99d23" {
		t.Errorf("Elapsed(4000d) = %q, want the clamped 99d23", got)
	}
}
