package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The chrome slice's own tests: the settings panel (settings.go) and the
// welcome box (welcome.go).
//
// Both write through seams the rest of the tree owns — the settings registry
// and the door's resume function — so every test here supplies its own: a
// profile directory under t.TempDir(), and a fake resume that records what it
// was asked for. Nothing in this file touches the machine it runs on.

// sheetApp is a surface with a settings panel over a profile of its own.
func sheetApp(t *testing.T) (*app, string) {
	t.Helper()
	dir := t.TempDir()
	// The registry resolves the environment BEFORE the file (internal/config),
	// and a developer with AFORGE_ATTRIBUTION exported would otherwise be
	// testing their shell. Empty reads as unset everywhere in that package.
	for _, pin := range []string{
		"AFORGE_ATTRIBUTION", "AFORGE_NERD_FONT", "AFORGE_CHAT_LINEAR",
		"AFORGE_HISTORY", "AFORGE_DRAFT_PERSIST", "AFORGE_RAIL", "AFORGE_DOC_ENGINE",
		"AFORGE_CONTEXT_FILL_PCT", "AFORGE_DAILY_BUDGET", "EXA_API_KEY", "JINA_API_KEY",
		// The capability slots resolve their environment variable before the
		// profile too, now that the profile is where their writes land.
		"AFORGE_VISION_MODEL", "AFORGE_IMAGE_MODEL", "AFORGE_SPEECH_MODEL",
		"AFORGE_MUSIC_MODEL", "AFORGE_VIDEO_MODEL", "AFORGE_VOICE_MODEL",
	} {
		t.Setenv(pin, "")
	}
	t.Setenv("AFORGE_HOME", t.TempDir())
	a := newApp(t.Context(), Options{
		Agent:      &fakeAgent{model: "openai/gpt-4.1-mini"},
		Workspace:  "/tmp/lab",
		ProfileDir: dir,
	})
	a.width, a.height = 90, 30
	a.pal = newPalette(tokens.ANSI256, false)
	a.welcome = welcome{spent: true}
	a.entries = nil
	a.touch()
	return a, dir
}

// sheetLabels is the panel's rows as a reader sees them, headings included.
func sheetLabels(a *app) []string {
	lines, _, _, _ := a.sheetFrame(a.width, a.height)
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if text := strings.TrimSpace(plain(line)); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func sheetHas(a *app, want string) bool {
	for _, line := range sheetLabels(a) {
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}

// cursorTo walks the cursor onto the row with this key, and fails when the tab
// on show does not hold it.
func cursorTo(t *testing.T, a *app, key string) {
	t.Helper()
	for i, item := range a.sheet.items {
		if !item.heading() && item.row.Key == key {
			a.sheet.cursor = i
			return
		}
	}
	t.Fatalf("row %q is not on the %s tab", key, settingTabs[a.sheet.tab])
}

// ── the settings panel ──────────────────────────────────────────────────────

// EVERY REGISTRY ROW HAS A HOME. A setting nobody placed is a setting nobody
// can reach: the panel renders one tab at a time, so a row with no [settingMeta]
// is invisible on all five. This is the completeness gate internal/config keeps
// for its own sheet, kept here for the skin over it.
func TestEverySettingRowHasATab(t *testing.T) {
	registry := config.NewSettings(config.SettingsOptions{ProfileDir: t.TempDir()})
	for _, row := range registry.Rows() {
		meta, ok := settingMetaFor(row)
		if !ok {
			t.Fatalf("registry row %q has no tab — add one line to settingUI", row.Key)
		}
		if meta.label == "" || meta.about == "" {
			t.Fatalf("row %q reaches the panel with no %s", row.Key, "label or description")
		}
		found := false
		for _, tab := range settingTabs {
			found = found || tab == meta.tab
		}
		if !found {
			t.Fatalf("row %q sits on unknown tab %q", row.Key, meta.tab)
		}
	}
}

// /settings and ctrl+, both open it, and esc closes it.
func TestTheSettingsPanelOpensOnBothDoorsAndClosesOnEsc(t *testing.T) {
	a, _ := sheetApp(t)

	drive(t, a, tea.KeyPressMsg{Code: ',', Mod: tea.ModCtrl})
	if !a.sheet.open {
		t.Fatal("ctrl+, did not open the settings panel")
	}
	drive(t, a, key("esc"))
	if a.sheet.open {
		t.Fatal("esc did not close the settings panel")
	}

	typeLine(t, a, "/settings")
	if !a.sheet.open {
		t.Fatal("/settings did not open the settings panel")
	}
	// It is fullscreen: the input line and the HUD are not under it. The
	// legend's microcopy is the tell — it is on every ordinary frame and on no
	// panel row.
	if strings.Contains(plain(frame(a)), microcopy) {
		t.Fatal("the panel is drawn over a frame that is still showing its status line")
	}
	if !sheetHas(a, "ask before running") {
		t.Fatalf("the Session tab is missing its first row:\n%s", strings.Join(sheetLabels(a), "\n"))
	}
}

// ←/→ WALK THE TABS, and each one holds its own rows.
func TestTheSettingsTabsSwitchAndCarryTheirOwnRows(t *testing.T) {
	a, _ := sheetApp(t)
	a.openSettings()

	if got := settingTabs[a.sheet.tab]; got != tabSession {
		t.Fatalf("the panel opened on %q, want %q", got, tabSession)
	}
	for _, want := range []string{"ask before running", "session ceiling"} {
		if !sheetHas(a, want) {
			t.Fatalf("the Session tab is missing %q:\n%s", want, strings.Join(sheetLabels(a), "\n"))
		}
	}
	if sheetHas(a, "compact at") {
		t.Fatal("a Context row is showing on the Session tab")
	}
	// THE CREW IS ON PROVIDERS, with the model it answers under — one tab, one
	// question (settings.go's [modelsSection] says why it moved).
	if sheetHas(a, "small work") {
		t.Fatal("a crew row is showing on the Session tab")
	}

	drive(t, a, key("right"))
	if got := settingTabs[a.sheet.tab]; got != tabContext {
		t.Fatalf("→ landed on %q, want %q", got, tabContext)
	}
	if !sheetHas(a, "compact at") || sheetHas(a, "ask before running") {
		t.Fatalf("the Context tab did not replace the Session rows:\n%s",
			strings.Join(sheetLabels(a), "\n"))
	}

	// The walk clamps at both ends rather than wrapping — the rule every list
	// on this surface follows (palette.go).
	drive(t, a, key("left"), key("left"), key("left"))
	if got := settingTabs[a.sheet.tab]; got != tabSession {
		t.Fatalf("← past the first tab landed on %q", got)
	}
	for i := 0; i < len(settingTabs)+3; i++ {
		drive(t, a, key("right"))
	}
	// The last tab is the accounts one (connectcaps.go), which is where the walk
	// stops rather than wrapping round to the first.
	if got := settingTabs[a.sheet.tab]; got != tabConnections {
		t.Fatalf("→ past the last tab landed on %q", got)
	}
}

// TYPE-TO-SEARCH FILTERS ACROSS ALL FIVE TABS, groups the hits under the tab
// each one lives on, and moves the tab bar to the first of them.
func TestTheSettingsSearchFiltersAcrossEveryTab(t *testing.T) {
	a, _ := sheetApp(t)
	a.openSettings()
	// "model" is deliberately a word that lives on two tabs: the task model and
	// the fallback chain on Session, the crew and the slots on Providers.
	for _, r := range "model" {
		drive(t, a, key(string(r)))
	}

	// The assertion is over the ITEMS and not the drawn frame: the panel shows
	// sixteen rows and a cross-tab search for a common word matches more than
	// that, so a frame check would be asserting about the scroll position rather
	// than about the filter.
	var items []string
	for _, item := range a.sheet.items {
		if item.heading() {
			items = append(items, item.head)
			continue
		}
		items = append(items, item.meta.label)
	}
	joined := strings.Join(items, "\n")
	for _, want := range []string{tabSession, tabProviders, "small work", "your model"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("the search dropped %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "keep drafts") {
		t.Fatalf("the search kept a row that does not match:\n%s", joined)
	}
	// The tab follows the first match, so backing the search out leaves the
	// person where the thing they found lives.
	if got := settingTabs[a.sheet.tab]; got != tabSession {
		t.Fatalf("the tab bar did not follow the first match: %q", got)
	}
	// And the cursor is ON that first match rather than on its heading.
	item, ok := a.sheet.current()
	if !ok || item.heading() {
		t.Fatal("the search left the cursor on a heading")
	}

	// esc backs out the search before it backs out of the panel.
	drive(t, a, key("esc"))
	if !a.sheet.open || a.sheet.searching() {
		t.Fatal("esc did not drop the search first")
	}
}

// A BOOLEAN TOGGLES IN PLACE and lands in the profile.
func TestASettingsToggleWritesTheRegistryKey(t *testing.T) {
	a, dir := sheetApp(t)
	a.openSettings()
	drive(t, a, key("right"), key("right"), key("right")) // Display
	cursorTo(t, a, config.KeyHistoryEnabled)

	if !config.HistoryEnabledAt(dir) {
		t.Fatal("input history did not start on")
	}
	drive(t, a, key("enter"))
	if config.HistoryEnabledAt(dir) {
		t.Fatalf("enter did not turn input history off: %v", a.sheet.msg)
	}
	drive(t, a, key("enter"))
	if !config.HistoryEnabledAt(dir) {
		t.Fatal("a second enter did not turn it back on")
	}
}

// AN ENUM CYCLES IN PLACE, in the registry's own order.
func TestASettingsCycleWritesTheRegistryKey(t *testing.T) {
	a, dir := sheetApp(t)
	a.openSettings()
	cursorTo(t, a, config.KeyToolApprovalMode)

	if got := config.ToolApprovalModeAt(dir); got != "prompt" {
		t.Fatalf("the gate did not start at prompt: %q", got)
	}
	drive(t, a, key("enter"))
	if got := config.ToolApprovalModeAt(dir); got != "allow" {
		t.Fatalf("the cycle wrote %q, want the next choice after prompt", got)
	}
	drive(t, a, key("enter"))
	if got := config.ToolApprovalModeAt(dir); got != "deny" {
		t.Fatalf("the second cycle wrote %q, want deny", got)
	}
}

// A TEXT ROW OPENS A SUBMENU: enter saves, esc cancels, empty clears.
func TestASettingsTextRowWritesTheRegistryKey(t *testing.T) {
	a, dir := sheetApp(t)
	a.openSettings()
	cursorTo(t, a, config.KeySpendRail)

	drive(t, a, key("enter"))
	if a.sheet.edit == nil {
		t.Fatal("enter on a text row did not open the submenu")
	}
	a.sheet.edit.box.setText("2.50")
	drive(t, a, key("enter"))
	if a.sheet.edit != nil {
		t.Fatal("enter did not close the submenu")
	}
	if got := config.SpendRailUSDAt(dir); got != 2.5 {
		t.Fatalf("the submenu wrote %v, want 2.5", got)
	}

	// esc changes nothing at all.
	drive(t, a, key("enter"))
	a.sheet.edit.box.setText("9")
	drive(t, a, key("esc"))
	if got := config.SpendRailUSDAt(dir); got != 2.5 {
		t.Fatalf("esc wrote %v anyway", got)
	}

	// A refusal is shown in the registry's own words, and nothing is written.
	drive(t, a, key("enter"))
	a.sheet.edit.box.setText("later")
	drive(t, a, key("enter"))
	if a.sheet.msg == "" {
		t.Fatal("a value the registry refused was swallowed")
	}
	if got := config.SpendRailUSDAt(dir); got != 2.5 {
		t.Fatalf("a refused value was written anyway: %v", got)
	}

	// An empty box clears the row.
	cursorTo(t, a, config.KeyToolApprovals)
	drive(t, a, key("enter"))
	a.sheet.edit.box.setText("read:allow")
	drive(t, a, key("enter"))
	if got := config.ToolApprovalsAt(dir); got != "read:allow" {
		t.Fatalf("the exceptions row holds %q", got)
	}
	drive(t, a, key("enter"))
	a.sheet.edit.box.reset()
	drive(t, a, key("enter"))
	if got := config.ToolApprovalsAt(dir); got != "" {
		t.Fatalf("an empty box left %q behind", got)
	}
}

// A ROW THAT DIFFERS FROM THE DEFAULT IS MARKED, and only then.
func TestAChangedSettingIsMarked(t *testing.T) {
	a, _ := sheetApp(t)
	a.openSettings()
	cursorTo(t, a, config.KeyToolApprovalMode)

	item, _ := a.sheet.current()
	if a.sheet.changed(item) {
		t.Fatal("an untouched row is already marked")
	}
	line := plain(strings.Join(a.sheet.rowLines(item, true, false, a.width, a.pal), "\n"))
	if strings.Contains(line, changedMark) {
		t.Fatalf("an untouched row drew the mark: %q", line)
	}

	drive(t, a, key("enter"))
	item, _ = a.sheet.current()
	if !a.sheet.changed(item) {
		t.Fatal("a row written by hand is not marked as changed")
	}
	line = plain(strings.Join(a.sheet.rowLines(item, true, false, a.width, a.pal), "\n"))
	if !strings.Contains(line, changedMark) {
		t.Fatalf("the changed row is missing its mark: %q", line)
	}
}

// A CREDENTIAL IS NEVER PRINTED IN FULL — not in the list, not in the box a
// person typed it into, and not on the frame around either.
//
// The masking itself is internal/config's (a secret row's reader hands back
// dots and a tail, so this panel never holds the key). What is asserted here is
// that the panel does not undo it: it renders what the row reads, and its edit
// box opens on the same masked value the registry's writer knows to treat as
// "unchanged".
func TestACredentialRowRendersMasked(t *testing.T) {
	a, dir := sheetApp(t)
	a.openSettings()
	drive(t, a, key("right")) // Context
	cursorTo(t, a, config.KeyExaKey)

	drive(t, a, key("enter"))
	if a.sheet.edit == nil || !a.sheet.edit.secret {
		t.Fatal("the credential row did not open a masked submenu")
	}
	a.sheet.edit.box.setText("exa-0123456789abcdef")
	drive(t, a, key("enter"))
	if got := config.ExaKeyAt(dir); got != "exa-0123456789abcdef" {
		t.Fatalf("the key was not saved whole: %q", got)
	}

	item, _ := a.sheet.current()
	line := plain(strings.Join(a.sheet.rowLines(item, true, false, a.width, a.pal), "\n"))
	if strings.Contains(line, "0123456789") {
		t.Fatalf("the credential is on screen in full: %q", line)
	}
	if !strings.Contains(line, "•") || !strings.Contains(line, "cdef") {
		t.Fatalf("the credential is not masked the way it should be: %q", line)
	}
	if strings.Contains(plain(frame(a)), "0123456789") {
		t.Fatal("the credential is somewhere else on the frame in full")
	}

	// Opening the row again and pressing enter changes NOTHING: the box holds
	// the mask, and the registry knows a mask means "as it was".
	drive(t, a, key("enter"))
	box, _ := a.sheet.editLine(a.width, a.pal)
	if strings.Contains(plain(box), "0123456789") {
		t.Fatalf("the edit box shows the credential: %q", plain(box))
	}
	drive(t, a, key("enter"))
	if got := config.ExaKeyAt(dir); got != "exa-0123456789abcdef" {
		t.Fatalf("re-opening the row overwrote the key: %q", got)
	}
}

// A MODEL ROW OPENS THE SELECT SUBMENU — the palette idiom — and the
// conversation's own row lands on the running session, which is the one model
// slot this surface can honestly answer for.
func TestASettingsSelectSubmenuSwitchesTheModel(t *testing.T) {
	a, _ := sheetApp(t)
	a.models = func() []Model {
		return []Model{
			{ID: "openai/gpt-4.1-mini", ContextLength: 128_000},
			{ID: "anthropic/claude-sonnet-4.5", ContextLength: 200_000},
		}
	}
	a.openSettings()
	for i := 0; i < 4; i++ {
		drive(t, a, key("right")) // Providers
	}
	cursorTo(t, a, config.ModelSettingKey("talk"))

	drive(t, a, key("enter"))
	if a.sheet.sel == nil {
		t.Fatal("a model row did not open the select submenu")
	}
	// The submenu opens ON the model in use, the way the picker does.
	if chosen, _ := a.sheet.sel.choice(); chosen != "openai/gpt-4.1-mini" {
		t.Fatalf("the submenu opened on %q", chosen)
	}
	drive(t, a, key("down"), key("enter"))
	if a.sheet.sel != nil {
		t.Fatal("enter did not close the submenu")
	}
	if a.model != "anthropic/claude-sonnet-4.5" {
		t.Fatalf("the session is on %q", a.model)
	}
	// The window went with it, which is what keeps compaction honest.
	if got := a.agent.(*fakeAgent).window; got != 200_000 {
		t.Fatalf("the session was told the window is %d", got)
	}

	// A slot this surface did not open answers in the registry's own words
	// rather than pretending to have written something.
	cursorTo(t, a, config.ModelSettingKey("work"))
	drive(t, a, key("enter"))
	drive(t, a, key("enter"))
	if a.sheet.msg == "" {
		t.Fatal("a slot with no seam silently swallowed the change")
	}
}

// THE PANEL IS MOUSE-NAVIGABLE: a click on a tab word switches tabs, a click on
// a row selects it, and a second click answers it.
func TestTheSettingsPanelTakesTheMouse(t *testing.T) {
	a, dir := sheetApp(t)
	a.openSettings()

	spans := tabSpans()
	_, hits, _, _ := a.sheetFrame(a.width, a.height)
	bar := -1
	for y, hit := range hits {
		if hit.kind == sheetHitTabs {
			bar = y
			break
		}
	}
	if bar < 0 {
		t.Fatal("the panel drew no tab bar")
	}
	drive(t, a, clickAt(spans[3].from, bar))
	drive(t, a, releaseAt(spans[3].from, bar))
	if got := settingTabs[a.sheet.tab]; got != tabDisplay {
		t.Fatalf("a click on the Display tab landed on %q", got)
	}

	// Find the screen row the history toggle is drawn on, then click it twice:
	// once to select, once to answer.
	cursorTo(t, a, config.KeyHistoryEnabled)
	want := a.sheet.cursor
	a.sheet.cursor = 0
	_, hits, _, _ = a.sheetFrame(a.width, a.height)
	row := -1
	for y, hit := range hits {
		if hit.kind == sheetHitRow && hit.index == want {
			row = y
			break
		}
	}
	if row < 0 {
		t.Fatal("the toggle row was not drawn")
	}
	drive(t, a, clickAt(2, row))
	drive(t, a, releaseAt(2, row))
	if a.sheet.cursor != want {
		t.Fatal("a click did not move the cursor to the row it landed on")
	}
	drive(t, a, motionAt(row))
	if a.hoveredSheetRow() != want {
		t.Fatal("the pointer over a row is not recorded as hover")
	}
	drive(t, a, clickAt(2, row))
	drive(t, a, releaseAt(2, row))
	if config.HistoryEnabledAt(dir) {
		t.Fatal("the second click did not answer the row")
	}
}

// ── the welcome box ─────────────────────────────────────────────────────────

// welcomeApp is an empty session with a wired recent list.
func welcomeApp(t *testing.T, recent []Session) (*app, *[]string) {
	t.Helper()
	t.Setenv("AFORGE_HOME", t.TempDir())
	resumed := []string{}
	a := newApp(t.Context(), Options{
		Agent:          &fakeAgent{model: "openai/gpt-4.1-mini"},
		Workspace:      "/tmp/lab",
		RecentSessions: func() []Session { return recent },
		Resume: func(file string) (Agent, error) {
			resumed = append(resumed, file)
			return &fakeAgent{model: "openai/gpt-4.1-mini"}, nil
		},
	})
	a.width, a.height = 90, 30
	a.pal = newPalette(tokens.ANSI256, false)
	a.touch()
	return a, &resumed
}

func fourSessions() []Session {
	now := time.Now()
	return []Session{
		{Title: "porting the parser", File: "/s/one.jsonl", At: now.Add(-20 * time.Minute)},
		{Title: "the welcome box", File: "/s/two.jsonl", At: now.Add(-3 * time.Hour)},
		{Title: "a quiet refactor", File: "/s/three.jsonl", At: now.Add(-50 * time.Hour)},
		{Title: "reading the registry", File: "/s/four.jsonl", At: now.Add(-9 * 24 * time.Hour)},
	}
}

// THE BOX OPENS ON AN EMPTY SESSION, carries the wordmark, the model, the place
// and four slots, and sits ABOVE the input.
func TestTheWelcomeBoxOpensOnAnEmptySession(t *testing.T) {
	a, _ := welcomeApp(t, fourSessions())
	if !a.welcome.open {
		t.Fatal("an empty session did not open the welcome box")
	}
	// Settled, so the assertion is about what it says and not about a frame.
	a.welcome.step = welcomeFrames

	screen := plain(frame(a))
	for _, want := range []string{
		"openai/gpt-4.1-mini · lab", "recent sessions",
		"porting the parser", "the welcome box", "a quiet refactor", "reading the registry",
	} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the welcome box is missing %q:\n%s", want, screen)
		}
	}
	// The wordmark is drawn in the surface's own letterforms, three rows of it.
	rows := wordmarkRows(false)
	if len(rows) != 3 {
		t.Fatalf("the wordmark is %d rows", len(rows))
	}
	for _, row := range rows {
		if !strings.Contains(screen, row) {
			t.Fatalf("the wordmark row %q is not on screen:\n%s", row, screen)
		}
	}
	// It is above the input line, and the input line is still there.
	lines := strings.Split(screen, "\n")
	box, prompt := -1, -1
	for i, line := range lines {
		if strings.Contains(line, "recent sessions") {
			box = i
		}
		if strings.Contains(line, glyphYou) {
			prompt = i
		}
	}
	if box < 0 || prompt < 0 || box > prompt {
		t.Fatalf("the box is not above the input (box %d, input %d):\n%s", box, prompt, screen)
	}
	// The relative times are coarse and readable.
	if !strings.Contains(screen, "20m") || !strings.Contains(screen, "3h") ||
		!strings.Contains(screen, "2d") {
		t.Fatalf("the recent times are missing:\n%s", screen)
	}
}

// AN EMPTY LIST STILL DRAWS FOUR SLOTS' WORTH OF BOX, and says so.
func TestTheWelcomeBoxSaysWhenThereAreNoSessions(t *testing.T) {
	a, _ := welcomeApp(t, nil)
	a.welcome.step = welcomeFrames
	full, _ := welcomeApp(t, fourSessions())
	full.welcome.step = welcomeFrames

	if !strings.Contains(plain(frame(a)), "no recent sessions") {
		t.Fatalf("an empty list did not say so:\n%s", plain(frame(a)))
	}
	if a.welcomeHeight() != full.welcomeHeight() {
		t.Fatalf("the box is %d rows empty and %d rows full — the slots are not fixed",
			a.welcomeHeight(), full.welcomeHeight())
	}
}

// A RESUMED SESSION NEVER SEES IT: the transcript already answers the question
// the box asks.
func TestTheWelcomeBoxStaysAwayFromAConversation(t *testing.T) {
	agent := &fakeAgent{model: "m", past: []session.DisplayEntry{{Role: "user", Text: "hello"}}}
	a := newApp(t.Context(), Options{Agent: agent, Workspace: "/tmp/lab", Resumed: true})
	if a.welcome.open {
		t.Fatal("a resumed session opened the welcome box over its own transcript")
	}
}

// THE FIRST SUBMIT DISMISSES IT, AND NOTHING BRINGS IT BACK.
func TestTheWelcomeBoxGoesOnTheFirstSubmit(t *testing.T) {
	a, _ := welcomeApp(t, fourSessions())
	a.welcome.step = welcomeFrames
	typeLine(t, a, "hello")

	if a.welcome.open {
		t.Fatal("the box survived the first submit")
	}
	if strings.Contains(plain(frame(a)), "recent sessions") {
		t.Fatal("the box is still on screen after a submit")
	}
	// A second empty screen does not bring it back.
	a.entries = nil
	a.touch()
	if strings.Contains(plain(frame(a)), "recent sessions") {
		t.Fatal("the box came back")
	}
}

// ANY KEY DISMISSES IT — and the key still does what it always does.
func TestTheWelcomeBoxGoesOnAnyKey(t *testing.T) {
	a, _ := welcomeApp(t, fourSessions())
	drive(t, a, key("h"))
	if a.welcome.open {
		t.Fatal("a keystroke did not dismiss the box")
	}
	if a.input.String() != "h" {
		t.Fatalf("the keystroke that dismissed the box was eaten: %q", a.input.String())
	}
}

// ↑/↓ AND ENTER RESUME THE SESSION THEY PICKED, through the door's own seam.
func TestARecentSessionResumesThroughTheSeam(t *testing.T) {
	a, resumed := welcomeApp(t, fourSessions())

	drive(t, a, key("up"))
	if a.welcome.sel != 0 || !a.welcome.open {
		t.Fatalf("↑ did not select the most recent session (sel %d, open %v)",
			a.welcome.sel, a.welcome.open)
	}
	drive(t, a, key("down"))
	if a.welcome.sel != 1 {
		t.Fatalf("↓ walked to slot %d", a.welcome.sel)
	}
	drive(t, a, key("enter"))
	if len(*resumed) != 1 || (*resumed)[0] != "/s/two.jsonl" {
		t.Fatalf("enter resumed %v, want the second session", *resumed)
	}
	if a.welcome.open {
		t.Fatal("resuming left the box up")
	}
	if !strings.Contains(plain(frame(a)), "resumed /s/two.jsonl") {
		t.Fatalf("the surface did not say which session it opened:\n%s", plain(frame(a)))
	}
}

// A CLICK ON A ROW RESUMES THAT ROW, and a click anywhere else dismisses.
func TestAClickOnARecentSessionResumesIt(t *testing.T) {
	a, resumed := welcomeApp(t, fourSessions())
	a.welcome.step = welcomeFrames

	// Asked of the frame rather than computed from it. The welcome box is lifted
	// out of the chrome block and drawn at the top (view.go's [welcomeLift]), so
	// a test that worked out the row from the chrome's own length would be
	// asserting a layout instead of the thing that matters: that the row a
	// pointer lands on is the session drawn there.
	_, height := a.size()
	row := -1
	for y := 0; y < height; y++ {
		if mark, ok := a.chromeAt(y); ok && mark.kind == chromeWelcome && a.welcomeSlotAt(mark.index) == 2 {
			row = y
			break
		}
	}
	if row < 0 {
		t.Fatal("the third recent session was not drawn as a clickable row")
	}
	drive(t, a, motionAt(row))
	if a.hoveredSlot() != 2 {
		t.Fatalf("the pointer over a recent row recorded hover %d", a.hoveredSlot())
	}
	drive(t, a, clickAt(2, row))
	drive(t, a, releaseAt(2, row))
	if len(*resumed) != 1 || (*resumed)[0] != "/s/three.jsonl" {
		t.Fatalf("the click resumed %v, want the third session", *resumed)
	}
}

// THE ANIMATION IS ONE-SHOT. It settles, and the clock it was running stops.
func TestTheWelcomeAnimationRunsOnce(t *testing.T) {
	a, _ := welcomeApp(t, fourSessions())
	if !a.welcome.animating() {
		t.Fatal("the box opened already settled")
	}
	if a.Init() == nil {
		t.Fatal("the box did not ask for the paint clock")
	}
	for i := 0; i < welcomeFrames+5; i++ {
		a.paint()
	}
	if a.welcome.animating() {
		t.Fatalf("the animation is still running after %d frames", welcomeFrames+5)
	}
	if a.welcome.step != welcomeFrames {
		t.Fatalf("the animation ran to %d frames, want %d", a.welcome.step, welcomeFrames)
	}
	if cmd := a.paint(); cmd != nil {
		t.Fatal("a settled box is still asking for frames")
	}

	// Settled, the wordmark is one colour and one colour only: the sweep has
	// left. Two frames apart, the screen is identical.
	settled := frame(a)
	a.paint()
	if frame(a) != settled {
		t.Fatal("the settled box is still moving")
	}
	// Init still asks the repository what branch the legend should say
	// (render.go), and that is the ONLY thing a settled surface asks for: no
	// frame clock, which is what an idle wakeup would be.
	for _, produced := range runCmd(a.Init()) {
		if _, clock := produced.(frameMsg); clock {
			t.Fatal("a settled box asked for the clock again")
		}
	}
}

// clickAt and motionAt are the pointer messages the program loop delivers.
func clickAt(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}

// releaseAt is clickAt's other half: the body acts on release now
// (dragselect.go), so a simulated click is a press and a release in place.
func releaseAt(x, y int) tea.MouseReleaseMsg {
	return tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft}
}

// ── the numbers: the meter, the warm share, the savings note ────────────────

// The status meter is TOKENS OVER WINDOW, and the percentage only when there is
// one worth reading.
func TestTheContextSegmentReadsTokensOverWindow(t *testing.T) {
	cases := []struct {
		name    string
		tokens  int
		window  int
		want    string
		crowded bool
	}{
		{
			// The shape the wave was specified in.
			name: "the ordinary reading", tokens: 12_400, window: 128_000,
			want: "12.4k/128k · 10%",
		},
		{
			// Under one percent the percentage is DROPPED, not rounded to 0 or
			// floored to 1. Parking at "1%" for the first twenty turns is what
			// the byte-counting meter this replaced actually did.
			name: "under one percent", tokens: 900, window: 1_000_000,
			want: "900/1M",
		},
		{
			// A round figure is round: never "128.0k".
			name: "a round figure", tokens: 128_000, window: 1_000_000,
			want: "128k/1M · 13%",
		},
		{
			// 85% of a 200k window is past 80% of its 170k compaction
			// threshold, so the segment stops being furniture.
			name: "close to compaction", tokens: 170_000, window: 200_000,
			want: "170k/200k · 85%", crowded: true,
		},
		{
			// Half a window is nowhere near the threshold.
			name: "half a window", tokens: 100_000, window: 200_000,
			want: "100k/200k · 50%",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			a := newTestApp(&fakeAgent{model: "m", weight: test.tokens})
			a.ctxWindow, a.ctxTokens = test.window, test.tokens
			got, crowded := a.contextSegment()
			if got != test.want {
				t.Fatalf("the segment reads %q, want %q", got, test.want)
			}
			if crowded != test.crowded {
				t.Fatalf("crowded = %v, want %v", crowded, test.crowded)
			}
			if line := plain(frame(a)); !strings.Contains(line, test.want) {
				t.Fatalf("the status line is missing %q:\n%s", test.want, line)
			}
		})
	}

	// A window nobody has said is no segment at all, rather than a fraction of
	// an unknown.
	a := newTestApp(&fakeAgent{model: "nobody/knows", weight: 5000})
	a.ctxWindow, a.ctxTokens = 0, 5000
	if got, _ := a.contextSegment(); got != "" {
		t.Fatalf("the segment was drawn without a window: %q", got)
	}
}

// The crowded segment is PAINTED, and it is the only thing on the line besides
// the state word that ever is.
func TestTheContextSegmentIsPaintedOnlyWhenItIsCrowded(t *testing.T) {
	calm := newTestApp(&fakeAgent{model: "m"})
	calm.ctxWindow, calm.ctxTokens = 200_000, 100_000
	crowded := newTestApp(&fakeAgent{model: "m"})
	crowded.ctxWindow, crowded.ctxTokens = 200_000, 170_000

	quiet, loud := calm.status(90), crowded.status(90)
	segment, _ := crowded.contextSegment()
	if !strings.Contains(plain(loud), segment) {
		t.Fatalf("the crowded segment is missing from the line:\n%q", plain(loud))
	}
	// The paint is the difference. Compared as raw strings, the calm line's
	// meter carries the dim escape and the crowded one's does not.
	if strings.Contains(loud, calm.pal.dim(segment)) {
		t.Fatal("a conversation about to compact is still drawn as furniture")
	}
	if calmSegment, _ := calm.contextSegment(); !strings.Contains(quiet, calm.pal.dim(calmSegment)) {
		t.Fatal("a calm meter is painted; the line's own facts are always dim")
	}
}

// The session-total warm share, and the dialect reconciliation showing through
// to the screen.
func TestTheWarmShareSegmentIsTheSessionsCachedInput(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	if got := a.warmSegment(); got != "" {
		t.Fatalf("a session with no cache accounting drew %q", got)
	}

	// OpenAI-style: the cached tokens are inside the input count.
	a.inputTokens, a.cacheRead = 10_000, 6_200
	if got := a.warmSegment(); got != "⟲ 62%" {
		t.Fatalf("the warm share reads %q, want ⟲ 62%%", got)
	}
	if line := plain(a.status(90)); !strings.Contains(line, "⟲ 62%") {
		t.Fatalf("the status line is missing the warm share:\n%s", line)
	}

	// Anthropic-style: they sit beside it. Same 62%, not 620%.
	a.inputTokens, a.cacheRead = 3_800, 6_200
	if got := a.warmSegment(); got != "⟲ 62%" {
		t.Fatalf("the disjoint dialect reads %q, want ⟲ 62%%", got)
	}
}

// THE SAVINGS NOTE, and the arithmetic under it: a cache read is CHEAPER, never
// free, so the saving is the gap between the two prices and not the whole
// prompt price.
func TestTheSavingsNoteIsPricedFromTheModelsOwnRow(t *testing.T) {
	agent := &fakeAgent{model: "vendor/priced"}
	a := newTestApp(agent)
	a.models = func() []Model {
		return []Model{{
			ID: "vendor/priced", ContextLength: 128_000,
			// $10/M prompt, $1/M cache read: a nine-dollar-per-million gap.
			PromptPrice: 0.00001, CacheReadPrice: 0.000001,
		}}
	}
	a.model = "vendor/priced"

	// 9,800 cached tokens × $0.000009 = $0.0882.
	a.cacheNote(session.Usage{Input: 12_000, CacheRead: 9_800})
	line := lastNote(t, a)
	if !strings.Contains(line, "⟲ 9.8k cached") {
		t.Fatalf("the note is missing the cached tokens: %q", line)
	}
	if !strings.Contains(line, "saved $0.0882") {
		t.Fatalf("the note says %q, want a saving of .0882 — cached tokens times "+
			"the gap between the prompt price and the cache-read price", line)
	}

	// A turn that read nothing warm says NOTHING, rather than reporting a zero
	// every turn of a session on a provider that does not cache.
	before := len(a.entries)
	a.cacheNote(session.Usage{Input: 12_000})
	if len(a.entries) != before {
		t.Fatalf("a turn with no cache reads still wrote a note: %q", lastNote(t, a))
	}
}

// Without a published price pair the line degrades to the token count. "9.8k
// cached" is a true thing this surface knows; "saved $0.0000" is not.
func TestTheSavingsNoteDegradesToTheCountWhenNobodyPublishedAPrice(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "vendor/unpriced"})
	a.models = func() []Model { return []Model{{ID: "vendor/unpriced", ContextLength: 128_000}} }
	a.model = "vendor/unpriced"

	a.cacheNote(session.Usage{Input: 12_000, CacheRead: 9_800})
	line := lastNote(t, a)
	if !strings.Contains(line, "⟲ 9.8k cached") {
		t.Fatalf("the note is missing the cached tokens: %q", line)
	}
	if strings.Contains(line, "saved") {
		t.Fatalf("the note priced a model nobody published a price for: %q", line)
	}
}

// A PROMPT PRICE ON ITS OWN IS NOT A PRICE PAIR, and this is the half of the
// guard that was missing. Around two rows in five publish a prompt price and no
// cache-read price at all (internal/catalog: zero is "the provider did not
// say", and a cache read is never free) — and reading that absence as a zero
// books the WHOLE prompt price as a saving, which is this surface claiming the
// cache made those tokens free. The count is what it actually knows.
func TestTheSavingsNoteSaysNoMoneyWhenOnlyThePromptPriceIsPublished(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "vendor/half-priced"})
	a.models = func() []Model {
		return []Model{{ID: "vendor/half-priced", ContextLength: 128_000, PromptPrice: 0.00001}}
	}
	a.model = "vendor/half-priced"

	a.cacheNote(session.Usage{Input: 12_000, CacheRead: 9_800})
	line := lastNote(t, a)
	if !strings.Contains(line, "⟲ 9.8k cached") {
		t.Fatalf("the note is missing the cached tokens: %q", line)
	}
	if strings.Contains(line, "saved") {
		t.Fatalf("the note booked the whole prompt price as a cache saving: %q", line)
	}
	// And nothing reached the running total behind the status line either, so
	// the session's warm share keeps the segment it had: the rate, and no cash.
	if a.cacheSaved != 0 {
		t.Fatalf("the session banked %.4f from a model with no cache-read price", a.cacheSaved)
	}
	a.inputTokens, a.cacheRead = 12_000, 9_800
	if got := a.warmSegment(); got != "⟲ 81%" {
		t.Fatalf("the warm share reads %q, want the rate alone", got)
	}
}

// The two formatters the whole meter is written in.
func TestTokenAndSavedWords(t *testing.T) {
	for _, test := range []struct {
		tokens int
		want   string
	}{
		{0, "0"},
		{842, "842"},
		{1000, "1k"},
		{12_400, "12.4k"},
		{128_000, "128k"},
		// One decimal rounds 999,999 to "1000.0k", which is a figure with the
		// wrong unit on it.
		{999_999, "1M"},
		{1_200_000, "1.2M"},
	} {
		if got := tokenWord(test.tokens); got != test.want {
			t.Fatalf("tokenWord(%d) = %q, want %q", test.tokens, got, test.want)
		}
	}
	for _, test := range []struct {
		usd  float64
		want string
	}{
		{0.0041, "$0.0041"},
		{0.0882, "$0.0882"},
		{1.5, "$1.50"},
	} {
		if got := savedWord(test.usd); got != test.want {
			t.Fatalf("savedWord(%v) = %q, want %q", test.usd, got, test.want)
		}
	}
}

// lastNote is the text of the last surface-side line the app wrote.
func lastNote(t *testing.T, a *app) string {
	t.Helper()
	for i := len(a.entries) - 1; i >= 0; i-- {
		if a.entries[i].kind == entryNote {
			return a.entries[i].text
		}
	}
	t.Fatal("the surface wrote no note")
	return ""
}

// ── the three fullscreen pages ──────────────────────────────────────────────

// threePageApp is a surface where all three fullscreen pages can actually open:
// a profile for the settings panel, a task in the record for the task page, and
// a machine with a second conversation on it for home.
func threePageApp(t *testing.T) *app {
	t.Helper()
	a, _ := sheetApp(t)
	lab := newHomeLab(t)
	here := lab.session("alpha", "one", "This conversation", "/tmp/alpha", time.Now())
	lab.session("beta", "two", "Somewhere else", "/tmp/beta", time.Now().Add(-time.Hour))
	a.homeRoot = lab.root
	a.file = here
	a.comp.tasks = []session.TaskIndexEntry{
		pastTask("4", "port-the-parser", "Port the parser", 2*time.Hour),
	}
	return a
}

// ONLY ONE PAGE EVER OWNS THE FRAME. The settings panel, the task page and home
// each take the frame WHOLE, and view.go can draw exactly one of them — so
// opening any one has to close the other two ([app.standDownFullscreen]).
// Without this the second page opened would take the keyboard from behind the
// first, and esc would give the frame back to a screen nobody could see.
func TestOpeningOneFullscreenPageClosesTheOtherTwo(t *testing.T) {
	// Every ordered pair of the three, so no open path is trusted on the say-so
	// of another one.
	open := map[string]func(*app){
		"the settings panel": func(a *app) { a.openSettings() },
		"the task page":      func(a *app) { a.openTaskSheet() },
		"home":               func(a *app) { a.openHome() },
	}
	up := map[string]func(*app) bool{
		"the settings panel": func(a *app) bool { return a.sheet.open },
		"the task page":      func(a *app) bool { return a.taskSheet.open },
		"home":               func(a *app) bool { return a.home.open },
	}
	for first := range open {
		for second := range open {
			if first == second {
				continue
			}
			t.Run(first+" then "+second, func(t *testing.T) {
				a := threePageApp(t)
				open[first](a)
				if !up[first](a) {
					t.Fatalf("%s did not open at all", first)
				}
				open[second](a)
				if !up[second](a) {
					t.Fatalf("%s did not open over %s", second, first)
				}
				if up[first](a) {
					t.Fatalf("%s is still up under %s", first, second)
				}
				for name, showing := range up {
					if name != second && showing(a) {
						t.Fatalf("%s is up beside %s", name, second)
					}
				}
			})
		}
	}
}

// AND THE FRAME AGREES WITH THE FLAGS. The exclusivity is only worth anything if
// the screen a person is looking at is the page they just opened, so this asks
// the frame itself rather than the fields behind it.
func TestTheFrameDrawsThePageThatWasOpenedLast(t *testing.T) {
	a := threePageApp(t)

	a.openSettings()
	if frame, _, _ := a.frame(); !strings.Contains(plain(frame), tabSession) {
		t.Fatalf("the settings panel is not what the frame draws:\n%s", frame)
	}
	// Home over the panel: the frame must change hands, not merely add a flag.
	a.openHome()
	home, _, _ := a.frame()
	if strings.Contains(plain(home), tabProviders) {
		t.Fatalf("the settings panel is still being drawn under home:\n%s", home)
	}
	if !strings.Contains(plain(home), "Somewhere Else") {
		t.Fatalf("home is not what the frame draws:\n%s", home)
	}
	// And the task page over home.
	if !a.openTaskSheet() {
		t.Fatal("the task page refused to open over home")
	}
	page, _, _ := a.frame()
	if strings.Contains(plain(page), "Somewhere Else") {
		t.Fatalf("home is still being drawn under the task page:\n%s", page)
	}
	if !strings.Contains(plain(page), "Port the parser") {
		t.Fatalf("the task page is not what the frame draws:\n%s", page)
	}
}
