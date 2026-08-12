package tokens

import "time"

// The motion vocabulary (11, 18): ONE cadence, three motions, and the exact
// keyframes of each.
//
// 11 permits exactly three moving things product-wide — the braille spinner on
// a running row, the slow breathe on `planning…`, and the composer's cursor —
// and says nothing else animates. That law is about WHAT moves. This file is
// about HOW: the interval every animated cell shares, the frame list each
// motion cycles, and the period one full cycle takes. It exists because those
// numbers were previously scattered as bare literals (a 120ms here, an
// `8 * DefaultInterval` there) and a scattered number is a number nobody can
// tune, review, or hold two surfaces to.
//
// The rule for anything added below: a motion is a NAME, a FRAME LIST, a
// PERIOD, and a sentence saying where it is legal. A frame list with no period
// is a spinner somebody will drive at whatever rate their surface happens to
// repaint at, which is the failure this file ends.
//
// blocks/clock.go carries the driving half — [blocks.Clock] latches one instant
// per frame so every live glyph on screen is phase-locked — and spells these
// same numbers as twins, because the edge runs tokens → blocks and the engine
// may not import the vocabulary. motion_test.go pins the twins equal.

// MotionInterval is THE house animation step: every animated cell in the
// product advances on this grid and no surface may pick its own.
//
// 120ms, and the band around it is narrow for measured reasons at both ends.
// Below about 100ms a braille cycle stops reading as rotation and starts
// reading as noise — the eye cannot resolve ten distinct frames a second, so
// the row shimmers rather than turns — and every step is a repaint of every
// live row, so the cost is paid in wakeups the user cannot even see. Above
// about 150ms the same cycle visibly steps: a ten-frame spinner at 160ms takes
// 1.6s to come round and each frame is long enough to be read as a separate
// glyph rather than as one turning thing. 120ms puts the full braille rotation
// at [SpinnerPeriod] = 1.2s, which is the calm end of the legible band.
//
// It is also the grid the phase-lock is built on: [blocks.Clock.Frame] is
// floor(now/interval) % frames, so two rows given the same latched instant show
// the same frame, and a repaint landing inside one step produces byte-identical
// rows and therefore zero dirty rows (8.1.3). A second interval anywhere in the
// product breaks both properties at once — the rows drift apart AND the shell
// starts waking on two schedules.
const MotionInterval = 120 * time.Millisecond

// MotionIntervalMin and MotionIntervalMax are the band [MotionInterval] must
// stay inside, with the reasons stated at MotionInterval. They are here so a
// future tuning is a review of two numbers rather than a rediscovery of why
// 60ms felt frantic.
const (
	MotionIntervalMin = 100 * time.Millisecond
	MotionIntervalMax = 150 * time.Millisecond
)

// MotionPeriodMin and MotionPeriodMax bound one full cycle of any motion in the
// table. Under 0.8s a cycle reads as urgency — a thing that wants something
// from you — which is amber's job and not a spinner's (see [Amber]); over 2s
// the motion stops answering "is this alive?" within the glance that asked.
const (
	MotionPeriodMin = 800 * time.Millisecond
	MotionPeriodMax = 2 * time.Second
)

// SpinnerPeriod is one full rotation of [SpinnerFrames] at the house cadence:
// ten braille frames × 120ms = 1.2s. It is DERIVED rather than authored, so
// adding or removing a frame moves the period instead of silently changing
// what "one rotation" means. len() of an array is a Go constant, so this costs
// nothing at runtime.
const SpinnerPeriod = time.Duration(len(SpinnerFrames)) * MotionInterval

// PulseFrames is the breathe: the three-tier dot the `planning…` line wears
// while the model is thinking, up and back down.
//
// The set is a SIZE ramp on one shape, not four different marks. That is the
// whole design: a growing and shrinking dot reads as breathing, while four
// distinct glyphs read as a second spinner, and 11 already spent its spinner.
// The cycle is written out rather than mirrored in code (·, •, ●, • rather than
// three frames walked forward and back) because the eased dwell in
// [blocks.Pulse] indexes a flat list, and a list that says exactly what is
// drawn is a list a designer can read.
//
// BYTE COLLISIONS, DELIBERATE: `·` is also [GlyphSeparator] and
// [GlyphProseBullet]; `●` is also [GlyphStepDone]. The glyph lane ruled the
// collisions acceptable — a slot is a meaning and not a byte, and these three
// are the honest small/medium/large dots in a repertoire every terminal has —
// so they are named here as their own slots and walked by the width gate under
// their own names (see [Glyphs]).
//
// WIDTH: all three are East_Asian_Width=Ambiguous, which is the property that
// matters. Not "all narrow" — all THE SAME, together, under both rulers, so the
// row cannot change width mid-breath under a CJK locale the way 5.21's ◐◓◑◒
// spinner would have. glyph_test.go's TestAnimatedSetsAgreeOnWidth measures it.
var PulseFrames = [4]string{"·", "•", "●", "•"}

// PulseSteps is the breathe's period measured in house steps, and
// [PulsePeriod] is the same number as a duration: 12 × 120ms = 1.44s per full
// breath.
//
// TUNED FROM 8 STEPS (960ms). At 960ms the dot cycled just under once a second,
// which is a resting heart rate — fast enough that a `planning…` line read as
// impatient, and close enough to the spinner's 1.2s rotation that the two
// motions beat against each other on a screen showing both. 1.44s is a slow
// breath, it is audibly not the spinner's period, and it sits inside
// [MotionPeriodMin]..[MotionPeriodMax]. 12 is also 4 frames × 3 steps, so each
// frame gets whole steps at flat easing and the eased version has room to
// linger without any frame being skipped outright.
const (
	PulseSteps  = 12
	PulsePeriod = PulseSteps * MotionInterval
)

// PulseEase is how strongly the breathe's dwell bends: 0 is a flat tick, and
// 0.6 dwells roughly 2.5× longer mid-cycle than at the edges. It is what makes
// the dot BREATHE rather than count — a flat four-frame cycle at any period is
// a clock, and the thing being said here is "someone is thinking", not "3.6
// seconds have passed". The easing function and its monotonicity live at
// [blocks.Pulse.Index].
const PulseEase = 0.6

// CaretMotion names 11's third motion so the table is complete, and records
// that we do not own its cadence. The composer's cursor is requested from the
// terminal through DECSCUSR and blinks at whatever rate the user's terminal
// (and their own preference) says; the shell's only jobs are to ask for it and
// to restore the user's cursor on exit. There is deliberately no interval here
// to tune: a caret we drew ourselves would be a fourth animation competing with
// the terminal's own, and it would keep blinking in a frozen render.
const CaretMotion = "caret"

// Motion is one row of the keyframe table: a name, the frames it cycles, how
// long one full cycle takes, how its dwell is eased, and the one sentence
// saying where it is legal.
type Motion struct {
	// Name is the motion's stable name; 18's table keys on it.
	Name string
	// Frames is the cycle in draw order. Empty means the motion has no frames
	// of ours — see [CaretMotion].
	Frames []string
	// Period is one full cycle. Zero means we do not drive it.
	Period time.Duration
	// Ease is the dwell bend in [0,1); 0 is a flat tick.
	Ease float64
	// Legal is where this motion may appear, in one sentence. A motion with no
	// legal surface is a motion that should not exist.
	Legal string
}

// Motions returns the whole motion vocabulary, in 11's order. It is the code
// form of 18's keyframe table: motion_test.go walks it to hold every frame set
// width-stable and every period inside the calm band, and a `?` help surface or
// a motion-preview screen can walk the same rows.
//
// A fourth entry here is a change to 11 and needs the law amended first.
func Motions() []Motion {
	return []Motion{
		{
			Name:   "spinner",
			Frames: SpinnerFrames[:],
			Period: SpinnerPeriod,
			Legal: "transient tool rows only (8.1.6) — rows that live for " +
				"seconds. Never on a rail card, an agent row, or anything durable: " +
				"a dancing glyph on a long-lived object is a lie about liveness.",
		},
		{
			Name:   "breathe",
			Frames: PulseFrames[:],
			Period: PulsePeriod,
			Ease:   PulseEase,
			Legal: "the thinking line (`planning…`) while a model is working " +
				"and has produced nothing yet. One per surface: two things " +
				"breathing at once is two things claiming to be the live one.",
		},
		{
			Name: CaretMotion,
			Legal: "the composer, and only as the terminal's own cursor via " +
				"DECSCUSR. We request it and restore the user's on exit; we never " +
				"draw a blinking cell ourselves.",
		},
	}
}

// GaugeThresholds is the context gauge's cell ladder (5.17): the fraction at
// which each cell of [GaugeCells] takes over. The rungs are even fifths, which
// is the honest choice for a five-cell eighth-block ramp — the cells are
// themselves a linear height ramp, so a non-linear threshold table would draw a
// bar that disagrees with its own height.
//
// The ladder is a TABLE rather than an arithmetic expression inside [Gauge] for
// the reason the rest of this package is tables: a threshold that only exists
// as `int(fraction * 5)` cannot be read by a designer, cannot be quoted in 18,
// and cannot be changed without re-deriving what the old numbers were.
//
// It is deliberately NOT where the amber rule lives. The gauge's HEIGHT is a
// linear reading of how full the window is; its COLOUR is a judgement about
// whether a human needs to act, and that judgement is dual (a percentage AND an
// absolute token count) because a 1M-window model at 75% still has more
// headroom than a 200k model has in total. See [ContextWarnFraction],
// [ContextWarnPoint] and [ContextToken].
var GaugeThresholds = [len(GaugeCells)]float64{0, 0.2, 0.4, 0.6, 0.8}
