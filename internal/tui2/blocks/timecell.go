package blocks

import (
	"strconv"
	"time"
)

// TimeCell is the freeze-at-commit discipline of 8.1.2 made into a value.
//
// A time-derived cell (elapsed, countdown) captures the wall clock at REBUILD
// and reuses it verbatim once its rows commit. Committed bytes must never
// drift: a finalized block re-rendered an hour later at a new width must still
// say "4m 12s", because that is what it said when it was committed. The
// corollary from 8.1.2 is that "elapsed that ages" (5.21) lives only in live
// regions — the rail, the HUD — never in a committed transcript row.
//
// Usage: Sample the cell from the frame clock on every rebuild while the block
// is live, and Freeze it when the block finalizes. After Freeze, Sample is a
// no-op and the rendered string is constant forever.
type TimeCell struct {
	since  time.Time
	at     time.Time
	frozen bool

	// text caches the last formatted value so a live cell allocates roughly
	// once per displayed unit rather than once per frame, and a frozen cell
	// allocates never.
	text string
	key  int64
}

// NewTimeCell starts a cell counting from since.
func NewTimeCell(since time.Time) TimeCell {
	return TimeCell{since: since, at: since}
}

// Sample captures the wall clock for this rebuild. It is ignored once frozen —
// that is the whole discipline.
func (c *TimeCell) Sample(now time.Time) {
	if c.frozen {
		return
	}
	c.at = now
}

// Freeze commits the cell at its last sample. Call it when the owning block
// finalizes.
func (c *TimeCell) Freeze() { c.frozen = true }

// FreezeAt commits the cell at a given instant, for the common case where
// finalization and the last sample are the same event.
func (c *TimeCell) FreezeAt(now time.Time) {
	if !c.frozen {
		c.at = now
	}
	c.frozen = true
}

// Frozen reports whether the cell has committed.
func (c *TimeCell) Frozen() bool { return c.frozen }

// Since is the instant the cell counts from.
func (c *TimeCell) Since() time.Time { return c.since }

// Elapsed is the duration at the captured instant, never at "now".
func (c *TimeCell) Elapsed() time.Duration {
	if c.since.IsZero() || c.at.Before(c.since) {
		return 0
	}
	return c.at.Sub(c.since)
}

// String is the elapsed value in the width-stable form of 5.21. It is stable
// for the life of a frozen cell.
func (c *TimeCell) String() string {
	d := c.Elapsed()
	key := elapsedKey(d)
	if c.text != "" && c.key == key {
		return c.text
	}
	c.key, c.text = key, FormatElapsed(d)
	return c.text
}

// elapsedKey is the coarse value [FormatElapsed] actually shows, so the cached
// string is reused for every frame inside one displayed unit.
func elapsedKey(d time.Duration) int64 {
	switch {
	case d < time.Minute:
		return int64(d / time.Second)
	case d < time.Hour:
		return int64(time.Minute) + int64(d/time.Minute)
	default:
		return int64(time.Hour) + int64(d/time.Minute)
	}
}

// FormatElapsed renders a duration the way the transcript says durations:
// seconds under a minute, whole minutes under an hour, then h+mm. The unit
// granularity switches at fixed points and never jitters mid-row (5.21).
func FormatElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	var buf [12]byte
	out := buf[:0]
	switch {
	case d < time.Minute:
		out = strconv.AppendInt(out, int64(d/time.Second), 10)
		out = append(out, 's')
	case d < time.Hour:
		out = strconv.AppendInt(out, int64(d/time.Minute), 10)
		out = append(out, 'm')
	default:
		out = strconv.AppendInt(out, int64(d/time.Hour), 10)
		out = append(out, 'h')
		minutes := int64(d/time.Minute) % 60
		if minutes < 10 {
			out = append(out, '0')
		}
		out = strconv.AppendInt(out, minutes, 10)
	}
	return string(out)
}

// ElapsedCellWidth is the column an elapsed cell reserves so nothing to its
// right ever dances: the widest form is "59m59s"-class, and our vocabulary tops
// out at "23h59".
const ElapsedCellWidth = 5

// Cell is the elapsed value padded to [ElapsedCellWidth], right-aligned, which
// is how it goes into a status row beside other tabular telemetry.
func (c *TimeCell) Cell() string { return PadLeft(c.String(), ElapsedCellWidth) }
