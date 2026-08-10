package blocks

import (
	"math"
	"time"
)

// DefaultInterval is one animation step. Everything that moves moves on this
// grid, so a room with five running rows animates as one organism rather than
// five independent twitches (8.1.3).
const DefaultInterval = 120 * time.Millisecond

// Spinner is the transient-tool-row glyph cycle (5.21). It is for rows that
// live for seconds. Rail cards and agent rows never spin — a dancing glyph on a
// durable object is a lie about liveness (8.1.6).
var Spinner = [...]string{"◐", "◓", "◑", "◒"}

// Clock is the ONE animation clock. Every live glyph in a frame derives its
// frame index from the same latched instant, so parallel rows are phase-locked;
// and because the frame index is floor(now/interval) % frames, a render tick
// that lands inside the same step produces byte-identical rows and therefore
// zero dirty rows (8.1.3: no wasted identical paints).
//
// The shell latches once per frame and asks [Clock.NextTick] when to wake up
// again; render ticks then coincide with glyph ticks by construction.
//
// A Clock is not safe for concurrent use.
type Clock struct {
	// Interval is one animation step. Zero means [DefaultInterval].
	Interval time.Duration
	// Calm freezes every derived animation on its first frame — the
	// reduced-motion equivalent of 5.13. The caret is the shell's business;
	// everything this package drives stops.
	Calm bool

	now   time.Time
	epoch time.Time
}

// NewClock returns a clock stepping at interval (zero means [DefaultInterval]).
func NewClock(interval time.Duration) *Clock {
	return &Clock{Interval: interval}
}

func (c *Clock) interval() time.Duration {
	if c.Interval <= 0 {
		return DefaultInterval
	}
	return c.Interval
}

// Latch fixes the instant every glyph in the coming frame will use. Calling it
// twice with instants inside one step is the whole point: the second frame is
// byte-identical to the first.
func (c *Clock) Latch(now time.Time) {
	if c.epoch.IsZero() {
		c.epoch = now
	}
	c.now = now
}

// Now is the latched instant.
func (c *Clock) Now() time.Time { return c.now }

// Epoch is the instant the clock was first latched. Velocities are measured
// from it so a shimmer's position is a pure function of the latched time.
func (c *Clock) Epoch() time.Time { return c.epoch }

// SetEpoch pins the velocity origin, for deterministic tests and goldens.
func (c *Clock) SetEpoch(at time.Time) { c.epoch = at }

// Step is floor(now/interval): the number of the animation step the latched
// instant falls in. Every phase-locked derivation is a function of it.
func (c *Clock) Step() int64 {
	if c.now.IsZero() {
		return 0
	}
	return c.now.UnixNano() / int64(c.interval())
}

// Frame is the classic floor(now/interval) % frames. Calm returns 0.
func (c *Clock) Frame(frames int) int {
	if frames <= 1 || c.Calm {
		return 0
	}
	step := c.Step() % int64(frames)
	if step < 0 {
		step += int64(frames)
	}
	return int(step)
}

// NextTick is when the next glyph changes, i.e. when the shell should schedule
// its next render. Under Calm nothing changes, so it returns the zero time and
// the shell schedules nothing.
func (c *Clock) NextTick() time.Time {
	if c.Calm || c.now.IsZero() {
		return time.Time{}
	}
	step := c.interval()
	return c.now.Add(step - time.Duration(c.now.UnixNano()%int64(step)))
}

// Glyph is the shared-clock spinner frame for a transient tool row.
func (c *Clock) Glyph() string { return Spinner[c.Frame(len(Spinner))] }

// Phase is the position inside one period, in [0,1). Calm returns 0.
func (c *Clock) Phase(period time.Duration) float64 {
	if c.Calm || period <= 0 || c.now.IsZero() {
		return 0
	}
	return float64(c.now.UnixNano()%int64(period)) / float64(period)
}

// Elapsed is how long the latched instant is past the epoch.
func (c *Clock) Elapsed() time.Duration {
	if c.now.IsZero() || c.epoch.IsZero() {
		return 0
	}
	return c.now.Sub(c.epoch)
}

// Pulse is the thinking pulse: a frame cycle whose dwell is EASED rather than
// flat — fast at the cycle edges, slow through the middle, so it breathes
// instead of ticking (8.1.4). Frames must all be the same printable width so
// trailing text never shifts.
type Pulse struct {
	// Frames is the glyph cycle, in order.
	Frames []string
	// Period is one full cycle. Zero means eight [DefaultInterval] steps.
	Period time.Duration
	// Ease is how strongly the dwell bends, in [0,1). 0 is a flat tick; the
	// default 0.6 dwells roughly 2.5x longer mid-cycle than at the edges.
	Ease float64
}

// DefaultPulse is the thinking pulse: a three-tier dot that breathes.
var DefaultPulse = Pulse{Frames: []string{"·", "•", "●", "•"}}

func (p Pulse) period() time.Duration {
	if p.Period <= 0 {
		return 8 * DefaultInterval
	}
	return p.Period
}

func (p Pulse) ease() float64 {
	if p.Ease <= 0 {
		return 0.6
	}
	if p.Ease >= 1 {
		return 0.95
	}
	return p.Ease
}

// Index is the eased frame index for the clock's latched instant.
//
// The easing is e(x) = x + a·sin(2πx)/(2π), which is monotone for a < 1, maps
// [0,1] onto itself exactly, and has slope 1+a at the cycle edges and 1-a in
// the middle: the frame advances fast at the edges and lingers mid-cycle.
func (p Pulse) Index(c *Clock) int {
	n := len(p.Frames)
	if n <= 1 || c == nil || c.Calm {
		return 0
	}
	x := c.Phase(p.period())
	a := p.ease()
	e := x + a*math.Sin(2*math.Pi*x)/(2*math.Pi)
	index := int(e * float64(n))
	if index < 0 {
		return 0
	}
	if index >= n {
		return n - 1
	}
	return index
}

// Glyph is the pulse's current frame, or "" if it has none.
func (p Pulse) Glyph(c *Clock) string {
	if len(p.Frames) == 0 {
		return ""
	}
	return p.Frames[p.Index(c)]
}

// Shimmer is a band that sweeps a run of cells at a FIXED VELOCITY, so a long
// row and a short row shimmer at the same visible speed and smoothness does not
// depend on length (8.1.4). It is a highlight range, not a glyph substitution:
// nothing reflows.
type Shimmer struct {
	// Velocity is the band's speed in cells per second. Zero means 24.
	Velocity float64
	// Band is the band's width in cells. Zero means 8.
	Band int
}

func (s Shimmer) velocity() float64 {
	if s.Velocity <= 0 {
		return 24
	}
	return s.Velocity
}

func (s Shimmer) band() int {
	if s.Band <= 0 {
		return 8
	}
	return s.Band
}

// Position is the band's leading edge in cells, unclamped: it runs from -Band
// (fully off the left) to span (fully off the right) and wraps. It advances at
// exactly Velocity cells per second regardless of span, which is the fixed
// velocity of 8.1.4 — a long row and a short row shimmer at the same speed.
func (s Shimmer) Position(c *Clock, span int) int {
	if span <= 0 || c == nil || c.Calm {
		return 0
	}
	band := s.band()
	cycle := span + 2*band
	pos := int(c.Elapsed().Seconds()*s.velocity()) % cycle
	if pos < 0 {
		pos += cycle
	}
	return pos - band
}

// Range is the half-open cell range [start, end) the band covers over a span of
// span cells at the clock's latched instant. It sweeps in from the left edge
// and out through the right, so the band is partially off-span at both ends and
// the sweep reads as continuous. Calm returns an empty range.
func (s Shimmer) Range(c *Clock, span int) (int, int) {
	if span <= 0 || c == nil || c.Calm {
		return 0, 0
	}
	start := s.Position(c, span)
	end := start + s.band()
	if start < 0 {
		start = 0
	}
	if end > span {
		end = span
	}
	if start >= end {
		return 0, 0
	}
	return start, end
}

// NextTick is when the band moves by one whole cell — the shimmer's own glyph
// tick, so a shell driving a shimmer can align its repaint with it instead of
// repainting identical rows.
func (s Shimmer) NextTick(c *Clock) time.Time {
	if c == nil || c.Calm || c.now.IsZero() {
		return time.Time{}
	}
	cell := time.Duration(float64(time.Second) / s.velocity())
	if cell <= 0 {
		return time.Time{}
	}
	elapsed := c.Elapsed()
	return c.now.Add(cell - elapsed%cell)
}

// Paint applies the shimmer to a plain (unstyled) run of text: cells inside the
// band are painted live, the rest settled. text must carry no escape sequences
// of its own — the band is a cell range, and a pre-styled string has no honest
// cell boundaries.
func (s Shimmer) Paint(c *Clock, text string, st Styler) string {
	span := stringWidth(text)
	start, end := s.Range(c, span)
	sty := styler(st)
	if start >= end {
		return sty.Paint(text, StateSettled, HueNone)
	}
	var b builder
	b.grow(len(text) + 32)
	b.styled(sty, cut(text, 0, start), StateSettled, HueNone)
	b.styled(sty, cut(text, start, end), StateLive, HueAlive)
	b.styled(sty, cut(text, end, span), StateSettled, HueNone)
	return b.String()
}
