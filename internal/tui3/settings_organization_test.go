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
