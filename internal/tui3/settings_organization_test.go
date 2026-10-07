package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
)

func TestSettingsOrganizationCategoriesAndAdvancedSearch(t *testing.T) {
	a, _ := sheetApp(t)
	a.openSettings()
	if settingTabs[a.sheet.tab] != tabGeneral {
		t.Fatal("settings must open on General")
	}
	want := "General,Models,Memory,Tasks,AI teams,Permissions,Spending,Connections,Privacy"
	if strings.Join(settingTabs, ",") != want {
		t.Fatalf("categories: %v", settingTabs)
	}
	for tab := range settingTabs {
		a.sheet.tab = tab
		a.sheet.build()
		for _, item := range a.sheet.items {
			if item.row.Key != "" && (item.row.ChatPresentation().Advanced || item.row.ChatPresentation().Hidden) {
				t.Fatalf("collapsed category shows %s", item.row.Key)
			}
		}
	}
	a.sheet.query.setText(config.KeyPromptProfile)
	a.sheet.build()
	found := false
	for _, item := range a.sheet.items {
		found = found || item.row.Key == config.KeyPromptProfile
	}
	if !found {
		t.Fatal("global search must find a collapsed advanced setting by its existing key")
	}
}

func TestSettingsOrganizationSidebarHitTargets(t *testing.T) {
	a, _ := sheetApp(t)
	a.width = 120
	a.height = 35
	a.openSettings()
	_, hits, _, _ := a.sheetFrame(a.width, a.height)
	target := -1
	for y, hit := range hits {
		if hit.category == 3 {
			target = y
			break
		}
	}
	if target < 0 {
		t.Fatal("Memory category has no pointer target")
	}
	a.sheetPress(3, target)
	if settingTabs[a.sheet.tab] != tabMemory {
		t.Fatal("sidebar click did not select Memory")
	}
	cursor := a.sheet.cursor
	a.sheetHoverAt(3, target)
	if a.sheet.cursor != cursor || a.hoveredSheetRow() >= 0 {
		t.Fatal("category hover changed or previewed a setting")
	}
	a.width = 80
	_, hits, _, _ = a.sheetFrame(a.width, a.height)
	for _, hit := range hits {
		if hit.category > 0 {
			t.Fatal("compact frame retains sidebar targets")
		}
	}
	if !strings.Contains(plain(frame(a)), tabMemory) {
		t.Fatal("compact bar lost selected category")
	}
}

func TestSettingsOrganizationHintsPreserveExistingValue(t *testing.T) {
	a, dir := sheetApp(t)
	a.openSettings()
	a.sheet.query.setText(config.KeyHints)
	a.sheet.build()
	item, ok := a.sheet.current()
	if !ok || item.row.Key != config.KeyHints {
		t.Fatal("hints search failed")
	}
	before := item.row.Value()
	a.activate()
	rows := config.NewSettings(config.SettingsOptions{ProfileDir: dir}).Rows()
	for _, row := range rows {
		if row.Key == config.KeyHints {
			if row.Value() == before {
				t.Fatal("toggle did not persist")
			}
			item.row = row
			shown := plain(strings.Join(a.sheet.rowLines(item, true, false, 80, a.pal), "\n"))
			positive := "on"
			if row.Value() == "on" {
				positive = "off"
			}
			if !strings.Contains(shown, positive) {
				t.Fatalf("positive hint value absent: %s", shown)
			}
			return
		}
	}
	t.Fatal("persisted hint row missing")
}

func TestSettingsOrganizationConnectionSearchFromEveryCategory(t *testing.T) {
	a, _ := capsApp(t, twoAccounts)
	for tab := range settingTabs {
		a.sheet.tab = tab
		a.sheet.query.setText("Slack")
		a.sheet.build()
		found := false
		for _, item := range a.sheet.items {
			found = found || (item.conn != nil && item.conn.service == "slack")
		}
		if !found {
			t.Fatalf("Slack cannot be found from %s", settingTabs[tab])
		}
	}
}

func TestSettingsOrganizationCaretAndMemoryDoor(t *testing.T) {
	a, _ := sheetApp(t)
	a.width = 120
	a.height = 35
	a.openSettings()
	a.sheet.query.setText("mouse")
	a.sheet.build()
	_, _, x, y := a.sheetFrame(a.width, a.height)
	if x < settingsSidebarWidth || y < 0 || !a.caret {
		t.Fatalf("sidebar search caret misplaced: %d,%d visible=%v", x, y, a.caret)
	}
	a.sheet.query.reset()
	for i, title := range settingTabs {
		if title == tabMemory {
			a.sheet.tab = i
		}
	}
	a.sheet.build()
	if len(a.sheet.items) == 0 || !a.sheet.items[0].memoryDoor {
		t.Fatal("Memory has no inspect door")
	}
	a.sheet.cursor = 0
	a.activate()
	if !a.at(pageMemory) {
		t.Fatal("inspect door did not open saved memories")
	}
}

func TestSettingsOrganizationRenderedDetailsAndPinnedScope(t *testing.T) {
	a, _ := sheetApp(t)
	a.width, a.height = 80, 30
	t.Setenv("CODEAF_DAILY_BUDGET", "22")
	a.openSettings()
	cursorTo(t, a, config.KeyMemoryEnabled)
	visible := strings.Join(strings.Fields(plain(frame(a))), " ")
	for _, want := range []string{"Restart the CLI to apply this to already-open chats", "Turning this off does not delete saved memories."} {
		if !strings.Contains(visible, want) {
			t.Fatalf("critical memory detail absent from rendered 80-column frame: %s\n%s", want, visible)
		}
	}
	cursorTo(t, a, config.KeyDailyBudget)
	visible = strings.Join(strings.Fields(plain(frame(a))), " ")
	if !strings.Contains(visible, "held by CODEAF_DAILY_BUDGET") || !strings.Contains(visible, "unset it to change this here") {
		t.Fatalf("pinned scope remedy absent: %s", visible)
	}
}

func TestSettingsOrganizationHoverKeepsRenderedTargetsStable(t *testing.T) {
	a, _ := sheetApp(t)
	a.width, a.height = 80, 30
	a.openSettings()
	cursorTo(t, a, config.KeyHints)
	_, before, _, _ := a.sheetFrame(a.width, a.height)
	hovered := -1
	for y, hit := range before {
		if hit.kind == sheetHitRow && hit.index != a.sheet.cursor {
			hovered = y
			break
		}
	}
	if hovered < 0 {
		t.Fatal("no second visible row")
	}
	a.sheetHoverAt(3, hovered)
	_, after, _, _ := a.sheetFrame(a.width, a.height)
	for y, hit := range before {
		if hit.kind == sheetHitRow && (after[y].kind != hit.kind || after[y].index != hit.index) {
			t.Fatalf("hover moved target at row %d", y)
		}
	}
}

func TestSettingsOrganizationEditorsOwnPointer(t *testing.T) {
	a, _ := sheetApp(t)
	a.openSettings()
	cursorTo(t, a, config.KeyDailyBudget)
	a.activate()
	if a.sheet.edit == nil {
		t.Fatal("budget did not open editor")
	}
	tab := a.sheet.tab
	_, hits, _, _ := a.sheetFrame(a.width, a.height)
	for y, hit := range hits {
		if hit.kind == sheetHitTabs || hit.kind == sheetHitRow {
			a.sheetPress(2, y)
		}
	}
	if a.sheet.tab != tab || a.sheet.edit == nil {
		t.Fatal("underlying click escaped text editor")
	}
	a.sheet.edit = nil
	cursorTo(t, a, config.ModelSettingKey(talkSlot))
	a.activate()
	if a.sheet.sel == nil {
		t.Fatal("chat model did not open picker")
	}
	tab = a.sheet.tab
	_, hits, _, _ = a.sheetFrame(a.width, a.height)
	for y, hit := range hits {
		if hit.kind == sheetHitTabs {
			a.sheetPress(2, y)
		}
	}
	if a.sheet.tab != tab || a.sheet.sel == nil {
		t.Fatal("underlying category click escaped model picker")
	}
}

func TestSettingsOrganizationInvalidEditKeepsDraft(t *testing.T) {
	a, _ := sheetApp(t)
	a.openSettings()
	cursorTo(t, a, config.KeyTaskParallel)
	row, _ := a.sheet.registry.Row(config.KeyTaskParallel)
	before := row.Value()
	a.activate()
	if a.sheet.edit == nil {
		t.Fatal("integer setting did not open editor")
	}
	a.sheet.edit.box.setText("not-a-number")
	a.sheetEditKey(key("enter"))
	if a.sheet.edit == nil || a.sheet.edit.box.String() != "not-a-number" {
		t.Fatal("validation discarded the editable draft")
	}
	if row.Value() != before {
		t.Fatal("invalid input changed persisted setting")
	}
	if a.sheet.msg == "" || !strings.Contains(plain(frame(a)), a.sheet.msg) {
		t.Fatalf("validation error not visible while editor is open: %q", a.sheet.msg)
	}
	a.sheet.edit.box.setText("3")
	a.sheetEditKey(key("enter"))
	if a.sheet.edit != nil || row.Value() != "3" {
		t.Fatal("correcting the draft did not save and close")
	}
	cursorTo(t, a, config.KeyTaskParallel)
	a.activate()
	a.sheet.edit.box.setText("4")
	a.sheetEditKey(key("esc"))
	if a.sheet.edit != nil || row.Value() != "3" || a.sheet.msg != "" {
		t.Fatal("Escape did not cancel draft cleanly")
	}
}

func TestSettingsOrganizationRenderedEditorDraftAndCaret(t *testing.T) {
	for _, width := range []int{80, 120} {
		t.Run(string(rune(width)), func(t *testing.T) {
			a, _ := sheetApp(t)
			a.width, a.height = width, 30
			a.openSettings()
			cursorTo(t, a, config.KeyTaskParallel)
			a.activate()
			a.sheet.edit.box.setText("not-a-number")
			a.sheetEditKey(key("enter"))
			lines, _, x, y := a.sheetFrame(a.width, a.height)
			if !a.caret || y < 0 || y >= len(lines) || !strings.Contains(plain(lines[y]), "not-a-number") || x <= 2 {
				t.Fatalf("draft or caret absent at %d: %d,%d\n%s", width, x, y, plain(strings.Join(lines, "\n")))
			}
			a.sheet.edit.box.setText("3")
			lines, _, _, y = a.sheetFrame(a.width, a.height)
			if !strings.Contains(plain(lines[y]), "3") {
				t.Fatal("corrected draft is invisible")
			}
			a.sheetEditKey(key("esc"))
			cursorTo(t, a, config.KeyExaKey)
			a.activate()
			a.sheet.edit.box.setText("exa-private-value")
			lines, _, end, y := a.sheetFrame(a.width, a.height)
			if strings.Contains(plain(strings.Join(lines, "\n")), "exa-private-value") {
				t.Fatal("credential leaked in editor frame")
			}
			a.sheet.edit.box.left()
			_, _, left, newY := a.sheetFrame(a.width, a.height)
			if !a.caret || y != newY || left >= end {
				t.Fatal("masked editor caret does not follow cursor movement")
			}
		})
	}
}

func TestSettingsOrganizationCountEditorInlineValidationAndClearing(t *testing.T) {
	for _, size := range []struct {
		name  string
		width int
	}{{"narrow", 80}, {"wide", 120}} {
		t.Run(size.name, func(t *testing.T) {
			a, dir := sheetApp(t)
			a.width, a.height = size.width, 30
			a.openSettings()
			cursorTo(t, a, config.KeyTaskParallel)
			a.activate()
			a.sheet.edit.box.setText("invalid")
			a.sheetEditKey(key("enter"))
			if a.sheet.edit == nil || config.TaskParallelAt(dir) != 0 {
				t.Fatal("invalid draft changed stored count or closed editor")
			}
			lines, _, _, y := a.sheetFrame(a.width, a.height)
			if !strings.Contains(plain(lines[y]), "invalid") || y+1 >= len(lines) || !strings.Contains(plain(lines[y+1]), "not a whole number") {
				t.Fatalf("error is not directly below input:\n%s", plain(strings.Join(lines, "\n")))
			}
			frame := plain(strings.Join(lines, "\n"))
			if strings.Count(frame, "not a whole number") != 1 || !strings.Contains(frame, "blank for no limit") {
				t.Fatalf("missing blank semantics or duplicate error:\n%s", frame)
			}
			a.sheet.edit.box.setText("4")
			a.sheetEditKey(key("enter"))
			if a.sheet.edit != nil || config.TaskParallelAt(dir) != 4 {
				t.Fatal("valid count was not saved")
			}
			cursorTo(t, a, config.KeyTaskParallel)
			a.activate()
			if a.sheet.edit == nil || a.sheet.edit.box.String() != "4" {
				t.Fatal("reopened count did not retain 4")
			}
			a.sheet.edit.box.reset()
			a.sheetEditKey(key("enter"))
			if a.sheet.edit != nil || config.TaskParallelAt(dir) != 0 {
				t.Fatal("blank did not remove the count cap")
			}
			cursorTo(t, a, config.KeyTaskParallel)
			a.activate()
			if a.sheet.edit == nil || a.sheet.edit.box.String() != "" {
				t.Fatal("unlimited count did not reopen as blank")
			}
		})
	}
	ordinary := config.Setting{Kind: config.SettingCount, Label: "repair attempts"}
	if strings.Contains(sheetEditNote("repair attempts", ordinary), "blank for no limit") {
		t.Fatal("nonoptional count promises incorrect blank semantics")
	}
}
