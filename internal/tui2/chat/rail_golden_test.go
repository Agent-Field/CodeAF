package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/golden"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Golden coverage for the unified rail: two sections, three collapse states.
//
// A rail is furniture, and furniture is exactly the kind of thing an assertion
// cannot check. What these frames pin is the SHAPE — where the two headings
// sit, that the connectors hang under the card they belong to and nowhere else,
// that a thread's left-at line is one tier down from its name, that the handle
// is one column carrying at most one cell — and they pin it at both ends of the
// contrast ladder, because a rail that only read as furniture in truecolor
// would be a rail that had smuggled its structure into colour.
//
// The harness's three structural checks come free and are worth naming here:
// exactly height rows, nothing wider than the viewport, and no control byte
// that is not an SGR sequence. The connectors are the reason: ├ ─ │ are the one
// place this surface draws characters whose width a terminal could disagree
// about.

// railGoldenBackend is the fixture the rail's own frames are read from: two
// named conversations with a line in each, and a board with one live job that
// has a plan and one settled job that does not.
func railGoldenBackend() *threadsBackend {
	return threadsGoldenBackend()
}

// railView is the ordinary window at whatever rung the caller names, with one
// conversation carrying an unseen delivery so the ONE ornament is in a stored
// frame at every profile rather than only in an assertion.
func railView(profile tokens.Profile, rung string, unseen bool) golden.View {
	return func(width, height int, theme golden.Theme) []string {
		app := goldenAppRail(railGoldenBackend(), profile, theme, rung)
		drivePoll(app)
		if unseen {
			// A thread this window HAS visited, with something newer in it since.
			// That is the only shape the dot is allowed to appear in: a window
			// that dotted a thread it had never opened would be announcing that
			// the product is new rather than that anything happened.
			app.noteThreadSeen("session-two", fixedNow().Add(-40*time.Hour))
			app.reopenRail()
		}
		return strings.Split(app.Frame(width, height), "\n")
	}
}

// railQuietView is the composed empty state: a board with nothing on it. The
// work section says so in one faint line and the rail still reads as furniture
// rather than as an error.
func railQuietView(profile tokens.Profile) golden.View {
	return func(width, height int, theme golden.Theme) []string {
		backend := railGoldenBackend()
		backend.nodes = nil
		backend.usage = nil
		app := goldenAppRail(backend, profile, theme, "open")
		drivePoll(app)
		return strings.Split(app.Frame(width, height), "\n")
	}
}

// newRoomCardView is the `+ new` door's own frame: the door selected, its card in
// the main pane, and the map still holding the keyboard — which is exactly what a
// reader gets by pressing the digit.
//
// It is a stored frame because this card is COPY and geometry and nothing else,
// and both are the kind of thing an assertion can only spot-check. What the
// frames pin is the shape: a titled rule where a lifecycle glyph used to be, one
// paragraph at the readable measure rather than the full width of a 120-column
// terminal, and the two ways in as verb·key chips — with the notes behind them
// leaving as ONE column when the pane cannot pay for them.
func newRoomCardView(profile tokens.Profile, mouth bool) golden.View {
	return func(width, height int, theme golden.Theme) []string {
		app := goldenAppRail(railGoldenBackend(), profile, theme, "open")
		drivePoll(app)
		app.setScope(true)
		for i, row := range app.railModel.Rows() {
			if row.ID == rowNewRoomID {
				app.applyScope(app.railModel.Select(i))
				break
			}
		}
		if mouth {
			// The keyboard back on the composer, which is the incident's own
			// state: the pane is a card and the mouth is bound elsewhere. This is
			// the frame where the composer has to NAME the room its words go to.
			app.focusConversation()
			app.refresh()
		}
		return strings.Split(app.Frame(width, height), "\n")
	}
}

// goldenAppRail is [goldenApp] with the sidebar's persisted rung wired, which is
// the pair the entry point supplies (cmd/aforge/chatv2_rail.go).
func goldenAppRail(backend Backend, profile tokens.Profile, theme golden.Theme, rung string) *App {
	return New(Options{
		Backend:   backend,
		Commander: &fakeCommander{model: "anthropic/claude-k3"},
		Session:   testSession,
		Profile:   profile,
		Linear:    theme.Calm,
		Now:       fixedNow,
		PollEvery: 1,
		Root:      "/home/someone/aforge-v2",
		Home:      "/home/someone",
		Rail:      rung,
	})
}

// railSizes are the widths the three rungs have to survive: the two ends of the
// rail's own breakpoint, an ordinary terminal where the column cannot be paid
// for and the handle stands instead, and the floor where neither fits.
var railSizes = []golden.Size{
	{Width: 60, Height: 20},
	{Width: 80, Height: 24},
	{Width: 89, Height: 24},
	{Width: 90, Height: 24},
	{Width: 120, Height: 30},
}

func TestGoldenRailOpen(t *testing.T) {
	for _, tier := range contrastTiers() {
		t.Run(tier.name, func(t *testing.T) {
			golden.RunSizes(t, "rail-open-"+tier.name, railView(tier.profile, "open", true),
				railSizes, goldenThemes)
		})
	}
}

func TestGoldenRailSlim(t *testing.T) {
	for _, tier := range contrastTiers() {
		t.Run(tier.name, func(t *testing.T) {
			golden.RunSizes(t, "rail-slim-"+tier.name, railView(tier.profile, "slim", true),
				railSizes, goldenThemes)
		})
	}
}

func TestGoldenRailHidden(t *testing.T) {
	for _, tier := range contrastTiers() {
		t.Run(tier.name, func(t *testing.T) {
			golden.RunSizes(t, "rail-hidden-"+tier.name, railView(tier.profile, "hidden", true),
				railSizes, goldenThemes)
		})
	}
}

func TestGoldenRailQuiet(t *testing.T) {
	for _, tier := range contrastTiers() {
		t.Run(tier.name, func(t *testing.T) {
			golden.RunSizes(t, "rail-quiet-"+tier.name, railQuietView(tier.profile),
				railSizes, goldenThemes)
		})
	}
}

func TestGoldenNewRoomCard(t *testing.T) {
	for _, tier := range contrastTiers() {
		t.Run(tier.name, func(t *testing.T) {
			golden.RunSizes(t, "new-room-card-"+tier.name, newRoomCardView(tier.profile, false),
				railSizes, goldenThemes)
			golden.RunSizes(t, "new-room-mouth-"+tier.name, newRoomCardView(tier.profile, true),
				railSizes, goldenThemes)
		})
	}
}

// The stored frames — the ones a reviewer reads.
//
// The open rail is kept at BOTH profiles because the tree is this wave's one new
// structure and the tertiary tier the connectors live in is the first thing a
// colour tier takes away; everything else is stored plain, where a reviewer can
// see the shape without reading escapes.
func TestGoldenRailSnapshots(t *testing.T) {
	cases := []struct {
		name    string
		view    golden.View
		width   int
		height  int
		snapKey string
	}{
		// The tree, wide, at both ends of the ladder.
		{"open", railView(tokens.TrueColor, "open", true), 120, 30, "rail-open-truecolor"},
		{"open-plain", railView(tokens.NoColor, "open", true), 120, 30, "rail-open-plain"},
		// The column at exactly its breakpoint, where it is narrowest.
		{"open-breakpoint-plain", railView(tokens.NoColor, "open", true), 90, 24, "rail-open-plain"},
		// The handle, with the dot and without it.
		{"slim-plain", railView(tokens.NoColor, "slim", true), 120, 30, "rail-slim-plain"},
		{"slim-quiet-plain", railView(tokens.NoColor, "slim", false), 120, 30, "rail-slim-quiet-plain"},
		// The width that FORCES the handle: the preference says open and 80
		// columns cannot pay for a 28-column rail beside a 60-column transcript.
		{"forced-slim-plain", railView(tokens.NoColor, "open", true), 80, 24, "rail-slim-plain"},
		// Nothing at all.
		{"hidden-plain", railView(tokens.NoColor, "hidden", true), 120, 30, "rail-hidden-plain"},
		// The composed empty state, wide and narrow.
		{"quiet-plain", railQuietView(tokens.NoColor), 120, 30, "rail-quiet-plain"},
		{"quiet-narrow-plain", railQuietView(tokens.NoColor), 90, 24, "rail-quiet-plain"},
		// The fresh room's card: wide, at the width where its notes leave, and
		// once in colour so the three tiers it spends are in a stored frame.
		{"new-room-plain", newRoomCardView(tokens.NoColor, false), 120, 30, "new-room-card-plain"},
		{"new-room-truecolor", newRoomCardView(tokens.TrueColor, false), 120, 30, "new-room-card-truecolor"},
		{"new-room-breakpoint-plain", newRoomCardView(tokens.NoColor, false), 90, 24, "new-room-card-plain"},
		// The same card with the keyboard on the composer: the frame the incident
		// happened in, with the line that now names where the words go.
		{"new-room-mouth-plain", newRoomCardView(tokens.NoColor, true), 120, 30, "new-room-mouth-plain"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			golden.Snap(t, testCase.snapKey, testCase.view,
				testCase.width, testCase.height, golden.Theme{Mode: golden.Dark})
		})
	}
}

// railGoldenFixtureSanity keeps the frames above honest about what they are
// showing: a fixture whose live job lost its parts would store a picture of a
// tree with no branches and nothing would fail.
func TestRailGoldenFixtureHasATreeToDraw(t *testing.T) {
	backend := railGoldenBackend()
	parts := 0
	for _, node := range backend.nodes {
		if node.Parent == "job-1" {
			parts++
		}
	}
	if parts < 2 {
		t.Fatalf("the fixture's live job has %d parts: there is no tree to draw", parts)
	}
	if len(backend.sessions) < 2 {
		t.Fatalf("the fixture has %d conversations: there is no thread list to draw",
			len(backend.sessions))
	}
	var settled int
	for _, node := range backend.nodes {
		if node.Status == store.Done && node.Parent == "" {
			settled++
		}
	}
	if settled == 0 {
		t.Fatal("the fixture has no settled job, so nothing pins that history does not expand")
	}
}
