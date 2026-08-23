package tui3

import (
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// ── THE DESIGN LANGUAGE, HELD TO ITS OWN NUMBERS ────────────────────────────
//
// docs/DESIGN-LANGUAGE.md is the prose; this file is the part of it a build can
// fail on. Four laws are pinned here: the palette is CLOSED (nothing outside
// styles.go authors a colour), the signal hues are ISOLUMINANT, and THE GROUND
// LADDER's three drawable steps land in the band they were aimed at, and THE
// SPACING LADDER's shared steps keep their measured values.

// ── 0. THE SPACING LADDER ───────────────────────────────────────────────────

// THE SHARED STEPS KEEP THE VALUES THE DOCUMENT NAMES. These constants exist
// only where independent builders must agree; pinning them here keeps a local
// padding decision from quietly becoming a new rung.
func TestTheSpacingLadderKeepsItsSharedSteps(t *testing.T) {
	if spacingBlockRows != 1 {
		t.Fatalf("the block step is %d rows, want one", spacingBlockRows)
	}
	if spacingRuleClearance != 1 {
		t.Fatalf("the hairline clearance is %d rows, want one", spacingRuleClearance)
	}
	if spacingConversationLead != 2 || noteLead != spacingConversationLead || railGripCols != spacingConversationLead {
		t.Fatalf("the two-cell leads drifted: conversation=%d note=%d rail=%d",
			spacingConversationLead, noteLead, railGripCols)
	}
	if homeGutter != 4 {
		t.Fatalf("home's gutter is %d cells, want four", homeGutter)
	}
}

// TWO OPTIONAL BLOCKS MAY ASK FOR THE SAME BOUNDARY, BUT THE BOUNDARY REMAINS
// ONE ROW. This is the mechanically checkable half of the ladder; paragraphs
// may contain blank rows of their own, so a package-wide source scan would
// mistake content and phone-sized hit targets for layout.
func TestARepeatedBlockBoundaryDoesNotGrowASecondBlankRow(t *testing.T) {
	rows := separated([]string{"first"})
	rows = separated(rows)
	rows = append(rows, "second")
	if got := strings.Join(rows, "|"); got != "first||second" {
		t.Fatalf("repeated block boundary = %q, want one blank row", got)
	}
}

// ── 1. CONSISTENCY BY REFUSAL ───────────────────────────────────────────────

// hexColour is how a colour gets authored in Go: six hex digits behind a hash.
// It is the spelling [mustHue] takes and the only one this package has ever
// used to decide a colour.
var hexColour = regexp.MustCompile(`#[0-9a-fA-F]{6}`)

// colourAuthors are the files allowed to spell a colour, and the list is the
// whole enforcement: every other file in the package is refused.
//
// styles.go is THE AUTHORITY — the top of that file says the palette lives
// there and nowhere else, and this test is what turns that sentence into a
// gate. The other three are here because a test that PINS a colour is the
// opposite of a file that CHOOSES one: styles.go's own comments quote hexes
// that would silently drift if nothing asserted them, and asserting them means
// writing them down a second time. Each is named rather than waved through by
// a blanket "*_test.go is fine", because the friction is the feature — a new
// file that wants to hold a colour has to come here and say why.
var colourAuthors = map[string]string{
	"styles.go": "THE CURATED PASTEL AUTHORITY: the palette is authored here",

	"designlanguage_test.go": "names the assumed terminal grounds the ladder is aimed at",
	"bundle_test.go":         "pins the light ladder's authored values",
	"thinking_test.go":       "pins the three stops [fadeOf] derives from hueDim",
}

// THE PALETTE IS CLOSED. A colour with no role is a colour nobody can change,
// because changing it means finding it — and a surface whose hues are scattered
// across forty files is a surface that drifts one call site at a time. So the
// rule is not "prefer the palette", it is that this package's other files
// CANNOT SPELL A COLOUR AT ALL: they ask the palette for a role and take
// whatever the ladder in force answers with, which is also the entire mechanism
// that made the light theme cost its call sites nothing.
//
// The limit is stated so nobody mistakes it for more than it is: this catches
// the HEX spelling, which is how a colour is authored. A test asserting a
// finished escape sequence in decimal is pinning something styles.go already
// decided, and imagepreview.go's per-pixel sequences carry the image's own
// colours rather than this surface's.
func TestNothingButStylesAuthorsAColour(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		if _, allowed := colourAuthors[name]; allowed {
			continue
		}
		body, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		for _, line := range strings.Split(string(body), "\n") {
			found := hexColour.FindString(line)
			if found == "" {
				continue
			}
			t.Fatalf("%s authors the colour %s:\n\t%s\n"+
				"give this colour a role in styles.go and ask the palette for it. "+
				"The palette is closed: styles.go is the only file in this package "+
				"that decides what a thing is coloured.",
				name, found, strings.TrimSpace(line))
		}
	}
}

// backgroundAuthors are the files allowed to open an SGR 48 — a BACKGROUND.
//
// styles.go's [palette.background] is the one place this surface lifts a run of
// cells off the terminal's own ground, and THE GROUND LADDER is the whole list
// of reasons it may. imagepreview.go is the exception and is not really one: a
// half-block cell carries an IMAGE's own two pixels, top in the ink and bottom
// in the ground, and those colours belong to the picture rather than to this
// palette.
var backgroundAuthors = map[string]string{
	"styles.go":       "[palette.background] draws THE GROUND LADDER's three steps",
	"imagepreview.go": "a half-block cell's lower pixel is the image's colour, not ours",
}

// REST IS THE DEFAULT, AND THIS IS WHAT KEEPS IT ONE. A row nobody is pointing
// at, has not chosen and has not marked wears the terminal's own background —
// which is only true for as long as nothing else in the package can paint one.
// Every tinted run on this surface therefore comes through the ladder, and
// "which step is this" is a question with three answers rather than forty.
func TestOnlyTheGroundLadderPaintsABackground(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if _, allowed := backgroundAuthors[name]; allowed {
			continue
		}
		body, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		for _, line := range strings.Split(string(body), "\n") {
			code := line
			if at := strings.Index(strings.TrimSpace(line), "//"); at == 0 {
				continue
			}
			if !strings.Contains(code, "48;5;") && !strings.Contains(code, "48;2;") {
				continue
			}
			t.Fatalf("%s paints a background of its own:\n\t%s\n"+
				"ask the palette for a step of THE GROUND LADDER instead — cursor, "+
				"selected or mark. A row with no step is at rest, and rest is the "+
				"absence of a paint.", name, strings.TrimSpace(line))
		}
	}
}

// ── 2. THE SIGNAL BAND ──────────────────────────────────────────────────────

// lightnessOf is the HSL lightness of one authored colour, 0-100. It is the
// midpoint of the brightest and darkest channels and nothing else — the same
// arithmetic every palette in the world is quoted in, so the numbers in
// docs/DESIGN-LANGUAGE.md and the numbers here are the same numbers.
func lightnessOf(h hue) float64 {
	r, g, b := int(h.r), int(h.g), int(h.b)
	hi := max(max(r, g), b)
	lo := min(min(r, g), b)
	return float64(hi+lo) / 2 / 255 * 100
}

// signalBand is how wide the signal hues may spread in HSL lightness, in
// points. Fifteen is the band a wide reading of calm terminal palettes
// converges on, and it is the arithmetic behind "many colours, still quiet".
const signalBand = 15.0

// THE SIGNAL HUES SIT INSIDE A FIFTEEN-POINT LIGHTNESS BAND, on both ladders.
//
// The eye reads lightness as figure and ground and hue as identity, so a set of
// signals at one lightness reads as a single field at a glance and resolves
// into colours only when somebody looks — while one signal five points lighter
// than the rest reads as more important than the rest, whatever its hue meant.
// A diff's minus lines were exactly that defect for four waves.
//
// The reading tiers — ink, muted, dim — are excluded BY NAME rather than by
// silence: lightness is the whole of their meaning, they are a ladder on
// purpose, and a test that quietly skipped them would read as an oversight.
func TestTheSignalHuesAreIsoluminant(t *testing.T) {
	for _, ladder := range []struct {
		name    string
		signals map[string]hue
		reading map[string]hue
	}{
		{
			name: "dark",
			signals: map[string]hue{
				"accent": hueAccent, "add": hueAdd, "del": hueDel,
				"bad": hueBad, "ask": hueAsk, "warn": hueWarn, "data": hueData,
			},
			reading: map[string]hue{"ink": hueInk, "muted": hueMuted, "dim": hueDim},
		},
		{
			name: "light",
			signals: map[string]hue{
				"accent": lightAccent, "add": lightAdd, "del": lightDel,
				"bad": lightBad, "ask": lightAsk, "warn": lightWarn, "data": lightData,
			},
			reading: map[string]hue{"ink": lightInk, "muted": lightMuted, "dim": lightDim},
		},
	} {
		t.Run(ladder.name, func(t *testing.T) {
			type reading struct {
				name string
				at   float64
			}
			var got []reading
			for name, h := range ladder.signals {
				got = append(got, reading{name, lightnessOf(h)})
			}
			sort.Slice(got, func(i, j int) bool { return got[i].at < got[j].at })
			low, high := got[0], got[len(got)-1]
			if spread := high.at - low.at; spread > signalBand {
				var table []string
				for _, r := range got {
					table = append(table, r.name+" L "+trimFloat(r.at))
				}
				t.Fatalf("the %s signal hues spread %s points of lightness, want at most %s:\n\t%s\n"+
					"%s is the low outlier and %s the high one. Move the outlier's LIGHTNESS "+
					"only — hold its hue and saturation so it stays itself — and re-check its "+
					"xterm-256 neighbour afterwards.",
					ladder.name, trimFloat(spread), trimFloat(signalBand),
					strings.Join(table, "\n\t"), low.name, high.name)
			}
			// And the exclusion is deliberate: the reading tiers are a ladder, so
			// at least one of them had better be outside the band the signals share.
			outside := false
			for _, h := range ladder.reading {
				at := lightnessOf(h)
				if at < low.at-1 || at > high.at+1 {
					outside = true
				}
			}
			if !outside {
				t.Fatalf("every reading tier on the %s ladder now sits inside the signal band, "+
					"which means the body, the second voice and the surface's own murmur are "+
					"all one loudness — the tiers are a ladder and must read as one", ladder.name)
			}
		})
	}
}

// ── 3. THE GROUND LADDER ────────────────────────────────────────────────────

// The grounds a real dark terminal actually sits on. We cannot ask (see THE
// GROUND LADDER in styles.go for why), so the ladder is aimed at this range and
// this range is written down rather than assumed in somebody's head.
var (
	darkGrounds  = []string{"#101014", "#1a1b26", "#1e1e2e"}
	darkMid      = "#1a1b26"
	lightGrounds = []string{"#FFFFFF", "#ECEFF4"}
	lightMid     = "#FFFFFF"
)

// THE THREE DRAWABLE STEPS LAND IN THE BAND THEY WERE AIMED AT, measured
// against the middle of the assumed ground, and they are STRICTLY INCREASING
// against every ground in the range.
//
// The bands are wider than the aim by a little, and the slack is honest rather
// than lazy: the light ladder's cursor step was already inside the intent when
// this wave started and a value in band is not touched, so the ceiling has to
// clear the 1.22 it measures at. A step that drifts far enough to matter still
// fails here.
func TestTheGroundLadderLandsInItsBand(t *testing.T) {
	for _, ladder := range []struct {
		name                     string
		grounds                  []string
		mid                      string
		cursor, selected, marked hue
	}{
		{"dark", darkGrounds, darkMid, hueCursor, hueSelected, hueMark},
		{"light", lightGrounds, lightMid, lightCursor, lightSelected, lightMark},
	} {
		t.Run(ladder.name, func(t *testing.T) {
			steps := []struct {
				name      string
				ground    hue
				low, high float64
			}{
				{"cursor", ladder.cursor, 1.10, 1.25},
				{"selected", ladder.selected, 1.32, 1.52},
				{"mark", ladder.marked, 1.75, 2.30},
			}
			for _, step := range steps {
				at := contrastOf(step.ground, ladder.mid)
				if at < step.low || at > step.high {
					t.Fatalf("the %s ladder's %s step is %s:1 against %s, want %s-%s:1 — "+
						"raise or lower the authored value in styles.go until it lands in the band",
						ladder.name, step.name, trimFloat(at), ladder.mid,
						trimFloat(step.low), trimFloat(step.high))
				}
			}
			for _, ground := range ladder.grounds {
				cursor := contrastOf(ladder.cursor, ground)
				selected := contrastOf(ladder.selected, ground)
				marked := contrastOf(ladder.marked, ground)
				if !(cursor < selected && selected < marked) {
					t.Fatalf("the %s ladder is not a ladder against %s: cursor %s, selected %s, mark %s",
						ladder.name, ground, trimFloat(cursor), trimFloat(selected), trimFloat(marked))
				}
			}
			// A GROUND IS NEVER A TINT. All three steps round into the 256
			// palette's grey ramp rather than its colour cube, because a
			// background that resolved into a hue would be a background that
			// looked like it meant something, and no step here means anything by
			// itself.
			for name, h := range map[string]hue{
				"cursor": ladder.cursor, "selected": ladder.selected, "mark": ladder.marked,
			} {
				if h.idx < 232 {
					t.Fatalf("the %s ladder's %s step resolves to xterm-256 %d, which is in the "+
						"colour cube — a ground must land on the grey ramp (232-255)",
						ladder.name, name, h.idx)
				}
			}
		})
	}
}

// REST IS NOT A COLOUR, and the three steps that are never collapse into each
// other.
//
// The ladder's first step has no entry in the ramp and cannot be given one: an
// unremarkable row is painted by not painting it, which is the emptiness law
// wearing its background clothes. What can be asserted is the consequence — the
// three steps that DO draw are three, on the rung where colours get rounded as
// well as on the rung where they do not, so a person can tell "the pointer is
// here" from "this is the chosen one" from "you have this marked" without being
// told which is which.
func TestTheGroundLadderStepsNeverCollapse(t *testing.T) {
	seen := map[uint8]string{}
	for name, h := range map[string]hue{
		"dark cursor": hueCursor, "dark selected": hueSelected, "dark mark": hueMark,
		"light cursor": lightCursor, "light selected": lightSelected, "light mark": lightMark,
	} {
		if other, clash := seen[h.idx]; clash {
			t.Fatalf("%s and %s both resolve to xterm-256 %d — two steps of the ladder "+
				"become one step on every 256-colour terminal", name, other, h.idx)
		}
		seen[h.idx] = name
	}
}

// ── the arithmetic ──────────────────────────────────────────────────────────

// contrastOf is the WCAG ratio between an authored hue and an assumed ground.
// It is here rather than in styles.go on purpose: nothing at runtime knows what
// the terminal's background is, so nothing at runtime may compute this. It is a
// number the AUTHOR checks, and a test is where an author's checks live.
func contrastOf(h hue, ground string) float64 {
	r, g, b, ok := parseHex(ground)
	if !ok {
		panic("tui3: malformed assumed ground " + ground)
	}
	first, second := luminanceOf(h.r, h.g, h.b), luminanceOf(r, g, b)
	if first < second {
		first, second = second, first
	}
	return (first + 0.05) / (second + 0.05)
}

// luminanceOf is sRGB relative luminance, the sRGB transfer curve included —
// the linear-light average that contrast is defined on, not the raw channels.
func luminanceOf(r, g, b uint8) float64 {
	channel := func(c uint8) float64 {
		v := float64(c) / 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(r) + 0.7152*channel(g) + 0.0722*channel(b)
}

// trimFloat spells a measured number the way the comments spell it: two places,
// no trailing zeroes, so a failure message reads like the table it is failing.
func trimFloat(f float64) string {
	s := strconv.FormatFloat(f, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}
