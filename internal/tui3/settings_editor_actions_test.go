package tui3

import (
	"github.com/Agent-Field/codeaf/internal/config"
	"strings"
	"testing"
)

func TestSettingsEditorActionsSaveCancelAndValidation(t *testing.T) {
	for _, size := range [][2]int{{80, 30}, {120, 30}, {40, 16}, {40, 12}} {
		a, dir := sheetApp(t)
		a.width, a.height = size[0], size[1]
		a.openSettings()
		cursorTo(t, a, config.KeyTaskParallel)
		a.activate()
		actionRow := func() int {
			t.Helper()
			lines, hits, x, y := a.sheetFrame(a.width, a.height)
			if y < 0 || y >= len(lines) || x < 0 {
				t.Fatal("editor caret absent")
			}
			for at, hit := range hits {
				if hit.kind == sheetHitEditActions {
					if !strings.Contains(plain(lines[at]), "Save") || !strings.Contains(plain(lines[at]), "Cancel") {
						t.Fatal("invisible actions")
					}
					return at
				}
			}
			t.Fatalf("actions absent at %v:\n%s", size, plain(strings.Join(lines, "\n")))
			return -1
		}
		y := actionRow()
		a.sheet.edit.box.setText("7")
		a.sheetPress(15, y)
		if a.sheet.edit == nil || config.TaskParallelAt(dir) != 0 {
			t.Fatal("blank action gap saved")
		}
		a.sheetHoverAt(3, y)
		if a.hot.kind != hoverSheet || a.hot.index != sheetEditSaveHover {
			t.Fatal("Save has no hover")
		}
		a.sheetPress(18, y)
		if a.sheet.edit != nil || config.TaskParallelAt(dir) != 0 {
			t.Fatal("Cancel saved draft")
		}
		a.activate()
		a.sheet.edit.box.setText("invalid")
		a.sheetPress(3, actionRow())
		if a.sheet.edit == nil || a.sheet.msg == "" || config.TaskParallelAt(dir) != 0 {
			t.Fatal("click save skipped validation")
		}
		a.sheet.edit.box.setText("4")
		a.sheetPress(3, actionRow())
		if a.sheet.edit != nil || config.TaskParallelAt(dir) != 4 {
			t.Fatal("click Save failed to persist")
		}
		cursorTo(t, a, config.KeyTaskParallel)
		a.activate()
		if a.sheet.edit.box.String() != "4" {
			t.Fatal("saved value did not reopen")
		}
		a.sheet.edit.box.setText("8")
		a.sheetEditKey(key("esc"))
		if config.TaskParallelAt(dir) != 4 {
			t.Fatal("keyboard Cancel saved")
		}
	}
}

func TestSettingsEditorShowsRestartBesideField(t *testing.T) {
	a, _ := sheetApp(t)
	a.width, a.height = 80, 30
	a.openSettings()
	cursorTo(t, a, config.KeyTaskParallel)
	a.activate()
	lines, _, _, _ := a.sheetFrame(a.width, a.height)
	frame := plain(strings.Join(lines, "\n"))
	if !strings.Contains(frame, "Restart the CLI") || !strings.Contains(frame, "blank for no limit") {
		t.Fatalf("missing field context:\n%s", frame)
	}
}

func TestSettingsEditorPendingWriteOffersClose(t *testing.T) {
	a, dir := sheetApp(t)
	a.width, a.height = 80, 30
	a.openSettings()
	cursorTo(t, a, config.KeyTaskParallel)
	a.activate()
	a.sheet.host = "build-host"
	a.sheet.edit.pending = true
	a.sheet.edit.box.setText("9")
	lines, hits, _, _ := a.sheetFrame(a.width, a.height)
	frame := plain(strings.Join(lines, "\n"))
	if !strings.Contains(frame, "Saving on build-host") || !strings.Contains(frame, "Close") || strings.Contains(frame, "Cancel") {
		t.Fatalf("pending write implies cancellation:\n%s", frame)
	}
	for y, h := range hits {
		if h.kind == sheetHitEditActions {
			a.sheetPress(3, y)
			if a.sheet.edit == nil || config.TaskParallelAt(dir) != 0 {
				t.Fatal("pending Save reissued write")
			}
			a.sheetPress(18, y)
			if a.sheet.edit != nil {
				t.Fatal("pending Close did not close editor")
			}
			return
		}
	}
	t.Fatal("pending actions absent")
}

func TestSettingsEditorNarrowActionBounds(t *testing.T) {
	a, _ := sheetApp(t)
	a.width, a.height = 24, 24
	a.openSettings()
	cursorTo(t, a, config.KeyTaskParallel)
	a.activate()
	lines, hits, _, _ := a.sheetFrame(a.width, a.height)
	for y, h := range hits {
		if h.kind == sheetHitEditActions {
			if !strings.Contains(plain(lines[y]), "Save    Cancel") {
				t.Fatalf("narrow labels: %s", plain(lines[y]))
			}
			a.sheetPress(10, y)
			if a.sheet.edit != nil {
				t.Fatal("narrow Cancel did not work")
			}
			return
		}
	}
	t.Fatal("narrow actions absent")
}
