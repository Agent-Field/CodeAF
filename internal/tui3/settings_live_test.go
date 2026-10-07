package tui3

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
)

func TestSettingsLivePanelRefreshesPresentation(t *testing.T) {
	cases := []struct {
		key, raw string
		check    func(*app) bool
	}{
		{config.KeyMouse, "off", func(a *app) bool { return !a.mouse }},
		{config.KeyTimestamps, "footers", func(a *app) bool { return a.stampsOn() }},
		{config.KeyWork, "open", func(a *app) bool { return a.workMode == "open" }},
		{config.KeyIcons, "plain", func(a *app) bool { return a.iconMode == config.IconsPlain }},
		{config.KeyQuickSwitch, "off", func(a *app) bool { return !a.hopQuick }},
		// The old registry row is "hide hints"; the TUI presents its inverse.
		{config.KeyHints, "on", func(a *app) bool { return !a.notices.enabled }},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			a, _ := sheetApp(t)
			a.openSettings()
			a.mouse, a.hopQuick, a.notices.enabled = true, true, true
			a.workMode = "fold"
			a.timestamps = config.TimestampsOff
			a.iconMode = config.IconsAuto
			a.railAway = true // A temporary keyboard choice must survive a preference save.
			a.dirty = false
			row, ok := a.sheet.registry.Row(tc.key)
			if !ok {
				t.Fatalf("missing %s", tc.key)
			}
			meta, _ := settingMetaFor(row)
			if !a.applySetting(sheetItem{row: row, meta: meta}, tc.raw) {
				t.Fatal(a.sheet.msg)
			}
			if !tc.check(a) || !a.dirty {
				t.Fatal("saved setting did not refresh the visible presentation immediately")
			}
			if !a.railAway {
				t.Fatal("preference refresh reopened the manually hidden task column")
			}
		})
	}
}

func TestSettingsLiveTurnCompletionAdoptsToolProfileWrites(t *testing.T) {
	a, _ := sheetApp(t)
	a.openSettings()
	registry := a.sheet.registry
	a.closeSettings()
	a.mouse, a.hopQuick, a.notices.enabled = true, true, true
	a.workMode = "fold"
	a.timestamps = config.TimestampsOff
	a.railAway = true
	for _, change := range [][2]string{
		{config.KeyMouse, "off"},
		{config.KeyTimestamps, "footers"},
		{config.KeyWork, "open"},
		{config.KeyQuickSwitch, "off"},
		{config.KeyHints, "on"},
	} {
		row, ok := registry.Row(change[0])
		if !ok {
			t.Fatal(change[0])
		}
		if err := row.Apply(change[1]); err != nil {
			t.Fatal(err)
		}
	}
	// A chat tool writes the registry; its completed turn is the refresh boundary.
	a.settle()
	if a.mouse || a.hopQuick || a.notices.enabled || a.workMode != "open" || !a.stampsOn() {
		t.Fatal("turn completion did not adopt saved profile preferences")
	}
	if !a.railAway {
		t.Fatal("turn refresh discarded the manually hidden task column")
	}
}
