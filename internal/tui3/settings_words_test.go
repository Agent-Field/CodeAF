package tui3

import (
	"github.com/Agent-Field/codeaf/internal/config"
	"strings"
	"testing"
)

func TestSettingsWordsKeepSavedChoicesAndSearchableReadings(t *testing.T) {
	for _, test := range []struct{ key, raw, shown string }{
		{config.KeyTaskStart, "sized", "assess while working"},
		{config.KeyTaskStart, "single", "skip assessment"},
		{config.KeyTaskSettle, "auto", "let the chat decide"},
		{config.KeyToolApprovalMode, "prompt", "ask by default"},
		{config.KeyWork, "fold", "collapsed"},
		{config.KeyModelPool, "read", "use only"},
	} {
		t.Run(test.key+test.raw, func(t *testing.T) {
			a, _ := sheetApp(t)
			a.openSettings()
			cursorTo(t, a, test.key)
			item, _ := a.sheet.current()
			if err := item.row.Apply(test.raw); err != nil {
				t.Fatal(err)
			}
			if got := item.row.Value(); got != test.raw {
				t.Fatalf("saved choice %q", got)
			}
			if got := settingValueWord(item.row); got != test.shown {
				t.Fatalf("reading %q, want %q", got, test.shown)
			}
			for _, query := range []string{test.raw, test.shown} {
				if itemAt(searchItems(t, a, query), test.key) < 0 {
					t.Fatalf("cannot find %s via %q", test.key, query)
				}
			}
		})
	}
}

func TestSettingsSearchAndHelpNameTheirCurrentScope(t *testing.T) {
	a, _ := sheetApp(t)
	a.width, a.height = 120, 35
	a.openSettings()
	a.raiseSettings()
	if !strings.Contains(a.sheet.keysLine(), "esc close") {
		t.Fatal("resting Escape must say close")
	}
	typeQuery(t, a, "mouse")
	if !strings.Contains(plain(frame(a)), "Search results") {
		t.Fatal("global results still look like one category")
	}
	if !strings.Contains(a.sheet.keysLine(), "esc clear search") {
		t.Fatal("search Escape must name what it clears")
	}
	drive(t, a, key("esc"))
	if !a.at(pageSettings) || a.sheet.searching() {
		t.Fatal("clearing search closed settings")
	}
}
