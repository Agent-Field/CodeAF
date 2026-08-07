package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

func tipTestModel(now *time.Time) *Model {
	model := New(&fakeBackend{}, "tips")
	model.standingNow = func() time.Time { return *now }
	return model
}

func TestIdleTipTimingDismissalNoRepeatAndSpacing(t *testing.T) {
	now := time.Date(2026, time.August, 6, 14, 0, 0, 0, time.Local)
	model := tipTestModel(&now)
	if tip := model.idleTipLine(now); tip != "" {
		t.Fatalf("tip appeared before the idle clock started: %q", tip)
	}
	now = now.Add(tipIdleAfter - time.Second)
	if tip := model.idleTipLine(now); tip != "" {
		t.Fatalf("tip appeared before 20s idle: %q", tip)
	}
	now = now.Add(time.Second)
	first := model.idleTipLine(now)
	if first != curatedTips[0].text {
		t.Fatalf("first tip = %q, want %q", first, curatedTips[0].text)
	}

	// A key that does not create a draft still dismisses immediately.
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	if tip := model.idleTipLine(now); tip != "" {
		t.Fatalf("keypress did not dismiss tip: %q", tip)
	}
	now = now.Add(tipIdleAfter)
	if tip := model.idleTipLine(now); tip != "" {
		t.Fatalf("tip ignored 60s spacing after keypress: %q", tip)
	}
	now = now.Add(tipSpacing - tipIdleAfter)
	second := model.idleTipLine(now)
	if second == "" || second == first {
		t.Fatalf("tip rotation repeated or stayed empty: first=%q second=%q", first, second)
	}
	if !model.tipSeen[0] || !model.tipSeen[1] {
		t.Fatalf("session no-repeat ledger = %#v", model.tipSeen)
	}
}

func TestIdleTipRotatesOnlyAfterSixtySeconds(t *testing.T) {
	now := time.Date(2026, time.August, 6, 14, 0, 0, 0, time.Local)
	model := tipTestModel(&now)
	model.tipActivityAt = now.Add(-tipIdleAfter)
	first := model.idleTipLine(now)
	now = now.Add(tipSpacing - time.Second)
	if got := model.idleTipLine(now); got != first {
		t.Fatalf("tip rotated at 59s: got %q, want %q", got, first)
	}
	now = now.Add(time.Second)
	if got := model.idleTipLine(now); got == "" || got == first {
		t.Fatalf("tip did not rotate at 60s: first=%q got=%q", first, got)
	}
}

func TestIdleTipSuppressesFeaturesAlreadyUsedThisSession(t *testing.T) {
	now := time.Date(2026, time.August, 6, 14, 0, 0, 0, time.Local)
	model := tipTestModel(&now)
	_ = model.toggleVoice()
	model.tipActivityAt = now.Add(-tipIdleAfter)
	if got := model.idleTipLine(now); got != curatedTips[1].text {
		t.Fatalf("voice-used suppression chose %q, want %q", got, curatedTips[1].text)
	}

	now = now.Add(time.Hour)
	model = tipTestModel(&now)
	_ = model.toggleVoice()
	model.snapshot = store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{ID: "learned", Parent: store.RootID, Group: charterGroupMarker},
	}}
	model.cardSnapshot = model.snapshot
	model.tipActivityAt = now.Add(-tipIdleAfter)
	if got := model.idleTipLine(now); got != curatedTips[2].text {
		t.Fatalf("standing-used suppression chose %q, want %q", got, curatedTips[2].text)
	}

	now = now.Add(time.Hour)
	model = tipTestModel(&now)
	_ = model.toggleVoice()
	model.snapshot = store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{ID: "learned", Parent: store.RootID, Group: charterGroupMarker},
	}}
	model.cardSnapshot = model.snapshot
	_ = model.executeSlash("/budget")
	model.tipActivityAt = now.Add(-tipIdleAfter)
	if got := model.idleTipLine(now); got != curatedTips[3].text {
		t.Fatalf("budget-used suppression chose %q, want %q", got, curatedTips[3].text)
	}
}

func TestIdleTipNeverAppearsDuringActiveWork(t *testing.T) {
	now := time.Date(2026, time.August, 6, 14, 0, 0, 0, time.Local)
	model := tipTestModel(&now)
	model.tipActivityAt = now.Add(-time.Hour)
	model.pending = []store.Command{{Kind: store.CommandSplice, Instruction: "active compile"}}
	if got := model.idleTipLine(now); got != "" {
		t.Fatalf("tip appeared during active work: %q", got)
	}
}

func TestContextHelpLineFollowsFocusZone(t *testing.T) {
	model := New(&fakeBackend{}, "help")
	tests := []struct {
		name  string
		focus paneFocus
		want  []string
	}{
		{"input", focusInput, []string{"alt+v voice", "v receipts"}},
		{"rail", focusGraph, []string{"enter inspect", "alt+g hide"}},
		{"card", focusCards, []string{"enter details", "alt+g tasks"}},
		{"header", focusHeader, []string{"enter models/tasks", "esc back"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model.focus = test.focus
			line := model.contextHelpLine()
			for _, want := range test.want {
				if !strings.Contains(line, want) {
					t.Fatalf("%s help missing %q: %q", test.name, want, line)
				}
			}
			if items := strings.Count(line, " · ") + 1; items > 4 {
				t.Fatalf("%s help has %d items: %q", test.name, items, line)
			}
		})
	}
}
