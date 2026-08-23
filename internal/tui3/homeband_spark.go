package tui3

// `hands` — WHAT THE MACHINE'S HANDS HAVE BEEN DOING, AS A SHAPE.
//
//	hands
//	⢀⡠⠔⠒⠑⠢⡀⠀⠀⢀⠔⠊⠉⠉⠑⠢⢄⡀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀
//	⠊⠁⠀⠀⠀⠀⠈⠑⠒⠁⠀⠀⠀⠀⠀⠀⠀⠈⠉⠒⠒⠒⠒⠒⠒⠒⠒⠒⠒⠒
//
// Two rows of braille dots, one dot per reading, newest at the right: how many
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
// ── IT MOVES BECAUSE THE DATA MOVES ──
//
// There is no clock in this file. A sample is taken when home takes its own
// reading of the machine — once per [homeEvery] (homemachine.go's
// [app.sampleHands]) — so the chart slides one dot every three seconds while
// something is running, and the frames that redraw it are the frames the one
// spinner has already woken (homespinner.go). A machine with nothing out takes
// the same samples, they are all the same reading, and the line lies FLAT AND
// STILL: nothing on this band ever animates on its own account, and it costs the
// wire nothing that home was not already spending.
//
// ── AND IT IS ABSENT RATHER THAN EMPTY ──
//
// A window that has been zero from end to end draws NO BAND — not a heading over
// a flat line along the floor, which is the definition of furniture and what the
// emptiness law exists to refuse. The first thing that runs brings the band onto
// the card; three minutes after the last one finishes it goes away again.
//
// ── SAMPLES SURVIVE NOTHING ──
//
// The ring is in memory, per run of this program, and it resets on restart:
// there is no file, nothing under the state directory and no history. Open
// aforge and the chart is empty until the next few beats fill it in. That is the
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
// the trace is drawn in, and the two read as one statement about right now.
//
// AND IT DOES NOT TOUCH `today`'S PLACE. `today` is the card's closing
// arithmetic and has been the last band on it since the card existed; the day's
// figures are what a glance ends on, and a chart wedged under them would have
// left the card finishing on a shape rather than on a number. The keys are
// spaced so a band lands between two others without moving either
// (homebands.go's third law), and nothing above or below this one moved.
func init() {
	registerHomeBand(homeBand{
		name:  "hands",
		order: bandOrderHands,
		kinds: []bandKind{bandKindMachine},
		draw:  drawHandsBand,
	})
}

// machineHandsWord is the band's heading, quoted in internal/manual/chat/home.md
// exactly as it is spelled here. It is the word the card's own reading uses for
// the thing ([machineFacts.hands]) rather than a second name for it.
const machineHandsWord = "hands"

// machineHandsRows is how tall the chart is drawn. TWO ROWS IS EIGHT STEPS of
// height in braille and it is the whole budget: this is a band on a card of
// bands, sitting between two lists a person actually reads, and a chart that
// took four rows would have made the card about the chart.
const machineHandsRows = 2

// machineHandsFloor is the narrowest chart worth drawing. Under twelve cells the
// trace is a smudge — twenty-four samples of a sixty-sample window, squeezed
// into less room than the heading over it — and a shape nobody can read is a
// shape not worth the two rows it costs.
const machineHandsFloor = 12

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
//     characters, and a surface being read aloud reads the dots out one at a
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
	rows := brailleSpark(a.handsRing, machineHandsRows, ctx.width)
	if len(rows) == 0 {
		return nil
	}
	// THE HEADING IS STRUCTURE AND THE TRACE IS A MOVING THING, and they take the
	// two roles this card already spends on exactly those: [hueMuted] for the
	// label, as every band on this card wears, and [hueMuted] again for the dots,
	// which is the hue this surface paints WORK IN FLIGHT in everywhere — the
	// still `●` on a moving row, the spinner, the watch clause while a pass is
	// running (docs/HOME-BRIDGE.md's colour law). Never the accent: the budget is
	// one lit element per screen and it belongs to the live or chosen thing,
	// which a chart of the last three minutes is not.
	out := make([]string, 0, len(rows)+1)
	out = append(out, ctx.pal.muted(fit(machineHandsWord, ctx.width)))
	for _, row := range rows {
		out = append(out, ctx.pal.muted(row))
	}
	return out
}
