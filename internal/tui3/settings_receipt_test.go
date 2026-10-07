package tui3

import (
	"github.com/Agent-Field/codeaf/internal/config"
	"strings"
	"testing"
)

func TestSettingsReceiptOnlyAfterSuccessfulWrite(t *testing.T) {
	a, dir := sheetApp(t)
	a.openSettings()
	cursorTo(t, a, config.KeyTaskParallel)
	a.activate()
	a.sheet.edit.box.setText("bad")
	a.sheetEditKey(key("enter"))
	if a.sheet.savedKey != "" || config.TaskParallelAt(dir) != 0 {
		t.Fatal("failed save claimed success")
	}
	a.sheet.edit.box.setText("4")
	a.sheetEditKey(key("enter"))
	if a.sheet.savedKey != config.KeyTaskParallel || config.TaskParallelAt(dir) != 4 {
		t.Fatal("successful save missing receipt")
	}
	if !strings.Contains(plain(strings.Join((placeSettings{}).note(a, 80), "")), "Saved") {
		t.Fatal("receipt not rendered")
	}
	a.activate()
	if a.sheet.savedKey != "" {
		t.Fatal("old receipt remained while editing")
	}
	a.sheet.edit.box.setText("8")
	a.sheetEditKey(key("esc"))
	if a.sheet.savedKey != "" || config.TaskParallelAt(dir) != 4 {
		t.Fatal("cancel claimed save")
	}
}
