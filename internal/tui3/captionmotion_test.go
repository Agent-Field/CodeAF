package tui3

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

var motionRGB = regexp.MustCompile(`\x1b\[38;2;(\d+);(\d+);(\d+)m([^\x1b]*)\x1b\[39m`)

// motionColors reads the emitted terminal colours rather than the animation's
// formula, so a hard edge or a flash fails even if its constants look plausible.
func motionColors(t *testing.T, painted string) [][3]int {
	t.Helper()
	var colors [][3]int
	for _, match := range motionRGB.FindAllStringSubmatch(painted, -1) {
		var rgb [3]int
		for i := range rgb {
			rgb[i], _ = strconv.Atoi(match[i+1])
		}
		g := uniseg.NewGraphemes(match[4])
		for g.Next() {
			colors = append(colors, rgb)
		}
	}
	if len(colors) == 0 {
		t.Fatalf("no painted characters in %q", painted)
	}
	return colors
}

func TestCaptionMotionHasNoFlashAcrossItsWholeCycle(t *testing.T) {
	for _, tc := range []struct {
		name string
		ramp ramp
	}{{"dark", darkRamp}, {"light", lightRamp}} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp(&fakeAgent{model: "m"})
			a.pal = newPalette(tokens.TrueColor, false)
			a.pal.ramp = tc.ramp
			text := "Starting the local server and checking its response"
			base, ink := tc.ramp.narr, tc.ramp.ink
			lo := [3]int{int(base.r), int(base.g), int(base.b)}
			hi := [3]int{int(ink.r), int(ink.g), int(ink.b)}
			var previous [][3]int
			changed := false
			visible := false
			for frame := 0; frame <= 120; frame++ {
				captionTimeAt(a, time.Duration(frame)*shimmerPeriod/120)
				painted := a.shimmer(text)
				if ansi.Strip(painted) != text || ansi.StringWidth(painted) != ansi.StringWidth(text) {
					t.Fatalf("frame %d moves or changes the text", frame)
				}
				colors := motionColors(t, painted)
				for i, rgb := range colors {
					for ch, value := range rgb {
						lift, span := value-lo[ch], hi[ch]-lo[ch]
						if lift*span < 0 || abs(lift) > abs(span) {
							t.Fatalf("frame %d character %d flashes beyond reading ink: %v", frame, i, rgb)
						}
						if previous != nil && abs(value-previous[i][ch]) > 12 {
							t.Fatalf("frame %d character %d jumps from %v to %v", frame, i, previous[i], rgb)
						}
						changed = changed || lift != 0
						visible = visible || abs(lift)*100 > abs(span)*90
					}
				}
				if (frame == 0 || frame == 120) && painted != a.pal.narr(text) {
					t.Fatalf("frame %d interrupts the quiet loop boundary", frame)
				}
				previous = colors
			}
			if !changed || !visible {
				t.Fatal("a running caption never reaches a legible highlight")
			}
		})
	}
}

func TestCaptionMotionKeepsJoinedCharactersWhole(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.pal = newPalette(tokens.TrueColor, false)
	text := "Reading cafe\u0301 files 👩🏽‍💻 and 日本語"
	for frame := 0; frame < 120; frame++ {
		captionTimeAt(a, time.Duration(frame)*shimmerPeriod/120)
		painted := a.shimmer(text)
		if ansi.Strip(painted) != text || ansi.StringWidth(painted) != ansi.StringWidth(text) {
			t.Fatalf("frame %d changes Unicode text or width", frame)
		}
		for _, cluster := range []string{"e\u0301", "👩🏽‍💻"} {
			if !strings.Contains(painted, cluster) {
				t.Fatalf("frame %d puts a colour boundary inside %q", frame, cluster)
			}
		}
	}
}

func TestCaptionMotionRespectsStaticTerminalTiers(t *testing.T) {
	for _, profile := range []tokens.Profile{tokens.NoColor, tokens.ANSI16, tokens.ANSI256, tokens.TrueColor} {
		a := newTestApp(&fakeAgent{model: "m"})
		a.pal = newPalette(profile, false)
		a.linear = profile == tokens.TrueColor
		captionTimeAt(a, 0)
		want := a.shimmer("Starting the local server")
		for frame := 1; frame < 120; frame++ {
			captionTimeAt(a, time.Duration(frame)*shimmerPeriod/120)
			if got := a.shimmer("Starting the local server"); got != want {
				t.Fatalf("profile %s moves in static tier at frame %d", profile, frame)
			}
		}
	}
}

func TestCaptionMotionDoesNotRepaintTheQuietTextCharacterByCharacter(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.pal = newPalette(tokens.TrueColor, false)
	text := "Starting the local server and checking that the page responds correctly"
	for frame := 0; frame < 120; frame++ {
		captionTimeAt(a, time.Duration(frame)*shimmerPeriod/120)
		painted := a.shimmer(text)
		// A soft highlight is a small local change, even on a long caption. This
		// permits its feather but rejects a pair of escapes for every quiet letter.
		if spans := len(motionRGB.FindAllStringSubmatch(painted, -1)); spans > 30 {
			t.Fatalf("frame %d spends %d colour spans on one local highlight", frame, spans)
		}
	}
}

// motionAt drives the clock the shipped view reads; painted-frame counts are
// deliberately unrelated so these tests cannot pass with a frame-count timer.
func captionTimeAt(a *app, elapsed time.Duration) {
	a.turnBegan = time.Unix(100, 0)
	a.clock = func() time.Time { return a.turnBegan.Add(elapsed) }
}

func TestCaptionMotionFollowsTimeWhenFramesAreDropped(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.pal = newPalette(tokens.TrueColor, false)
	captionTimeAt(a, 0)
	base := a.shimmer("Starting the local server")
	a.paints = 400
	if got := a.shimmer("Starting the local server"); got != base {
		t.Fatal("painting twice at the same instant advanced the sweep")
	}
	captionTimeAt(a, shimmerPeriod/2)
	middle := a.shimmer("Starting the local server")
	if middle == base {
		t.Fatal("elapsed time with no intervening frames left the sweep frozen")
	}
	captionTimeAt(a, shimmerPeriod+shimmerPeriod/2)
	if got := a.shimmer("Starting the local server"); got != middle {
		t.Fatal("a skipped cycle changed the phase")
	}
}
