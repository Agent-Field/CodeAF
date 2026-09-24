package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// THE CREW PANEL'S SCREENS, PINNED WORD FOR WORD. Each is the plain text of the
// block exactly as the overlay draws it at one width, over the catalog crewLab
// routes on, so a change to what any of them says — a column moved, a word
// respelled, a warning that grew a second copy — fails here and prints both
// screens. The marks are the vocabulary's, asked of the surface ({pin}, {tick},
// {fail}, {star}), because which repertoire draws them is the terminal's
// business and not the screen's. The paint is not pinned: the tests beside
// these assert colour where colour is the subject.

// crewSnap compares a screen against the one it is pinned to.
func crewSnap(t *testing.T, a *app, name, want string) {
	t.Helper()
	want = strings.NewReplacer(
		"{pin}", a.icon(tokens.GPinned), "{tick}", a.icon(tokens.GSettled),
		"{fail}", a.icon(tokens.GFailed), "{star}", a.icon(tokens.GRecommended),
	).Replace(strings.TrimPrefix(want, "\n"))
	if got := crewScreen(a); got != want {
		t.Fatalf("the %s screen moved.\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func TestCrewSnapshotPanel(t *testing.T) {
	a, dir := crewLab(t)
	if err := config.SetCrewPin(dir, crewroute.Checker, "moonshotai/kimi-k3"); err != nil {
		t.Fatal(err)
	}
	typeLine(t, a, "/crew")
	crewSnap(t, a, "panel", `
╭─ crew ───────────────────────────────────────────────────────────────────────────────────── esc ─╮
│› worker    auto · likely glm-5.3-flash                                                           │
│  planner   auto · likely glm-5.3-flash                                                           │
│  checker   {pin} kimi-k3                                                                             │
│                                                                                                  │
│  models    ‹ all › (4)                                                                           │
│  cap       none                                                                                  │
╰─ enter change · esc close · ? keys ──────────────────────────────────────────────────────────────╯`)
}

func TestCrewSnapshotSeatList(t *testing.T) {
	a, _ := crewLab(t)
	typeLine(t, a, "/crew")
	drive(t, a, key("enter"))
	crewSnap(t, a, "seat list", `
╭─ crew · worker ──────────────────────────────────────────────────────────────────────────── esc ─╮
│›   auto — codeaf picks per task                                                                  │
│  {star} z-ai/glm-5.3-flash  $0.15/$0.50 · openrouter · suggested                                      │
│    anthropic/claude-opus-5  $5/$25 · openrouter                                                  │
│    deepseek/deepseek-v4-flash  $0.08/$0.16 · openrouter                                          │
│    moonshotai/kimi-k3  $3/$15 · openrouter                                                       │
╰─ type to filter · enter pick · → routes · esc back ──────────────────────────────────────────────╯`)
}

func TestCrewSnapshotRefusal(t *testing.T) {
	a, dir := crewLab(t)
	if err := config.SetCrewAllowed(dir, "open"); err != nil {
		t.Fatal(err)
	}
	typeLine(t, a, "/crew")
	drive(t, a, key("enter"))
	typeText(t, a, "opus")
	drive(t, a, key("enter"))
	crewSnap(t, a, "refusal", `
╭─ crew · worker ──────────────────────────────────────────────────────────────────────────── esc ─╮
│›   anthropic/claude-opus-5  $5/$25 · openrouter · not allowed                                    │
│  {fail} claude-opus-5 is not in your allowed models (open) — enter to allow it                        │
╰─ type to filter · enter pick · → routes · esc back ──────────────────────────────────────────────╯`)
}

func TestCrewSnapshotPriceBeingTyped(t *testing.T) {
	a, _ := crewLab(t)
	typeLine(t, a, "/crew")
	drive(t, a, key("down"), key("down"), key("down"), key("right"), key("right"), key("enter"), key("ctrl+u"))
	typeText(t, a, "0.5")
	crewSnap(t, a, "price being typed", `
╭─ crew ───────────────────────────────────────────────────────────────────────────────────── esc ─╮
│  worker    auto · likely glm-5.3-flash                                                           │
│  planner   auto · likely glm-5.3-flash                                                           │
│  checker   auto · likely deepseek-v4-flash                                                       │
│                                                                                                  │
│› models    ‹ price ›  ≤ $[ 0.5 ] in / $[ 5 ] out (2)  {tick}                                          │
│  cap       none                                                                                  │
│  no strong checker among the models you allow · open-ended work will be checked weakly           │
╰─ enter change · esc close · ? keys ──────────────────────────────────────────────────────────────╯`)
}

func TestCrewSnapshotChecklist(t *testing.T) {
	a, dir := crewLab(t)
	if err := config.SetCrewAllowed(dir, "≤1/5"); err != nil {
		t.Fatal(err)
	}
	typeLine(t, a, "/crew")
	drive(t, a, key("down"), key("down"), key("down"), key("right"), key("enter"))
	crewSnap(t, a, "checklist", `
╭─ crew · allowed models · 2 of 4 ─────────────────────────────────────────────────────────── esc ─╮
│› {tick} openrouter  whole provider · metered                                                          │
│  {tick} z-ai/glm-5.3-flash  $0.15/$0.50                                                               │
│    moonshotai/kimi-k3  $3/$15                                                                    │
│  {tick} deepseek/deepseek-v4-flash  $0.08/$0.16                                                       │
│    anthropic/claude-opus-5  $5/$25                                                               │
╰─ type to filter · space or enter tick · esc back ────────────────────────────────────────────────╯`)
}

func TestCrewSnapshotNarrow(t *testing.T) {
	a, dir := crewLab(t)
	if err := config.SetCrewPin(dir, crewroute.Checker, "moonshotai/kimi-k3"); err != nil {
		t.Fatal(err)
	}
	a.width = 56
	typeLine(t, a, "/crew")
	crewSnap(t, a, "narrow", `
╭─ crew ───────────────────────────────────────── esc ─╮
│› worker    auto                                      │
│  planner   auto                                      │
│  checker   {pin} kimi-k3                                 │
│                                                      │
│  models    ‹ all › (4)                               │
│  cap       none                                      │
╰─ enter change · esc close · ? keys ──────────────────╯`)
}

func TestCrewSnapshotNoProviders(t *testing.T) {
	a, _ := crewLab(t)
	t.Setenv(config.APIKeyEnv, "")
	typeLine(t, a, "/crew")
	crewSnap(t, a, "no providers", `
╭─ crew ───────────────────────────────────────────────────────────────────────────────────── esc ─╮
│› worker    auto                                                                                  │
│  planner   auto                                                                                  │
│  checker   auto                                                                                  │
│                                                                                                  │
│  models    ‹ all › (0)                                                                           │
│  cap       none                                                                                  │
│  no providers connected — /connect adds one                                                      │
╰─ enter change · esc close · ? keys ──────────────────────────────────────────────────────────────╯`)
}

func TestCrewSnapshotUndoOffer(t *testing.T) {
	a, _ := crewLab(t)
	typeLine(t, a, "/crew")
	drive(t, a, key("down"), key("down"), key("down"), key("right"))
	crewSnap(t, a, "undo offer", `
╭─ crew ───────────────────────────────────────────────────────────────────────────────────── esc ─╮
│  worker    auto · likely glm-5.3-flash                                                           │
│  planner   auto · likely glm-5.3-flash                                                           │
│  checker   auto · likely deepseek-v4-flash                                                       │
│                                                                                                  │
│› models    ‹ open › (3)  {tick}                                                                       │
│  cap       none                                                                                  │
╰─ enter change · esc close · ? keys ───────────────────────────────────────────────────── z undo ─╯`)
}
