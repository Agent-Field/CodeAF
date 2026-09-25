package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	modelcatalog "github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// THE CREW PANEL, DRIVEN THE WAY A PERSON DRIVES IT: the keys of the five
// journeys the panel was designed around, counted, and the screens each one
// leaves behind — the panel, the seat list, a price being typed, the
// checklist, a narrow window, a machine with nothing connected, and the undo
// offer.

// crewLab is a surface with a provider key and a small catalog behind it, the
// ordinary state a crew is chosen in. The catalog is the one internal/config's
// own crew tests route over, so a figure here is a figure there.
func crewLab(t *testing.T) (*app, string) {
	t.Helper()
	for _, name := range []string{config.APIKeyEnv, "OPENAI_API_KEY", config.ModelEnv, config.PlanModelEnv,
		config.CheckModelEnv, "CODEAF_BASE_URL", "DEEPSEEK_API_KEY", "ZHIPU_API_KEY", "MOONSHOT_API_KEY",
		"MINIMAX_API_KEY", "DASHSCOPE_API_KEY"} {
		t.Setenv(name, "")
	}
	a, dir := sheetApp(t)
	t.Setenv(config.APIKeyEnv, "sk-or-v1-crewpanel-0123456789")
	previous := config.CrewCatalog
	t.Cleanup(func() { config.CrewCatalog = previous })
	rows := []modelcatalog.Model{
		{ID: "z-ai/glm-5.3-flash", CanonicalSlug: "z-ai/glm-5.3-flash-20260826", OpenWeights: true, PromptPrice: 1.5e-7, CompletionPrice: 5e-7, CacheReadPrice: 5e-8,
			IntelligenceIndex: 41.8, CodingIndex: 71.5, AgenticIndex: 50.9, ArenaElo: 1348, ContextLength: 1310720, Parameters: []string{"tools"}},
		{ID: "moonshotai/kimi-k3", CanonicalSlug: "moonshotai/kimi-k3-20260715", OpenWeights: true, PromptPrice: 3e-6, CompletionPrice: 1.5e-5, CacheReadPrice: 3e-7,
			IntelligenceIndex: 43.6, CodingIndex: 76.2, AgenticIndex: 50, ArenaElo: 1421, ContextLength: 1048576, Parameters: []string{"tools"}},
		{ID: "deepseek/deepseek-v4-flash", CanonicalSlug: "deepseek/deepseek-v4-flash-20260423", OpenWeights: true, PromptPrice: 8.246e-8, CompletionPrice: 1.6492e-7, CacheReadPrice: 1.6492e-8,
			IntelligenceIndex: 24.2, CodingIndex: 56.2, AgenticIndex: 22.2, ArenaElo: 1216, ContextLength: 1048576, Parameters: []string{"tools"}},
		{ID: "anthropic/claude-opus-5", CanonicalSlug: "anthropic/claude-opus-5-20260723", PromptPrice: 5e-6, CompletionPrice: 2.5e-5, CacheReadPrice: 5e-7,
			IntelligenceIndex: 50.8, CodingIndex: 78, AgenticIndex: 56.5, ArenaElo: 1372, ContextLength: 1000000, Parameters: []string{"tools"}},
	}
	config.CrewCatalog = func() []modelcatalog.Model { return rows }
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	a.clock = func() time.Time { return now }
	a.width, a.height = 100, 40
	return a, dir
}

// crewScreen is the panel's block as a reader sees it.
func crewScreen(a *app) string { return strings.Join(plainOverlay(a), "\n") }

// press drives keys and counts them, so a journey's cost is a number a test
// can hold the design to.
type crewJourney struct {
	t     *testing.T
	a     *app
	count int
}

func (j *crewJourney) keys(names ...string) {
	j.t.Helper()
	for _, name := range names {
		drive(j.t, j.a, key(name))
		j.count++
	}
}

func (j *crewJourney) typed(text string) {
	j.t.Helper()
	for _, r := range text {
		drive(j.t, j.a, key(string(r)))
		j.count++
	}
}

// open is `/crew` and enter, which is one step of a journey however many
// letters it takes to type.
func (j *crewJourney) open() {
	j.t.Helper()
	typeLine(j.t, j.a, "/crew")
	j.count++
	if !j.a.crewUI.open {
		j.t.Fatalf("/crew opened nothing:\n%s", plain(mustFrame(j.a)))
	}
}

// ── the five journeys ───────────────────────────────────────────────────────

// CHECK: /crew, esc. The panel says the three seats, the models, the
// providers and the cap, and esc takes it down.
func TestCrewJourneyCheck(t *testing.T) {
	a, _ := crewLab(t)
	j := &crewJourney{t: t, a: a}
	j.open()
	screen := crewScreen(a)
	for _, want := range []string{crewTitleWord, "worker", "planner", "checker", "auto",
		"models", "‹ all ›", "providers", "cap", "none", crewMainKeys, "esc"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the panel does not say %q:\n%s", want, screen)
		}
	}
	// THE ONE CONNECTED PROVIDER IS A CHIP, on, with the `+` beside it.
	if row := crewLineWith(t, screen, "providers"); !strings.Contains(row, a.icon(tokens.GSettled)+" openrouter  "+crewConnectChip) {
		t.Errorf("the providers row reads %q", row)
	}
	j.keys("esc")
	if a.crewUI.open {
		t.Fatal("esc left the panel up")
	}
	if j.count != 2 {
		t.Fatalf("checking the crew took %d steps", j.count)
	}
	t.Logf("check: %d steps", j.count)
}

// PIN THE CHECKER: /crew ↓↓ enter "kim" enter — and the panel comes back with
// the pin on the row and a tick beside it.
func TestCrewJourneyPinAndUnpin(t *testing.T) {
	a, dir := crewLab(t)
	j := &crewJourney{t: t, a: a}
	j.open()
	j.keys("down", "down", "enter")
	if a.crewUI.view != crewPicking || a.crewUI.pick.seat != crewroute.Checker {
		t.Fatalf("enter on the checker opened %v", a.crewUI.view)
	}
	j.typed("kim")
	j.keys("enter")
	pin, ok := config.CrewPinAt(dir, crewroute.Checker)
	if !ok || pin.Model != "moonshotai/kimi-k3" || pin.Provider != "" {
		t.Fatalf("the checker reads %+v (%v)", pin, ok)
	}
	if j.count != 8 {
		t.Fatalf("pinning the checker took %d steps, want 8 (/crew ↓ ↓ enter k i m enter)", j.count)
	}
	t.Logf("pin checker: %d steps", j.count)
	screen := crewScreen(a)
	if a.crewUI.view != crewMain || a.crewUI.cursor != 2 {
		t.Fatalf("the panel did not come back to the checker:\n%s", screen)
	}
	row := crewLineWith(t, screen, "checker")
	if !strings.Contains(row, a.icon(tokens.GPinned)+" kimi-k3") || !strings.Contains(row, a.icon(tokens.GSettled)) {
		t.Fatalf("the checker row does not show the pin and its tick: %q", row)
	}
	if !strings.Contains(screen, crewUndoWord) {
		t.Fatalf("a change offers no undo:\n%s", screen)
	}

	// UNPIN: enter enter. The list opens on `auto`, which is the whole of why
	// unpinning needs no verb of its own.
	u := &crewJourney{t: t, a: a}
	u.keys("enter")
	if list := crewScreen(a); !strings.Contains(crewLineWith(t, list, "codeaf picks per task"), "›") {
		t.Fatalf("the list did not open on auto:\n%s", list)
	}
	u.keys("enter")
	if _, ok := config.CrewPinAt(dir, crewroute.Checker); ok {
		t.Fatal("choosing auto left the checker pinned")
	}
	if u.count != 2 {
		t.Fatalf("unpinning took %d steps", u.count)
	}
	t.Logf("unpin: %d steps", u.count)
}

// OPEN-WEIGHT ONLY: ↓↓↓ →. The models row is walked where it stands, and the
// step is written as it is taken.
func TestCrewJourneyOpenWeightOnly(t *testing.T) {
	a, dir := crewLab(t)
	j := &crewJourney{t: t, a: a}
	j.open()
	j.keys("down", "down", "down", "right")
	if got := config.CrewAllowedAt(dir).String(); got != "open" {
		t.Fatalf("the allowed rule reads %q", got)
	}
	row := crewLineWith(t, crewScreen(a), "models")
	// The catalog has three open models of four, and the row counts them.
	if !strings.Contains(row, "‹ open ›") || !strings.Contains(row, "(3)") {
		t.Fatalf("the models row reads %q", row)
	}
	if j.count != 5 {
		t.Fatalf("open-weight only took %d steps", j.count)
	}
	t.Logf("open-weight only: %d steps", j.count)
}

// SET A CAP: move to cap, type 5, enter. And the hole refuses what is not a
// dollar amount, and an emptied hole is no cap.
func TestCrewJourneyCap(t *testing.T) {
	a, dir := crewLab(t)
	j := &crewJourney{t: t, a: a}
	j.open()
	j.keys("down", "down", "down", "down", "down")
	j.typed("5")
	if row := crewLineWith(t, crewScreen(a), "cap"); !strings.Contains(row, "$[ 5 ]") {
		t.Fatalf("typing on the cap row did not open its hole: %q", row)
	}
	j.keys("enter")
	if got := config.CrewCapAt(dir); got != 5 {
		t.Fatalf("the cap reads %v", got)
	}
	if j.count != 8 {
		t.Fatalf("setting the cap took %d steps", j.count)
	}
	t.Logf("set cap: %d steps", j.count)
	if row := crewLineWith(t, crewScreen(a), "cap"); !strings.Contains(row, "per task $5 · daily $5.00") {
		t.Fatalf("the cap row reads %q", row)
	}

	// A FIGURE THAT IS NOT DOLLARS IS REFUSED and the cap stays.
	j.typed("5..5")
	j.keys("enter")
	if got := config.CrewCapAt(dir); got != 5 {
		t.Fatalf("a bad figure moved the cap to %v", got)
	}
	if screen := crewScreen(a); !strings.Contains(screen, "not a dollar amount") {
		t.Fatalf("the refusal was not said:\n%s", screen)
	}
	// AND AN EMPTIED HOLE IS NONE.
	j.keys("enter", "backspace", "backspace", "backspace", "backspace", "enter")
	if got := config.CrewCapAt(dir); got != 0 {
		t.Fatalf("an emptied hole left the cap at %v", got)
	}
	if row := crewLineWith(t, crewScreen(a), "cap"); !strings.Contains(row, "none") {
		t.Fatalf("the cap row reads %q", row)
	}
}

// SET THE PER-TASK LIMIT on the same row: enter, tab to the per-task hole,
// type, enter. An emptied per-task hole is the default, never no limit.
func TestCrewJourneyTaskCap(t *testing.T) {
	a, dir := crewLab(t)
	j := &crewJourney{t: t, a: a}
	j.open()
	j.keys("down", "down", "down", "down", "down")
	if row := crewLineWith(t, crewScreen(a), "cap"); !strings.Contains(row, "per task $5 · daily none") {
		t.Fatalf("the cap row reads %q", row)
	}
	j.keys("enter", "tab")
	if row := crewLineWith(t, crewScreen(a), "cap"); !strings.Contains(row, "per task $[ 5 ]") {
		t.Fatalf("tab did not open the per-task hole: %q", row)
	}
	j.keys("backspace")
	j.typed("12")
	j.keys("enter")
	if got := config.CrewTaskCapAt(dir); got != 12 {
		t.Fatalf("the per-task limit reads %v", got)
	}
	if got := config.CrewCapAt(dir); got != 0 {
		t.Fatalf("setting the per-task limit moved the daily cap to %v", got)
	}
	if row := crewLineWith(t, crewScreen(a), "cap"); !strings.Contains(row, "per task $12 · daily none") {
		t.Fatalf("the cap row reads %q", row)
	}
	j.keys("enter", "tab", "backspace", "backspace", "enter")
	if got := config.CrewTaskCapAt(dir); got != 5 {
		t.Fatalf("an emptied per-task hole left the limit at %v", got)
	}
}

// ── the seat list ───────────────────────────────────────────────────────────

// A PIN OUTSIDE THE ALLOWED MODELS IS REFUSED ON ITS OWN ROW, and the next
// enter lets the model in and pins it — one key to fix it, both undone
// together.
func TestCrewRefusesAPinOutsideTheRuleAndOffersTheFix(t *testing.T) {
	a, dir := crewLab(t)
	if err := config.SetCrewAllowed(dir, "open"); err != nil {
		t.Fatal(err)
	}
	j := &crewJourney{t: t, a: a}
	j.open()
	j.keys("enter")
	j.typed("opus")
	if row := crewLineWith(t, crewScreen(a), "claude-opus-5"); !strings.Contains(row, "not allowed") {
		t.Fatalf("a model the rule leaves out is not marked: %q", row)
	}
	j.keys("enter")
	if _, ok := config.CrewPinAt(dir, crewroute.Worker); ok {
		t.Fatal("a pin outside the allowed models was written")
	}
	screen := crewScreen(a)
	if !strings.Contains(screen, "claude-opus-5 is not in your allowed models (open) — "+crewAllowFixWord) {
		t.Fatalf("the refusal is not on the row:\n%s", screen)
	}
	j.keys("enter")
	pin, ok := config.CrewPinAt(dir, crewroute.Worker)
	if !ok || pin.Model != "anthropic/claude-opus-5" {
		t.Fatalf("the fix did not pin: %+v", pin)
	}
	if got := config.CrewAllowedAt(dir).String(); got != "open +anthropic/claude-opus-5" {
		t.Fatalf("the fix widened the rule to %q", got)
	}
	// AND ONE z TAKES BOTH BACK.
	j.keys("z")
	if _, ok := config.CrewPinAt(dir, crewroute.Worker); ok {
		t.Fatal("undo left the pin")
	}
	if got := config.CrewAllowedAt(dir).String(); got != "open" {
		t.Fatalf("undo left the rule at %q", got)
	}
}

// THE LIST IS AUTO FIRST, THE ROUTER'S SUGGESTION NEXT AND MARKED, and every
// row says its price and one provider. → lays a model's routes out under it,
// and a route pins that provider.
func TestCrewSeatListShapeAndRoutes(t *testing.T) {
	a, dir := crewLab(t)
	j := &crewJourney{t: t, a: a}
	j.open()
	j.keys("enter")
	lines := plainOverlay(a)
	if !strings.Contains(lines[1], crewAutoWord) {
		t.Fatalf("the list does not open on auto:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[2], a.icon(tokens.GRecommended)) || !strings.Contains(lines[2], "suggested") {
		t.Fatalf("the suggestion is not second and marked:\n%s", strings.Join(lines, "\n"))
	}
	if row := crewLineWith(t, strings.Join(lines, "\n"), "kimi-k3"); !strings.Contains(row, "$3/$15") || !strings.Contains(row, "openrouter") {
		t.Fatalf("a row does not say its price and provider: %q", row)
	}
	j.typed("kimi")
	j.keys("right")
	screen := crewScreen(a)
	if !strings.Contains(screen, crewAnyRouteWord) || !strings.Contains(crewLineWith(t, screen, "metered"), "openrouter") {
		t.Fatalf("→ did not lay out the routes:\n%s", screen)
	}
	j.keys("down", "down", "enter")
	pin, ok := config.CrewPinAt(dir, crewroute.Worker)
	if !ok || pin.String() != "moonshotai/kimi-k3@openrouter" {
		t.Fatalf("the route pinned %+v", pin)
	}
	if row := crewLineWith(t, crewScreen(a), "worker"); !strings.Contains(row, "@openrouter") {
		t.Fatalf("a pinned route is not on the seat: %q", row)
	}
}

// ESC GOES BACK EXACTLY ONE LEVEL: the routes, then the list, then the panel.
func TestCrewEscIsOneLevel(t *testing.T) {
	a, _ := crewLab(t)
	j := &crewJourney{t: t, a: a}
	j.open()
	j.keys("enter")
	j.typed("kimi")
	j.keys("right", "esc")
	if a.crewUI.view != crewPicking || a.crewUI.pick.unfold != "" {
		t.Fatal("esc on the routes did not fold them and stop")
	}
	j.keys("esc")
	if a.crewUI.view != crewMain || !a.crewUI.open {
		t.Fatal("esc on the list did not come back to the panel")
	}
	j.keys("esc")
	if a.crewUI.open {
		t.Fatal("esc on the panel did not close it")
	}
}

// ── the models row ──────────────────────────────────────────────────────────

// A PRICE IS TYPED INTO THE ROW: two holes, the in ceiling then the out one,
// written as one rule.
func TestCrewPriceIsTypedOnTheRow(t *testing.T) {
	a, dir := crewLab(t)
	j := &crewJourney{t: t, a: a}
	j.open()
	j.keys("down", "down", "down", "right", "right")
	if got := config.CrewAllowedAt(dir).String(); got != "≤1/5" {
		t.Fatalf("stepping onto price wrote %q", got)
	}
	j.keys("enter", "ctrl+u")
	j.typed("0.5")
	row := crewLineWith(t, crewScreen(a), "models")
	if !strings.Contains(row, "≤ $[ 0.5 ] in / $[ 5 ] out") {
		t.Fatalf("the price row is not two holes: %q", row)
	}
	j.keys("enter", "ctrl+u")
	j.typed("2")
	j.keys("enter")
	if got := config.CrewAllowedAt(dir).String(); got != "≤0.5/2" {
		t.Fatalf("the ceilings wrote %q", got)
	}
	// Two models of four are under half a dollar in and two out.
	if row := crewLineWith(t, crewScreen(a), "models"); !strings.Contains(row, "(2)") {
		t.Fatalf("the row does not count what the price admits: %q", row)
	}
}

// CUSTOM IS THE CHECKLIST: the models, each ticked where the rule admits it —
// and a tick writes the shortest rule that says it. A provider is not a line
// of it: providers are the providers row's.
func TestCrewChecklist(t *testing.T) {
	a, dir := crewLab(t)
	if err := config.SetCrewAllowed(dir, "open -deepseek"); err != nil {
		t.Fatal(err)
	}
	j := &crewJourney{t: t, a: a}
	j.open()
	j.keys("down", "down", "down")
	if row := crewLineWith(t, crewScreen(a), "models"); !strings.Contains(row, "‹ custom ›") || !strings.Contains(row, "open -deepseek") {
		t.Fatalf("a rule with an exception is not custom: %q", row)
	}
	j.keys("enter")
	if a.crewUI.view != crewChecking {
		t.Fatal("enter on custom did not open the checklist")
	}
	screen := crewScreen(a)
	if strings.Contains(screen, "whole provider") || strings.Contains(screen, "  openrouter") {
		t.Fatalf("the checklist still lists a provider:\n%s", screen)
	}
	if strings.Contains(crewLineWith(t, screen, "deepseek-v4-flash"), a.icon(tokens.GSettled)) {
		t.Fatalf("an excluded model is ticked:\n%s", screen)
	}
	j.typed("kimi")
	j.keys(" ")
	if got := config.CrewAllowedAt(dir).String(); got != "open -deepseek -moonshotai/kimi-k3" {
		t.Fatalf("unticking kimi wrote %q", got)
	}
	j.keys(" ")
	if got := config.CrewAllowedAt(dir).String(); got != "open -deepseek" {
		t.Fatalf("ticking it back wrote %q", got)
	}
	j.keys("esc")
	if a.crewUI.view != crewMain {
		t.Fatal("esc on the checklist did not come back to the panel")
	}
}

// ── what cannot work ────────────────────────────────────────────────────────

// NO PROVIDER IS THE ONE WARNING, with the command that fixes it.
func TestCrewWithNoProviderSaysSo(t *testing.T) {
	a, _ := crewLab(t)
	t.Setenv(config.APIKeyEnv, "")
	typeLine(t, a, "/crew")
	if screen := crewScreen(a); !strings.Contains(screen, crewNoProviderWord) {
		t.Fatalf("no provider was not said:\n%s", screen)
	}
}

// A PIN WHOSE PROVIDER WENT AWAY IS MARKED ON ITS ROW.
func TestCrewMarksAnUnavailablePin(t *testing.T) {
	a, dir := crewLab(t)
	if err := config.SetCrewPin(dir, crewroute.Checker, "moonshotai/kimi-k3@openrouter"); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.APIKeyEnv, "")
	typeLine(t, a, "/crew")
	if row := crewLineWith(t, crewScreen(a), "checker"); !strings.Contains(row, "unavailable · openrouter is not connected") {
		t.Fatalf("an unreachable pin is not marked: %q", row)
	}
}

// A NARROW WINDOW DROPS THE HINTS FIRST AND CLIPS NOTHING.
func TestCrewAtANarrowWidth(t *testing.T) {
	a, _ := crewLab(t)
	a.width = 56
	typeLine(t, a, "/crew")
	for _, line := range plainOverlay(a) {
		if w := ansi.StringWidth(line); w > a.width {
			t.Fatalf("a line runs to %d cells on a %d-cell frame: %q", w, a.width, line)
		}
		if strings.Contains(line, "usually") || strings.Contains(line, "likely") {
			t.Fatalf("a narrow frame kept the hint: %q", line)
		}
		if strings.Contains(line, "…") {
			t.Fatalf("a narrow frame clipped a row: %q", line)
		}
	}
}

// ── undo ────────────────────────────────────────────────────────────────────

// Z PUTS THE ROWS BACK, and only while the offer is on the edge.
func TestCrewUndoIsAWindow(t *testing.T) {
	a, dir := crewLab(t)
	j := &crewJourney{t: t, a: a}
	j.open()
	j.keys("down", "down", "down", "right")
	j.keys("z")
	if got := config.CrewAllowedAt(dir).String(); got != "all" {
		t.Fatalf("undo left the rule at %q", got)
	}
	j.keys("right")
	later := a.now().Add(crewUndoFor + time.Second)
	a.clock = func() time.Time { return later }
	if strings.Contains(crewScreen(a), crewUndoWord) {
		t.Fatal("the undo offer outlived its window")
	}
	j.keys("z")
	if got := config.CrewAllowedAt(dir).String(); got != "open" {
		t.Fatalf("a late z undid the change: %q", got)
	}
}

// ── the pointer, the keys, the shortcuts, settings ──────────────────────────

// A PRESS ON A ROW IS ENTER ON IT, and a press on an arrow walks the row.
func TestCrewTakesThePointer(t *testing.T) {
	a, dir := crewLab(t)
	typeLine(t, a, "/crew")
	crewClick(t, a, "checker", "checker")
	if a.crewUI.view != crewPicking || a.crewUI.pick.seat != crewroute.Checker {
		t.Fatal("a press on the checker did not open its list")
	}
	drive(t, a, key("esc"))
	crewClick(t, a, "models", "›")
	if got := config.CrewAllowedAt(dir).String(); got != "open" {
		t.Fatalf("a press on › wrote %q", got)
	}
	crewClick(t, a, "models", "‹")
	if got := config.CrewAllowedAt(dir).String(); got != "all" {
		t.Fatalf("a press on ‹ wrote %q", got)
	}
}

// crewClick presses the drawn line holding line, on the cell where at is.
func crewClick(t *testing.T, a *app, line, at string) {
	t.Helper()
	_, height := a.size()
	frameLines := strings.Split(plain(mustFrame(a)), "\n")
	for y := 0; y < height && y < len(frameLines); y++ {
		mark, ok := a.chromeAt(y)
		if !ok || mark.kind != chromeOverlay || !strings.Contains(frameLines[y], line) {
			continue
		}
		x := strings.Index(frameLines[y], at)
		if at == "›" {
			x = strings.LastIndex(frameLines[y], at)
		}
		x = ansi.StringWidth(frameLines[y][:x])
		drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		return
	}
	t.Fatalf("no panel line holds %q:\n%s", line, strings.Join(frameLines, "\n"))
}

// ? IS THE KEY LIST, the pointer's gestures included.
func TestCrewKeysView(t *testing.T) {
	a, _ := crewLab(t)
	typeLine(t, a, "/crew")
	drive(t, a, key("?"))
	screen := crewScreen(a)
	for _, want := range []string{"enter", "esc", "click", "wheel", "undo"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the key list does not say %q:\n%s", want, screen)
		}
	}
	drive(t, a, key("esc"))
	if a.crewUI.view != crewMain {
		t.Fatal("esc on the key list did not come back")
	}
}

// A SHORTCUT WRITES THE SAME ROW AND OPENS THE PANEL WITH THE TICK ON IT.
func TestCrewShortcutOpensThePanelOnItsRow(t *testing.T) {
	a, _ := crewLab(t)
	typeLine(t, a, "/crew pin checker moonshotai/kimi-k3")
	if !a.crewUI.open || a.crewUI.cursor != 2 {
		t.Fatalf("the shortcut did not open the panel on the checker (open %v, cursor %d)", a.crewUI.open, a.crewUI.cursor)
	}
	if row := crewLineWith(t, crewScreen(a), "checker"); !strings.Contains(row, a.icon(tokens.GSettled)) {
		t.Fatalf("the changed row has no tick: %q", row)
	}
}

// SETTINGS HAS ONE `seats` ROW WHERE THE THREE SEAT ROWS WERE, and it opens
// the panel; esc on the panel comes back to it.
func TestSettingsSeatsRowOpensTheCrewPanel(t *testing.T) {
	a, _ := crewLab(t)
	a.openSettings()
	for settingTabs[a.sheet.tab] != tabProviders {
		a.sheet.tabBy(1)
	}
	door := -1
	for i, item := range a.sheet.items {
		if item.crewDoor {
			door = i
		}
		if item.row.Key != "" && crewSeatKey(item.row.Key) {
			t.Fatalf("the Providers tab still draws the seat row %s", item.row.Key)
		}
	}
	if door < 0 {
		t.Fatal("the Providers tab has no seats row")
	}
	a.sheet.cursor = door
	runCmd(a.activate())
	if !a.crewUI.open || a.at(pageSettings) {
		t.Fatal("the seats row did not open the crew panel")
	}
	drive(t, a, key("esc"))
	if !a.at(pageSettings) || settingTabs[a.sheet.tab] != tabProviders || a.sheet.cursor != door {
		t.Fatalf("esc did not come back to the seats row (settings %v, tab %s, cursor %d)",
			a.at(pageSettings), settingTabs[a.sheet.tab], a.sheet.cursor)
	}
	// AND A SEARCH FOR A SEAT FINDS THE ONE DOOR.
	typeText(t, a, "checker")
	found := false
	for _, item := range a.sheet.items {
		if item.crewDoor {
			found = true
		}
	}
	if !found {
		t.Fatal("a search for the checker does not find the seats row")
	}
}

// crewLineWith is the one line of a block holding want.
func crewLineWith(t *testing.T, screen, want string) string {
	t.Helper()
	for _, line := range strings.Split(screen, "\n") {
		if strings.Contains(line, want) {
			return line
		}
	}
	t.Fatalf("no line holds %q:\n%s", want, screen)
	return ""
}

// THE CREW'S MARKS ARE DRAWN BY EVERY TERMINAL: with the rich icon tier on, the
// panel's pin, ticks and rings are still the plain tier's, because a terminal
// without the font draws a private-use codepoint as a blank cell — a pinned
// checker that reads as "checker  kimi-k3".
func TestTheCrewPanelDrawsNoPrivateUseMarks(t *testing.T) {
	a, dir := crewLab(t)
	if err := config.SetCrewPin(dir, crewroute.Checker, "moonshotai/kimi-k3"); err != nil {
		t.Fatal(err)
	}
	a.iconMode = config.IconsRich
	a.settleIcons()
	j := &crewJourney{t: t, a: a}
	j.open()
	screen := crewScreen(a)
	for _, r := range screen {
		if (r >= 0xE000 && r <= 0xF8FF) || r >= 0xF0000 {
			t.Fatalf("the panel draws %U, a private-use mark:\n%s", r, screen)
		}
	}
	if !strings.Contains(screen, tokens.Plain.Glyph(tokens.GPinned)) || !strings.Contains(screen, tokens.Plain.Glyph(tokens.GSettled)) {
		t.Errorf("the panel does not draw the plain pin and tick:\n%s", screen)
	}
}
