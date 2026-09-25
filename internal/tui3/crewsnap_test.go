package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/router"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// THE CREW PANEL'S SCREENS, PINNED WORD FOR WORD. Each is the plain text of the
// block exactly as the overlay draws it at one width, over the catalog crewLab
// routes on, so a change to what any of them says — a column moved, a word
// respelled, a warning that grew a second copy — fails here and prints both
// screens. The marks are the vocabulary's, asked of the surface ({pin}, {tick},
// {fail}, {star}, {off}), because which repertoire draws them is the terminal's
// business and not the screen's. The paint is not pinned: the tests beside
// these assert colour where colour is the subject.

// crewSnap compares a screen against the one it is pinned to.
func crewSnap(t *testing.T, a *app, name, want string) {
	t.Helper()
	want = strings.NewReplacer(
		"{pin}", a.icon(tokens.GPinned), "{tick}", a.icon(tokens.GSettled),
		"{fail}", a.icon(tokens.GFailed), "{star}", a.icon(tokens.GRecommended), "{off}", a.icon(tokens.GQueued),
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
│  planner   auto · likely kimi-k3                                                                 │
│  checker   {pin} kimi-k3                                                                             │
│                                                                                                  │
│  models    ‹ all › (4)                                                                           │
│  providers {tick} openrouter  +                                                                       │
│  cap       per task $5 · daily none                                                              │
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
│  checker   auto · likely glm-5.3-flash                                                           │
│                                                                                                  │
│› models    ‹ price ›  ≤ $[ 0.5 ] in / $[ 5 ] out (2)  {tick}                                          │
│  providers {tick} openrouter  +                                                                       │
│  cap       per task $5 · daily none                                                              │
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
│› {tick} z-ai/glm-5.3-flash  $0.15/$0.50                                                               │
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
│  providers {tick} openrouter  +                           │
│  cap       per task $5 · daily none                  │
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
│  providers +                                                                                     │
│  cap       per task $5 · daily none                                                              │
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
│  planner   auto · likely kimi-k3                                                                 │
│  checker   auto · likely kimi-k3                                                                 │
│                                                                                                  │
│› models    ‹ open › (3)  {tick}                                                                       │
│  providers {tick} openrouter  +                                                                       │
│  cap       per task $5 · daily none                                                              │
╰─ enter change · esc close · ? keys ───────────────────────────────────────────────────── z undo ─╯`)
}

func TestCrewSnapshotProvidersRow(t *testing.T) {
	a, dir := crewProvidersLab(t)
	if err := config.SetCrewProviderOn(dir, "ollama", false); err != nil {
		t.Fatal(err)
	}
	crewToProviders(t, a)
	crewSnap(t, a, "providers row", `
╭─ crew ───────────────────────────────────────────────────────────────────────────────────── esc ─╮
│  worker    auto · likely glm-5.3-flash                                                           │
│  planner   auto · likely kimi-k3                                                                 │
│  checker   auto · likely glm-5.3-flash                                                           │
│                                                                                                  │
│  models    ‹ all › (4)                                                                           │
│› providers {tick} openrouter  {tick} z-ai sub  {off} ollama local  {tick} my-vllm  +                                │
│  cap       per task $5 · daily none                                                              │
╰─ enter change · space toggle · esc close · ? keys ───────────────────────────────────────────────╯`)
}

func TestCrewSnapshotProvidersList(t *testing.T) {
	a, dir := crewProvidersLab(t)
	if err := config.SetCrewProviderOn(dir, "ollama", false); err != nil {
		t.Fatal(err)
	}
	// A task whose worker and planner rode OpenRouter and whose checker rode the
	// plan: the whole of its cost is OpenRouter's.
	record := router.CrewRecord{TaskClass: "bugfix", Seats: map[string]string{"worker": "a", "planner": "b", "checker": "c"},
		Providers: map[string]string{"worker": "openrouter", "planner": "openrouter", "checker": "z-ai"},
		Kinds:     map[string]string{"worker": "metered", "planner": "metered", "checker": "plan"}}
	router.LogCrewDecision(config.ProfilePath(dir, ""), "crew:snap", record, nil)
	router.LogCrewOutcome(config.ProfilePath(dir, ""), "crew:snap", record, router.CrewAccepted, 0.21)
	crewToProviders(t, a)
	drive(t, a, key("enter"))
	crewSnap(t, a, "providers list", `
╭─ crew · providers · 3 of 4 on ───────────────────────────────────────────────────────────── esc ─╮
│› {tick} openrouter  api key          4 models  · today $0.210   on                                    │
│  {tick} z-ai        subscription     1 model   · nothing today  on                                    │
│  {off} ollama      local            0 models  · nothing today  off                                   │
│  {tick} my-vllm     custom endpoint  pins only · nothing today  on                                    │
│                                                                                                  │
│  free routes  off · rate-limited, may log prompts                                                │
╰─ space or enter toggle · esc back ───────────────────────────────────────────────────────────────╯`)
}

func TestCrewSnapshotFreeRoutesOn(t *testing.T) {
	a, _ := crewLab(t)
	crewToProviders(t, a)
	drive(t, a, key("enter"), key("down"), key(" "))
	crewSnap(t, a, "free routes on", `
╭─ crew · providers · 1 of 1 on ───────────────────────────────────────────────────────────── esc ─╮
│  {tick} openrouter  api key  4 models · nothing today  on                                             │
│                                                                                                  │
│› free routes  on · rate-limited, may log prompts  {tick}                                              │
╰─ space or enter toggle · esc back ────────────────────────────────────────────────────── z undo ─╯`)
}
