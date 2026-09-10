package tui3

// THE SPARKLINE MACHINERY — ONE PLACE THAT TURNS A RUN OF READINGS INTO A SHAPE.
//
// The context spark ([app.ctxSpark]) stands inside a sentence beside the figure
// it belongs to — on the status sheet and /status, since the row gave it up on
// 2026-09-09: a row of block bars, and the arithmetic that turns a reading, a
// ceiling and a number of steps into a height.
//
// IT HAD A SECOND CALLER AND THE SURFACE IT DREW ON IS GONE. Home's machine card
// carried a `hands` chart — the shape of what this machine had had in flight over
// the last few minutes — and that card was reached by walking the cursor up off
// the top of home's list onto no row at all. `↑` off the top row reaches the TAB
// BAR now (pages.go's [barCursor]), so the card and its chart went with the state
// that opened them. This stays one function because it is still the ONE answer to
// "how tall is this sample", and the day a second one rounded differently would be
// the day two sparks on one screen disagreed about the same number.
//
// ── WHY BLOCK BARS AND NOT BRAILLE ──
//
// The `hands` chart was two rows of braille for a wave, and braille is the
// denser alphabet: a cell is two dots wide and four tall, so two rows carry
// eight steps of height and two samples per cell of width. It was the wrong
// density for this reading. ONE DOT IS NOT A LINE — a braille trace draws a
// single dot per sample and joins nothing, so a machine moving between nought
// and three workers drew scattered specks that a person read as punctuation
// rather than as a shape. A block bar is a whole cell: one sample, one solid
// mark, and a run of them reads as a continuous silhouette at every fill level,
// with a flat stretch along the bottom reading as a floor rather than as dust.
//
// The reading this machinery draws is a SMALL INTEGER — how many things are in
// flight, usually nought to five — and eight steps of vertical resolution buy
// nothing a person can see when the data has four values in it. Coarse and
// legible beats fine and speckled, so both callers now take the bars.
//
// ── AND IT IS NOT DRAWN WHERE A SHAPE CANNOT BE READ ──
//
// A SPARKLINE IS SHAPE, and the two tiers that cannot read shape do not get one
// ([app.ctxSpark] states the law and this file's other reader repeats it): a
// terminal that cannot be trusted with box drawing renders a row of replacement
// characters, and a surface being read aloud announces the bars one at a time.
// The decision belongs to each caller, because each of them has a different
// thing to keep when the shape goes.

// sparkBars is the alphabet, lowest first. The lowest bar is a MARK and not a
// blank: a reading of nothing is a reading, and a chart that drew air for it
// would be a chart with holes in it where its quietest stretches were.
const sparkBars = "▁▂▃▄▅▆▇"

// sparkLevel is the arithmetic every caller shares: which of `steps` heights
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

// barSpark draws readings as one row of block bars — one sample per cell,
// newest at the right, at most `cells` wide.
//
// ── THE CEILING IS EITHER STATED OR THE WINDOW'S OWN PEAK ──
//
// A caller with a real scale to measure against passes it: the context spark
// has the compaction threshold, and a bar drawn against it means the same thing
// from one turn to the next. A caller with no such number passes nought and the
// window scales to its own tallest reading — which makes the chart a statement
// about CHANGE and never about magnitude, and is why a self-scaling window with
// no height in it draws NOTHING at all rather than a flat line along its own
// floor. That flat line is furniture, and the emptiness law asks for the
// absence instead.
func barSpark(readings []int, ceiling, cells int) string {
	if cells < 1 || len(readings) == 0 {
		return ""
	}
	// NEWEST AT THE RIGHT, so a window longer than the room takes its TAIL.
	if len(readings) > cells {
		readings = readings[len(readings)-cells:]
	}
	if ceiling < 1 {
		if ceiling = sparkPeak(readings); ceiling < 1 {
			return ""
		}
	}
	bars := []rune(sparkBars)
	out := make([]rune, 0, len(readings))
	for _, reading := range readings {
		out = append(out, bars[sparkLevel(reading, ceiling, len(bars))])
	}
	return string(out)
}
