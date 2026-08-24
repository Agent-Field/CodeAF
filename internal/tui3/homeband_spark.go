package tui3

import "strings"

// `agents` — WHAT THE MACHINE'S AGENTS HAVE BEEN DOING, AS A SHAPE.
//
//	agents · in flight, the last few minutes
//	▁▁▁▂▂▃▅▇▇▅▃▂▁▁▁▁▂▂▂▁▁▁▁▁▁▁▁▁▁▁
//
// One row of block bars, one bar per reading, newest at the right: how many
// things this machine has had in flight over the last few minutes. It is the
// only chart on this surface and it is here for the reason a chart is ever
// worth its rows — a figure answers "how many" and cannot answer "and was it
// always"; a machine sitting at three workers and a machine that has just
// climbed to three are the same number and two completely different afternoons.
//
// ── THE FIGURE IS SAID ONCE, AND NOT HERE ──
//
// The pulse line at the top of the screen says `3 working` (pulse.go), off the
// same reading this band draws from ([app.machineFactsAt]). So this band prints
// NO NUMBER AT ALL: it is the shape and the pulse is the count, and a chart that
// labelled its own last sample would be the screen saying one fact twice, three
// rows apart, in two spellings that could drift.
//
// ── BUT EVERY MARK HAS A WORD NEAR IT ──
//
// No number is not the same as no words. A bare row of bars under a bare noun
// is the icon-only minimalism docs/DESIGN-LANGUAGE.md refuses by name — the mark
// says it fast and the word says it certainly — and a person meeting this band
// cold read the trace as stray punctuation, which is exactly what an unlabelled
// glyph gets read as. So the heading carries one dim clause saying what the
// shape IS ([machineHandsClause]), and it says the window in prose rather than
// in figures, because a figure here would be the number this band does not
// print arriving through the back door.
//
// ── IT MOVES BECAUSE THE DATA MOVES ──
//
// There is no clock in this file. A sample is taken when home takes its own
// reading of the machine — once per [homeEvery] (homemachine.go's
// [app.sampleHands]) — so the chart slides one bar every three seconds while
// something is running, and the frames that redraw it are the frames the one
// spinner has already woken (homespinner.go). A machine with nothing out takes
// the same samples, they are all the same reading, and the line lies FLAT AND
// STILL: nothing on this band ever animates on its own account, and it costs the
// wire nothing that home was not already spending.
//
// ── AND IT IS ABSENT RATHER THAN EMPTY, AND ABSENT RATHER THAN HALF-DRAWN ──
//
// A window that has been zero from end to end draws NO BAND — not a heading over
// a flat line along the floor, which is the definition of furniture and what the
// emptiness law exists to refuse. The first thing that runs brings the band onto
// the card; three minutes after the last one finishes it goes away again.
//
// AND A RING THAT IS NOT FULL ENOUGH TO BE A SHAPE DRAWS NO BAND EITHER
// ([machineHandsFill]). The ring is in memory and empty at launch, so for the
// first beats of every run there is nothing to draw a chart out of; two or three
// bars beside a heading are not a small chart, they are a mark a person cannot
// tell from punctuation. The band waits until it has a line to show.
//
// ── SAMPLES SURVIVE NOTHING ──
//
// The ring is in memory, per run of this program, and it resets on restart:
// there is no file, nothing under the state directory and no history. Open
// aforge and the chart is absent until the next few beats fill it in. That is the
// honest shape of a reading nobody kept, and the alternative — a spark restored
// from disk — would be this card drawing yesterday's machine as though it were
// now.

// ── THE GEOGRAPHY: WHY IT SITS SECOND ──
//
// The machine card reads `keeping an eye on` · `hands` · `since you left` ·
// `today`, and the chart takes [bandOrderHands] — between the watchlist's 25 and
// `since you left`'s 35 — for two reasons and not one.
//
// IT IS THE ONLY BAND ON THIS CARD ABOUT NOW. `keeping an eye on` is the future
// (what will wake, and when), `since you left` is the past (what happened while
// you were away), `today` is the past summed. A chart of the last three minutes
// is the present tense, and the present belongs high, beside the one band that
// can also be moving this instant — the watchlist's marks turn in the same hue
// the trace's newest end is drawn in, and the two read as one statement about
// right now.
//
// AND IT DOES NOT TOUCH `today`'S PLACE. `today` is the card's closing
// arithmetic and has been the last band on it since the card existed; the day's
// figures are what a glance ends on, and a chart wedged under them would have
// left the card finishing on a shape rather than on a number. The keys are
// spaced so a band lands between two others without moving either
// (homebands.go's third law), and nothing above or below this one moved.
func init() {
	registerHomeBand(homeBand{
		name:  "agents",
		order: bandOrderHands,
		kinds: []bandKind{bandKindMachine},
		draw:  drawHandsBand,
	})
}

// machineHandsWord is the band's heading, quoted in internal/manual/chat/home.md
// exactly as it is spelled here. The heading says `agents` — the owner's word
// for what a person is looking at — while the card's own reading keeps its
// internal name ([machineFacts.hands]); the two are one fact and this constant
// is the only place the person-facing spelling of it lives.
const machineHandsWord = "agents"

// machineHandsClause is the word beside the mark: what the shape IS, said in the
// clause grammar the rest of this surface uses (` · ` and a lower-case phrase).
//
// IT NAMES THE WINDOW IN PROSE AND NEVER IN FIGURES. The ring is
// [handsRingSize] readings on home's [homeEvery] beat, which is three minutes —
// but a chart whose whole law is that it prints no number may not print one in
// its own label, and "the last few minutes" stays true if either constant ever
// moves. It is DROPPED rather than truncated on a card too narrow to hold it
// ([drawHandsBand]), which is the rule every chip row on this surface follows:
// half a phrase is worse than the heading on its own.
const machineHandsClause = " · in flight, the last few minutes"

// machineHandsRows is how tall the chart is drawn. ONE ROW IS THE WHOLE BUDGET:
// this is a band on a card of bands, sitting between two lists a person actually
// reads, and the reading it draws — a small count of things in flight — has four
// or five values in it on a busy afternoon. A second row would buy vertical
// resolution the data does not have and spend a row the card does.
const machineHandsRows = 1

// machineHandsFloor is the narrowest chart worth drawing. Under twelve cells the
// trace is a smudge — twelve samples of a sixty-sample window, in less room than
// the heading over it — and a shape nobody can read is a shape not worth the row
// it costs.
const machineHandsFloor = 12

// machineHandsFill is how many readings the ring must hold before this band
// draws anything at all, and it IS [machineHandsFloor] rather than a figure of
// its own.
//
// One sample is one cell ([barSpark]), so a ring of N readings is a chart N
// cells wide — and a chart narrower than the narrowest width this band will draw
// at is the same unreadable smudge whether the card was too narrow or the
// program had only just started. One constant answers both, and a second one
// beside it would be the same judgment written down twice.
const machineHandsFill = machineHandsFloor

// drawHandsBand is that chart.
//
// ── WHERE IT DOES NOT DRAW ──
//
//   - AT THE LIST TIER. Under [homeMinDetail] home is the list and nothing else
//     (homebridge.go's [homeTierList]) — there is no card for this band to be on.
//     The test asserts it anyway and the check is written anyway, because the
//     claim being made is about WIDTH and not about which pane happens to exist
//     today: a chart needs its width, and the pulse's `N working` is what a
//     narrow frame keeps of this fact.
//
//   - AND ON THE TWO TIERS THAT CANNOT READ A SHAPE. A SPARKLINE IS SHAPE
//     (spark.go): a terminal without box drawing renders thirty replacement
//     characters, and a surface being read aloud reads the bars out one at a
//     time, three times a minute, forever. Both keep the count on the pulse,
//     which is the fact.
func drawHandsBand(a *app, ctx bandContext) []string {
	if a.home.tier == homeTierList || ctx.width < machineHandsFloor {
		return nil
	}
	if a.pal.ascii || a.linear {
		return nil
	}
	// THE READING IS ASKED FOR RATHER THAN LOOKED UP, because asking is what
	// takes the sample: [app.machineFactsAt] is the machine's own beat and this
	// band draws whatever that beat has put in the ring.
	a.machineFactsAt(ctx.now)
	if len(a.handsRing) < machineHandsFill {
		return nil
	}
	trace := barSpark(a.handsRing, 0, ctx.width)
	if trace == "" {
		return nil
	}
	head := machineHandsWord
	if len(head+machineHandsClause) <= ctx.width {
		head = ctx.pal.muted(head) + ctx.pal.dim(machineHandsClause)
	} else {
		head = ctx.pal.muted(fit(head, ctx.width))
	}
	return []string{head, handsTrace(ctx.pal, trace)}
}

// handsTrace paints the row of bars as a DEPTH FADE: the newest end at the
// front, everything older receding toward the background.
//
// ── LIGHTNESS IS RECENCY, AND HUE IS NEVER MAGNITUDE ──
//
// docs/DESIGN-LANGUAGE.md gives this surface exactly one gradient — depth fade
// over zebra — and one thing for it to mean: "a gradient tells you which end is
// which". So the ramp runs along TIME and not along the reading. A colormap
// keyed to the count (quiet green climbing to a loud red) would be a new colour
// meaning invented for one band, on a chart whose whole law is that it is about
// change and not about size, and the eye would read the loud end as an alarm
// nobody raised.
//
// ── THE NEWEST END IS `muted` AND THE REST IS THE THINKING WINDOW'S OWN RAMP ──
//
// [hueMuted] is what this surface paints WORK IN FLIGHT in everywhere — the
// still `●` on a moving row, the spinner, the watch clause while a pass is
// running (docs/HOME-BRIDGE.md's colour law) — so the bar for what is happening
// right now wears the hue that fact already has on this card, and the watchlist
// two bands up reads as one statement with it. Older bars step back through
// [thoughtFade]'s stops, which are [hueDim] at three opacities and the only
// authored gradient this palette has; no value is minted here. Never the accent:
// the budget is one lit element per screen and it belongs to the live or chosen
// thing, which three minutes of history is not. Never [hueData] either — that
// hue is for a datum inside a sentence, and this row is not a sentence.
//
// ── AND WHERE THERE IS NO GRADIENT THERE IS ONE TIER ──
//
// Below the 256-colour rung, and under NO_COLOR, [palette.fade] has nothing to
// spend and answers in the dim tier for every stop ([palette.fading] is the one
// place that is decided). A trace drawn half in `muted` and half in `dim` there
// would be a two-step ramp claiming to be a gradient, so the whole row comes
// back in the one tier the newest end would have worn — exactly the degradation
// the thinking window already makes.
func handsTrace(pal palette, trace string) string {
	bars := []rune(trace)
	if len(bars) == 0 {
		return ""
	}
	if !pal.fading() {
		return pal.muted(trace)
	}
	// The stops, oldest first, with the live tier on the end: the fade ramp is
	// authored newest-last ([palette.fade]), so reading it in order and adding
	// `muted` after it is the whole ladder in the order the row is drawn.
	tiers := len(pal.ramp.fade) + 1
	var out strings.Builder
	for at, bar := range bars {
		tier := at * tiers / len(bars)
		if tier >= tiers-1 {
			out.WriteString(pal.muted(string(bar)))
			continue
		}
		out.WriteString(pal.fade(string(bar), tier))
	}
	return out.String()
}
