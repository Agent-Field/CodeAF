package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/golden"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Golden coverage for the chats surface.
//
// Four visuals ship in this wave and all four are here, at both ends of the
// contrast ladder and at both ends of the width ladder: the switcher over the
// conversation, the title chip on the bar row, the ruled line that marks a
// thread break, and the open-threads band on the board home.
//
// The harness's three structural checks are what these buy — exactly height
// rows, nothing wider than the viewport, no control byte that is not an SGR
// sequence — and they are exactly the failures a new overlay and a new block
// introduce: a glyph that turns out to be two columns wide, a rule that
// miscounted a painted cell, a truncation that cut through an escape.

// threadsGoldenBackend is the chats fixture with a fixed clock: two named
// threads, one line in each, and a board behind them.
func threadsGoldenBackend() *threadsBackend {
	board := board()
	board.sessions = []store.Session{
		{ID: testSession, Title: "the wisp parity push",
			LastActive: fixedNow().Add(-2 * time.Hour)},
		{ID: "session-two", Title: "importer rewrite",
			LastActive: fixedNow().Add(-30 * time.Hour)},
	}
	backend := &threadsBackend{boardBackend: board}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleUser,
		Body: "how is the parity push going"})
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent,
		Body: "the diff is ready when you are"})
	backend.add(store.Message{SessionID: "session-two", Role: store.RoleAgent,
		Body: "parked on the schema question"})
	return backend
}

// switcherView is the switcher raised over the conversation, which is the whole
// picture a reader sees when they press the key — the overlay, its chrome ring,
// and the room it is floating over.
func switcherView(profile tokens.Profile) golden.View {
	return func(width, height int, theme golden.Theme) []string {
		app := goldenApp(threadsGoldenBackend(), profile, theme)
		drivePoll(app)
		// One thread this window has visited and left, with something newer in
		// it: the ONE ornament, so the `●` is covered by a stored frame at every
		// profile rather than only by an assertion.
		app.noteThreadSeen("session-two", fixedNow().Add(-40*time.Hour))
		if cmd := app.openSwitcher(); cmd != nil {
			_ = cmd()
		}
		return strings.Split(app.Frame(width, height), "\n")
	}
}

// chipView is the ordinary conversation with the title chip on its bar row. It
// is the dressed thread the other goldens already cover, plus the one fact this
// wave put on the row, so a regression in either shows up here.
func chipView(profile tokens.Profile) golden.View {
	return func(width, height int, theme golden.Theme) []string {
		app := goldenApp(threadsGoldenBackend(), profile, theme)
		drivePoll(app)
		return strings.Split(app.Frame(width, height), "\n")
	}
}

// breakView is a window that has just arrived in another thread: the ruled join
// at the top of the transcript, the arrived thread's own journal under it, and
// the chip renamed on the bar row.
func breakView(profile tokens.Profile) golden.View {
	return func(width, height int, theme golden.Theme) []string {
		app := goldenApp(threadsGoldenBackend(), profile, theme)
		drivePoll(app)
		if cmd := app.switchThread("session-two"); cmd != nil {
			_ = cmd()
		}
		drivePoll(app)
		return strings.Split(app.Frame(width, height), "\n")
	}
}

// overviewView is the page the place line's root segment lands on: every
// conversation, the `+ new` door, and the work under them.
//
// One thread carries the unseen `●` — this window has been in `session-two` and
// something has landed there since — so the ornament is covered by a stored
// frame at every profile rather than only by an assertion.
func overviewView(profile tokens.Profile) golden.View {
	return func(width, height int, theme golden.Theme) []string {
		app := goldenApp(threadsGoldenBackend(), profile, theme)
		drivePoll(app)
		app.noteThreadSeen("session-two", fixedNow().Add(-40*time.Hour))
		app.showOverview()
		return strings.Split(app.Frame(width, height), "\n")
	}
}

// freshOverviewView is the frame a first-run window draws: two words, two
// sentences and one door. It is pinned because it is the frame this page is
// judged on — 12.10's rule is that an empty surface and an unwired one must
// never look alike, and only a stored frame keeps that true.
func freshOverviewView(profile tokens.Profile) golden.View {
	return func(width, height int, theme golden.Theme) []string {
		app := goldenApp(&fakeBackend{}, profile, theme)
		drivePoll(app)
		app.showOverview()
		return strings.Split(app.Frame(width, height), "\n")
	}
}

// threadsSizes are the widths these surfaces have to survive. The overlay is
// sized from the LENS rect and goes fullscreen below the dialog breakpoints
// (tui2/layout.go), so the list spans both sides of that line.
var threadsSizes = []golden.Size{
	{Width: 24, Height: 8},
	{Width: 40, Height: 12},
	{Width: 60, Height: 16},
	{Width: 71, Height: 19},
	{Width: 72, Height: 20},
	{Width: 90, Height: 24},
	{Width: 120, Height: 30},
}

func TestGoldenThreadSwitcher(t *testing.T) {
	for _, tier := range contrastTiers() {
		t.Run(tier.name, func(t *testing.T) {
			golden.RunSizes(t, "threads-switcher-"+tier.name, switcherView(tier.profile),
				threadsSizes, goldenThemes)
		})
	}
}

func TestGoldenTitleChip(t *testing.T) {
	for _, tier := range contrastTiers() {
		t.Run(tier.name, func(t *testing.T) {
			golden.RunSizes(t, "threads-chip-"+tier.name, chipView(tier.profile),
				threadsSizes, goldenThemes)
		})
	}
}

func TestGoldenThreadBreak(t *testing.T) {
	for _, tier := range contrastTiers() {
		t.Run(tier.name, func(t *testing.T) {
			golden.RunSizes(t, "threads-break-"+tier.name, breakView(tier.profile),
				threadsSizes, goldenThemes)
		})
	}
}

func TestGoldenOverview(t *testing.T) {
	for _, tier := range contrastTiers() {
		t.Run(tier.name, func(t *testing.T) {
			golden.RunSizes(t, "overview-"+tier.name, overviewView(tier.profile),
				threadsSizes, goldenThemes)
		})
	}
}

func TestGoldenFreshOverview(t *testing.T) {
	for _, tier := range contrastTiers() {
		t.Run(tier.name, func(t *testing.T) {
			golden.RunSizes(t, "overview-fresh-"+tier.name, freshOverviewView(tier.profile),
				threadsSizes, goldenThemes)
		})
	}
}

// The stored frames — the ones a reviewer reads. One per visual, at the two
// widths that matter most: an ordinary terminal, and the narrow shape where
// every degrade path fires at once. The switcher is stored at BOTH profiles
// because it is the wave's one new overlay and the sheet's ground is the first
// thing a colour tier takes away.
func TestGoldenThreadSnapshots(t *testing.T) {
	cases := []struct {
		name    string
		view    golden.View
		width   int
		height  int
		snapKey string
	}{
		{"switcher", switcherView(tokens.TrueColor), 100, 24, "threads-switcher-truecolor"},
		{"switcher-plain", switcherView(tokens.NoColor), 100, 24, "threads-switcher-plain"},
		{"switcher-narrow-plain", switcherView(tokens.NoColor), 60, 16, "threads-switcher-plain"},
		{"chip-plain", chipView(tokens.NoColor), 100, 24, "threads-chip-plain"},
		{"chip-narrow-plain", chipView(tokens.NoColor), 60, 16, "threads-chip-plain"},
		{"break-plain", breakView(tokens.NoColor), 100, 24, "threads-break-plain"},
		{"break-narrow-plain", breakView(tokens.NoColor), 60, 16, "threads-break-plain"},
		// The overview at both profiles: it is the page root lands on, and the
		// two things a colour tier takes away — the unseen dot's cyan and the
		// selection rail's ground — are both on it.
		{"overview", overviewView(tokens.TrueColor), 100, 24, "overview-truecolor"},
		{"overview-plain", overviewView(tokens.NoColor), 100, 24, "overview-plain"},
		{"overview-narrow-plain", overviewView(tokens.NoColor), 60, 16, "overview-plain"},
		{"overview-fresh-plain", freshOverviewView(tokens.NoColor), 100, 24, "overview-fresh-plain"},
		{"overview-fresh-narrow-plain", freshOverviewView(tokens.NoColor), 60, 16, "overview-fresh-plain"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			golden.Snap(t, testCase.snapKey, testCase.view,
				testCase.width, testCase.height, golden.Theme{Mode: golden.Dark})
		})
	}
}

// And the calm rendering, stored once for the surface that changes most under
// it: linear mode turns the switcher into a plain numbered list, and a golden
// is the only thing that keeps that true.
func TestGoldenThreadSwitcherCalm(t *testing.T) {
	golden.Snap(t, "threads-switcher-calm", switcherView(tokens.NoColor), 100, 24,
		golden.Theme{Mode: golden.Dark, Calm: true})
}
