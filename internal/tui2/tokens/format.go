package tokens

import (
	"math"
	"time"
)

// Formatting laws (8.2.20, 10.2.8), as functions rather than as sprintf calls
// scattered through renders.
//
// Two properties are non-negotiable and both are tested:
//
//   - WIDTH-STABLE (5.21). Every live cell renders at a fixed width so nothing
//     to the right of it ever dances. The Cell forms below pad to a constant,
//     and format_test.go walks decades of values proving each stays inside it.
//   - ALLOCATION-FREE. The Append forms write into a caller-owned buffer and
//     allocate nothing; the string forms are one-allocation conveniences for
//     cold paths. A rail with twenty rows repaints on a timer, and a formatter
//     that allocates is a formatter that wakes the collector once per frame.
//
// Everything here is total: no error returns, no panics, no NaN escaping into a
// frame. A value a formatter cannot honestly render becomes [GlyphMissing] —
// never an estimate (10.2.8).

// Cell widths. These are display cells, not bytes; every numeric form is ASCII,
// and [GlyphMissing] is the one multi-byte single-cell exception.
const (
	// CountCellWidth fits "999", "1.5K", "25K", "1.2M" — the ladder never
	// produces more than four cells.
	CountCellWidth = 4
	// DurationCellWidth fits "4.5s", "45s", "3m12s", "59m59s", "2h14m",
	// "99d23h".
	DurationCellWidth = 6
	// ElapsedCellWidth fits "45s", "5m", "1h02", "99d23" — the aging form,
	// which drops the trailing unit so the granularity switch never jitters.
	ElapsedCellWidth = 5
	// PercentCellWidth fits "5.1%" and "100%".
	PercentCellWidth = 4
	// ContextCellWidth fits "5.1%/1M" and "100%/128K".
	ContextCellWidth = 9
	// MoneyCellWidth fits "$999.99" and "$1.2K".
	MoneyCellWidth = 7
)

// countClamp is the largest count the ladder renders honestly inside
// [CountCellWidth]. Nothing this product counts — tokens, steps, workers,
// bytes — comes near it; clamping keeps the width promise total instead of
// almost-total.
const countClamp = 999_499_999_999_999

var countSuffix = [...]byte{'K', 'M', 'G', 'T'}

// AppendCount writes the number ladder (8.2.20): 999, 1.5K, 25K, 1M. A rung
// with a leading digit under ten keeps one decimal; above that the decimal is
// noise on a number nobody reads that precisely. A trailing ".0" is always
// dropped, so a round million is "1M".
func AppendCount(dst []byte, n int64) []byte {
	if n < 0 {
		n = 0
	}
	if n > countClamp {
		n = countClamp
	}
	if n < 1000 {
		return appendInt(dst, n)
	}
	unit := 0
	div := int64(1000)
	for unit < len(countSuffix)-1 && n >= div*1000 {
		div *= 1000
		unit++
	}
	whole := n / div
	if whole < 10 {
		rem := n % div
		tenths := (rem*10 + div/2) / div
		if tenths >= 10 {
			whole++
			tenths = 0
		}
		dst = appendInt(dst, whole)
		if tenths != 0 && whole < 10 {
			dst = append(dst, '.', byte('0'+tenths))
		}
		return append(dst, countSuffix[unit])
	}
	rounded := (n + div/2) / div
	if rounded >= 1000 && unit < len(countSuffix)-1 {
		unit++
		div *= 1000
		rounded = (n + div/2) / div
	}
	dst = appendInt(dst, rounded)
	return append(dst, countSuffix[unit])
}

// Count is [AppendCount] as a string.
func Count(n int64) string {
	var buf [8]byte
	return string(AppendCount(buf[:0], n))
}

// AppendCountCell is [AppendCount] right-aligned in [CountCellWidth].
func AppendCountCell(dst []byte, n int64) []byte {
	var buf [8]byte
	return appendCell(dst, AppendCount(buf[:0], n), CountCellWidth)
}

// CountCell is [AppendCountCell] as a string.
func CountCell(n int64) string {
	var buf [CountCellWidth]byte
	return string(AppendCountCell(buf[:0], n))
}

// AppendDuration writes the duration ladder (8.2.20): 4.5s, 3m12s, 2h14m.
// Sub-minute durations keep a tenth because that is the resolution at which a
// person judges "fast"; above a minute the seconds are structure, not
// precision, so they are truncated rather than rounded — an elapsed reading
// that rounds up has told a small lie about work that has not happened yet.
func AppendDuration(dst []byte, d time.Duration) []byte {
	if d < 0 {
		d = 0
	}
	switch {
	case d < 10*time.Second:
		tenths := int64((d + 50*time.Millisecond) / (100 * time.Millisecond))
		if tenths >= 100 {
			return append(dst, '1', '0', 's')
		}
		dst = appendInt(dst, tenths/10)
		if t := tenths % 10; t != 0 {
			dst = append(dst, '.', byte('0'+t))
		}
		return append(dst, 's')
	case d < time.Minute:
		secs := int64((d + 500*time.Millisecond) / time.Second)
		if secs >= 60 {
			return append(dst, '1', 'm', '0', '0', 's')
		}
		dst = appendInt(dst, secs)
		return append(dst, 's')
	case d < time.Hour:
		dst = appendInt(dst, int64(d/time.Minute))
		dst = append(dst, 'm')
		dst = appendPad2(dst, int64((d%time.Minute)/time.Second))
		return append(dst, 's')
	case d < 24*time.Hour:
		dst = appendInt(dst, int64(d/time.Hour))
		dst = append(dst, 'h')
		dst = appendPad2(dst, int64((d%time.Hour)/time.Minute))
		return append(dst, 'm')
	default:
		days := int64(d / (24 * time.Hour))
		hours := int64((d % (24 * time.Hour)) / time.Hour)
		if days > 99 {
			days, hours = 99, 23
		}
		dst = appendInt(dst, days)
		dst = append(dst, 'd')
		dst = appendPad2(dst, hours)
		return append(dst, 'h')
	}
}

// Duration is [AppendDuration] as a string.
func Duration(d time.Duration) string {
	var buf [DurationCellWidth]byte
	return string(AppendDuration(buf[:0], d))
}

// AppendDurationCell is [AppendDuration] right-aligned in [DurationCellWidth].
func AppendDurationCell(dst []byte, d time.Duration) []byte {
	var buf [DurationCellWidth]byte
	return appendCell(dst, AppendDuration(buf[:0], d), DurationCellWidth)
}

// DurationCell is [AppendDurationCell] as a string.
func DurationCell(d time.Duration) string {
	var buf [DurationCellWidth]byte
	return string(AppendDurationCell(buf[:0], d))
}

// AppendElapsed writes the aging form (5.21): 45s, 5m, 1h02, 99d23. The unit
// granularity switches at fixed width and the trailing unit is dropped once the
// reading has two parts, so a row that crosses an hour does not shove the
// column beside it. This is the form for live regions only — a committed
// transcript row freezes its elapsed at finalization (8.1.2) and never ages.
func AppendElapsed(dst []byte, d time.Duration) []byte {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		dst = appendInt(dst, int64(d/time.Second))
		return append(dst, 's')
	case d < time.Hour:
		dst = appendInt(dst, int64(d/time.Minute))
		return append(dst, 'm')
	case d < 24*time.Hour:
		dst = appendInt(dst, int64(d/time.Hour))
		dst = append(dst, 'h')
		return appendPad2(dst, int64((d%time.Hour)/time.Minute))
	default:
		days := int64(d / (24 * time.Hour))
		hours := int64((d % (24 * time.Hour)) / time.Hour)
		if days > 99 {
			days, hours = 99, 23
		}
		dst = appendInt(dst, days)
		dst = append(dst, 'd')
		return appendPad2(dst, hours)
	}
}

// Elapsed is [AppendElapsed] as a string.
func Elapsed(d time.Duration) string {
	var buf [ElapsedCellWidth]byte
	return string(AppendElapsed(buf[:0], d))
}

// AppendElapsedCell is [AppendElapsed] right-aligned in [ElapsedCellWidth].
func AppendElapsedCell(dst []byte, d time.Duration) []byte {
	var buf [ElapsedCellWidth]byte
	return appendCell(dst, AppendElapsed(buf[:0], d), ElapsedCellWidth)
}

// ElapsedCell is [AppendElapsedCell] as a string.
func ElapsedCell(d time.Duration) string {
	var buf [ElapsedCellWidth]byte
	return string(AppendElapsedCell(buf[:0], d))
}

// ElapsedToken implements "elapsed that ages" (5.21): dim while the work is
// inside its estimate, one tier brighter once it is past. It brightens; it
// never turns amber — amber means a human is needed (5.16), and a slow worker
// is not asking for anything. With no estimate the reading stays chrome.
func ElapsedToken(elapsed, estimate time.Duration) Token {
	if estimate > 0 && elapsed > estimate {
		return Promote(TextTertiary)
	}
	return TextTertiary
}

// AppendPercent writes a fraction as a percentage: 5.1% below ten, 62% above,
// 100% at the top. One decimal under ten because that is where a tenth of a
// point is still a visible amount of context; above it the decimal is churn.
func AppendPercent(dst []byte, fraction float64) []byte {
	if math.IsNaN(fraction) {
		return append(dst, GlyphMissing...)
	}
	switch {
	case fraction < 0:
		fraction = 0
	case fraction > 1:
		fraction = 1
	}
	tenths := int64(math.Round(fraction * 1000))
	if tenths < 100 {
		dst = appendInt(dst, tenths/10)
		if t := tenths % 10; t != 0 {
			dst = append(dst, '.', byte('0'+t))
		}
		return append(dst, '%')
	}
	dst = appendInt(dst, (tenths+5)/10)
	return append(dst, '%')
}

// Percent is [AppendPercent] as a string.
func Percent(fraction float64) string {
	var buf [PercentCellWidth]byte
	return string(AppendPercent(buf[:0], fraction))
}

// AppendPercentCell is [AppendPercent] right-aligned in [PercentCellWidth].
// It pads by CELLS, not by bytes: AppendPercent renders [GlyphMissing] for a
// fraction that does not exist, and that mark is three bytes wide and one cell
// wide. Padding it by byte length would leave the column two cells short —
// which is exactly the dancing-neighbour bug 5.21 forbids, arriving only when a
// number goes missing.
func AppendPercentCell(dst []byte, fraction float64) []byte {
	var buf [PercentCellWidth]byte
	return appendCellRunes(dst, AppendPercent(buf[:0], fraction), PercentCellWidth)
}

// PercentCell is [AppendPercentCell] as a string.
func PercentCell(fraction float64) string {
	var buf [PercentCellWidth + 2]byte
	return string(AppendPercentCell(buf[:0], fraction))
}

// AppendContext writes the context form (8.2.20): "5.1%/1M". The window rides
// along because a percentage of an unnamed window is not information — 5% of
// 1M and 5% of 128K are different situations. A window of zero renders
// [GlyphMissing]: unknown is not zero.
func AppendContext(dst []byte, used, window int64) []byte {
	if window <= 0 {
		return append(dst, GlyphMissing...)
	}
	if used < 0 {
		used = 0
	}
	dst = AppendPercent(dst, float64(used)/float64(window))
	dst = append(dst, '/')
	return AppendCount(dst, window)
}

// Context is [AppendContext] as a string.
func Context(used, window int64) string {
	var buf [ContextCellWidth]byte
	return string(AppendContext(buf[:0], used, window))
}

// AppendContextCell is [AppendContext] right-aligned in [ContextCellWidth].
func AppendContextCell(dst []byte, used, window int64) []byte {
	var buf [ContextCellWidth + 2]byte
	return appendCellRunes(dst, AppendContext(buf[:0], used, window), ContextCellWidth)
}

// ContextCell is [AppendContextCell] as a string.
func ContextCell(used, window int64) string {
	var buf [ContextCellWidth + 2]byte
	return string(AppendContextCell(buf[:0], used, window))
}

// AppendMoney writes money at the resolution the TUI reads it: exact cents up
// to $999.99, then the count ladder ("$1.2K"). This is the surface form; the
// prompt form is [AppendMoneyDime], and the difference is deliberate — see
// that function.
func AppendMoney(dst []byte, usd float64) []byte {
	if math.IsNaN(usd) {
		return append(dst, GlyphMissing...)
	}
	if usd < 0 {
		usd = 0
	}
	cents := int64(math.Round(usd * 100))
	if cents >= 100_000 {
		dst = append(dst, '$')
		return AppendCount(dst, cents/100)
	}
	dst = append(dst, '$')
	dst = appendInt(dst, cents/100)
	dst = append(dst, '.')
	return appendPad2(dst, cents%100)
}

// Money is [AppendMoney] as a string.
func Money(usd float64) string {
	var buf [MoneyCellWidth]byte
	return string(AppendMoney(buf[:0], usd))
}

// AppendMoneyCell is [AppendMoney] right-aligned in [MoneyCellWidth].
func AppendMoneyCell(dst []byte, usd float64) []byte {
	var buf [MoneyCellWidth + 2]byte
	return appendCellRunes(dst, AppendMoney(buf[:0], usd), MoneyCellWidth)
}

// MoneyCell is [AppendMoneyCell] as a string.
func MoneyCell(usd float64) string {
	var buf [MoneyCellWidth + 2]byte
	return string(AppendMoneyCell(buf[:0], usd))
}

// AppendMoneyDime mirrors the head's dimeUSD idiom exactly: round to a dime,
// print two decimals ($8.65 → "$8.70"). Position by volatility applies to
// precision as well as to order — a figure may only be as precise as it is
// stable, and a cent on a live job ticks constantly while nobody cancels a job
// over three cents.
//
// Where each form belongs: dimes for anything written into a model's context
// or a board that is rewritten on every turn (the head's rule, and the reason
// it exists); exact cents for anything a person reads as a number — the TUI,
// receipts, the store.
func AppendMoneyDime(dst []byte, usd float64) []byte {
	if math.IsNaN(usd) {
		return append(dst, GlyphMissing...)
	}
	if usd < 0 {
		usd = 0
	}
	dimes := int64(math.Round(usd * 10))
	dst = append(dst, '$')
	dst = appendInt(dst, dimes/10)
	dst = append(dst, '.')
	return append(dst, byte('0'+dimes%10), '0')
}

// MoneyDime is [AppendMoneyDime] as a string.
func MoneyDime(usd float64) string {
	var buf [16]byte
	return string(AppendMoneyDime(buf[:0], usd))
}

// AppendMissingCell writes [GlyphMissing] right-aligned in width cells.
// Missing data renders "—", never an estimate (10.2.8) and never a zero: a
// number that has not arrived and a number that is zero are different facts.
func AppendMissingCell(dst []byte, width int) []byte {
	for i := 1; i < width; i++ {
		dst = append(dst, ' ')
	}
	return append(dst, GlyphMissing...)
}

// MissingCell is [AppendMissingCell] as a string.
func MissingCell(width int) string {
	var buf [16 + 3]byte
	if width > 16 {
		width = 16
	}
	return string(AppendMissingCell(buf[:0], width))
}

// AppendEstimate writes the honesty mark (10.2.8): a "~" before an estimated
// number. Callers write the value cell immediately after it, and size the
// column with [EstimateWidth] so an estimate and a measurement line up.
func AppendEstimate(dst []byte) []byte { return append(dst, GlyphEstimate...) }

// EstimateWidth is the column width a cell needs when its values may be
// estimates: one more, for the "~".
func EstimateWidth(width int) int { return width + 1 }

// appendCell right-aligns an ASCII form in width cells. Used by every numeric
// cell; the byte length is the display width because the ladders emit ASCII.
func appendCell(dst, s []byte, width int) []byte {
	for i := len(s); i < width; i++ {
		dst = append(dst, ' ')
	}
	return append(dst, s...)
}

// appendCellRunes right-aligns a form that may contain [GlyphMissing], whose
// three bytes occupy one cell. Only the formatters that can return a missing
// mark pay for the branch.
func appendCellRunes(dst, s []byte, width int) []byte {
	w := len(s)
	if len(s) == len(GlyphMissing) && string(s) == GlyphMissing {
		w = 1
	}
	for i := w; i < width; i++ {
		dst = append(dst, ' ')
	}
	return append(dst, s...)
}

func appendPad2(dst []byte, n int64) []byte {
	if n < 0 {
		n = 0
	}
	if n < 10 {
		dst = append(dst, '0')
	}
	return appendInt(dst, n)
}

func appendInt(dst []byte, n int64) []byte {
	if n < 0 {
		dst = append(dst, '-')
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
		if n == 0 {
			break
		}
	}
	return append(dst, buf[i:]...)
}
