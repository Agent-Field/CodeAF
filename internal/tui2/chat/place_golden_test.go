package chat

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/golden"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Golden coverage for the place line.
//
// The row is one line of prose about where the reader is, and what an assertion
// cannot check about it is the SHAPE: where the separators sit in the grey ramp,
// that the current segment is the brightest thing on the row and the ancestors
// are not, that the collapse mark and the ends survive together, that the
// ornament sits at the right edge and leaves whole when it cannot be afforded,
// and that the mouth mark is one cell rather than a sentence.
//
// It is pinned at both ends of the contrast ladder for the reason every other
// frame in this package is: a row that only read as a path in truecolor would be
// a row that had smuggled its structure into colour (§16's grey ramp).
//
// The harness's three structural checks come free and matter here more than
// most: this is a single row with a right-aligned cell on it, so a glyph a
// terminal disagrees about the width of, or a truncation that cut through an
// escape, would show up as a row wider than its viewport.

// placeGoldenWidths are the widths this row actually has to survive: the floors,
// both sides of the collapse breakpoint, and the ordinary terminal. Height is
// always 1 — the component owes exactly one row and the harness checks it.
var placeGoldenWidths = []golden.Size{
	{Width: 1, Height: 1},
	{Width: 2, Height: 1},
	{Width: 14, Height: 1},
	{Width: 22, Height: 1},
	{Width: 30, Height: 1},
	{Width: 48, Height: 1},
	{Width: 60, Height: 1},
	{Width: 80, Height: 1},
	{Width: 120, Height: 1},
}

// placeGoldenPath is the doc's own example, four deep: the surface, the
// conversation, a task inside it, and a part inside that.
func placeGoldenPath() []placeSeg {
	return []placeSeg{
		{Word: "aforge", Kind: placeRoot},
		{Word: "pricing ideation", Kind: placeThread},
		{Word: "30-page story", Kind: placeScope},
		{Word: "chapter two", Kind: placePart},
	}
}

// placeLineView renders the row on its own, at a profile, with whatever state
// the caller wants pinned.
func placeLineView(profile tokens.Profile, mouth int, unseen bool, running, focus int) golden.View {
	return func(width, height int, theme golden.Theme) []string {
		line := newPlaceLine(tokens.NewStyler(profile, tokens.FocusNormal))
		line.setPath(placeGoldenPath(), mouth, unseen, running)
		line.focus = focus
		rows := make([]string, 0, height)
		rows = append(rows, line.Render(width))
		for len(rows) < height {
			rows = append(rows, "")
		}
		return rows[:height]
	}
}

// The path with nothing else true: no delivery landed, nothing running, the
// composer bound to the room the reader is standing in. This is the ordinary
// frame, and it is the one the collapse ladder is read off.
func TestGoldenPlaceLine(t *testing.T) {
	for _, tier := range contrastTiers() {
		t.Run(tier.name, func(t *testing.T) {
			golden.RunSizes(t, "place-"+tier.name,
				placeLineView(tier.profile, -1, false, 0, -1),
				placeGoldenWidths, goldenThemes)
		})
	}
}

// The same path with the ONE ornament on it: a delivery landed where the reader
// was not looking, and three jobs are moving. The narrow sizes are the point —
// the ornament gives its cells back whole rather than crowding the answer.
func TestGoldenPlaceLineWithTheOrnament(t *testing.T) {
	for _, tier := range contrastTiers() {
		t.Run(tier.name, func(t *testing.T) {
			golden.RunSizes(t, "place-ornament-"+tier.name,
				placeLineView(tier.profile, -1, true, 3, -1),
				placeGoldenWidths, goldenThemes)
		})
	}
}

// The mouth mark: the reader is standing on a part page and the words they type
// would land in the conversation two rungs up (13.19's rule, said as one cell).
func TestGoldenPlaceLineNamesWhereWordsGo(t *testing.T) {
	for _, tier := range contrastTiers() {
		t.Run(tier.name, func(t *testing.T) {
			golden.RunSizes(t, "place-mouth-"+tier.name,
				placeLineView(tier.profile, 1, false, 0, -1),
				placeGoldenWidths, goldenThemes)
		})
	}
}

// The walk, mid-stride: the keyboard is on the row and the second segment is
// the one enter would jump to. The band is the SELECTION band the rail and the
// bar's current tab already stand on, not a colour of its own.
func TestGoldenPlaceLineUnderTheWalk(t *testing.T) {
	for _, tier := range contrastTiers() {
		t.Run(tier.name, func(t *testing.T) {
			golden.RunSizes(t, "place-walk-"+tier.name,
				placeLineView(tier.profile, -1, false, 0, 1),
				placeGoldenWidths, goldenThemes)
		})
	}
}

// And the row as the composer region actually hands it over: the hug's top edge,
// on the ground the draft stands on, with the place line above the words. It is
// the frame the whole wave is about, so it is stored rather than described.
func TestGoldenPlaceLineOnTheHug(t *testing.T) {
	for _, tier := range contrastTiers() {
		t.Run(tier.name, func(t *testing.T) {
			golden.RunSizes(t, "place-hug-"+tier.name, placeHugView(tier.profile),
				goldenSizes, goldenThemes)
		})
	}
}

// The STORED frames — the ones a reviewer reads. Three widths, because the row
// has three shapes: whole, middle-collapsed, and collapsed with the current
// name itself giving. Each is stored with and without the ornament, so the
// ladder's give-back is on the page rather than only in an assertion.
func TestGoldenPlaceSnapshots(t *testing.T) {
	cases := []struct {
		name    string
		view    golden.View
		width   int
		snapKey string
	}{
		{"whole", placeLineView(tokens.TrueColor, -1, false, 0, -1), 80, "place-truecolor"},
		{"whole-plain", placeLineView(tokens.NoColor, -1, false, 0, -1), 80, "place-plain"},
		// The middle collapses and the ends do not.
		{"collapsed-plain", placeLineView(tokens.NoColor, -1, false, 0, -1), 30, "place-plain"},
		// And the current name itself gives, from the middle.
		{"pinched-plain", placeLineView(tokens.NoColor, -1, false, 0, -1), 22, "place-plain"},
		// The one ornament, wide and then too narrow to afford it.
		{"ornament", placeLineView(tokens.TrueColor, -1, true, 3, -1), 80, "place-ornament-truecolor"},
		{"ornament-plain", placeLineView(tokens.NoColor, -1, true, 3, -1), 80, "place-ornament-plain"},
		{"ornament-dropped-plain", placeLineView(tokens.NoColor, -1, true, 3, -1), 30, "place-ornament-plain"},
		// The mouth mark, and the walk's band.
		{"mouth-plain", placeLineView(tokens.NoColor, 1, false, 0, -1), 80, "place-mouth-plain"},
		{"walk", placeLineView(tokens.TrueColor, -1, false, 0, 1), 80, "place-walk-truecolor"},
		{"walk-plain", placeLineView(tokens.NoColor, -1, false, 0, 1), 80, "place-walk-plain"},
		// And the row where it actually lives: the composer region's top edge.
		{"hug", placeHugView(tokens.TrueColor), 80, "place-hug-truecolor"},
		{"hug-plain", placeHugView(tokens.NoColor), 80, "place-hug-plain"},
	}
	for _, testCase := range cases {
		height := 1
		if strings.HasPrefix(testCase.name, "hug") {
			height = 4
		}
		t.Run(testCase.name, func(t *testing.T) {
			golden.Snap(t, testCase.snapKey, testCase.view, testCase.width, height,
				golden.Theme{Mode: golden.Dark})
		})
	}
}

// placeHugView renders the composer region alone — the place line, the draft and
// the padding row — so the row's ground and its neighbours are pinned without a
// whole transcript moving underneath them.
func placeHugView(profile tokens.Profile) golden.View {
	return func(width, height int, theme golden.Theme) []string {
		app := goldenApp(&fakeBackend{}, profile, theme)
		drivePoll(app)
		stack, ok := app.composer.(*composerStack)
		if !ok {
			return make([]string, height)
		}
		app.placeBar.setPath(placeGoldenPath(), 1, true, 2)
		rows := strings.Split(stack.Render(width, height), "\n")
		for len(rows) < height {
			rows = append(rows, "")
		}
		return rows[:height]
	}
}
