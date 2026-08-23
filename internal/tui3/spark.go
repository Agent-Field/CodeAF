package tui3

// THE SPARKLINE MACHINERY — ONE PLACE THAT TURNS A RUN OF READINGS INTO A SHAPE.
//
// Two surfaces on this program draw a spark and they draw it in two alphabets.
// The status line's context spark ([app.ctxSpark]) is ONE ROW of block bars
// standing beside a figure it belongs to; the machine card's `hands` band
// (homeband_spark.go) is TWO ROWS of braille dots standing on their own. What
// they SHARE is the arithmetic — a reading, a ceiling and a number of steps
// become a height — and that is the whole of what lives here.
//
// It is one function rather than two loops for the reason [bandClauses] is one
// function: a second quantizer is a second answer to "how tall is this sample",
// and the day the two rounded differently would be the day two sparks on one
// screen disagreed about the same number.
//
// ── WHY BRAILLE FOR A CHART AND BARS FOR A LINE ──
//
// A block bar is one cell of vertical resolution per row and it is the right
// alphabet for a spark that sits INSIDE a sentence: `10% ▁▂▂▃▅▆` is a clause,
// and a clause may not be two rows tall. A braille cell is two dots wide and
// four tall, so two rows of it carry eight steps of height and two samples per
// cell of width — which is what makes a chart a chart rather than a wide word.
// Neither alphabet is better; they are answers to two different questions about
// how much room the shape is allowed.
//
// ── AND NEITHER IS DRAWN WHERE A SHAPE CANNOT BE READ ──
//
// A SPARKLINE IS SHAPE, and the two tiers that cannot read shape do not get one
// ([app.ctxSpark] states the law and this file's other reader repeats it): a
// terminal that cannot be trusted with box drawing renders a row of replacement
// characters, and a surface being read aloud announces the dots one at a time.
// The decision belongs to each caller, because each of them has a different
// thing to keep when the shape goes.

// sparkBars is the one-row alphabet, lowest first. It is the status line's
// ([app.ctxSpark]).
const sparkBars = "▁▂▃▄▅▆▇"

// sparkLevel is the arithmetic both alphabets share: which of `steps` heights
// one reading stands at, measured against a ceiling, floored at nothing and
// capped at the top step.
//
// A READING AT OR ABOVE THE CEILING IS THE TOP STEP AND NEVER OFF THE END. The
// ceiling is a scale rather than a limit — the context spark measures against
// the compaction threshold, which a conversation may genuinely pass — and a
// chart that panicked at its own top rung would be a chart that could only draw
// the situations nobody needs it for.
func sparkLevel(reading, ceiling, steps int) int {
	if steps < 1 || ceiling < 1 {
		return 0
	}
	at := reading * steps / ceiling
	if at >= steps {
		at = steps - 1
	}
	if at < 0 {
		at = 0
	}
	return at
}

// sparkPeak is the largest reading in a window, which is what a chart with no
// stated ceiling scales itself against.
func sparkPeak(readings []int) int {
	peak := 0
	for _, reading := range readings {
		if reading > peak {
			peak = reading
		}
	}
	return peak
}

// ── the braille grid ────────────────────────────────────────────────────────

// brailleBase is the empty braille cell, U+2800. Every dot is a bit set on top
// of it, which is why one cell can be built by OR-ing into a rune.
const brailleBase rune = 0x2800

// sparkDotRows is how many dot rows one braille cell holds, and sparkDotCols
// how many dots wide it is. They are Unicode's numbers and not ours.
const (
	sparkDotRows = 4
	sparkDotCols = 2
)

// brailleDot is the bit each dot of a cell is worth, by column then by row.
//
// THE ORDER IS UNICODE'S AND IT IS NOT SEQUENTIAL — the six-dot block came
// first and the eighth-height row was added underneath it afterwards, so the
// bottom row of each column is the high bit rather than the fourth. Writing the
// table out is the only honest way to say that; arithmetic would be a bug
// wearing a formula.
var brailleDot = [sparkDotCols][sparkDotRows]rune{
	{0x01, 0x02, 0x04, 0x40},
	{0x08, 0x10, 0x20, 0x80},
}

// brailleSpark draws readings as a DOTTED TRACE — one dot per sample, newest at
// the right — `rows` braille cells tall and at most `cells` cells wide. It
// answers nothing at all for a window with no height in it.
//
// ── ONE DOT PER SAMPLE, NOT A FILLED BAR ──
//
// A bar chart says "this much"; a trace says "this shape", and the shape is the
// whole reason a chart is worth two rows of a card that is otherwise words. The
// figure itself is said in one place and one place only — the pulse line's
// `N working` (pulse.go) — so the chart is free to be about movement, and a
// dotted line is the quietest mark that can be about movement. It is also the
// cheapest thing this surface can put on a wire: a row of braille is a row of
// braille however busy the machine is.
//
// ── IT SCALES TO ITS OWN PEAK, AND SAYS SO BY SAYING NOTHING ELSE ──
//
// There is no ceiling on how many hands a machine may have out, so there is
// nothing to measure against but the window itself. The top dot row is the
// busiest moment in the window and the bottom is nothing at all. That makes the
// chart a statement about CHANGE and never about magnitude, which is exactly
// the division of labour the pulse line already set up: the pulse says how many,
// the spark says how it got there.
//
// A WINDOW WITH NO PEAK DRAWS NOTHING. All-zero is a machine that has done
// nothing for the whole window, and a flat line along the floor of an empty
// chart is furniture — the emptiness law asks for the absence instead.
func brailleSpark(readings []int, rows, cells int) []string {
	if rows < 1 || cells < 1 || len(readings) == 0 {
		return nil
	}
	// NEWEST AT THE RIGHT, so a window longer than the room takes its TAIL.
	if room := cells * sparkDotCols; len(readings) > room {
		readings = readings[len(readings)-room:]
	}
	ceiling := sparkPeak(readings)
	if ceiling < 1 {
		return nil
	}
	// AND THE RIGHT EDGE IS THE RIGHT EDGE EVEN WITH AN ODD SAMPLE COUNT: the
	// blank dot column goes at the FRONT, so the newest reading always lands in
	// the last dot of the last cell rather than half a cell short of it.
	cells = (len(readings) + sparkDotCols - 1) / sparkDotCols
	lead := cells*sparkDotCols - len(readings)
	steps := rows * sparkDotRows
	grid := make([][]rune, rows)
	for row := range grid {
		grid[row] = make([]rune, cells)
		for cell := range grid[row] {
			grid[row][cell] = brailleBase
		}
	}
	for at, reading := range readings {
		// THE TRACE GROWS UPWARD. Level zero is the bottom dot row of the bottom
		// cell, which is where a quiet machine's line lies still.
		dot := steps - 1 - sparkLevel(reading, ceiling, steps)
		x := at + lead
		grid[dot/sparkDotRows][x/sparkDotCols] |= brailleDot[x%sparkDotCols][dot%sparkDotRows]
	}
	out := make([]string, rows)
	for row := range grid {
		out[row] = string(grid[row])
	}
	return out
}
