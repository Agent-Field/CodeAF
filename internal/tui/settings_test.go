package tui

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// settingsCommander is the fake commander plus the one capability the sheet
// needs: a registry pointed at a throwaway profile directory, so a test can
// persist a real value without touching the developer's own configuration.
type settingsCommander struct {
	*fakeCommander
	registry *config.Settings
	split    int
}

func (c *settingsCommander) Settings() *config.Settings { return c.registry }
func (c *settingsCommander) SplitPct() int              { return c.split }
func (c *settingsCommander) SaveSplitPct(pct int)       { c.split = pct }

func newSettingsModel(t *testing.T) (*Model, *settingsCommander, string) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{
		"AFORGE_DAILY_BUDGET", "AFORGE_PRACTICE_BUDGET", "AFORGE_PRACTICE_IDLE",
		"AFORGE_BRIEF_AFTER", "AFORGE_TENURE_AFTER", "AFORGE_DOC_ENGINE", "AFORGE_VISION_MODEL",
	} {
		t.Setenv(name, "")
	}
	commander := &settingsCommander{fakeCommander: newFakeCommander()}
	commander.registry = config.NewSettings(config.SettingsOptions{
		ProfileDir:   dir,
		ModelValue:   commander.CurrentModel,
		SetModel:     commander.SetModel,
		SplitPct:     commander.SplitPct,
		SaveSplitPct: commander.SaveSplitPct,
	})
	model := NewWithCommander(&fakeBackend{}, "settings", commander)
	_, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	return model, commander, dir
}

func settingsRowIndex(t *testing.T, model *Model, key string) int {
	t.Helper()
	for index, row := range model.settingsRows() {
		if row.Key == key {
			return index
		}
	}
	t.Fatalf("no settings row keyed %q", key)
	return 0
}

func TestSlashSettingsAndHeaderGearOpenTheSameSheet(t *testing.T) {
	model, _, _ := newSettingsModel(t)
	_ = model.executeSlash("/settings")
	if model.palette != paletteSettings {
		t.Fatalf("/settings opened palette %v", model.palette)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.palette != paletteNone || !model.inputFocused {
		t.Fatalf("esc left palette=%v inputFocused=%v", model.palette, model.inputFocused)
	}

	_ = model.View()
	if model.headerSettingsBounds.width == 0 {
		t.Fatal("the header gear has no click bounds")
	}
	_, _ = model.Update(tea.MouseMsg{
		X: model.headerSettingsBounds.x, Y: model.headerSettingsBounds.y,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if model.palette != paletteSettings {
		t.Fatalf("the header gear opened palette %v", model.palette)
	}
	_ = model.View()
	_, _ = model.Update(tea.MouseMsg{
		X: model.headerSettingsBounds.x, Y: model.headerSettingsBounds.y,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if model.palette != paletteNone {
		t.Fatal("clicking the gear again did not close the sheet")
	}

	// The comma is a command outside the input and a character inside it.
	model.focus = focusChat
	model.inputFocused = false
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{','}})
	if model.palette != paletteSettings {
		t.Fatalf("the bare comma opened palette %v", model.palette)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model.focus = focusInput
	model.inputFocused = true
	_ = model.input.Focus()
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi,")})
	if model.palette == paletteSettings || model.input.Value() != "hi," {
		t.Fatalf("a typed comma opened the sheet: draft=%q", model.input.Value())
	}
}

// The sheet has to be a page a person can read: every category present, the
// focused row's hint and only the focused row's hint, and no line wider than
// the frame at any width.
func TestSettingsSheetIsOneCalmColumnAtEveryWidth(t *testing.T) {
	for _, width := range []int{40, 60, 100, 140} {
		model, _, _ := newSettingsModel(t)
		_, _ = model.Update(tea.WindowSizeMsg{Width: width, Height: 60})
		_ = model.openSettings()
		view := model.View()
		plain := ansi.Strip(view)
		for _, category := range config.SettingCategories {
			if !strings.Contains(plain, category) {
				t.Fatalf("width %d is missing the %q group:\n%s", width, category, plain)
			}
		}
		if !strings.Contains(plain, "environment") {
			t.Fatalf("width %d dropped the environment footer:\n%s", width, plain)
		}
		for _, line := range strings.Split(view, "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("width %d produced a %d-cell line: %q", width, got, ansi.Strip(line))
			}
		}
	}

	model, _, _ := newSettingsModel(t)
	_ = model.openSettings()
	model.settingsIndex = settingsRowIndex(t, model, config.KeyDailyBudget)
	plain := ansi.Strip(model.View())
	budget, _ := model.settingsRegistry.Row(config.KeyDailyBudget)
	practice, _ := model.settingsRegistry.Row(config.KeyPracticeBudget)
	if !strings.Contains(plain, strings.Fields(budget.Hint)[0]) {
		t.Fatalf("the focused row does not explain itself:\n%s", plain)
	}
	if strings.Contains(plain, practice.Hint) {
		t.Fatalf("every hint rendered at once — this is a wall, not a sheet:\n%s", plain)
	}
}

func TestSettingsNavigatesAndEditsEveryKindAndPersists(t *testing.T) {
	model, commander, dir := newSettingsModel(t)
	_ = model.openSettings()

	// A dollar row: enter opens the inline editor, nonsense is refused in
	// plain language, and the accepted value persists.
	model.settingsIndex = settingsRowIndex(t, model, config.KeyDailyBudget)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.settingsEditing {
		t.Fatal("enter did not open the inline editor")
	}
	model.settingsEditor.SetValue("twenty")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.settingsEditing || model.settingsError == "" {
		t.Fatalf("nonsense was accepted: editing=%v error=%q", model.settingsEditing, model.settingsError)
	}
	if plain := ansi.Strip(model.View()); !strings.Contains(plain, model.settingsError) {
		t.Fatalf("the refusal is not shown under the row:\n%s", plain)
	}
	model.settingsEditor.SetValue("31.5")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.settingsEditing || model.settingsError != "" {
		t.Fatalf("a good value did not land: editing=%v error=%q", model.settingsEditing, model.settingsError)
	}
	if got, err := config.DailyBudgetUSDAt(dir); err != nil || got != 31.5 {
		t.Fatalf("persisted daily budget = %v err=%v", got, err)
	}
	if !strings.Contains(ansi.Strip(model.View()), "$31.5") {
		t.Fatal("the row is the receipt, and it does not show the new value")
	}
	if len(model.messages) != 0 {
		t.Fatal("a settings change spoke in the thread")
	}

	// A bool row toggles on enter and reads back after a reopen.
	model.settingsIndex = settingsRowIndex(t, model, config.KeyProposeSkills)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if config.ProposeSkillsAt(dir) {
		t.Fatal("enter did not toggle the bool row off")
	}
	model.closeSettings()
	_ = model.openSettings()
	row, _ := model.settingsRegistry.Row(config.KeyProposeSkills)
	if row.Value() != "off" {
		t.Fatalf("the toggle did not survive a reopen: %q", row.Value())
	}

	// A percent row: the learning dial persists what was typed.
	model.settingsIndex = settingsRowIndex(t, model, config.KeyDemandShare)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model.settingsEditor.SetValue("35")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := config.PracticeDemandPctAt(dir); got != 35 {
		t.Fatalf("the learning dial persisted %d%%", got)
	}

	// A choice row cycles through its rungs.
	model.settingsIndex = settingsRowIndex(t, model, config.KeyDocumentEngine)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	engine, _ := config.DocumentEngineAt(dir)
	if engine != "local" {
		t.Fatalf("the document engine cycled to %q, want local", engine)
	}

	// The divider is the row the surface itself owns: it lands live.
	model.settingsIndex = settingsRowIndex(t, model, config.KeySplitPct)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model.settingsEditor.SetValue("55")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if commander.split != 55 || model.splitPct != 55 {
		t.Fatalf("the divider saved=%d live=%d", commander.split, model.splitPct)
	}

	// Arrow and vim keys walk the same rows.
	model.settingsIndex = 0
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if model.settingsIndex != 2 {
		t.Fatalf("down then j selected row %d", model.settingsIndex)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if model.settingsIndex != 1 {
		t.Fatalf("k selected row %d", model.settingsIndex)
	}
}

// The recent fix, held: while an inline editor is open j and k are letters.
func TestSettingsEditorTypesVimKeysRatherThanNavigating(t *testing.T) {
	model, _, _ := newSettingsModel(t)
	_ = model.openSettings()
	model.settingsIndex = settingsRowIndex(t, model, config.KeyVisionModel)
	before := model.settingsIndex
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("jk/kimi")})
	if model.settingsIndex != before {
		t.Fatalf("typing moved the selection from %d to %d", before, model.settingsIndex)
	}
	if got := model.settingsEditor.Value(); got != "jk/kimi" {
		t.Fatalf("the editor received %q", got)
	}
	// The sheet is modal above the chords too: a paste is not a microphone and
	// a place chord does not walk out from under an open editor.
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}, Alt: true})
	if model.voiceState != voiceIdle || model.palette != paletteSettings || model.activePlace() != placeThread {
		t.Fatalf("a chord escaped the open sheet: voice=%v palette=%v", model.voiceState, model.palette)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	row, _ := model.settingsRegistry.Row(config.KeyVisionModel)
	if row.Value() != "jk/kimi" {
		t.Fatalf("the typed value did not persist: %q", row.Value())
	}
}

func TestSettingsEscapesOneRungAtATime(t *testing.T) {
	model, _, _ := newSettingsModel(t)
	model.focus = focusChat
	model.inputFocused = false
	_ = model.openSettings()
	model.settingsIndex = settingsRowIndex(t, model, config.KeyBriefAfter)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.settingsEditing {
		t.Fatal("the editor did not open")
	}
	_, quit := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if quit != nil || model.settingsEditing || model.palette != paletteSettings {
		t.Fatalf("esc closed more than the editor: palette=%v editing=%v", model.palette, model.settingsEditing)
	}
	_, quit = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if quit != nil || model.palette != paletteNone {
		t.Fatal("esc did not close the sheet")
	}
	if model.focus != focusChat || model.inputFocused {
		t.Fatalf("the sheet did not hand focus back: focus=%v input=%v", model.focus, model.inputFocused)
	}
	row, _ := model.settingsRegistry.Row(config.KeyBriefAfter)
	if row.Value() != "4h" {
		t.Fatalf("an abandoned edit still wrote: %q", row.Value())
	}
}

// A model row is a door onto the picker that already exists, and the picker
// knows to come back to the sheet it was opened from.
func TestSettingsModelRowOpensTheSharedPickerAndReturns(t *testing.T) {
	model, commander, _ := newSettingsModel(t)
	_ = model.openSettings()
	model.settingsIndex = settingsRowIndex(t, model, config.ModelSettingKey("work"))
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.palette != paletteModel || model.modelRole != "work" {
		t.Fatalf("the work row opened palette=%v role=%q", model.palette, model.modelRole)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.palette != paletteSettings {
		t.Fatalf("esc from the picker landed on %v, want the sheet", model.palette)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model.paletteSelected = 1
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if commander.setRole != "work" || commander.setModel != "beta/model-two" {
		t.Fatalf("the picker set %s → %s", commander.setRole, commander.setModel)
	}
	if model.palette != paletteSettings {
		t.Fatalf("choosing a model left the sheet: palette=%v", model.palette)
	}
	if !strings.Contains(ansi.Strip(model.View()), "model-two") {
		t.Fatal("the model row does not show its new value")
	}

	// The models door still returns to the models palette, not the sheet.
	model.closeSettings()
	_ = model.openModelsPalette()
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.palette != paletteModels {
		t.Fatalf("the models door's picker returned to %v", model.palette)
	}
}

func TestSettingsRefusesToFightTheEnvironment(t *testing.T) {
	t.Setenv("AFORGE_DAILY_BUDGET", "12")
	model, _, dir := newSettingsModel(t)
	t.Setenv("AFORGE_DAILY_BUDGET", "12")
	_ = model.openSettings()
	model.settingsIndex = settingsRowIndex(t, model, config.KeyDailyBudget)
	plain := ansi.Strip(model.View())
	if !strings.Contains(plain, "pinned by AFORGE_DAILY_BUDGET") {
		t.Fatalf("the pinned row does not name its pin:\n%s", plain)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.settingsEditing {
		t.Fatal("a pinned row opened an editor")
	}
	if !strings.Contains(model.settingsError, "AFORGE_DAILY_BUDGET") {
		t.Fatalf("the refusal does not explain itself: %q", model.settingsError)
	}
	if plain := ansi.Strip(model.View()); !strings.Contains(plain, model.settingsError) {
		t.Fatalf("the refusal is not shown under the row:\n%s", plain)
	}
	if _, err := config.DailyBudgetUSDAt(dir); err != nil {
		t.Fatal(err)
	}
	if plain := ansi.Strip(model.View()); !strings.Contains(plain, "$12") {
		t.Fatalf("the pinned row does not read the environment's value:\n%s", plain)
	}
}

func TestSettingsScrollsAndClicksRowsLikeTheKeyboard(t *testing.T) {
	model, _, _ := newSettingsModel(t)
	_, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 16})
	_ = model.openSettings()
	_ = model.View()
	if model.settingsMaxOffset() == 0 {
		t.Fatal("a short frame unexpectedly fit the whole sheet")
	}
	_, _ = model.Update(tea.MouseMsg{
		X: model.settingsBounds.x + 2, Y: model.settingsBounds.y + 2,
		Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
	})
	if model.settingsOffset != 3 {
		t.Fatalf("the wheel scrolled to %d", model.settingsOffset)
	}

	// Walking down with the keyboard has to bring the selection back into view.
	model.settingsIndex = 0
	_ = model.View()
	if model.settingsOffset != 0 {
		t.Fatalf("the selection did not pull the sheet back: offset=%d", model.settingsOffset)
	}
	hit := model.settingsRowHits[len(model.settingsRowHits)-1]
	_, _ = model.Update(tea.MouseMsg{
		X: hit.bounds.x + 3, Y: hit.bounds.y,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if model.settingsIndex != hit.index {
		t.Fatalf("the click selected row %d, want %d", model.settingsIndex, hit.index)
	}

	// A click outside the sheet closes it, exactly as help does. The sheet is
	// re-opened first because the row just activated may itself have opened
	// something — which row that is depends on how many the frame can hold.
	_ = model.openSettings()
	_ = model.View()
	_, _ = model.Update(tea.MouseMsg{X: 0, Y: model.height - 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if model.palette != paletteNone {
		t.Fatal("a click outside did not close the sheet")
	}
}

// Without a commander there is no profile directory to write into, so the door
// says so instead of guessing one.
func TestSettingsWithoutACommanderRefusesQuietly(t *testing.T) {
	model := New(&fakeBackend{}, "no-commander")
	_ = model.executeSlash("/settings")
	if model.palette == paletteSettings {
		t.Fatal("the sheet opened without a registry")
	}
	if !strings.Contains(model.status, "settings aren't available in this window") {
		t.Fatalf("status = %q", model.status)
	}
}
