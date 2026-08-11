package chat

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/golden"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Golden coverage for the dressed surface.
//
// The harness checks three things on every render it drives — exactly height
// rows, no row wider than the viewport, and no control byte that is not an SGR
// sequence — which between them cover the failure modes a re-dress introduces:
// a wrapper that miscounted a painted cell, a glyph that turned out to be two
// columns wide, a truncation that cut through an escape.
//
// It runs at both ends of the contrast ladder. TrueColor is the surface as a
// modern terminal draws it, every token resolved to a 24-bit pastel; NoColor is
// the same frame with the token layer switched off entirely, which is what a
// pipe, a CI log and the accessible rendering all see. A dressing that only
// worked in one of the two would be a dressing that had smuggled meaning into
// colour — exactly what 5.16's grey ramp exists to prevent.

// goldenSizes are the widths this surface actually has to survive: the two
// floors, the rail breakpoints on both sides, and the ordinary terminal.
var goldenSizes = []golden.Size{
	{Width: 1, Height: 1},
	{Width: 2, Height: 2},
	{Width: 24, Height: 8},
	{Width: 40, Height: 12},
	{Width: 60, Height: 20},
	{Width: 79, Height: 24},
	{Width: 80, Height: 24},
	{Width: 100, Height: 24},
	{Width: 120, Height: 30},
}

// goldenThemes drives the calm axis (10.1.5's linear rendering, which freezes
// the one live glyph this surface has) alongside the ordinary one.
var goldenThemes = []golden.Theme{
	{Mode: golden.Dark},
	{Mode: golden.Dark, Calm: true},
}

// dressedView renders the settled transcript: one of every row kind, at a
// profile, with the clock and the ground pinned so a frame is a function of the
// journal and the size alone.
func dressedView(profile tokens.Profile) golden.View {
	return func(width, height int, theme golden.Theme) []string {
		backend := &fakeBackend{}
		dressedThread(backend)
		app := goldenApp(backend, profile, theme)
		drivePoll(app)
		return strings.Split(app.Frame(width, height), "\n")
	}
}

// liveView renders a turn mid-stream: the awaiting line, its interrupt hint,
// and a reply that has not been journaled yet.
func liveView(profile tokens.Profile) golden.View {
	return func(width, height int, theme golden.Theme) []string {
		backend := &fakeBackend{}
		dressedThread(backend)
		app := goldenApp(backend, profile, theme)
		drivePoll(app)
		app.Update(streamBatchMsg{events: []StreamEvent{
			{Kind: StreamStarted, Session: testSession},
			{Kind: StreamDelta, Session: testSession, Delta: `{"reply":"Reading navctx.rs now`},
		}})
		return strings.Split(app.Frame(width, height), "\n")
	}
}

// cutView renders the truncation law's two halves (12.5.2) — the sticky header
// badge and the rule under the body — so a re-dress cannot quietly lose them at
// a width nobody looked at.
func cutView(profile tokens.Profile) golden.View {
	return func(width, height int, theme golden.Theme) []string {
		backend := &fakeBackend{}
		backend.add(store.Message{
			SessionID: testSession, Role: store.RoleAgent,
			Body: "Here is the diagram you asked for, in full:\n\n```svg\n<svg viewBox=\"0 0 10 10\">",
			Parts: []store.MessagePart{
				store.EndedMark(store.EndedPart{How: store.EndLength}),
			},
		})
		app := goldenApp(backend, profile, theme)
		drivePoll(app)
		return strings.Split(app.Frame(width, height), "\n")
	}
}

func goldenApp(backend Backend, profile tokens.Profile, theme golden.Theme) *App {
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
	})
}

// drivePoll runs one store read through the app without a testing.T, which is
// what the golden harness's View signature leaves room for.
func drivePoll(app *App) {
	app.polling = true
	if cmd := app.pollCmd(); cmd != nil {
		if result, ok := cmd().(pollResultMsg); ok {
			app.applyPoll(result)
		}
	}
	app.polling = false
}

func TestGoldenDressedTranscript(t *testing.T) {
	for _, tier := range contrastTiers() {
		t.Run(tier.name, func(t *testing.T) {
			golden.RunSizes(t, "chat-"+tier.name, dressedView(tier.profile), goldenSizes, goldenThemes)
		})
	}
}

func TestGoldenLiveTurn(t *testing.T) {
	for _, tier := range contrastTiers() {
		t.Run(tier.name, func(t *testing.T) {
			golden.RunSizes(t, "chat-live-"+tier.name, liveView(tier.profile), goldenSizes, goldenThemes)
		})
	}
}

func TestGoldenCutTurn(t *testing.T) {
	for _, tier := range contrastTiers() {
		t.Run(tier.name, func(t *testing.T) {
			golden.RunSizes(t, "chat-cut-"+tier.name, cutView(tier.profile), goldenSizes, goldenThemes)
		})
	}
}

// The stored frames. These are the ones a reviewer reads: an ordinary terminal
// with colour, the same terminal without it, and the narrow shape where every
// degrade path fires at once.
func TestGoldenSnapshots(t *testing.T) {
	cases := []struct {
		name    string
		view    golden.View
		width   int
		height  int
		theme   golden.Theme
		snapKey string
	}{
		{"ordinary", dressedView(tokens.TrueColor), 80, 24, golden.Theme{Mode: golden.Dark}, "chat-truecolor"},
		{"ordinary-plain", dressedView(tokens.NoColor), 80, 24, golden.Theme{Mode: golden.Dark}, "chat-plain"},
		{"narrow-plain", dressedView(tokens.NoColor), 40, 16, golden.Theme{Mode: golden.Dark}, "chat-plain"},
		{"live-plain", liveView(tokens.NoColor), 80, 20, golden.Theme{Mode: golden.Dark}, "chat-live-plain"},
		{"cut-plain", cutView(tokens.NoColor), 80, 16, golden.Theme{Mode: golden.Dark}, "chat-cut-plain"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			golden.Snap(t, testCase.snapKey, testCase.view, testCase.width, testCase.height, testCase.theme)
		})
	}
}

func contrastTiers() []struct {
	name    string
	profile tokens.Profile
} {
	return []struct {
		name    string
		profile tokens.Profile
	}{
		{"truecolor", tokens.TrueColor},
		{"plain", tokens.NoColor},
	}
}
