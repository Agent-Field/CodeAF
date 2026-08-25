package tui3

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── THE DESIGN LANGUAGE, HELD TO ITS OWN NUMBERS ────────────────────────────
//
// docs/DESIGN-LANGUAGE.md is the prose; this file is the part of it a build can
// fail on. Five laws are pinned here: the palette is CLOSED (nothing outside
// styles.go authors a colour), the signal hues are ISOLUMINANT, THE GROUND
// LADDER's three drawable steps land in the band they were aimed at, THE
// SPACING LADDER's shared steps keep their measured values, and THE GLARE LAW
// holds the body ink under the ceiling where a white stops reading and starts
// shining.

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
	"adaptive_test.go":       "names the MEASURED grounds the derivation is tabled against",
	"bundle_test.go":         "pins the light ladder's authored values",
	"thinking_test.go":       "pins the three stops [fadeOf] derives from hueDim",
	// [hueLive] is the one hue in the table whose whole meaning is a RELATION —
	// it is the value [hueInk] carried before the body was calmed, and it says
	// "this reply is still arriving" only for as long as it stands a step above
	// the ink. A silent edit to either end would leave the surface compiling, the
	// palette closed, and the effect gone with nothing failing, so the pair is
	// written down a second time where the relation can be asserted.
	"settle_test.go": "pins the live tier against the body ink it is defined against",
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

// EVERY GROUND ON THIS SURFACE COMES THROUGH THE LADDER, AND THE LADDER NOW HAS
// A FLOOR. This law was written as "rest is the default" — a row nobody is
// pointing at wears the terminal's own background — and FIDELITY.md item 13
// changes what it is a law ABOUT rather than repealing it. The owner signed the
// change on 2026-08-25 ("follow the exact design"), and it reads like this now:
//
//   - THE CONVERSATION IS UNCHANGED. A transcript still paints no ground at all.
//     Rest there is still the absence of a paint, for the reason it always was:
//     a reply is read for minutes over a page somebody else chose, and a chat
//     that repainted it would be a program with an opinion about their theme.
//   - THE PLACES BRING THEIR OWN PAGE. Home and the six places paint #12121A
//     over every cell of the frame ([huePlaceGround]), so on those surfaces rest
//     is the FLOOR STEP of the ladder rather than the absence of one, and the
//     cursor and selection steps are measured against a ground that is known
//     instead of assumed. The old comment's reason — that the terminal's
//     background is unknowable — is answered by not needing to know it.
//
// What is unchanged is the thing this test actually checks, and it is why the
// test body did not move a line: styles.go is still the ONLY file in the package
// that may open an SGR 48. A place does not paint its own page; it is handed one
// by [palette.groundRows] on a frame that is already finished, so "which ground
// is this" is still a question with four answers rather than forty.
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

// placeSignalBand is THE SAME LAW, WIDENED BY ONE POINT FOR THE PLACES, AND THE
// OWNER SIGNED IT.
//
// 2026-08-25, twice: "follow the exact design", and "colors we have are also
// like the ones in design". docs/design/home-rethink/FIDELITY.md item 1 asks for
// the design's own three signal hexes on the place surfaces and says in as many
// words that this test is ADJUSTED DELIBERATELY in the same commit. So the
// number here is a decision that was made rather than a measurement that came
// out differently, and this comment is the record of it.
//
// What the extra point actually buys, measured (DATA-AUDIT.md §17.3 computed the
// same figures and recommended against them, which is the recommendation the
// owner overruled):
//
//	amber #EECE96   L 76.1
//	green #A2E2BC   L 76.1
//	cyan  #A4D7EA   L 78.0    the design's own three spread 1.9 points
//	bad   #D08770   L 62.7    the one hue a place keeps from the conversation
//
// THE DESIGN'S OWN THREE ARE FAR TIGHTER THAN THE LAW ASKS — 1.9 points where
// fifteen is allowed — and the whole of the spread is the FAILURE hue, which the
// design's preamble does not name and which a place keeps anyway because a failed
// row stripped of colour is a failure carried by one cell of punctuation
// (styles.go's THE ONE-ACCENT LAW flags it to the owner). Sixteen is the smallest
// number that admits the design unaltered; the day the owner settles what a
// failure looks like on a place, this comes back to fifteen or lower.
const placeSignalBand = 16.0

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
		band    float64
		signals map[string]hue
		reading map[string]hue
	}{
		{
			name: "dark",
			band: signalBand,
			signals: map[string]hue{
				"accent": hueAccent, "add": hueAdd, "del": hueDel,
				"bad": hueBad, "ask": hueAsk, "warn": hueWarn, "data": hueData,
				// money joined the band in the places wave: it is a signal like
				// the rest, so it answers to the band like the rest.
				"money": hueMoney,
			},
			reading: map[string]hue{"ink": hueInk, "muted": hueMuted, "narr": hueNarr, "dim": hueDim},
		},
		{
			name: "light",
			band: signalBand,
			signals: map[string]hue{
				"accent": lightAccent, "add": lightAdd, "del": lightDel,
				"bad": lightBad, "ask": lightAsk, "warn": lightWarn, "data": lightData,
				"money": lightMoney,
			},
			reading: map[string]hue{"ink": lightInk, "muted": lightMuted, "narr": lightNarr, "dim": lightDim},
		},
		{
			// THE PLACE LADDER, at the design's own values and its own band
			// ([placeSignalBand] carries the owner's signature and the arithmetic).
			// It is a third walk rather than three more entries in the dark one
			// because the two tables are two surfaces: a place never draws the
			// conversation's violet question and the conversation never draws the
			// design's amber, so holding them to one band would be measuring a
			// spread no eye can see at once.
			name: "place",
			band: placeSignalBand,
			signals: map[string]hue{
				"ask": hueAskPlace, "live": hueLivePlace, "money": hueMoneyPlace,
				// The failure hue is the one a place keeps rather than re-authors,
				// so it is walked here: it is on the screen and the law is about
				// what shares a screen.
				"bad": hueBad,
			},
			reading: map[string]hue{"tier1": hueTier1, "tier2": hueTier2, "tier3": hueTier3},
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
			if spread := high.at - low.at; spread > ladder.band {
				var table []string
				for _, r := range got {
					table = append(table, r.name+" L "+trimFloat(r.at))
				}
				t.Fatalf("the %s signal hues spread %s points of lightness, want at most %s:\n\t%s\n"+
					"%s is the low outlier and %s the high one. Move the outlier's LIGHTNESS "+
					"only — hold its hue and saturation so it stays itself — and re-check its "+
					"xterm-256 neighbour afterwards.",
					ladder.name, trimFloat(spread), trimFloat(ladder.band),
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

// NO TWO ROLES ON ONE LADDER RESOLVE TO THE SAME xterm-256 INDEX.
//
// styles.go asks for this check on every line it authors, and it was asked for
// one line at a time: the accent's own note names 146, [hueAsk]'s names 140 and
// why 146 would have been a disaster, [hueDel]'s names 167 against [hueBad]'s
// 173. Every one of those checks was done. What nobody did was ASK THE WHOLE
// TABLE AT ONCE — and the table had an answer: [hueMuted] and [hueData] both
// rounded to 110, so on every 256-colour terminal the payload rule's datum was
// painted in the second voice's own colour, which is the identity hue doing
// nothing while appearing to work. Four waves, and it was found by somebody
// reading a comment rather than by anything failing.
//
// So the check that used to be a habit is a gate. It is the SECOND RUNG that is
// held, deliberately: the authored hexes are all distinct by construction, and
// the rung where they stop being distinct is the fallback nobody looks at —
// which is also where the MAJORITY of terminals actually are.
//
// The reading tiers are in the walk beside the signals, because they are roles
// too and a body ink that landed on the second voice's index would be worse than
// either collision the file already worries about. What is NOT in the walk is
// the GROUND ladder (its three steps have their own test, and a ground is not an
// ink) and the identity ring (its own law is distinctness around a wheel, and it
// is allowed to fall back to glyphs).
func TestNoTwoRolesShareA256Index(t *testing.T) {
	for _, ladder := range []struct {
		name  string
		roles map[string]hue
	}{
		{"dark", map[string]hue{
			"ink": hueInk, "live": hueLive, "accent": hueAccent, "muted": hueMuted,
			"narr": hueNarr, "dim": hueDim, "add": hueAdd, "del": hueDel, "bad": hueBad,
			"ask": hueAsk, "warn": hueWarn, "data": hueData, "violet": hueViolet,
			"money": hueMoney,
			// THE PLACE LADDER'S SIX ARE IN THE DARK WALK RATHER THAN IN A WALK OF
			// THEIR OWN, and that is the strict reading rather than the convenient
			// one: a place inherits every role it does not re-author (styles.go's
			// [placeRampFrom]), so the conversation's table and the design's six
			// genuinely do share a screen and a collision between them would be two
			// meanings painted one grey on most terminals. Computed, and all six
			// are clear: 255, 248, 102, 222, 152, 151. The near miss worth naming is
			// tier3's 102 against [hueNarr]'s 103 — one step apart, and the two are
			// never on one surface anyway.
			"tier1": hueTier1, "tier2": hueTier2, "tier3": hueTier3,
			"askPlace": hueAskPlace, "livePlace": hueLivePlace, "moneyPlace": hueMoneyPlace,
		}},
		{"light", map[string]hue{
			"ink": lightInk, "live": lightLive, "accent": lightAccent, "muted": lightMuted,
			"narr": lightNarr, "dim": lightDim, "add": lightAdd, "del": lightDel, "bad": lightBad,
			"ask": lightAsk, "warn": lightWarn, "data": lightData, "violet": hueViolet,
			"money": lightMoney,
		}},
	} {
		t.Run(ladder.name, func(t *testing.T) {
			// Sorted, so the failure names the same pair every time somebody runs it.
			var names []string
			for name := range ladder.roles {
				names = append(names, name)
			}
			sort.Strings(names)
			seen := map[uint8]string{}
			for _, name := range names {
				idx := ladder.roles[name].idx
				if other, clash := seen[idx]; clash {
					t.Fatalf("on the %s ladder %s and %s both resolve to xterm-256 %d — "+
						"two roles become ONE COLOUR on every 256-colour terminal, which is "+
						"most of them. Move the LIGHTNESS of whichever of the two is the "+
						"smaller surface, hold its hue and saturation, and re-check the "+
						"isoluminant band afterwards (styles.go's THE SIGNAL BAND)",
						ladder.name, other, name, idx)
				}
				seen[idx] = name
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
	// placeGround is not an assumption at all, and that is the point: it is the
	// colour the places PAINT ([huePlaceGround]), written here a second time so
	// this file can measure against it the way it measures against the grounds it
	// had to guess. The day the two disagree, the place ladder's own laws below
	// are measuring a page nobody is standing on.
	placeGround = "#12121A"
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
		cursorBand, selectedBand [2]float64
		cursor, selected, marked hue
	}{
		{"dark", darkGrounds, darkMid,
			[2]float64{1.10, 1.25}, [2]float64{1.32, 1.52}, hueCursor, hueSelected, hueMark},
		{"light", lightGrounds, lightMid,
			[2]float64{1.10, 1.25}, [2]float64{1.32, 1.52}, lightCursor, lightSelected, lightMark},
		// THE PLACE LADDER, MEASURED AGAINST ITS OWN PAGE, AND ITS SELECTED BAND
		// IS THE DESIGN'S OWN — the owner signed it (FIDELITY.md item 13).
		//
		// #262633 measures 1.25:1 against #12121A. Against the middle of the
		// range the conversation has to assume it measures 1.15, which is why
		// DATA-AUDIT.md §17.3 filed it as "a quieter band aimed at a darker
		// assumed ground" and refused it: on a page this surface did not paint it
		// lands in the cursor band instead of the selected one. On the page the
		// places DO paint, it lands where a selection belongs. The band below is
		// the design's value with the slack any authored number gets, and the two
		// steps do not overlap, which is the fact a person actually reads.
		{"place", []string{placeGround}, placeGround,
			[2]float64{1.08, 1.19}, [2]float64{1.20, 1.35}, huePlaceCursor, huePlaceBand, hueMark},
	} {
		t.Run(ladder.name, func(t *testing.T) {
			steps := []struct {
				name      string
				ground    hue
				low, high float64
			}{
				{"cursor", ladder.cursor, ladder.cursorBand[0], ladder.cursorBand[1]},
				{"selected", ladder.selected, ladder.selectedBand[0], ladder.selectedBand[1]},
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
		// The place ladder's three, and the one nudged index in the whole file is
		// here: #262633's true neighbour is 235, which is [hueCursor]'s, so the
		// band states 236 instead (styles.go's THE PLACE GROUNDS gives the
		// arithmetic and the rule — move the index, never the authored colour).
		// This test is what would have caught the collision, and it is what holds
		// the fix.
		"place page": huePlaceGround, "place cursor": huePlaceCursor, "place band": huePlaceBand,
	} {
		if other, clash := seen[h.idx]; clash {
			t.Fatalf("%s and %s both resolve to xterm-256 %d — two steps of the ladder "+
				"become one step on every 256-colour terminal", name, other, h.idx)
		}
		seen[h.idx] = name
	}
}

// ── 4. THE GLARE LAW ────────────────────────────────────────────────────────

// glareCeiling is the loudest the READING TIER's top rung may be against the
// middle of its assumed ground, and glareFloor is the quietest.
//
// Eleven is where a white stops being legible and starts being a lamp. Past it
// the strokes halate on a dark terminal, the counters of a, e and o fill in, and
// every quieter thing on the row reads as switched off — so the body, which is
// the one colour a person looks at for minutes at a time, is held BELOW it. Eight
// is the other wall and it is the ordinary one: under it the body stops being
// comfortably readable and starts asking to be leant toward.
//
// The band is 8–11 rather than a point because the ground is unknown (see THE
// GROUND LADDER in styles.go for the same wall met from the other side) and
// because the two ladders are authored, not derived. What is NOT slack is the
// ceiling: this whole law exists because #D8DEE9 measured 12.65 here and read as
// glare on every screenshot anybody took of it.
const (
	glareCeiling = 11.0
	glareFloor   = 8.0
)

// placeGlareCeiling is THE GLARE LAW's place-scoped ceiling, AND THE OWNER SIGNED
// IT. It is the single most deliberate number in this file, so it says exactly
// what was traded.
//
// #E6E6F0 is the design's body tier and it is also the precise white this whole
// law was written against: styles.go names it "the glare this whole law was
// written about", [oldBodyWhite] holds it as the absolute ceiling the live tier
// may never reach, and DATA-AUDIT.md §17.3 measured it at 13.79:1 against the
// middle of the assumed dark range and concluded "the mockups' body text cannot
// be adopted". On 2026-08-25 the owner read that and answered "follow the exact
// design", twice. FIDELITY.md items 1 and 13 are the instruction.
//
// TWO THINGS MAKE THIS AN EXCEPTION RATHER THAN A HOLE, and both are facts about
// the places rather than opinions about the colour:
//
//   - THE GROUND IS KNOWN. The chat's ceiling is 11 because the ground is
//     unknowable and a body ink has to be comfortable across a whole range of
//     terminals. A place paints its own ground ([huePlaceGround]), so there is
//     one number rather than a range, and it is measured against that number:
//     15.03:1 against #12121A. The reason the old law had to assume the worst
//     does not apply to a surface that brings its own page.
//   - IT IS NOT WHAT A PERSON READS FOR MINUTES. The chat's ceiling protects
//     PROSE — paragraphs of a reply, read continuously. Tier 1 on a place is a
//     title, a shelf's sentence, a model's id: a word or a line, glanced at, on a
//     screen a person is scanning rather than reading. SCREEN 2a's own scale says
//     so — level 3 is "the subject", the thing itself, and it is one line.
//
// The floor stays at 8: a body that has gone faint is faint on any ground, and
// nothing about the design asks for that.
const placeGlareCeiling = 15.5

// glareInkOverMuted is how far the body must stand above the surface's second
// voice, as a ratio of their two contrasts against the same ground.
//
// It is the retune's own failure mode written down. Bringing the body down is a
// move TOWARD [hueMuted], and a body ink that arrived on top of the tier below it
// would have traded glare for a screen with no reading ladder at all — one
// loudness for the answer, the tool names, the headings and the wordmark alike.
// A quarter again is the smallest gap that still reads as two tiers rather than
// as one tier drawn twice.
const glareInkOverMuted = 1.25

// THE BODY MAY NOT BE THE BRIGHTEST THING ON THE SCREEN.
//
// The signal band above governs hues that mean a KIND of thing and deliberately
// does not govern the reading tiers, because lightness is the whole of what a
// reading tier says. This is the law that does govern them, and it governs the
// top rung in particular: the tier the answer itself is written in.
//
// It is stated against the MIDDLE of each assumed ground rather than against all
// of them, exactly as THE GROUND LADDER's own band is. A fixed ink reads one
// notch louder on a blacker terminal and one notch quieter on a lighter one;
// holding the darkest end of the range to the ceiling would be authoring for a
// terminal at the edge of the range and letting every terminal inside it go
// quiet.
//
// A future retune that pushes the body back up fails here with the number it
// chose, which is the whole point — the last one was undone by nothing louder
// than somebody wanting the text to "pop".
//
// THE LIVE TIER IS NOT WALKED HERE, and its absence is a decision rather than an
// omission: [hueLive] is the law's one stated exception and has a ceiling of its
// own, in [TestTheLiveTierIsTheGlareLawsOneException] below.
func TestTheBodyInkDoesNotGlare(t *testing.T) {
	for _, ladder := range []struct {
		name            string
		mid             string
		ceiling         float64
		ink, muted, dim hue
	}{
		{"dark", darkMid, glareCeiling, hueInk, hueMuted, hueDim},
		{"light", lightMid, glareCeiling, lightInk, lightMuted, lightDim},
		// THE PLACE LADDER, MEASURED AGAINST THE GROUND IT PAINTS ITSELF rather
		// than against a range somebody assumed — which is the whole reason its
		// ceiling can be a different number ([placeGlareCeiling] carries the
		// owner's signature). The reading ladder is still asserted to be a ladder,
		// and it is a wide one: 15.03, 7.69, 4.87.
		{"place", placeGround, placeGlareCeiling, hueTier1, hueTier2, hueTier3},
	} {
		t.Run(ladder.name, func(t *testing.T) {
			ink := contrastOf(ladder.ink, ladder.mid)
			if ink > ladder.ceiling {
				t.Fatalf("the %s ladder's body ink is %s:1 against %s, want at most %s:1 — "+
					"this is THE GLARE LAW (styles.go): the body is the one colour somebody "+
					"reads for minutes at a time, and above the ceiling it halates instead of "+
					"reading. Lower the LIGHTNESS of the ink in styles.go, hold its hue, and "+
					"re-check its xterm-256 neighbour afterwards.",
					ladder.name, trimFloat(ink), ladder.mid, trimFloat(ladder.ceiling))
			}
			if ink < glareFloor {
				t.Fatalf("the %s ladder's body ink is %s:1 against %s, want at least %s:1 — "+
					"the body has gone past comfortable and into faint",
					ladder.name, trimFloat(ink), ladder.mid, trimFloat(glareFloor))
			}

			// AND THE READING LADDER IS STILL A LADDER. Coming down is a move
			// toward the tier below, so the step that the move could have spent is
			// the step this asserts.
			muted := contrastOf(ladder.muted, ladder.mid)
			dim := contrastOf(ladder.dim, ladder.mid)
			if !(ink > muted && muted > dim) {
				t.Fatalf("the %s reading tiers are not a ladder against %s: ink %s, muted %s, dim %s",
					ladder.name, ladder.mid, trimFloat(ink), trimFloat(muted), trimFloat(dim))
			}
			if ink < muted*glareInkOverMuted {
				t.Fatalf("the %s ladder's body (%s:1) has come down onto its second voice (%s:1) "+
					"against %s — want the body at least %s× the muted tier, or the answer and "+
					"the surface's own words are one loudness",
					ladder.name, trimFloat(ink), trimFloat(muted), ladder.mid,
					trimFloat(glareInkOverMuted))
			}
		})
	}
}

// oldBodyWhite is [tokens.TextPrimary]'s own hex — the white internal/tui2/prose
// painted a reply in until this wave threaded the body ink through the Styler,
// and the exact glare THE GLARE LAW was written about.
//
// It is spelled here rather than reached for because tokens does not hand its
// table out as colours (styles.go's [hue.tokenColor] crosses that seam in one
// direction only, and the reverse trip would be this package taking a second
// colour authority). What that costs is a hex that could drift from tokens', and
// what it buys is a bound with a MEANING: whatever the number, the live tier may
// not climb back into the territory the two-whites defect occupied. Its own
// escape sequence is asserted against tokens directly, in
// [TestTheTranscriptBodyWearsTheSurfacesOwnInk] below, so the drift that would
// matter is caught where it matters.
const oldBodyWhite = "#E6E6F0"

// THE LIVE TIER IS THE GLARE LAW'S ONE EXCEPTION, AND AN EXCEPTION HAS A BOUND.
//
// [hueLive] sits above the ceiling the law holds [hueInk] under, deliberately: it
// paints a reply only while that reply is still arriving (render.go's
// [app.liveTail]) and drains back to the body ink the moment the turn settles.
// The law is about a colour somebody reads for MINUTES, and this one is gone in
// seconds — which is precisely why THE ACCENT BUDGET lets a paragraph lead here
// and nowhere else.
//
// An exception with nothing holding it is a hole. Two things hold this one:
//
//   - THE ABSOLUTE CEILING. Live may not reach the white this wave took away.
//     #E6E6F0 is what prose used to paint every reply in, on every terminal, all
//     the time; a live tier that arrived there would have restored the defect and
//     merely renamed it "the streaming text".
//   - THE STEP. Live is defined as ONE step above the body, and adaptive.go's
//     [liveStep] is where that step's width is written down. Holding the authored
//     pairs inside it is what stops a future retune from buying the effect by
//     widening the gap — a leap is not a step, and a leap that started under the
//     ceiling would still end over it the first time the ink came down.
//
// Both ladders are walked, because the light one travels the other way: on a page
// a growing edge leads by being DARKER, so the step is measured as a ratio of
// contrasts and the arithmetic is the same in both directions.
func TestTheLiveTierIsTheGlareLawsOneException(t *testing.T) {
	glare := contrastOf(mustHue(oldBodyWhite, flat), darkMid)
	for _, ladder := range []struct {
		name      string
		mid       string
		ink, live hue
	}{
		{"dark", darkMid, hueInk, hueLive},
		{"light", lightMid, lightInk, lightLive},
	} {
		t.Run(ladder.name, func(t *testing.T) {
			ink := contrastOf(ladder.ink, ladder.mid)
			live := contrastOf(ladder.live, ladder.mid)
			// THE TIER EXISTS, OR IS HONESTLY ABSENT. Equal is legal and means the
			// effect is not drawn at all (styles.go's [hueLive]); quieter than the
			// body is neither.
			if live < ink {
				t.Fatalf("the %s ladder's live tier is %s:1 against %s and its body ink is %s:1 — "+
					"a streaming reply may not be QUIETER than the settled text beside it",
					ladder.name, trimFloat(live), ladder.mid, trimFloat(ink))
			}
			if step := live / ink; step > liveStep.high {
				t.Fatalf("the %s ladder stands live %s× its body ink, want at most %s× — "+
					"THE LIVE TIER IS ONE STEP AND NOT A LEAP (adaptive.go's [liveStep]). "+
					"A gap this wide reads as two kinds of text rather than as one kind "+
					"still being written, and it is how a retune walks the streaming "+
					"paragraph back into glare without ever touching the ceiling",
					ladder.name, trimFloat(step), trimFloat(liveStep.high))
			}
		})
	}
	// AND THE ABSOLUTE CEILING, which is a DARK-LADDER statement because the white
	// it names is one: #E6E6F0 on a page is not glare, it is invisible. The light
	// ladder's live tier is a near-black and is held by the step above.
	if live := contrastOf(hueLive, darkMid); live >= glare {
		t.Fatalf("the live tier is %s:1 against %s, which is the %s:1 of tokens' own %s — "+
			"THE GLARE LAW's one exception may not climb back into the white this wave "+
			"took away (styles.go). Lower [hueLive]'s LIGHTNESS, hold its hue, and "+
			"re-check its xterm-256 neighbour afterwards",
			trimFloat(live), darkMid, trimFloat(glare), oldBodyWhite)
	}
}

// THE BODY WHITE THE TRANSCRIPT WEARS IS THE PALETTE'S OWN, ON EVERY RUNG THAT
// HAS A COLOUR TO SPEND.
//
// The law above governs a value in a table; this governs whether the value ever
// reaches the screen. A model's markdown is rendered by internal/tui2/prose,
// which resolves colour from internal/tui2/tokens, and tokens' body tier is a
// brighter white than anything this palette authors — so without the seam
// markdown.go threads ([tokens.Styler.WithBodyInk]) the one thing a person reads
// most is the one thing this palette does not paint.
//
// The 256 rung is checked as well as the truecolor one, and it is the rung that
// could actually break: the index is resolved by TOKENS' nearest-neighbour walk
// while every other cell on the screen is resolved by THIS package's, and the
// two use different distance metrics. They agree on #C6CDDA. The day they stop
// agreeing on some future ink, this fails rather than shipping two whites to
// every 256-colour terminal.
func TestTheTranscriptBodyWearsTheSurfacesOwnInk(t *testing.T) {
	// A stated environment rather than the process's: [newMarkdownStyler] is a
	// pure function of one so a test can ask what a named terminal gets, instead
	// of racing the package's own sync.Once for whatever the runner happened to
	// export.
	env := func(term string) func(string) string {
		return func(key string) string {
			if key == "TERM" {
				return term
			}
			return ""
		}
	}

	const doc = "A sentence the model wrote.\n"
	for _, rung := range []struct {
		name string
		term string
		want string
	}{
		{"256", "xterm-256color", sgr256(hueInk)},
		{"truecolor", "xterm-direct", sgrTrue(hueInk)},
	} {
		t.Run(rung.name, func(t *testing.T) {
			rows := renderMarkdownWith(newMarkdownStyler(env(rung.term)), doc, 60)
			if len(rows) == 0 {
				t.Fatal("no rows")
			}
			if !strings.Contains(rows[0], rung.want) {
				t.Fatalf("the transcript body is painted %q, want the palette's own ink %q — "+
					"markdown.go must hand prose a Styler carrying [hueInk] (styles.go, "+
					"THE GLARE LAW), or a reply is painted by internal/tui2/tokens' brighter "+
					"white while every row around it wears this one",
					rows[0], rung.want)
			}
			// And tokens' own body tier is nowhere on the row: ONE body white, not
			// two taking turns.
			if stale := tokens.TextPrimary.Fg(tokens.TrueColor, tokens.FocusNormal); rung.name == "truecolor" &&
				strings.Contains(rows[0], stale) {
				t.Fatalf("the transcript row still carries tokens' own body white %q: %q", stale, rows[0])
			}
		})
	}
}

// ── the arithmetic ──────────────────────────────────────────────────────────

// sgrTrue is one hue's TRUECOLOR foreground sequence — [sgr256]'s twin, and it
// lives here rather than beside it because the rung it pins belongs to THE GLARE
// LAW: the 24-bit answer and the 256 answer are resolved by different code, and
// only a test that asks for both can say the two agree.
func sgrTrue(h hue) string {
	return "\x1b[38;2;" + itoa(int(h.r)) + ";" + itoa(int(h.g)) + ";" + itoa(int(h.b)) + "m"
}

// contrastOf is the WCAG ratio between an authored hue and an ASSUMED ground —
// the hex an author wrote down, which is the only kind of ground this file deals
// in.
//
// The arithmetic underneath it is adaptive.go's ([luminanceOf],
// [contrastRatio]) and used to be a private copy here, on the stated grounds
// that nothing at runtime knew what the terminal's background was so nothing at
// runtime could compute a contrast. A terminal can be asked now. Two copies of
// one formula is the drift this repo's one-source-of-truth law exists to
// prevent — a test that measured the ladder differently from the code that
// derives it would pass while the surface was wrong — so this is a wrapper that
// parses a hex and nothing more.
func contrastOf(h hue, ground string) float64 {
	r, g, b, ok := parseHex(ground)
	if !ok {
		panic("tui3: malformed assumed ground " + ground)
	}
	return contrastRatio(luminanceOf(h.r, h.g, h.b), luminanceOf(r, g, b))
}

// trimFloat spells a measured number the way the comments spell it: two places,
// no trailing zeroes, so a failure message reads like the table it is failing.
func trimFloat(f float64) string {
	s := strconv.FormatFloat(f, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

// ── 5. THE PLAIN FLOOR, AND THE ALPHABET ────────────────────────────────────

// EVERY PLACE IS LEGIBLE WITH NOTHING BUT LETTERS.
//
// FIDELITY.md item 11(c) asks for this by name, and it is the honest half of
// item 13: a surface that paints its own page has to be a surface that reads
// with no page at all. Two capability floors meet here and they are independent
// questions — [tokens.DetectProfile]'s NoColor answer (this terminal was told
// not to be styled) and [detectASCII]'s veto (this terminal cannot be trusted
// with a box-drawing character) — so the walk below turns both of them on at
// once, which is the worst terminal aforge claims to run on: TERM=linux in a C
// locale with NO_COLOR set.
//
// Four things are asserted and each of them is a way the floor could be a lie:
//
//   - NOT ONE SGR SEQUENCE SURVIVES. A place that painted a ground, a band or a
//     hue here would be a program styling a terminal that said not to. What is
//     NOT forbidden is OSC 8 — a hyperlink is a destination rather than a style,
//     it is the one escape styles.go's own underline note excepts by name, and a
//     terminal that cannot follow one simply shows the text.
//   - THE FRAME IS STILL THE WHOLE FRAME. The layout may not depend on a paint:
//     if a row's width came from a padded background rather than from its text,
//     the count below moves.
//   - THE PLACE STILL SAYS WHERE YOU ARE. The tab bar's band is a colour and is
//     gone, so the place's own word had better still be on the screen — this is
//     the one fact the bar exists for, and it is the fact a floor most easily
//     eats.
//   - THE FOOT STILL NAMES A KEY. A screen with no colour and no way out is a
//     screen somebody is stuck on.
func TestEveryPlaceHoldsAtThePlainFloor(t *testing.T) {
	a := placeApp(t)
	// BOTH FLOORS AT ONCE, stated rather than detected: this test is about a
	// named terminal and not about whatever the runner happened to export.
	a.pal = newThemedPalette(tokens.NoColor, true, themeDark, nil)
	for _, width := range []int{80, 120, 200} {
		a.width, a.height = width, 26
		for _, id := range []page{pageHome, pageSpend, pageSearch} {
			a.showPage(id)
			if a.page != id {
				t.Fatalf("the %s place would not open at the plain floor", id.word())
			}
			frame, _, _ := a.frame()
			if strings.Contains(frame, "\x1b[") {
				t.Fatalf("at %d the %s place draws an SGR sequence at NO_COLOR:\n%q",
					width, id.word(), frame)
			}
			lines := strings.Split(frame, "\n")
			if len(lines) != a.height {
				t.Fatalf("at %d the %s place drew %d rows into %d — the layout is leaning "+
					"on a paint that is not there", width, id.word(), len(lines), a.height)
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > width {
					t.Fatalf("at %d the %s place overflows the plain frame: %q",
						width, id.word(), line)
				}
			}
			if !strings.Contains(frame, id.word()) {
				t.Fatalf("at %d the %s place does not say its own name with no colour to "+
					"say it with:\n%s", width, id.word(), frame)
			}
			if !strings.Contains(frame, "tab next place") && !strings.Contains(frame, "enter") {
				t.Fatalf("at %d the %s place names no key at the plain floor:\n%s",
					width, id.word(), frame)
			}
		}
	}
}

// NOTHING A PLACE DRAWS COMES FROM A NERD-FONT PRIVATE USE AREA.
//
// FIDELITY.md item 11(b): every glyph a place draws must render in JetBrains
// Mono, Menlo, SF Mono and DejaVu Sans Mono. No test can open a font, so what is
// checked is the one thing that makes a character unrenderable in ALL FOUR by
// construction — a code point in a Private Use Area, which is where every patched
// nerd-font puts its icons and where no unpatched font has anything at all.
//
// A place drawing one would look correct on the machine of whoever added it and
// like a row of empty boxes on every other.
func TestNoPlaceDrawsAPrivateUseGlyph(t *testing.T) {
	private := func(r rune) bool {
		return r >= 0xE000 && r <= 0xF8FF || // the Basic Multilingual Plane's own
			r >= 0xF0000 && r <= 0xFFFFD || // Supplementary Private Use Area-A
			r >= 0x100000 && r <= 0x10FFFD // and -B
	}
	a := placeApp(t)
	a.width, a.height = 160, 30
	for _, id := range []page{pageHome, pageSpend, pageSearch} {
		a.showPage(id)
		for _, r := range placeFrameText(a) {
			if private(r) {
				t.Fatalf("the %s place draws U+%04X, which is in a Private Use Area — it is "+
					"an icon from a patched font and it renders as an empty box on every "+
					"terminal whose font was not patched (styles.go, THE PLACE LADDER; "+
					"FIDELITY.md item 11)", id.word(), r)
			}
		}
	}
}

// THE TWO MARKS THE DESIGN RE-SPELLED ARE THE HOUSE ALPHABET'S OWN.
//
// home.go authors `?` and `◐` as literals because that is where this surface's
// glyph vocabulary lives, and internal/tui2/tokens holds the same two characters
// in named slots with the same meanings — [tokens.GlyphNeedsHuman], whose comment
// reads "always amber", and [tokens.GlyphWorking]. Two spellings of one alphabet
// is exactly the drift the one-source-of-truth law exists to catch, so the
// agreement is asserted rather than assumed.
//
// The store's own two are asserted at the same time, because [standSurfaceGlyph]
// translates between the alphabets with literals it cannot reach for
// (internal/standing does not hand its glyphs out, and CLAUDE.md forbids this
// package from reshaping the resident's). A translation whose left-hand side had
// drifted would silently do nothing.
func TestThePlaceMarksAreTheDesignsOwn(t *testing.T) {
	if homeAskGlyph != tokens.GlyphNeedsHuman {
		t.Fatalf("home's needs-a-human mark is %q and the house alphabet's is %q",
			homeAskGlyph, tokens.GlyphNeedsHuman)
	}
	if homeLiveGlyph != tokens.GlyphWorking {
		t.Fatalf("home's working mark is %q and the house alphabet's is %q",
			homeLiveGlyph, tokens.GlyphWorking)
	}
	if homeIdleGlyph != tokens.GlyphQueued {
		t.Fatalf("home's queued mark is %q and the house alphabet's is %q",
			homeIdleGlyph, tokens.GlyphQueued)
	}
	if got := standSurfaceGlyph(standStoreAskGlyph); got != homeAskGlyph {
		t.Fatalf("the store's needs-you mark translates to %q, want %q", got, homeAskGlyph)
	}
	if got := standSurfaceGlyph(standStoreLiveGlyph); got != homeLiveGlyph {
		t.Fatalf("the store's firing mark translates to %q, want %q", got, homeLiveGlyph)
	}
	// AND THE STORE STILL SPELLS WHAT THE TRANSLATION EXPECTS. This is the half
	// that would rot in silence.
	item := standing.Item{NeedsPerson: "the fix touches migrations"}
	if got := item.Glyph(false); got != standStoreAskGlyph {
		t.Fatalf("internal/standing now writes %q for needs-you, and standing.go still "+
			"translates %q — the translation has quietly stopped happening",
			got, standStoreAskGlyph)
	}
	if got := (standing.Item{}).Glyph(true); got != standStoreLiveGlyph {
		t.Fatalf("internal/standing now writes %q for firing, and standing.go still "+
			"translates %q", got, standStoreLiveGlyph)
	}
}

// NO PLACE DRAWS AN ITALIC.
//
// FIDELITY.md item 12: the design's own file uses none, and a terminal's italic
// is the least reliable attribute it has — half of them render it as a colour
// swap and some as nothing. [palette.italic] has exactly one caller in this
// package (thinking.go, the model's own reasoning, which is conversation and not
// a place), and this is what keeps that true from the other end: the frames
// themselves, checked for SGR 3.
func TestNoPlaceDrawsAnItalic(t *testing.T) {
	a := placeApp(t)
	a.width, a.height = 160, 30
	for _, id := range []page{pageHome, pageSpend, pageSearch} {
		a.showPage(id)
		frame, _, _ := a.frame()
		if strings.Contains(frame, "\x1b[3m") {
			t.Fatalf("the %s place draws an italic (SGR 3). The design uses none, and "+
				"emphasis past bold is brightness, case, indent or air — SCREEN 2a",
				id.word())
		}
	}
}
