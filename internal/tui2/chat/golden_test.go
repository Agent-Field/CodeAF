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

// activityView renders a turn MID-TOOL-ROUND: two calls settled, one still
// running, under the words the head has streamed so far and above the awaiting
// line. It is the frame the whole activity wave exists to produce, so it is
// pinned at every width the surface has to survive.
func activityView(profile tokens.Profile) golden.View {
	return func(width, height int, theme golden.Theme) []string {
		backend := &fakeBackend{}
		dressedThread(backend)
		app := goldenApp(backend, profile, theme)
		drivePoll(app)
		app.Update(streamBatchMsg{events: []StreamEvent{
			{Kind: StreamStarted, Session: testSession},
			{Kind: StreamToolBegin, Session: testSession, Delta: "looking at the work"},
			{Kind: StreamToolEnd, Session: testSession, Delta: "3 rows"},
			{Kind: StreamToolBegin, Session: testSession, Delta: "searching for «navctx»"},
			{Kind: StreamToolFailed, Session: testSession, Delta: "nothing matched"},
			{Kind: StreamDelta, Session: testSession, Delta: `{"reply":"Reading navctx.rs now`},
			{Kind: StreamToolBegin, Session: testSession, Delta: "reading «navctx.rs»"},
		}})
		return strings.Split(app.Frame(width, height), "\n")
	}
}

// collapsedActivityView renders the same turn AFTER its reply landed: the rows
// are one row, attached under the answer, behind the disclosure grammar.
func collapsedActivityView(profile tokens.Profile, open bool) golden.View {
	return func(width, height int, theme golden.Theme) []string {
		backend := &fakeBackend{}
		dressedThread(backend)
		app := goldenApp(backend, profile, theme)
		drivePoll(app)
		app.Update(streamBatchMsg{events: []StreamEvent{
			{Kind: StreamStarted, Session: testSession},
			{Kind: StreamToolBegin, Session: testSession, Delta: "looking at the work"},
			{Kind: StreamToolEnd, Session: testSession, Delta: "3 rows"},
			{Kind: StreamToolBegin, Session: testSession, Delta: "searching for «navctx»"},
			{Kind: StreamToolEnd, Session: testSession, Delta: "2 rows"},
			{Kind: StreamToolBegin, Session: testSession, Delta: "reading the plan for «task-9»"},
			{Kind: StreamToolEnd, Session: testSession, Delta: ""},
		}})
		backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent,
			Body: "Three things are running; navctx is the one that failed."})
		drivePoll(app)
		if open {
			// The reader's own door, opened. In linear mode there is none, and
			// the row is already the whole summary.
			if block, ok := app.transcript.Block(app.transcript.Len() - 1).(*messageBlock); ok {
				app.toggleFold(block)
			}
		}
		app.transcript.GotoBottom()
		return strings.Split(app.Frame(width, height), "\n")
	}
}

// stageView renders a pending commission mid-compile: the skeleton with the
// planner's own phase under its title, instead of the bare pulse.
func stageView(profile tokens.Profile) golden.View {
	return func(width, height int, theme golden.Theme) []string {
		backend := &stagedBackend{pendingBackend{boardBackend: boardBackend{}}}
		backend.add(store.Message{SessionID: testSession, Role: store.RoleUser,
			Body: "build me the panel site"})
		app := goldenApp(backend, profile, theme)
		drivePoll(app)
		backend.pending = []store.Command{{Seq: 900, SessionID: testSession,
			Kind: store.CommandSplice, Instruction: "Build the panel site from scratch"}}
		backend.journal++
		backend.stage(900, "setting working standards", 2, 3)
		drivePoll(app)
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

// The activity wave's three frames, at both ends of the contrast ladder and on
// both sides of the calm axis — which is also the linear rendering, where the
// collapse row is a plain summary with no door on it (10.1.5).
func TestGoldenLiveActivity(t *testing.T) {
	for _, tier := range contrastTiers() {
		t.Run(tier.name, func(t *testing.T) {
			golden.RunSizes(t, "chat-activity-"+tier.name, activityView(tier.profile),
				goldenSizes, goldenThemes)
		})
	}
}

func TestGoldenCollapsedActivity(t *testing.T) {
	for _, tier := range contrastTiers() {
		t.Run(tier.name, func(t *testing.T) {
			golden.RunSizes(t, "chat-activity-shut-"+tier.name,
				collapsedActivityView(tier.profile, false), goldenSizes, goldenThemes)
			golden.RunSizes(t, "chat-activity-open-"+tier.name,
				collapsedActivityView(tier.profile, true), goldenSizes, goldenThemes)
		})
	}
}

func TestGoldenPendingStage(t *testing.T) {
	for _, tier := range contrastTiers() {
		t.Run(tier.name, func(t *testing.T) {
			golden.RunSizes(t, "chat-stage-"+tier.name, stageView(tier.profile),
				goldenSizes, goldenThemes)
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
		// The activity wave's stored frames: the live rows mid-round, the one
		// row they become, that row opened, and the narrating skeleton. Narrow
		// as well as wide for the collapse row, because the summary is the one
		// thing here that has to survive a measure it cannot fit in.
		{"activity-plain", activityView(tokens.NoColor), 80, 20,
			golden.Theme{Mode: golden.Dark}, "chat-activity-plain"},
		{"activity-shut-plain", collapsedActivityView(tokens.NoColor, false), 80, 20,
			golden.Theme{Mode: golden.Dark}, "chat-activity-shut-plain"},
		{"activity-shut-narrow", collapsedActivityView(tokens.NoColor, false), 40, 16,
			golden.Theme{Mode: golden.Dark}, "chat-activity-shut-plain"},
		{"activity-shut-color", collapsedActivityView(tokens.TrueColor, false), 80, 20,
			golden.Theme{Mode: golden.Dark}, "chat-activity-shut-truecolor"},
		{"activity-open-plain", collapsedActivityView(tokens.NoColor, true), 80, 24,
			golden.Theme{Mode: golden.Dark}, "chat-activity-open-plain"},
		// The accessible rendering: a plain summary line and no door on it.
		{"activity-linear", collapsedActivityView(tokens.NoColor, false), 80, 20,
			golden.Theme{Mode: golden.Dark, Calm: true}, "chat-activity-linear"},
		{"stage-plain", stageView(tokens.NoColor), 80, 20,
			golden.Theme{Mode: golden.Dark}, "chat-stage-plain"},
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
