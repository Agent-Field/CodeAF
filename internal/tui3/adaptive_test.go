package tui3

import (
	"image/color"
	"reflect"
	"sort"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── THE DERIVATION, HELD TO THE LADDER IT CLAIMS TO BUILD ───────────────────
//
// adaptive.go's whole surface is pure functions from ONE measured colour to a
// finished ladder, which is what makes this file a table of grounds rather than
// a set of rendered screens. Every case below is a real terminal background:
// what people actually run, plus the two extremes the authored ladder could not
// aim at and the mid-tone that no ladder can be built on at full height.

// groundCase is one terminal's background and what makes it worth naming.
type groundCase struct {
	name  string
	hex   string
	dark  bool
	notes string
}

// The grounds. Each is either a background a lot of people are looking at right
// now or a case that broke something.
var groundCases = []groundCase{
	{"black", "#000000", true,
		"the extreme the authored ladder overshoots: the body climbs past 15:1 and reads as glare"},
	{"tokyonight", "#1a1b26", true, "the middle of the assumed dark range"},
	{"catppuccin", "#1e1e2e", true, "the light end of the assumed dark range"},
	{"gruvbox", "#282828", true, "a warm dark ground outside the assumed range"},
	{"white", "#FFFFFF", false, "the ground the light ladder was authored against"},
	{"nord page", "#ECEFF4", false,
		"THE NAMED DEFECT: styles.go says the whole light ladder drops close to invisible here"},
	{"solarized page", "#FDF6E3", false,
		"a strongly tinted cream — the case that proves a ground step may not hold HSL saturation"},
	{"silver", "#C0C0C0", false, "a light ground with real distance from white"},
	{"mid grey", "#808080", false,
		"the ground no ladder fits at full height: every band has to shrink together"},
}

func groundOf(t *testing.T, hex string) measuredGround {
	t.Helper()
	r, g, b, ok := parseHex(hex)
	if !ok {
		t.Fatalf("malformed ground %q", hex)
	}
	return measuredGround{r: r, g: g, b: b}
}

// ── THE GROUND LADDER, DERIVED ──────────────────────────────────────────────

// EVERY STEP LANDS ON ITS STATED RATIO AGAINST THE GROUND THAT WAS MEASURED —
// which is the entire difference between authoring and deriving. The authored
// ladder aims at three ratios and hits them on one terminal; this hits them on
// the terminal a person is actually using.
//
// The tolerance is a byte's worth of rounding and nothing more. A ratio is
// solved by bisection onto an eight-bit channel, so the last step of the search
// is the width of one value.
func TestTheGroundLadderIsDerivedFromTheMeasuredGround(t *testing.T) {
	const slack = 0.03
	for _, ground := range groundCases {
		t.Run(ground.name, func(t *testing.T) {
			m := groundOf(t, ground.hex)
			at := luminanceOf(m.r, m.g, m.b)
			got := adaptRamp(m)
			for _, step := range []struct {
				name string
				step hue
				want float64
			}{
				{"cursor", got.cursor, groundCursorRatio},
				{"selected", got.selected, groundSelectedRatio},
				{"mark", got.mark, groundMarkRatio},
			} {
				measured := contrastOn(step.step, at)
				if measured < step.want-slack || measured > step.want+slack {
					t.Fatalf("the %s step is %s:1 against %s, want %s:1 — %s",
						step.name, trimFloat(measured), ground.hex,
						trimFloat(step.want), ground.notes)
				}
				// A GROUND IS NEVER A TINT, whatever the terminal's own hue is.
				// The truecolor value carries the theme's colour; the 256 rung
				// may only ever land on the grey ramp.
				if step.step.idx < 232 {
					t.Fatalf("the derived %s step resolves to xterm-256 %d, which is in the "+
						"colour cube — a ground must land on the grey ramp (232-255)",
						step.name, step.step.idx)
				}
			}
			// AND THE THREE STEPS ARE THREE, on the rung where colours are
			// rounded as well as on the rung where they are not.
			seen := map[uint8]string{}
			for name, step := range map[string]hue{
				"cursor": got.cursor, "selected": got.selected, "mark": got.mark,
			} {
				if other, clash := seen[step.idx]; clash {
					t.Fatalf("the derived %s and %s steps both resolve to xterm-256 %d — "+
						"two states of one row become one state on every 256-colour terminal",
						name, other, step.idx)
				}
				seen[step.idx] = name
			}
		})
	}
}

// A DERIVED GROUND STEP CARRIES THE TERMINAL'S OWN TINT AND NEVER MORE OF IT
// THAN THE TERMINAL HAS.
//
// This is the defect a strongly tinted page found. HSL saturation is a ratio to
// the room a colour has left, so a cream eight points off the top of the scale
// measures as saturated as a highlighter — and a step derived by holding that
// saturation while lowering the lightness lands on gold, which is a ground that
// looks like it means something. The tint is held in absolute terms instead, so
// the spread between a step's channels is the spread between the ground's.
func TestADerivedGroundStepDoesNotAmplifyTheTerminalsTint(t *testing.T) {
	for _, ground := range groundCases {
		t.Run(ground.name, func(t *testing.T) {
			m := groundOf(t, ground.hex)
			spread := func(r, g, b uint8) int {
				hi := max(max(int(r), int(g)), int(b))
				lo := min(min(int(r), int(g)), int(b))
				return hi - lo
			}
			was := spread(m.r, m.g, m.b)
			got := adaptRamp(m)
			for name, step := range map[string]hue{
				"cursor": got.cursor, "selected": got.selected, "mark": got.mark,
			} {
				if now := spread(step.r, step.g, step.b); now > was {
					t.Fatalf("the %s step spreads its channels %d values against the ground's %d — "+
						"the terminal's tint was amplified rather than carried (%s)",
						name, now, was, ground.hex)
				}
			}
		})
	}
}

// ── THE READING TIERS ───────────────────────────────────────────────────────

// EVERY READING TIER LANDS IN ITS BAND, AND THE THREE STAY A LADDER.
//
// The bands are disjoint and ordered by construction (adaptive.go), so a tier
// pushed to either edge of its own band is still louder than the tier below it.
// What this test is really holding is the case that is not obvious: a ground
// that cannot carry the ladder at full height, where all three bands shrink by
// one factor rather than three tiers piling up against the same wall.
func TestTheReadingTiersLandInTheirBands(t *testing.T) {
	for _, ground := range groundCases {
		t.Run(ground.name, func(t *testing.T) {
			m := groundOf(t, ground.hex)
			at := luminanceOf(m.r, m.g, m.b)
			up := groundIsDark(m)
			got := adaptRamp(m)
			scale := reachScale(at, inkBand.high, up)
			const slack = 0.05
			for _, tier := range []struct {
				name string
				got  hue
				band band
			}{
				{"ink", got.ink, inkBand.scaled(scale)},
				{"muted", got.muted, mutedBand.scaled(scale)},
				{"dim", got.dim, dimBand.scaled(scale)},
			} {
				measured := contrastOn(tier.got, at)
				if measured < tier.band.low-slack || measured > tier.band.high+slack {
					t.Fatalf("the %s tier is %s:1 against %s, want %s-%s:1 — %s",
						tier.name, trimFloat(measured), ground.hex,
						trimFloat(tier.band.low), trimFloat(tier.band.high), ground.notes)
				}
			}
			ink := contrastOn(got.ink, at)
			muted := contrastOn(got.muted, at)
			dim := contrastOn(got.dim, at)
			if !(ink > muted && muted > dim) {
				t.Fatalf("the reading tiers are not a ladder against %s: ink %s, muted %s, dim %s",
					ground.hex, trimFloat(ink), trimFloat(muted), trimFloat(dim))
			}
		})
	}
}

// A VALUE IN BAND IS NOT TOUCHED, and this is the restraint that makes deriving
// safe to turn on for everybody at once.
//
// styles.go's own retune kept two light steps byte-identical because they were
// already inside the band they were aimed at, and this file keeps the same rule:
// a measurement that says the authored value is fine leaves the authored value
// alone, byte for byte, and a measurement that says otherwise moves it to the
// NEAREST EDGE of its band rather than to the middle. So the body ink a person
// on a white terminal has been reading for four waves is the same body ink after
// their terminal answers, and the dark ladder's second voice and murmur survive
// the middle of the range they were aimed at unchanged.
func TestAReadingTierInsideItsBandIsNotTouched(t *testing.T) {
	for _, held := range []struct {
		ground string
		name   string
		was    hue
		got    func(ramp) hue
	}{
		{"#FFFFFF", "light ink", lightInk, func(r ramp) hue { return r.ink }},
		{"#1a1b26", "dark muted", hueMuted, func(r ramp) hue { return r.muted }},
		{"#1a1b26", "dark dim", hueDim, func(r ramp) hue { return r.dim }},
		// #1e1e2e rather than the #282828 this case was first written against, and
		// the swap is THE GLARE LAW arriving in the same wave as this file. #282828
		// was picked as a dark ground the authored ink cleared comfortably — and it
		// did, at 10.91:1, while the ink was the brighter white the law came down
		// from. The calmed ink measures 9.23:1 there, under [inkBand]'s floor, so
		// the derivation LIFTS it and is right to: a body under nine and a half to
		// one on a grey-black screen is the "leaning toward the screen" end of the
		// band, and holding the case would have meant asserting that the correction
		// stays switched off exactly where it is wanted. #1e1e2e is catppuccin's own
		// background, inside the assumed range styles.go authored against, and the
		// calmed ink clears it at 10.27:1 — which is what this case was ever
		// testing: a ground the authored value already fits.
		{"#1e1e2e", "dark ink", hueInk, func(r ramp) hue { return r.ink }},
		// AND THE LIVE TIER IS HELD BY THE SAME RULE, on the ground its own step was
		// authored against. It is a RELATIVE band ([liveStep]) rather than one of
		// the three above, so "in band is not touched" has to be shown to survive
		// the extra arithmetic rather than assumed to.
		{"#1a1b26", "dark live", hueLive, func(r ramp) hue { return r.live }},
		{"#FFFFFF", "light live", lightLive, func(r ramp) hue { return r.live }},
	} {
		got := held.got(adaptRamp(groundOf(t, held.ground)))
		if got.r != held.was.r || got.g != held.was.g || got.b != held.was.b {
			t.Fatalf("%s was in band against %s and moved anyway: "+
				"#%02X%02X%02X became #%02X%02X%02X",
				held.name, held.ground, held.was.r, held.was.g, held.was.b, got.r, got.g, got.b)
		}
	}
	// And the two grounds the authored ladder could not aim at DO move, or this
	// whole wave bought nothing.
	black := adaptRamp(groundOf(t, "#000000"))
	if black.ink == hueInk {
		t.Fatal("the body ink did not move on a black terminal, which is the ground it overshoots")
	}
	nord := adaptRamp(groundOf(t, "#ECEFF4"))
	if nord.cursor == lightCursor || nord.selected == lightSelected || nord.mark == lightMark {
		t.Fatal("the ground ladder did not move on a tinted page, which is the defect styles.go names")
	}
}

// THE LIVE TIER FOLLOWS THE INK IT IS A STEP ABOVE, ON EVERY GROUND.
//
// [hueLive] is the one reading tier defined as a RELATION rather than as a
// contrast: it means "the body, still arriving", so what it owes is a stated
// distance from whatever the body ends up being on THIS terminal, not a number
// of its own. adaptive.go's [liveOver] is that rule, and this is the table that
// holds it — including the two answers that are degradations rather than
// failures.
//
// EQUAL IS A LEGAL READING. Where a ground has no headroom left above its body
// ink the tier is ABSENT, which styles.go's own note says is the right failure:
// a streaming reply looks exactly as it looked before the effect existed, rather
// than taking a step too small to see and calling it delivered.
func TestTheLiveTierFollowsTheDerivedInk(t *testing.T) {
	for _, ground := range groundCases {
		t.Run(ground.name, func(t *testing.T) {
			m := groundOf(t, ground.hex)
			at := luminanceOf(m.r, m.g, m.b)
			got := adaptRamp(m)
			ink := contrastOn(got.ink, at)
			live := contrastOn(got.live, at)
			if live < ink {
				t.Fatalf("the live tier is %s:1 against %s and the body ink is %s:1 — "+
					"a reply that is still arriving may never be QUIETER than the settled "+
					"text beside it (%s)",
					trimFloat(live), ground.hex, trimFloat(ink), ground.notes)
			}
			if got.live == got.ink {
				// The absence. It is only honest where the ground really has run out,
				// so the claim is checked rather than accepted.
				if reachOf(at, groundIsDark(m)) >= ink*liveStep.low {
					t.Fatalf("the live tier collapsed onto the body ink against %s, "+
						"which still reaches %s:1 — there was room for a step and none "+
						"was taken", ground.hex, trimFloat(reachOf(at, groundIsDark(m))))
				}
				return
			}
			// A byte's worth of rounding, for the reason every band in this file
			// allows one: the ratio is solved by bisection onto an eight-bit channel.
			const slack = 0.02
			if step := live / ink; step < liveStep.low-slack || step > liveStep.high+slack {
				t.Fatalf("the live tier stands %s× the body ink against %s, want %s-%s× — %s",
					trimFloat(step), ground.hex,
					trimFloat(liveStep.low), trimFloat(liveStep.high), ground.notes)
			}
			// AND IT STAYS A READING TIER. The reading ladder is grey by
			// construction, and a live tier that rounded into the colour cube would
			// be a paragraph that looked like it meant something.
			if got.live.idx < 232 && got.ink.idx >= 232 {
				t.Fatalf("against %s the body ink rounds to the grey ramp (%d) and its "+
					"live tier rounds into the colour cube (%d)",
					ground.hex, got.ink.idx, got.live.idx)
			}
		})
	}
}

// ── THE SIGNAL BAND ─────────────────────────────────────────────────────────

// THE SIGNAL HUES KEEP THEIR FIFTEEN-POINT BAND ON EVERY GROUND, which is the
// isoluminant law surviving derivation.
//
// It survives by construction rather than by luck: the only move this file may
// make on the signals is ONE lightness step taken by all seven together, which
// preserves the spread exactly, and where that step runs into black or white it
// can only compress the spread further. TestTheSignalHuesAreIsoluminant holds
// the authored sets; this holds every set that can be derived from them.
func TestTheDerivedSignalHuesKeepTheirBand(t *testing.T) {
	for _, ground := range groundCases {
		t.Run(ground.name, func(t *testing.T) {
			m := groundOf(t, ground.hex)
			got := adaptRamp(m)
			var lightness []float64
			for _, h := range []hue{got.accent, got.add, got.del, got.bad, got.ask, got.warn, got.data} {
				lightness = append(lightness, lightnessOf(h))
			}
			sort.Float64s(lightness)
			// The allowance is one byte, and it is the only slack this law is
			// given: a lifted hue is rounded back onto eight bits a channel, and
			// half a value at each end of a seven-colour set is about four tenths
			// of a point of lightness. Anything wider than that is the set having
			// been moved by more than one step, which is the failure.
			const rounding = 100.0 / 255
			if spread := lightness[len(lightness)-1] - lightness[0]; spread > signalBand+rounding {
				t.Fatalf("the derived signal hues spread %s points of lightness against %s, "+
					"want at most %s — the set may only be moved as one",
					trimFloat(spread), ground.hex, trimFloat(signalBand))
			}
		})
	}
}

// A SIGNAL SET THAT ALREADY CLEARS THE FLOOR IS NOT MOVED, and one that does not
// is moved until the worst of it does.
//
// The floor is deliberately below the reading tiers' — a signal is a plus sign,
// a cross or a single word, and styles.go spends real care keeping the set quiet.
// What is being caught is not "dim", it is a ground so close to the signal band
// that the seven stop separating from the page at all.
func TestTheSignalHuesAreVerifiedAgainstTheMeasuredGround(t *testing.T) {
	worstOn := func(r ramp, at float64) float64 {
		worst := 0.0
		for i, h := range []hue{r.accent, r.add, r.del, r.bad, r.ask, r.warn, r.data} {
			if got := contrastOn(h, at); i == 0 || got < worst {
				worst = got
			}
		}
		return worst
	}
	for _, ground := range groundCases {
		t.Run(ground.name, func(t *testing.T) {
			m := groundOf(t, ground.hex)
			at := luminanceOf(m.r, m.g, m.b)
			base := darkRamp
			if !groundIsDark(m) {
				base = lightRamp
			}
			was, got := worstOn(base, at), worstOn(adaptRamp(m), at)
			if was >= signalFloor {
				if got != was {
					t.Fatalf("the signal set cleared %s:1 against %s and was moved anyway",
						trimFloat(was), ground.hex)
				}
				return
			}
			if got < signalFloor {
				t.Fatalf("the worst signal is %s:1 against %s, want at least %s:1",
					trimFloat(got), ground.hex, trimFloat(signalFloor))
			}
		})
	}
	// The named case: a light ground far enough from white that the authored set
	// slides under the floor, and the derivation pulls all seven back over it.
	silver := groundOf(t, "#C0C0C0")
	at := luminanceOf(silver.r, silver.g, silver.b)
	if worstOn(lightRamp, at) >= signalFloor {
		t.Fatal("the authored light signals already clear the floor on silver — pick a harder case")
	}
	if worstOn(adaptRamp(silver), at) < signalFloor {
		t.Fatal("the derived signals still fail the floor on silver")
	}
}

// ── THE LADDER THE REPLY PICKS ──────────────────────────────────────────────

// A LIGHT REPLY FLIPS THE SURFACE TO THE LIGHT LADDER, and this is what makes
// theme detection EXACT rather than heuristic.
//
// [detectTheme] reads COLORFGBG, which is the only thing a terminal states about
// itself without being asked and which most terminals do not set at all — so an
// unset variable has always meant "dark", and a person on a white terminal whose
// shell never exported it got a pastel authored against a void. A background
// reply is the terminal itself answering, and it outranks the guess entirely.
func TestALightReplyFlipsTheLadder(t *testing.T) {
	for _, flip := range []struct {
		name  string
		hex   string
		light bool
	}{
		{"a white page", "#FFFFFF", true},
		{"a tinted page", "#ECEFF4", true},
		{"silver", "#C0C0C0", true},
		{"a black void", "#000000", false},
		{"tokyonight", "#1a1b26", false},
	} {
		t.Run(flip.name, func(t *testing.T) {
			m := groundOf(t, flip.hex)
			got := adaptRamp(m)
			// The ladder is named by the one role no derivation touches: the
			// identity ring is carried straight through from the base.
			onLight := len(got.ring) > 0 && got.ring[0] == lightTaskRing[0]
			if onLight != flip.light {
				t.Fatalf("%s picked the %s ladder", flip.hex, map[bool]string{true: "light", false: "dark"}[onLight])
			}
			// And the body reads AGAINST the page rather than with it, which is
			// the fact the ladder exists to state.
			ground := luminanceOf(m.r, m.g, m.b)
			body := luminanceOf(got.ink.r, got.ink.g, got.ink.b)
			if flip.light && body > ground {
				t.Fatalf("the body ink is lighter than the page it is written on (%s)", flip.hex)
			}
			if !flip.light && body < ground {
				t.Fatalf("the body ink is darker than the void it is written on (%s)", flip.hex)
			}
		})
	}
}

// A PERSON WHO SAID "LIGHT" OUT LOUD OUTRANKS THE TERMINAL. The measurement
// still governs every VALUE — a pinned ladder derived against the real ground is
// strictly better than one derived against an assumed one — but it may not
// choose the ladder, because a pin is somebody answering the question the
// measurement is asking.
func TestAPinnedLadderSurvivesTheMeasuredGround(t *testing.T) {
	pal := newThemedPalette(tokens.TrueColor, false, themeLight, nil)
	a := &app{pal: pal}
	if cmd := a.groundReply(replyOf(t, "#000000")); cmd != nil {
		t.Fatal("the background reply asked for follow-up work")
	}
	if len(a.pal.ramp.ring) == 0 || a.pal.ramp.ring[0] != lightTaskRing[0] {
		t.Fatal("a pinned light ladder was flipped by a dark terminal")
	}
	// And it WAS derived: the ground ladder against a black void is nothing the
	// light ladder ever authored.
	if a.pal.ramp.cursor == lightCursor {
		t.Fatal("a pinned ladder ignored the measured ground entirely")
	}
}

// ── THE HOOK ────────────────────────────────────────────────────────────────

// replyOf is a terminal answering [app.Init]'s question with the stated colour.
func replyOf(t *testing.T, hex string) tea.BackgroundColorMsg {
	t.Helper()
	r, g, b, ok := parseHex(hex)
	if !ok {
		t.Fatalf("malformed ground %q", hex)
	}
	return tea.BackgroundColorMsg{Color: color.RGBA{R: r, G: g, B: b, A: 0xFF}}
}

// SILENCE IS A SUPPORTED ANSWER AND COSTS NOTHING. A terminal that never replies
// keeps the authored palette forever, which is the palette every wave before this
// one shipped — so there is no timer to fire, no deadline to miss and no
// half-derived state to be caught in.
func TestASilentTerminalKeepsTheAuthoredPalette(t *testing.T) {
	a := newApp(t.Context(), Options{Agent: &fakeAgent{model: "m"}, Workspace: t.TempDir()})
	a.pal.ramp = darkRamp
	before := a.pal.ramp
	// Every message a surface sees in its first seconds, and none of them is the
	// reply.
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	a.Update(tea.BackgroundColorMsg{})
	if a.pal.measured {
		t.Fatal("a reply with no colour in it was taken as a measurement")
	}
	if a.pal.ramp.ink != before.ink || a.pal.ramp.cursor != before.cursor {
		t.Fatal("the palette changed without the terminal saying anything")
	}
	// And the question is actually asked, once, on the first frame: without it
	// no terminal can ever answer.
	if !batchHolds(a.Init(), tea.RequestBackgroundColor) {
		t.Fatal("Init never asks the terminal what colour it is")
	}
}

// batchHolds reports whether one of Init's standing commands IS the stated one.
//
// The batch is unwrapped rather than run, and that is not squeamishness: the
// commands beside this one open the task subscription, the wake lane and the
// stir lane, all of which block on a channel forever by design. What is compared
// is the FUNCTION, by address — a Cmd is not comparable with == and running it
// to see what it returns is the thing that must not happen here.
func batchHolds(cmd tea.Cmd, want tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		return false
	}
	at := reflect.ValueOf(want).Pointer()
	for _, one := range batch {
		if one != nil && reflect.ValueOf(one).Pointer() == at {
			return true
		}
	}
	return false
}

// A REPLY DROPS EVERY ROW THAT WAS PAINTED AGAINST THE OLD LADDER.
//
// A cached row is a finished string with escape sequences already inside it, so
// a palette that changed under one is a row that keeps drawing yesterday's
// colours until something else happens to make it stale — and on a transcript
// somebody has scrolled back through, "something else" can be never.
func TestABackgroundReplyDropsEveryCachedRow(t *testing.T) {
	a := newApp(t.Context(), Options{Agent: &fakeAgent{model: "m"}, Workspace: t.TempDir()})
	a.width, a.height = 80, 24
	a.pal.profile = tokens.TrueColor
	a.welcome = welcome{spent: true}
	a.entries = []entry{
		{kind: entryUser, text: "the first thing said"},
		{kind: entryAssistant, text: "and the answer to it"},
	}
	// Painting the transcript is what fills the caches.
	deck := a.conversation()
	for i := range a.entries {
		a.entryRows(deck, i, 60)
	}
	a.codeCache.put("a key", []string{"a painted row"})
	a.visible(60)
	for i := range a.entries {
		if !a.entries[i].built {
			t.Fatalf("entry %d was never cached, so this test proves nothing", i)
		}
	}
	// AND THE OTHER TWO LISTS A PERSON CAN BE LOOKING AT. The transcript is not
	// the only place this surface draws a conversation: a node's room and a run's
	// node journal are separate lists drawn by the same renderers (room.go,
	// roomorch.go), so a repaint that reached only [app.entries] would leave
	// yesterday's colours on whichever page somebody had open.
	a.room = a.newRoom(7, "")
	a.room.workOpen = map[int]bool{}
	a.room.entries = []entry{{kind: entryAssistant, text: "what the node said", built: true}}
	a.room.orch = &orchRun{
		journal: []entry{{kind: entryAssistant, text: "what a node in the run said", built: true}},
	}

	a.Update(replyOf(t, "#FFFFFF"))

	if !a.pal.measured {
		t.Fatal("the reply was not taken as a measurement")
	}
	for i := range a.entries {
		if !a.entries[i].stale {
			t.Fatalf("entry %d kept rows painted in the old ladder's colours", i)
		}
	}
	if !a.room.entries[0].stale {
		t.Fatal("a node's page kept rows painted in the old ladder's colours")
	}
	if !a.room.orch.journal[0].stale {
		t.Fatal("a run's node journal kept rows painted in the old ladder's colours")
	}
	if _, held := a.codeCache.get("a key"); held {
		t.Fatal("a highlighted block survived the palette that painted it")
	}
	if a.rows != nil || a.rowsWidth != 0 {
		t.Fatal("the laid-out screen list survived the palette that painted it")
	}
	// AND THE ROOM'S OWN LIST, which is the one that hides behind the entries.
	// [app.roomRows] returns its cache before it asks any entry for its rows, so a
	// page whose blocks were marked stale but whose LIST was not would keep
	// drawing the old ladder for as long as somebody stood on it.
	if !a.room.dirty {
		t.Fatal("a node's page kept its laid-out row list, so marking its blocks stale " +
			"changes nothing a reader standing in that room can see")
	}
}

// THE SAME ANSWER TWICE IS NOT A SECOND EVENT. Some terminals re-report their
// background on focus, and rebuilding a ladder is cheap while dropping every
// painted row on the surface is not.
func TestARepeatedReplyDoesNotRepaintTheSurface(t *testing.T) {
	a := newApp(t.Context(), Options{Agent: &fakeAgent{model: "m"}, Workspace: t.TempDir()})
	a.entries = []entry{{kind: entryUser, text: "said once"}}
	a.Update(replyOf(t, "#1a1b26"))
	a.entries[0].built, a.entries[0].stale = true, false
	a.Update(replyOf(t, "#1a1b26"))
	if a.entries[0].stale {
		t.Fatal("a repeated reply repainted the whole transcript")
	}
	// A DIFFERENT answer is a different event, because the terminal's background
	// really can change under a running session.
	a.Update(replyOf(t, "#FFFFFF"))
	if !a.entries[0].stale {
		t.Fatal("a changed background left yesterday's paint on screen")
	}
}

// AND THE ONE THING THAT IS NOT A ROW: THE STYLER THE REPLY ITSELF IS PAINTED
// WITH.
//
// A model's markdown is rendered by internal/tui2/prose, which resolves colour
// from internal/tui2/tokens, and the only reason a reply comes back in THIS
// palette's body white is markdown.go's seam — the ink is stated on the Styler
// prose is handed ([tokens.Styler.WithBodyInk]). That statement is made once,
// against the ladder in force at the time. Deriving a new ladder without
// re-making it would repaint every row on the surface against the measured
// ground and leave the ANSWER on the assumed one: the two-whites defect the
// readability wave removed, walking back in through the door the wave added.
//
// The rungs are switched on rather than skipped. [markdownStyler] detects its
// profile from the process environment, so which branch runs here depends on the
// terminal the suite is run in — and both branches are a real law:
// [tokens.Styler.WithBodyInk] states that ANSI16 and NoColor take NO override at
// all, because the sixteen are the user's own theme and an authored hex has no
// honest form there.
func TestTheAdaptedInkReachesTheReplyThatWearsIt(t *testing.T) {
	a := newApp(t.Context(), Options{Agent: &fakeAgent{model: "m"}, Workspace: t.TempDir()})
	a.width, a.height = 80, 24
	before := a.styler()
	if before != markdownStyler() {
		t.Fatal("a surface that has measured nothing must read the process-wide styler")
	}

	// A black terminal: the ground the authored ink overshoots, so the ladder
	// really moves and this test has something to prove.
	a.Update(replyOf(t, "#000000"))
	if a.pal.ramp.ink == hueInk {
		t.Fatal("the ink did not move on a black terminal, so this test proves nothing")
	}
	after := a.styler()
	if after == before {
		t.Fatal("the reply re-derived the palette and left the markdown styler on the " +
			"ink the surface started with — the body of every reply is now the one " +
			"thing on screen still painted against the assumed ground")
	}

	got := after.Fg(tokens.TextPrimary)
	switch p := after.Profile(); p {
	case tokens.TrueColor, tokens.ANSI256:
		want := tokens.NewStyler(p, tokens.FocusNormal).
			WithBodyInk(a.pal.ramp.ink.tokenColor()).Fg(tokens.TextPrimary)
		if got != want {
			t.Fatalf("the reply's body is painted %q, want the DERIVED ink %q", got, want)
		}
		if stale := before.Fg(tokens.TextPrimary); got == stale {
			t.Fatalf("the body ink is unchanged at %q after the ladder moved", got)
		}
	default:
		// No colour to spend, and therefore no override to carry: the answer is
		// the token's own, exactly as it was before the terminal spoke.
		if got != before.Fg(tokens.TextPrimary) {
			t.Fatalf("at %v the styler took an override it has no honest form for: %q", p, got)
		}
	}
}
