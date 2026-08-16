package blocks

import (
	"testing"
	"time"
)

func TestFrameIsFloorNowOverIntervalModFrames(t *testing.T) {
	c := NewClock(100 * time.Millisecond)
	origin := time.Unix(0, 0)
	for step := 0; step < 12; step++ {
		c.Latch(origin.Add(time.Duration(step) * 100 * time.Millisecond))
		if got, want := c.Frame(4), step%4; got != want {
			t.Fatalf("step %d: frame %d, want %d", step, got, want)
		}
	}
	// Inside a step the frame does not move — the property the whole
	// no-identical-repaints claim rests on.
	c.Latch(origin.Add(350 * time.Millisecond))
	first := c.Frame(4)
	c.Latch(origin.Add(399 * time.Millisecond))
	if c.Frame(4) != first {
		t.Fatal("the frame moved inside one interval")
	}
}

// All live glyphs in one frame come from one latched instant, so N running rows
// animate as one organism.
func TestGlyphsArePhaseLocked(t *testing.T) {
	c := NewClock(120 * time.Millisecond)
	c.Latch(base.Add(917 * time.Millisecond))
	first := c.Glyph()
	for i := 0; i < 20; i++ {
		if c.Glyph() != first {
			t.Fatal("two glyphs in one frame disagreed about the animation phase")
		}
	}
}

func TestNextTickLandsOnTheGlyphChange(t *testing.T) {
	c := NewClock(120 * time.Millisecond)
	now := base.Add(37 * time.Millisecond)
	c.Latch(now)
	next := c.NextTick()
	if !next.After(now) {
		t.Fatalf("next tick %v is not after %v", next, now)
	}
	before := c.Frame(4)
	c.Latch(next.Add(-time.Nanosecond))
	if c.Frame(4) != before {
		t.Fatal("the glyph changed before the scheduled tick")
	}
	c.Latch(next)
	if c.Frame(4) == before {
		t.Fatal("the glyph did not change at the scheduled tick")
	}
}

// Two consecutive frames with no state change produce ZERO dirty rows. This is
// the assertion 8.1.3 is for: render ticks coincide with glyph ticks, so the
// shell never repaints identical bytes.
func TestNoDirtyRowsWhenNothingMoved(t *testing.T) {
	tr := New(40, 8)
	tr.Strict = false
	tr.Append(newFixed("a", 3))
	tr.Append(&spin{id: "live", c: tr.Clock(), n: 2})

	now := base.Add(50 * time.Millisecond)
	tr.Frame(now)
	frame := tr.Frame(now.Add(10 * time.Millisecond)) // same animation step
	if !frame.Unchanged() {
		t.Fatalf("a frame inside one animation step reported %d dirty rows: %v", len(frame.Dirty), frame.Dirty)
	}

	// And it DOES report the change when the step turns over.
	frame = tr.Frame(now.Add(DefaultInterval))
	if frame.Unchanged() {
		t.Fatal("the spinner advanced a step and nothing was marked dirty")
	}
	for _, row := range frame.Dirty {
		if row < 3 {
			t.Fatalf("a committed row (%d) was marked dirty by an animation frame", row)
		}
	}
}

func TestCalmFreezesEverything(t *testing.T) {
	c := NewClock(0)
	c.Latch(base)
	c.Calm = true
	if c.Frame(len(Spinner)) != 0 || c.Glyph() != Spinner[0] {
		t.Fatal("calm did not freeze the spinner")
	}
	if !c.NextTick().IsZero() {
		t.Fatal("calm still asked the shell to wake up for an animation")
	}
	if start, end := (Shimmer{}).Range(c, 40); start != end {
		t.Fatal("calm did not freeze the shimmer")
	}
	if DefaultPulse.Index(c) != 0 {
		t.Fatal("calm did not freeze the pulse")
	}
}

// The shimmer is driven by velocity, so its speed does not depend on the length
// of what it sweeps: a 20-cell row and a 200-cell row move at the same rate.
func TestShimmerHasFixedVelocity(t *testing.T) {
	c := NewClock(0)
	c.Latch(base)
	c.SetEpoch(base)
	s := Shimmer{Velocity: 20, Band: 4}

	for _, span := range []int{20, 200} {
		c.Latch(base)
		start0 := s.Position(c, span)
		c.Latch(base.Add(500 * time.Millisecond)) // 10 cells at 20 cells/sec
		start1 := s.Position(c, span)
		if got := start1 - start0; got != 10 {
			t.Fatalf("span %d: band advanced %d cells in half a second at 20 cells/sec", span, got)
		}
	}
}

func TestShimmerWrapsAndStaysInside(t *testing.T) {
	c := NewClock(0)
	c.Latch(base)
	c.SetEpoch(base)
	s := Shimmer{Velocity: 60, Band: 6}
	for ms := 0; ms < 4000; ms += 7 {
		c.Latch(base.Add(time.Duration(ms) * time.Millisecond))
		start, end := s.Range(c, 30)
		if start < 0 || end > 30 || start > end {
			t.Fatalf("band [%d,%d) escaped a 30-cell span at %dms", start, end, ms)
		}
	}
}

func TestShimmerPaintKeepsWidth(t *testing.T) {
	c := NewClock(0)
	c.Latch(base)
	c.SetEpoch(base)
	s := Shimmer{Velocity: 24, Band: 5}
	text := "compiling the plan into a graph"
	for ms := 0; ms < 2000; ms += 13 {
		c.Latch(base.Add(time.Duration(ms) * time.Millisecond))
		if got := Width(s.Paint(c, text, Plain)); got != Width(text) {
			t.Fatalf("shimmer changed the row width at %dms: %d != %d", ms, got, Width(text))
		}
	}
}

// The pulse eases its dwell: it lingers mid-cycle and moves fast at the edges.
func TestPulseDwellIsEased(t *testing.T) {
	c := NewClock(0)
	p := Pulse{Frames: []string{"a", "b", "c", "d", "e", "f", "g", "h"}, Period: time.Second, Ease: 0.6}

	dwell := make([]int, len(p.Frames))
	last := -1
	for ms := 0; ms < 1000; ms++ {
		c.Latch(base.Add(time.Duration(ms) * time.Millisecond))
		i := p.Index(c)
		if i < 0 || i >= len(p.Frames) {
			t.Fatalf("frame index %d out of range at %dms", i, ms)
		}
		if i < last {
			t.Fatalf("the pulse ran backwards at %dms", ms)
		}
		last = i
		dwell[i]++
	}
	edge := dwell[0]
	middle := dwell[len(dwell)/2]
	if middle <= edge {
		t.Fatalf("dwell is not eased: edge frame %d ms, middle frame %d ms", edge, middle)
	}
	if float64(middle) < 1.5*float64(edge) {
		t.Fatalf("dwell barely eased: edge %d ms, middle %d ms", edge, middle)
	}
}

// TestTheCadenceConstantsAreTheDefaults pins the zero-value fallbacks to the
// named keyframe constants. A zero Pulse and a zero Shimmer are what a caller
// gets when it declares one without arguing about numbers, so the defaults ARE
// the house values or the names mean nothing.
func TestTheCadenceConstantsAreTheDefaults(t *testing.T) {
	if got := (Pulse{}).period(); got != DefaultPulsePeriod {
		t.Errorf("a zero Pulse breathes in %v, want the house period %v", got, DefaultPulsePeriod)
	}
	if got := (Pulse{}).ease(); got != DefaultPulseEase {
		t.Errorf("a zero Pulse eases by %v, want %v", got, DefaultPulseEase)
	}
	if got := (Shimmer{}).velocity(); got != DefaultShimmerVelocity {
		t.Errorf("a zero Shimmer sweeps at %v cells/sec, want %v", got, DefaultShimmerVelocity)
	}
	if got := (Shimmer{}).band(); got != DefaultShimmerBand {
		t.Errorf("a zero Shimmer's band is %d cells, want %d", got, DefaultShimmerBand)
	}
	if got := (&Clock{}).interval(); got != DefaultInterval {
		t.Errorf("a zero Clock steps every %v, want the house step %v", got, DefaultInterval)
	}
	// The two periods this package drives must be whole numbers of steps, or a
	// motion cannot be phase-locked to the shared clock.
	for name, period := range map[string]time.Duration{
		"the spinner's rotation": SpinnerPeriod,
		"the breathe":            DefaultPulsePeriod,
	} {
		if period%DefaultInterval != 0 {
			t.Errorf("%s (%v) is not a whole number of %v steps", name, period, DefaultInterval)
		}
	}
	if want := time.Duration(len(Spinner)) * DefaultInterval; SpinnerPeriod != want {
		t.Errorf("SpinnerPeriod = %v, but %d frames at %v is %v",
			SpinnerPeriod, len(Spinner), DefaultInterval, want)
	}
}

func TestPulseFramesAreOneCellWide(t *testing.T) {
	for _, f := range DefaultPulse.Frames {
		if Width(f) != 1 {
			t.Fatalf("pulse frame %q is %d cells wide; trailing text would shift", f, Width(f))
		}
	}
	for _, g := range Spinner {
		if Width(g) != 1 {
			t.Fatalf("spinner glyph %q is %d cells wide", g, Width(g))
		}
	}
}
