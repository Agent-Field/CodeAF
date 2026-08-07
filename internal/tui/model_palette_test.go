package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestModelPaletteSlotNavigationNumbersAndFocusOrder(t *testing.T) {
	model := NewWithCommander(&fakeBackend{}, "palette", newFakeCommander())
	model.focus = focusHeader
	model.inputFocused = false

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	if model.headerFocusIndex != 1 {
		t.Fatalf("right focused header index %d, want settings=1", model.headerFocusIndex)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	if model.headerFocusIndex != 2 {
		t.Fatalf("right focused header index %d, want tasks=2", model.headerFocusIndex)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	if model.headerFocusIndex != 3 {
		t.Fatalf("right focused header index %d, want help=3", model.headerFocusIndex)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	if model.headerFocusIndex != 0 {
		t.Fatalf("fourth right wrapped to header index %d, want models=0", model.headerFocusIndex)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.palette != paletteModels || model.modelSlotIndex != 0 {
		t.Fatalf("models enter opened palette=%v slot=%d", model.palette, model.modelSlotIndex)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if model.modelSlotIndex != 1 {
		t.Fatalf("down selected slot %d, want work=1", model.modelSlotIndex)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	if model.modelSlotIndex != 0 {
		t.Fatalf("up selected slot %d, want talk=0", model.modelSlotIndex)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'9'}})
	if model.palette != paletteModel || model.modelRole != "boost" {
		t.Fatalf("9 opened palette=%v role=%q, want boost picker", model.palette, model.modelRole)
	}
	_ = model.View()
	_, _ = model.updateMouseClick(model.paletteCloseBounds.x, model.paletteCloseBounds.y)
	if model.palette != paletteModels {
		t.Fatalf("picker close click returned to palette %v", model.palette)
	}
	_ = model.View()
	_, _ = model.updateMouseClick(model.paletteCloseBounds.x, model.paletteCloseBounds.y)
	if model.palette != paletteNone || model.focus != focusHeader {
		t.Fatalf("palette close click returned to palette=%v focus=%v", model.palette, model.focus)
	}
}

func TestEveryModelSlotUsesItsOwnSearchableCatalog(t *testing.T) {
	commander := newFakeCommander()
	commander.current = map[string]string{}
	commander.catalogs = make(map[string][]ModelChoice)
	for _, slot := range modelSlots {
		commander.current[slot] = "current/" + slot
		commander.catalogs[slot] = []ModelChoice{
			{Slug: "vendor/" + slot + "-first", Name: slot + " first"},
			{Slug: "vendor/" + slot + "-target", Name: slot + " target"},
		}
	}
	for _, slot := range modelSlots {
		model := NewWithCommander(&fakeBackend{}, "catalogs", commander)
		command := model.openModelPicker(slot)
		if command == nil {
			t.Fatalf("%s picker did not request its catalog", slot)
		}
		_, _ = model.Update(command())
		model.input.SetValue("target")
		choices := model.filteredModelChoices()
		if len(choices) != 1 || choices[0].Slug != "vendor/"+slot+"-target" {
			t.Fatalf("%s filtered choices = %+v", slot, choices)
		}
		model.input.Reset()
	}
}

func TestModelPaletteResponsivePanelAndHeaderWidthLadder(t *testing.T) {
	commander := newFakeCommander()
	commander.current["talk"] = "anthropic/claude-sonnet-5-long-2026-07-31"
	commander.current["work"] = "anthropic/claude-opus-5-extended-2026-07-31"
	commander.current["voice"] = "qwen/qwen3-asr-flash-2026-02-10"
	commander.current["image"] = "krea/krea-2-medium-turbo"
	commander.current["speech"] = "hexgrad/kokoro-82m"
	commander.current["music"] = "google/lyria-3-clip-preview"
	commander.current["video"] = "bytedance/seedance-1-5-pro"
	model := NewWithCommander(&fakeBackend{}, "ss", commander)
	model.status = "status-that-yields-before-models"
	model.statusUntil = time.Now().Add(time.Minute)

	for _, test := range []struct {
		width        int
		wantStatus   bool
		wantGlance   bool
		wantWideHint bool
	}{
		{width: 80, wantStatus: false, wantGlance: false, wantWideHint: false},
		{width: 100, wantStatus: false, wantGlance: true, wantWideHint: true},
		{width: 120, wantStatus: true, wantGlance: true, wantWideHint: true},
	} {
		model.setSize(test.width, 30)
		model.palette = paletteNone
		header := ansi.Strip(strings.Split(model.View(), "\n")[0])
		if strings.Contains(header, "status-that") != test.wantStatus {
			t.Fatalf("width %d status presence=%t header=%q", test.width, !test.wantStatus, header)
		}
		if strings.Contains(header, "talk claude-sonnet") != test.wantGlance {
			t.Fatalf("width %d glance header=%q", test.width, header)
		}
		if !strings.Contains(header, "models ⌄") || strings.Contains(header, "talk ⌄") ||
			strings.Contains(header, "work ⌄") || strings.Contains(header, "voice ⌄") {
			t.Fatalf("width %d did not keep one model control: %q", test.width, header)
		}

		_ = model.openModelsPalette()
		view := ansi.Strip(model.View())
		if strings.Contains(view, "↑/↓ choose") != test.wantWideHint {
			t.Fatalf("width %d wide hint mismatch:\n%s", test.width, view)
		}
		for _, expected := range []string{"talk", "work", "voice", "image", "speech", "music", "video", "boost", "qwen3-asr-flash"} {
			if !strings.Contains(view, expected) {
				t.Fatalf("width %d palette missing %q:\n%s", test.width, expected, view)
			}
		}
	}
}
