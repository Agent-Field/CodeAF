package tui3

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// revealClock pins the surface's clock seam and hands back the two things a
// walk test needs: a way to spend one slot of the paint clock, and the reading
// the walk is measured against.
//
// THE WALK IS A FUNCTION OF TIME (reveal.go), so a test drives it by letting
// time pass rather than by counting paints. [app.clock] is the seam every
// timing test in this package already installs.
func revealClock(a *app) (slot func(), at *time.Time) {
	base := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	now := base
	a.clock = func() time.Time { return now }
	a.revealMoved = now
	return func() {
		now = now.Add(frameInterval)
		a.tickReveal(now)
	}, &now
}

// catchUpReveal walks every live edge to the end of the bytes the stream
// already holds, BY LETTING TIME PASS: the walk is measured from the clock, so
// the way to finish one is to spend its slots. Tests that are about the
// TRANSCRIPT — what a fold produced, whether a forming reply is still source —
// must not also be tests of the walk: they catch up, then look.
func catchUpReveal(a *app) {
	if a == nil {
		return
	}
	at := a.now()
	a.revealMoved = at
	for i := 0; i < revealSlots+4 && a.liveRevealing(); i++ {
		at = at.Add(frameInterval)
		a.tickReveal(at)
	}
}

// ── WHAT IS A LUMP ──────────────────────────────────────────────────────────

func TestAShortBurstIsShownWhole(t *testing.T) {
	var shown int
	text := "it parses."
	catchReveal(&shown, text, len(text), isLump(len(text)), false)
	if shown != len(text) {
		t.Fatalf("a %d-byte burst started at %d, want the whole of it", len(text), shown)
	}
	if revealedText(text, shown, false) != text {
		t.Fatal("a short burst was held back")
	}
}

// A LINE IS NOT A LUMP. Forty bytes is a short line, and walking one moves the
// edge by a few characters twice — motion nobody perceives as writing. Holding
// it back is the surface adding latency and showing nothing for it, which is
// what #440 did and what twelve fixtures caught.
func TestALineFromTheWireLandsWhole(t *testing.T) {
	line := "the window was black, and then it was"
	if len(line) <= revealHead || len(line) > revealAtOnce {
		t.Fatalf("fixture is %d bytes, want past the head and inside the line", len(line))
	}
	var shown int
	catchReveal(&shown, line, len(line), isLump(len(line)), false)
	if revealedText(line, shown, false) != line {
		t.Fatalf("a %d-byte wire event was held back at %d", len(line), shown)
	}
	if revealing(shown, line, false) {
		t.Fatal("a line opened a walk")
	}
}

// AND A FOLD IS NOT A LUMP EITHER, however long the fold is. This drives the
// real folding lane — [waitEvent] over a channel already holding two short
// deltas — because the fold is the only thing that can answer the question, and
// asserting it on [catchReveal] alone would prove nothing about the plumbing.
func TestAFoldOfShortDeltasLandsWhole(t *testing.T) {
	parts := []string{"the loader reads the map, ", "and the map is never made."}
	whole := strings.Join(parts, "")
	if len(whole) <= revealAtOnce {
		t.Fatalf("the fold is %d bytes, want it past the line so the test can fail", len(whole))
	}
	ch := make(chan session.Event, len(parts))
	for _, part := range parts {
		ch <- session.Event{Kind: session.EventTextDelta, Text: part}
	}
	msg, _ := waitEvent(ch, 0)().(streamEventMsg)
	if msg.ev.Text != whole {
		t.Fatalf("the lane folded %q, want %q", msg.ev.Text, whole)
	}
	if msg.lump {
		t.Fatalf("a fold of %d- and %d-byte deltas was called a lump",
			len(parts[0]), len(parts[1]))
	}

	// AND THE PAGE AGREES, with no frame at all: what the wire delivered is on
	// the screen on the event.
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 80, 24
	a.applyEvent(msg.ev, msg.lump)
	if a.live < 0 {
		t.Fatal("the fold did not open a live block")
	}
	if got := a.entries[a.live].revealed(); got != whole {
		t.Fatalf("the fold drew %q, want the whole of it", got)
	}
}

// A SINGLE WIRE EVENT PAST THE LINE IS THE ONE THING THAT WALKS.
func TestALumpOpensOnItsHeadAndCatchesOnTheClock(t *testing.T) {
	lump := strings.Repeat("the loader never makes the map. ", 13)
	if len(lump) < 400 {
		t.Fatalf("the fixture is %d bytes, want the paragraph a stalled wire dumps", len(lump))
	}
	var shown int
	catchReveal(&shown, lump, len(lump), isLump(len(lump)), false)
	if shown <= 0 || shown > revealHead {
		t.Fatalf("a lump opened at %d, want at most the %d-byte head", shown, revealHead)
	}

	// Eight slots is the snappy bound: whatever the size, the last slot of the
	// window has caught up. One slot at a time, so the edge is seen to MOVE
	// rather than snap on the first paint.
	frames := 0
	for revealing(shown, lump, false) {
		frames++
		if !advanceReveal(&shown, lump, 1, false) {
			t.Fatal("the edge stopped moving while bytes were still unread")
		}
		if frames > revealSlots+1 {
			t.Fatalf("a lump took %d slots to arrive, want at most %d", frames, revealSlots)
		}
	}
	if shown != len(lump) {
		t.Fatalf("caught up at %d of %d", shown, len(lump))
	}
	if frames < 2 {
		t.Fatal("a lump landed in one slot: the edge never moved")
	}
}

// A BURST ARRIVING MID-WALK JOINS THE WALK. It must not jump the edge to the
// end — the reader would see the paragraph they were watching arrive finish
// itself the instant one more token landed — and it must not open a second one.
func TestAShortBurstMidWalkJoinsTheWalk(t *testing.T) {
	lump := strings.Repeat("the loader never makes the map. ", 13)
	var shown int
	catchReveal(&shown, lump, len(lump), true, false)
	advanceReveal(&shown, lump, 1, false)
	was := shown
	if was >= len(lump) {
		t.Fatal("the lump caught up in one slot")
	}

	grown := lump + " and then it was."
	catchReveal(&shown, grown, len(grown)-len(lump), false, false)
	if shown != was {
		t.Fatalf("a short burst moved the edge from %d to %d instead of joining the walk", was, shown)
	}
	if !revealing(shown, grown, false) {
		t.Fatal("a short burst ended a walk that was still running")
	}
	for i := 0; i < revealSlots+2 && revealing(shown, grown, false); i++ {
		advanceReveal(&shown, grown, 1, false)
	}
	if shown != len(grown) {
		t.Fatalf("the walk caught up at %d of %d", shown, len(grown))
	}
}

func TestLinearShowsTheBurstAtOnce(t *testing.T) {
	lump := strings.Repeat("αβγ ", 30)
	var shown int
	catchReveal(&shown, lump, len(lump), true, true)
	if shown != len(lump) {
		t.Fatalf("linear opened at %d of %d, want everything", shown, len(lump))
	}
}

func TestTheEdgeLandsOnARuneBoundary(t *testing.T) {
	text := "café 日本語"
	for n := 0; n <= len(text)+3; n++ {
		cut := cutUTF8(text, n)
		if cut < 0 || cut > len(text) {
			t.Fatalf("cutUTF8(%d) = %d, out of range", n, cut)
		}
		if cut < len(text) && !utf8.RuneStart(text[cut]) {
			t.Fatalf("cutUTF8(%d) = %d, mid-rune", n, cut)
		}
	}
}

func TestACaughtUpStreamShowsTheNextWordAtOnce(t *testing.T) {
	var shown int
	text := "hello"
	catchReveal(&shown, text, len(text), false, false)
	text += " there"
	catchReveal(&shown, text, len(" there"), false, false)
	if shown != len(text) {
		t.Fatalf("a short follow-up started at %d of %d, want the whole word", shown, len(text))
	}
}

// ── THE WALK RUNS ON THE CLOCK, NOT ON THE FRAME COUNT ──────────────────────

func TestTheWalkIsMeasuredInTimeAndNotInPaints(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 80, 24
	slot, at := revealClock(a)

	lump := strings.Repeat("the map is never made. ", 20)
	a.applyEvent(text(session.EventTextDelta, lump), true)
	if a.live < 0 {
		t.Fatal("the lump did not open a live block")
	}
	e := &a.entries[a.live]
	opened := e.revealed()
	if opened == e.text {
		t.Fatal("a lump was shown whole on the event")
	}

	// A PAINT THAT FIRES EARLY MOVES NOTHING. The clock has not turned, so the
	// edge has not either — however many times the frame is asked for.
	for i := 0; i < 5; i++ {
		a.tickReveal(*at)
	}
	if e.revealed() != opened {
		t.Fatal("the edge walked without any time passing")
	}

	slot()
	if e.revealed() == opened {
		t.Fatal("a slot of the clock did not walk the edge")
	}
	for i := 0; i < revealSlots+2 && e.revealing(); i++ {
		slot()
	}
	if e.revealed() != e.text {
		t.Fatalf("the clock did not catch the lump: %d of %d", e.edge, len(e.text))
	}
}

// AND A SLOWER LINK IS COVERED BY ELAPSED TIME. Three slots' worth of clock in
// one paint walks three slots, which is what makes a lump take the same quarter
// of a second over a connection as it does at the machine.
func TestOnePaintCoversEveryElapsedSlot(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	lump := strings.Repeat("the map is never made. ", 20)
	base := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	a.clock = func() time.Time { return base }
	a.revealMoved = base
	a.applyEvent(text(session.EventTextDelta, lump), true)
	e := &a.entries[a.live]

	one := *e
	oneShown := one.edge
	advanceReveal(&oneShown, one.text, 3, false)
	a.tickReveal(base.Add(3 * frameInterval))
	if e.edge != oneShown {
		t.Fatalf("one paint after three slots walked to %d, want the three-slot stride %d",
			e.edge, oneShown)
	}
}

func TestMeterEaseIsFastThenFine(t *testing.T) {
	shown := 0
	target := 150
	steps := []int{}
	for shown != target {
		next := easeInt(shown, target, 1)
		if next <= shown && target > shown {
			t.Fatalf("easeInt(%d, %d) = %d, did not advance", shown, target, next)
		}
		shown = next
		steps = append(steps, shown)
		if len(steps) > 12 {
			t.Fatalf("150 tokens took %d frames to arrive: %v", len(steps), steps)
		}
	}
	if len(steps) < 3 {
		t.Fatalf("150 tokens arrived in %d steps, want a walk: %v", len(steps), steps)
	}
	// First step takes the most — ease-out, not a linear count.
	if steps[0] < steps[1]-steps[0] {
		t.Fatalf("the first step was not the largest: %v", steps)
	}
}

// ── THE FIGURES COUNT UP ────────────────────────────────────────────────────

// THE STATUS LINE'S FIGURES WALK, AND THIS IS WHERE THAT IS PROVEN.
//
// #437 shipped the claim and not the behaviour: the eased token total was read
// by no renderer at all, and the context weight was pinned to the reading it was
// supposed to be easing towards, so two of the three figures could not have
// moved. Nothing on a real screen counted up. The three readers are asserted
// here together, under a pinned clock, because the defect was invisible exactly
// where the machinery looked right.
func TestTheStatusLineFiguresCountUp(t *testing.T) {
	agent := &fakeAgent{model: "m", weight: 4000}
	a := newTestApp(agent)
	base := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	now := base
	a.clock = func() time.Time { return now }
	a.ctxWindow = 1_000_000
	a.state = stateWorking
	a.revealMoved = now
	a.measureContext()

	// A turn's first reading, and a weight the pass under it has just changed.
	a.take(session.Usage{CostUSD: 0.0010, Input: 300, Output: 100})
	agent.weight = 40_000
	a.measureContext()
	if a.spendDrawn() != 0 || a.tokensDrawn() != 0 || a.ctxDrawn() != 4000 {
		t.Fatalf("the figures jumped on the event: $%v %d tok %d ctx",
			a.spendDrawn(), a.tokensDrawn(), a.ctxDrawn())
	}

	// A SECOND READING MID-WALK RAISES THE TARGET, it does not restart the walk.
	var costs []float64
	var toks, ctxs []int
	for i := 0; a.meterChasing; i++ {
		if i > 40 {
			t.Fatalf("the figures were still walking after %d slots: %v", i, costs)
		}
		now = now.Add(frameInterval)
		a.tickReveal(now)
		costs = append(costs, a.spendDrawn())
		toks = append(toks, a.tokensDrawn())
		ctxs = append(ctxs, a.ctxDrawn())
		if i == 3 {
			a.take(session.Usage{CostUSD: 0.0026, Input: 3000, Output: 1000})
		}
	}

	// Each figure passed through readings that are neither where it started nor
	// where it landed — which is the whole of "counts up" — and rose the whole
	// way, because a meter that went backwards would be a figure disagreeing
	// with the books it is chasing.
	for _, m := range []struct {
		name  string
		steps []float64
		want  float64
	}{
		{"the bill", costs, 0.0026},
		{"the token total", asFloats(toks), 4000},
		{"the context weight", asFloats(ctxs), 40000},
	} {
		between := 0
		for i, at := range m.steps {
			if i > 0 && at < m.steps[i-1] {
				t.Fatalf("%s went backwards at step %d: %v", m.name, i, m.steps)
			}
			if at > 0 && at < m.want {
				between++
			}
		}
		if between < 2 {
			t.Fatalf("%s jumped — %d readings between nothing and %v: %v",
				m.name, between, m.want, m.steps)
		}
		if got := m.steps[len(m.steps)-1]; got != m.want {
			t.Fatalf("%s landed on %v, want the books' %v", m.name, got, m.want)
		}
	}

	// AND THE SNAP RULE HOLDS. The moment the turn is no longer running the
	// exact books are drawn, with no frame needed to make it so.
	a.state = stateWorking
	a.take(session.Usage{CostUSD: 0.0100, Input: 9000, Output: 1000})
	if a.spendDrawn() == 0.0100 {
		t.Fatal("a fresh reading did not start a walk")
	}
	a.state = stateInterrupted
	if a.spendDrawn() != 0.0100 || a.tokensDrawn() != 10000 {
		t.Fatalf("a stopped turn still drew a figure in motion: $%v %d tok",
			a.spendDrawn(), a.tokensDrawn())
	}
}

func asFloats(in []int) []float64 {
	out := make([]float64, len(in))
	for i, n := range in {
		out[i] = float64(n)
	}
	return out
}

// A WIDE PAINT EASES, IT DOES NOT LAND.
//
// The catch fraction is per SLOT and a paint is as many slots wide as the time
// since the last one. Multiplying the fraction by the slots — which is what this
// file used to do — gives 135% of the gap at three slots, and every caller reads
// an overshoot as "land now". A terminal that is keeping up paints one slot at a
// time and hid it completely; a loaded one paints three, and every figure on the
// status line jumped in a single frame. Only a multi-slot assertion catches it.
func TestAWidePaintStillEases(t *testing.T) {
	for _, slots := range []int{2, 3, 5} {
		if got := caught(slots); got >= 1 {
			t.Fatalf("%d slots close %.2f of the gap — an ease that overshoots", slots, got)
		}
		if got, one := caught(slots), caught(1); got <= one {
			t.Fatalf("%d slots close %.2f, no more than one slot's %.2f", slots, got, one)
		}
		if got := easeInt(0, 10_000, slots); got == 10_000 {
			t.Fatalf("a %d-slot paint landed the token total in one frame", slots)
		}
		if got := easeCost(0, 0.10, slots); got == 0.10 {
			t.Fatalf("a %d-slot paint landed the bill in one frame", slots)
		}
		lump := strings.Repeat("the map is never made. ", 20)
		if got := revealStride(len(lump), slots); got >= len(lump) {
			t.Fatalf("a %d-slot paint drew the whole lump in one frame", slots)
		}
	}
}
