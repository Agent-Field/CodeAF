package tokens

import (
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/text/width"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
)

// The motion gate. 11 says three things move; motion.go says how fast and
// through which frames; this file is what makes those numbers a contract
// instead of a comment.

// TestTheHouseCadenceIsOneNumber pins the cadence twins. blocks cannot import
// tokens (the edge runs tokens → blocks), so the interval is spelled in both
// packages, and two spellings of one number is a drift waiting to happen unless
// something holds them equal.
func TestTheHouseCadenceIsOneNumber(t *testing.T) {
	if MotionInterval != blocks.DefaultInterval {
		t.Errorf("two house cadences: tokens says %v, blocks says %v — every "+
			"animated cell in the product moves on ONE grid or the rows drift apart "+
			"and the shell wakes on two schedules (8.1.3)",
			MotionInterval, blocks.DefaultInterval)
	}
	if MotionInterval < MotionIntervalMin || MotionInterval > MotionIntervalMax {
		t.Errorf("the house cadence %v is outside its stated band %v..%v: below the "+
			"floor a braille cycle reads as noise and costs wakeups nobody can see, "+
			"above the ceiling it visibly steps",
			MotionInterval, MotionIntervalMin, MotionIntervalMax)
	}
}

// TestTheKeyframeTwinsAgree pins every other number spelled on both sides of
// the tokens → blocks edge.
func TestTheKeyframeTwinsAgree(t *testing.T) {
	for _, pin := range []struct {
		what   string
		tokens any
		blocks any
	}{
		{"the spinner's rotation", SpinnerPeriod, blocks.SpinnerPeriod},
		{"the breathe's step count", PulseSteps, blocks.PulseSteps},
		{"the breathe's period", PulsePeriod, blocks.DefaultPulsePeriod},
		{"the breathe's dwell bend", PulseEase, blocks.DefaultPulseEase},
	} {
		if pin.tokens != pin.blocks {
			t.Errorf("two values for %s: tokens %v, blocks %v", pin.what, pin.tokens, pin.blocks)
		}
	}
	if len(PulseFrames) != len(blocks.DefaultPulse.Frames) {
		t.Fatalf("two breathe cycles: tokens has %d frames, blocks has %d",
			len(PulseFrames), len(blocks.DefaultPulse.Frames))
	}
	for i, frame := range PulseFrames {
		if blocks.DefaultPulse.Frames[i] != frame {
			t.Errorf("breathe frame %d parted: tokens %q, blocks %q",
				i, frame, blocks.DefaultPulse.Frames[i])
		}
	}
	// The shipped pulse must carry the house period explicitly rather than
	// relying on the zero-value fallback, so a reader of the var sees what it
	// does without chasing period().
	if blocks.DefaultPulse.Period != PulsePeriod {
		t.Errorf("the shipped pulse breathes at %v, not the house period %v",
			blocks.DefaultPulse.Period, PulsePeriod)
	}
}

// TestSpinnerPeriodIsDerived is the reason the rotation is written as
// len(frames) × interval and not as "1.2s": the ten-frame cycle replaced 5.21's
// four-frame one, and anything that had latched onto the old duration would
// have kept a spinner that came round at a different rate than it stepped.
func TestSpinnerPeriodIsDerived(t *testing.T) {
	if want := time.Duration(len(SpinnerFrames)) * MotionInterval; SpinnerPeriod != want {
		t.Errorf("SpinnerPeriod = %v, but %d frames at %v is %v",
			SpinnerPeriod, len(SpinnerFrames), MotionInterval, want)
	}
	if want := time.Duration(PulseSteps) * MotionInterval; PulsePeriod != want {
		t.Errorf("PulsePeriod = %v, but %d steps at %v is %v",
			PulsePeriod, PulseSteps, MotionInterval, want)
	}
}

// TestEveryMotionIsCalmAndWidthStable walks the keyframe table and holds every
// row to the two properties that make a motion shippable: it breathes inside
// the calm band, and it cannot change the width of the row it sits on.
//
// The width half is the same law that rejected 5.21's ◐◓◑◒ spinner, applied to
// the whole table rather than to one set: every frame measures one cell under
// the shipping ruler, and every frame agrees about East_Asian_Width, so a set
// that is one cell for us is one cell for ALL of us and two cells for all of a
// CJK-locale terminal. A set that is half-ambiguous shifts everything to its
// right when the locale changes, mid-animation.
func TestEveryMotionIsCalmAndWidthStable(t *testing.T) {
	motions := Motions()
	if len(motions) != 3 {
		t.Fatalf("the table has %d motions; 11 permits exactly three (spinner, "+
			"breathe, caret) and a fourth is a law change, not a table edit", len(motions))
	}
	seen := map[string]bool{}
	for _, m := range motions {
		if m.Name == "" {
			t.Error("a motion with no name cannot be quoted in 18")
		}
		if seen[m.Name] {
			t.Errorf("duplicate motion name %q", m.Name)
		}
		seen[m.Name] = true
		if m.Legal == "" {
			t.Errorf("%s says nothing about where it is legal; a motion with no "+
				"legal surface should not exist", m.Name)
		}
		if len(m.Frames) == 0 {
			// The caret: the terminal owns its cadence, so there is nothing of
			// ours to pin. It must also claim no period — a period here would
			// mean we thought we were driving it.
			if m.Period != 0 {
				t.Errorf("%s has no frames but claims a %v period", m.Name, m.Period)
			}
			continue
		}
		if m.Period < MotionPeriodMin || m.Period > MotionPeriodMax {
			t.Errorf("%s cycles in %v, outside the calm band %v..%v: faster reads as "+
				"urgency, which is amber's job and not a glyph's; slower stops "+
				"answering \"is this alive?\" inside the glance that asked",
				m.Name, m.Period, MotionPeriodMin, MotionPeriodMax)
		}
		if m.Period%MotionInterval != 0 {
			t.Errorf("%s's period %v is not a whole number of %v house steps, so it "+
				"cannot be phase-locked to the shared clock", m.Name, m.Period, MotionInterval)
		}
		if m.Ease < 0 || m.Ease >= 1 {
			t.Errorf("%s eases by %v; the dwell bend is in [0,1)", m.Name, m.Ease)
		}
		wantAmbiguous := isAmbiguous([]rune(m.Frames[0])[0])
		for i, f := range m.Frames {
			if got := ansi.StringWidth(f); got != 1 {
				t.Errorf("%s frame %d (%q) is %d cells", m.Name, i, f, got)
			}
			r := []rune(f)[0]
			if got := isAmbiguous(r); got != wantAmbiguous {
				t.Errorf("%s frame %d (%q) ambiguous=%v but frame 0 (%q) is %v: the row "+
					"would change width mid-motion under a CJK locale",
					m.Name, i, f, got, m.Frames[0], wantAmbiguous)
			}
			if width.LookupRune(r).Kind() == width.EastAsianWide ||
				width.LookupRune(r).Kind() == width.EastAsianFullwidth {
				t.Errorf("%s frame %d (%q) is wide in every locale", m.Name, i, f)
			}
		}
	}
}

// TestTheTableIsTheMotionsWeShip holds Motions() to the constants rather than
// letting the table become a third spelling of the frames.
func TestTheTableIsTheMotionsWeShip(t *testing.T) {
	byName := map[string]Motion{}
	for _, m := range Motions() {
		byName[m.Name] = m
	}
	spinner, ok := byName["spinner"]
	if !ok {
		t.Fatal("the table lost the spinner")
	}
	if len(spinner.Frames) != len(SpinnerFrames) || spinner.Period != SpinnerPeriod {
		t.Error("the spinner row disagrees with SpinnerFrames/SpinnerPeriod")
	}
	if spinner.Ease != 0 {
		t.Error("the spinner is a flat tick: a rotation that lingers is a rotation that stutters")
	}
	breathe, ok := byName["breathe"]
	if !ok {
		t.Fatal("the table lost the breathe")
	}
	if len(breathe.Frames) != len(PulseFrames) || breathe.Period != PulsePeriod || breathe.Ease != PulseEase {
		t.Error("the breathe row disagrees with PulseFrames/PulsePeriod/PulseEase")
	}
	if breathe.Ease == 0 {
		t.Error("an un-eased breathe is a clock, not a breath (8.1.4)")
	}
	if _, ok := byName[CaretMotion]; !ok {
		t.Fatal("the table lost the caret; 11's third motion is still a motion even " +
			"though the terminal drives it")
	}
}

// TestTheBreatheIsASizeRamp is the design claim the frame list makes, held in
// place: three distinct marks growing and shrinking on ONE shape, walked up and
// back down. Four distinct glyphs would be a second spinner, and 11 permits one.
func TestTheBreatheIsASizeRamp(t *testing.T) {
	if len(PulseFrames) != 4 {
		t.Fatalf("the breathe has %d frames; it is up-and-back-down over three sizes", len(PulseFrames))
	}
	if PulseFrames[1] != PulseFrames[3] {
		t.Errorf("the breathe does not come back down the way it went up: %q then %q",
			PulseFrames[1], PulseFrames[3])
	}
	distinct := map[string]bool{}
	for _, f := range PulseFrames {
		distinct[f] = true
	}
	if len(distinct) != 3 {
		t.Errorf("the breathe uses %d distinct marks; the ramp is small/medium/large", len(distinct))
	}
	// The ramp must actually rise: each frame's ink is a bigger dot than the
	// last. Codepoint order happens to encode it (U+00B7 < U+2022 < U+25CF) and
	// that is a coincidence worth NOT relying on, so this checks the identities
	// the vocabulary names instead.
	if PulseFrames[0] != GlyphSeparator || PulseFrames[2] != GlyphStepDone {
		t.Error("the breathe's small and large dots are no longer the bytes the " +
			"vocabulary owns; the deliberate collision documented at PulseFrames " +
			"has become an accidental one")
	}
}

// TestGaugeLadderIsATable holds the context gauge's rungs. The ladder is even
// fifths because the cells are themselves a linear height ramp: a non-linear
// threshold table would draw a bar that disagrees with its own height.
func TestGaugeLadderIsATable(t *testing.T) {
	if len(GaugeThresholds) != len(GaugeCells) {
		t.Fatalf("%d thresholds for %d cells", len(GaugeThresholds), len(GaugeCells))
	}
	if GaugeThresholds[0] != 0 {
		t.Error("the first rung must be 0: an empty window still draws a cell")
	}
	for i := 1; i < len(GaugeThresholds); i++ {
		if GaugeThresholds[i] <= GaugeThresholds[i-1] {
			t.Errorf("rung %d (%v) does not rise above rung %d (%v)",
				i, GaugeThresholds[i], i-1, GaugeThresholds[i-1])
		}
		if step := GaugeThresholds[i] - GaugeThresholds[i-1]; step < 0.199 || step > 0.201 {
			t.Errorf("rung %d is %v above rung %d; the ladder is even fifths",
				i, step, i-1)
		}
	}
	// Every rung is reachable, and each one hands over exactly at its threshold.
	for i, at := range GaugeThresholds {
		if got, want := Gauge(at), GaugeCells[i]; got != want {
			t.Errorf("Gauge(%v) = %q, want cell %d (%q)", at, got, i, want)
		}
		if i > 0 {
			if got, want := Gauge(at-0.001), GaugeCells[i-1]; got != want {
				t.Errorf("Gauge(%v) = %q, want the cell below (%q)", at-0.001, got, want)
			}
		}
	}
}

// TestTheAmberRuleIsTheGaugesOnlyColourJudgement pins the separation the gauge
// depends on: the CELL says how full, the TOKEN says whether a human is needed,
// and the token is amber past the warn point and chrome before it — never a
// third colour, and never a sixth cell.
func TestTheAmberRuleIsTheGaugesOnlyColourJudgement(t *testing.T) {
	const window = 200_000
	warn := ContextWarnPoint(window)
	if got := ContextToken(warn-1, window); got != TextTertiary {
		t.Errorf("below the warn point the gauge is %s, want the chrome tier: a window "+
			"with room left is not asking anyone for anything", got)
	}
	if got := ContextToken(warn, window); got != Amber {
		t.Errorf("at the warn point the gauge is %s, want amber", got)
	}
	if got := ContextToken(window, window); got != Amber {
		t.Errorf("a full window is %s, want amber", got)
	}
	// The dual rule: a huge window warns on the absolute count, not on 75% of
	// an enormous number.
	if got, want := ContextWarnPoint(1_000_000), int64(ContextWarnTokens); got != want {
		t.Errorf("a 1M window warns at %d, want %d — 75%% of a million is still more "+
			"headroom than most models have in total (8.2.17)", got, want)
	}
	if got, want := ContextWarnPoint(100_000), int64(float64(100_000)*ContextWarnFraction); got != want {
		t.Errorf("a 100k window warns at %d, want %d", got, want)
	}
	if ContextAlarm(1, 0) {
		t.Error("a window of unknown size cannot be past its warn point")
	}
}

// TestEverySemanticHueHasExactlyOneToken is the colour half of 18: the five-word
// vocabulary resolves to five distinct tokens and nothing shares a slot. A hue
// that resolved to the same token as another hue would mean two words for one
// colour, which is how a vocabulary stops being one.
func TestEverySemanticHueHasExactlyOneToken(t *testing.T) {
	want := map[Hue]Token{
		HueAttention: Amber,
		HueAlive:     Cyan,
		HueMoney:     Green,
		HueBroken:    Coral,
		HueIdentity:  Identity0, // the seedless placeholder; see ResolveToken
	}
	owner := map[Token]Hue{}
	for h := Hue(0); h < hueCount; h++ {
		if h == HueNone {
			continue
		}
		got := ResolveToken(h, StateSettled)
		if got != want[h] {
			t.Errorf("%s resolves to %s, want %s", h, got, want[h])
		}
		if prior, dup := owner[got]; dup {
			t.Errorf("%s and %s are both %s: two words for one colour", prior, h, got)
		}
		owner[got] = h
	}
	// A hue outranks the state axis, in every state. This is what keeps a
	// settled ✓ green and a settled ✗ coral (ResolveToken rule 1).
	for h := Hue(HueAttention); h <= HueBroken; h++ {
		first := ResolveToken(h, StateSettled)
		for s := State(0); s < stateCount; s++ {
			if got := ResolveToken(h, s); got != first {
				t.Errorf("%s changes colour when the row is %s (%s → %s): a cell that "+
					"means something keeps meaning it after the row settles", h, s, first, got)
			}
		}
	}
	// Amber is the ONLY token that means "a human is needed", so nothing else
	// in the package may resolve to it. The two doors are HueAttention and
	// ContextToken past the warn point; CutToken is deliberately not one.
	for c := CutKind(0); c < cutKindCount; c++ {
		if CutToken(c) == Amber {
			t.Errorf("a %s cut is amber; a cut turn is not a question — the head "+
				"re-produces and says so (12.5.3)", c)
		}
	}
	// Brightening never reaches a hue: a louder amber does not mean a more
	// urgent question, and the contrast pairs were tested at the base value.
	for _, tok := range []Token{Amber, Cyan, Green, Coral} {
		if Promote(tok) != tok || Demote(tok) != tok {
			t.Errorf("%s moves on the grey ramp; hues are already the loudest thing on a row", tok)
		}
	}
}

// TestEverySurfaceIsGatedAsAGround is the standing coverage rule for the
// elevation ladder, and it is written generically ON PURPOSE: the hug's two
// grounds, and any rung after them, must be walked by the contrast gate the day
// they are declared, without anybody remembering to extend a list here.
//
// [Legal] already states the composition law over IsSurface() rather than over
// named tokens, so a new ClassSurface token is legal ground for every focused
// foreground the moment it exists. This test is what proves that promise is
// kept — that the new rung actually appears in [Pairings] and therefore in
// contrast_test.go's walk — instead of a rung being declared as a bare Color
// somewhere and painted without ever meeting the gate.
func TestEverySurfaceIsGatedAsAGround(t *testing.T) {
	grounds := map[Token]int{}
	foregrounds := map[Token]int{}
	for _, p := range Pairings() {
		grounds[p.Ground]++
		foregrounds[p.Fg]++
	}
	for _, tok := range All() {
		if tok.IsSurface() {
			if grounds[tok] == 0 {
				t.Errorf("%s is a surface but no pairing ever puts text on it, so the "+
					"contrast gate never measures it", tok)
			}
			if foregrounds[tok] != 0 {
				t.Errorf("%s is drawn as a foreground; backgrounds and text are "+
					"different vocabularies (Legal rule 1)", tok)
			}
			continue
		}
		if foregrounds[tok] == 0 {
			t.Errorf("%s is a foreground that may never be drawn anywhere", tok)
		}
		// Every foreground must be legal on the plain ground at both focuses —
		// that is the floor the whole palette is stated against.
		for _, f := range []Focus{FocusNormal, FocusDimmed} {
			if !Legal(tok, f, Ground, FocusNormal) {
				t.Errorf("%s(%s) is not legal on the ground", tok, f)
			}
		}
		// And on every raised plane at normal focus, which is the rule that
		// makes a new rung free: no per-token opt-in.
		for _, g := range All() {
			if !g.IsSurface() {
				continue
			}
			if !Legal(tok, FocusNormal, g, FocusNormal) {
				t.Errorf("%s is not legal on %s; a raised plane that some text may not "+
					"sit on is a plane that cannot carry a row", tok, g)
			}
			if Legal(tok, FocusDimmed, g, FocusNormal) && g != Ground {
				t.Errorf("%s(dimmed) is legal on %s; an unfocused pane draws no raised "+
					"plane at all (Legal rule 3)", tok, g)
			}
		}
	}
}
