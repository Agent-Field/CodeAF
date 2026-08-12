package blocks

import (
	"math"
	"time"
)

// DefaultInterval is one animation step. Everything that moves moves on this
// grid, so a room with five running rows animates as one organism rather than
// five independent twitches (8.1.3).
//
// Twin of tokens.MotionInterval, which carries the argument for the number
// (120ms, inside a 100..150ms band with a measured reason at each end) and is
// the vocabulary's authority for it. The edge runs tokens → blocks, so the
// value is spelled twice and tokens/motion_test.go pins the two equal.
const DefaultInterval = 120 * time.Millisecond

// SpinnerPeriod is one full rotation of [Spinner] at the house cadence: ten
// braille frames × 120ms = 1.2s. Derived, never authored — adding a frame moves
// the period rather than quietly redefining "one rotation".
const SpinnerPeriod = time.Duration(len(Spinner)) * DefaultInterval

// The breathe's keyframe constants (11's second motion). Twins of
// tokens.PulseSteps / tokens.PulsePeriod / tokens.PulseEase, which carry the
// argument; the pins live in tokens/motion_test.go.
const (
	// PulseSteps is the breathe's period in house steps.
	PulseSteps = 12
	// DefaultPulsePeriod is one full breath: 12 × 120ms = 1.44s. It was 8
	// steps (960ms) and was slowed because a just-under-a-second cycle reads
	// as a resting pulse rather than a breath, and beat against the spinner's
	// 1.2s rotation on any screen showing both.
	DefaultPulsePeriod = PulseSteps * DefaultInterval
	// DefaultPulseEase is the dwell bend: ~2.5× longer mid-cycle than at the
	// edges, which is what makes the dot breathe instead of count.
	DefaultPulseEase = 0.6
)

// Spinner is the transient-tool-row glyph cycle (§11's one moving glyph). It is
// for rows that live for seconds. Rail cards and agent rows never spin — a
// dancing glyph on a durable object is a lie about liveness (8.1.6).
//
// It is braille, and the reason is measured rather than argued. 5.21 proposes
// ◐◓◑◒, and those four disagree about East-Asian width: ◐ and ◑ are Ambiguous —
// two cells under a CJK-locale terminal — while ◓ and ◒ are Neutral. A spinning
// row drawn from that set CHANGES WIDTH mid-spin, which is the width
// instability 5.17 bans outright, and everything to its right dances with it.
// ◐ is also already spent: it is the working STATE (tokens.GlyphWorking), and a
// glyph means exactly one thing product-wide. Braille is width-homogeneous
// under every mode and is the house spinner everywhere else.
//
// Twin of tokens.SpinnerFrames — same arrangement as [CutMark]: blocks cannot
// import tokens because the edge runs tokens → blocks, so the frames live here
// as well as there and a pin fails when they part. The frame COUNT is part of
// what is pinned, so nothing may assume four: every derivation goes through
// len(Spinner).
var Spinner = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

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
	// Period is one full cycle. Zero means [DefaultPulsePeriod].
	Period time.Duration
	// Ease is how strongly the dwell bends, in [0,1). 0 is a flat tick; the
	// default [DefaultPulseEase] dwells roughly 2.5x longer mid-cycle than at
	// the edges.
	Ease float64
}

// DefaultPulse is the thinking pulse: a three-tier dot that breathes, at the
// house period. Twin of tokens.PulseFrames — the size ramp `· • ● •`, one shape
// growing and shrinking, deliberately not four distinct marks (that would be a
// second spinner, and 11 permits one).
var DefaultPulse = Pulse{Frames: []string{"·", "•", "●", "•"}, Period: DefaultPulsePeriod}

func (p Pulse) period() time.Duration {
	if p.Period <= 0 {
		return DefaultPulsePeriod
	}
	return p.Period
}

func (p Pulse) ease() float64 {
	if p.Ease <= 0 {
		return DefaultPulseEase
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
//
// IT IS NOT ONE OF 11's THREE MOTIONS, and it currently has no caller on any
// v2 surface. It stays because it is the corrected form of a defect the legacy
// TUI still ships (internal/tui/shimmer.go steps the sweep by
// max(1, period/12) per tick, so a wide row sweeps faster than a narrow one —
// exactly what fixed velocity fixes), and the day that surface is rebuilt this
// is what it is rebuilt onto. Until 11 is amended to admit a fourth motion,
// wiring this to a v2 surface is a law change, not a wiring change.
type Shimmer struct {
	// Velocity is the band's speed in cells per second. Zero means
	// [DefaultShimmerVelocity].
	Velocity float64
	// Band is the band's width in cells. Zero means [DefaultShimmerBand].
	Band int
}

// The shimmer's keyframe constants. 24 cells/sec crosses an 80-column row in
// about 3.3s — a sweep the eye follows rather than one it catches — and an
// 8-cell band is wide enough to read as a gradient of light and narrow enough
// that a short row is never lit end to end at once.
const (
	DefaultShimmerVelocity = 24.0
	DefaultShimmerBand     = 8
)

func (s Shimmer) velocity() float64 {
	if s.Velocity <= 0 {
		return DefaultShimmerVelocity
	}
	return s.Velocity
}

func (s Shimmer) band() int {
	if s.Band <= 0 {
		return DefaultShimmerBand
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
