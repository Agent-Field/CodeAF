package tui3

import (
	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/config"
	"strings"
	"testing"
)

// chooseSheetValue follows the displayed choices; no registry writes bypass the UI.
func chooseSheetValue(t *testing.T, a *app, value string) {
	t.Helper()
	drive(t, a, key("enter"))
	if a.sheet.choice == nil {
		t.Fatal("choice setting did not open options")
	}
	drive(t, a, key("home"))
	found := false
	for i, v := range a.sheet.choice.item.row.Choices {
		if v == value {
			for n := 0; n < i; n++ {
				drive(t, a, key("down"))
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no option %q", value)
	}
	drive(t, a, key("enter"))
}

func TestSettingsChoicePreviewCancelSaveAndReopen(t *testing.T) {
	a, dir := sheetApp(t)
	a.openSettings()
	cursorTo(t, a, config.KeyWork)
	row, _ := a.sheet.registry.Row(config.KeyWork)
	before := row.Value()
	drive(t, a, key("enter"), key("down"))
	if a.sheet.choice == nil || config.WorkAt(dir) != before {
		t.Fatal("opening or previewing wrote the value")
	}
	screen := plain(frame(a))
	if !strings.Contains(screen, "current") || !strings.Contains(screen, "collapsed") || !strings.Contains(screen, "expanded") {
		t.Fatalf("choices do not explain the current and available values:\n%s", screen)
	}
	drive(t, a, key("esc"))
	if a.sheet.choice != nil || config.WorkAt(dir) != before {
		t.Fatal("cancel changed the value")
	}
	chooseSheetValue(t, a, "open")
	if a.sheet.choice != nil || config.WorkAt(dir) != "open" {
		t.Fatal("save did not persist chosen value")
	}
	drive(t, a, key("enter"))
	if a.sheet.choice.current != "open" || a.sheet.choice.item.row.Choices[a.sheet.choice.cursor] != "open" {
		t.Fatal("reopen did not mark the saved value")
	}
}

func TestSettingsChoiceMouseSaveAndCancel(t *testing.T) {
	for _, width := range []int{80, 120} {
		a, dir := sheetApp(t)
		a.width = width
		a.height = 35
		a.openSettings()
		cursorTo(t, a, config.KeyWork)
		drive(t, a, key("enter"))
		_, hits, _, _ := a.sheetFrame(width, a.height)
		target, cancel := -1, -1
		for y, hit := range hits {
			if hit.kind == sheetHitChoice && a.sheet.choice.item.row.Choices[hit.index] == "open" {
				target = y
			}
			if hit.kind == sheetHitChoiceCancel {
				cancel = y
			}
		}
		if target < 0 || cancel < 0 {
			t.Fatal("missing option/cancel pointer targets")
		}
		before := config.WorkAt(dir)
		a.sheetPress(3, cancel)
		if a.sheet.choice != nil || config.WorkAt(dir) != before {
			t.Fatal("mouse cancel wrote value")
		}
		drive(t, a, key("enter"))
		a.sheetHoverAt(3, target)
		if config.WorkAt(dir) != before {
			t.Fatal("hover wrote value")
		}
		a.sheetPress(3, target)
		if a.sheet.choice != nil || config.WorkAt(dir) != "open" {
			t.Fatal("mouse save failed")
		}
	}
}

func TestSettingsChoiceCompactKeepsSelectionAndCancel(t *testing.T) {
	for _, width := range []int{24, 40, 80} {
		a, _ := sheetApp(t)
		a.width = width
		a.height = 16
		a.openSettings()
		cursorTo(t, a, config.KeyTaskStart)
		drive(t, a, key("enter"))
		screen := plain(frame(a))
		if !strings.Contains(screen, "current") || !strings.Contains(screen, "Cancel") {
			t.Fatalf("%dc loses current or cancel:\n%s", width, screen)
		}
		drive(t, a, key("end"))
		_, hits, _, _ := a.sheetFrame(width, a.height)
		found := false
		for _, h := range hits {
			if h.kind == sheetHitChoice && h.index == a.sheet.choice.cursor {
				found = true
			}
		}
		if !found {
			t.Fatalf("%dc hides selected option", width)
		}
	}
}

func TestSettingsChoiceOwnsPasteAndWheel(t *testing.T) {
	a, dir := sheetApp(t)
	a.openSettings()
	cursorTo(t, a, config.KeyWork)
	before, query, cursor := config.WorkAt(dir), a.sheet.query.String(), a.sheet.cursor
	drive(t, a, key("enter"), tea.PasteMsg{Content: "should not search"})
	drive(t, a, tea.MouseWheelMsg{X: 2, Y: 0, Button: tea.MouseWheelDown})
	if a.sheet.query.String() != query || a.sheet.cursor != cursor || !a.at(pageSettings) {
		t.Fatal("modal input altered underlying settings")
	}
	if a.sheet.choice.cursor != len(a.sheet.choice.item.row.Choices)-1 || config.WorkAt(dir) != before {
		t.Fatal("wheel failed to preview without saving")
	}
}
