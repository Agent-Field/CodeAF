package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
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

// An open notebook is an active surface: no tip may appear underneath it, and
// a keypress inside the notebook still dismisses the idle clock, so a tip only
// returns after the notebook closes and the ordinary idle stretch passes.
func TestIdleTipYieldsWhileNotebookIsOpen(t *testing.T) {
	now := time.Date(2026, time.August, 6, 14, 0, 0, 0, time.Local)
	commander := newFakeCommander()
	commander.facts = []store.Fact{{
		Seq: 1, Scope: "user", Kind: store.FactPreference,
		Body: "Keep status updates compact.", Status: store.FactActive,
	}}
	model := NewWithCommander(&fakeBackend{}, "tips-notebook", commander)
	model.standingNow = func() time.Time { return now }

	// A tip is on screen, then the notebook opens over the idle surface.
	model.tipActivityAt = now.Add(-tipIdleAfter)
	if tip := model.idleTipLine(now); tip == "" {
		t.Fatal("idle tip did not surface before the notebook opened")
	}
	_ = model.executeSlash("/notebook")
	if !model.notebookOpen {
		t.Fatal("slash command did not open the notebook")
	}
	now = now.Add(time.Hour)
	model.tipActivityAt = now.Add(-time.Hour)
	if tip := model.idleTipLine(now); tip != "" {
		t.Fatalf("tip appeared while the notebook was open: %q", tip)
	}

	// A keypress inside the notebook restarts the idle clock like any other.
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.tipActivityAt.Equal(now) || model.tipCurrent != -1 {
		t.Fatalf("notebook keypress did not dismiss/reset the tip clock: at=%v current=%d",
			model.tipActivityAt, model.tipCurrent)
	}

	// Closing the notebook restores tip eligibility after the idle stretch.
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc}) // collapse expanded belief
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc}) // close the notebook
	if model.notebookOpen {
		t.Fatal("escape did not close the notebook")
	}
	now = now.Add(tipSpacing + tipIdleAfter)
	if tip := model.idleTipLine(now); tip == "" {
		t.Fatal("tip did not return after the notebook closed and idle passed")
	}
}

// The help overlay and /history results are active reading surfaces from the
// same family as the notebook: no tip may surface while either is up, and '?'
// on an empty draft opens help even when a tip is currently visible.
func TestIdleTipYieldsWhileHelpOverlayOrHistoryIsActive(t *testing.T) {
	now := time.Date(2026, time.August, 6, 14, 0, 0, 0, time.Local)
	model := tipTestModel(&now)

	// A tip is on screen; '?' still opens help and the tip yields.
	model.tipActivityAt = now.Add(-tipIdleAfter)
	if tip := model.idleTipLine(now); tip == "" {
		t.Fatal("idle tip did not surface before help opened")
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if model.palette != paletteHelp {
		t.Fatal("? on an empty draft did not open help over a visible tip")
	}
	if model.tipCurrent != -1 {
		t.Fatalf("opening help did not dismiss the tip: current=%d", model.tipCurrent)
	}
	model.tipActivityAt = now.Add(-time.Hour)
	if tip := model.idleTipLine(now); tip != "" {
		t.Fatalf("tip appeared under the help overlay: %q", tip)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.palette == paletteHelp {
		t.Fatal("escape did not close the help overlay")
	}

	// /history results hold the thread; the footer stays quiet beneath them.
	model.historyVisible = true
	model.tipActivityAt = now.Add(-time.Hour)
	if tip := model.idleTipLine(now); tip != "" {
		t.Fatalf("tip appeared while /history results were active: %q", tip)
	}
	model.historyVisible = false
	now = now.Add(tipSpacing + tipIdleAfter)
	model.tipActivityAt = now.Add(-tipIdleAfter)
	if tip := model.idleTipLine(now); tip == "" {
		t.Fatal("tip did not return after help and history closed")
	}
}

// The one footer line resolves by explicit priority: transient voice status →
// active boost indicator → pending-question context → idle tip → focus-zone
// help. When both boost and a tip are eligible, boost wins and the tip waits.
func TestFooterPriorityBoostOutranksIdleTipAndAltBDismisses(t *testing.T) {
	now := time.Date(2026, time.August, 6, 14, 0, 0, 0, time.Local)
	model := tipTestModel(&now)
	model.setSize(100, 30)

	// An idle tip is on screen.
	model.tipActivityAt = now.Add(-tipIdleAfter)
	view := ansi.Strip(model.View())
	if !strings.Contains(view, curatedTips[0].text) {
		t.Fatalf("idle tip did not surface:\n%s", view)
	}

	// alt+b both cycles boost AND dismisses the visible tip in one press.
	pressBoost(model)
	if model.boost != boostArmed || model.tipCurrent != -1 {
		t.Fatalf("alt+b left boost=%v tipCurrent=%d, want armed and dismissed", model.boost, model.tipCurrent)
	}

	// Long idle makes a tip eligible again, but it may not displace an armed
	// boost: the footer keeps the boost indicator, clickable, and the tip waits.
	now = now.Add(time.Hour)
	view = ansi.Strip(model.View())
	if !strings.Contains(view, "boost") || !strings.Contains(view, "next message") {
		t.Fatalf("armed boost indicator missing from footer:\n%s", view)
	}
	if model.boostBounds.width == 0 {
		t.Fatal("boost indicator lost its click target while a tip was eligible")
	}
	for _, tip := range curatedTips {
		if strings.Contains(view, tip.text) {
			t.Fatalf("tip %q displaced the armed boost indicator:\n%s", tip.text, view)
		}
	}

	// Transient voice status is the one thing that outranks boost, and only
	// for its moment: the borrowed row is not a boost click target.
	model.voiceHint = "voice caught that"
	model.voiceHintUntil = time.Now().Add(time.Minute)
	view = ansi.Strip(model.View())
	if !strings.Contains(view, "voice caught that") {
		t.Fatalf("transient voice status did not outrank boost:\n%s", view)
	}
	if model.boostBounds.width != 0 {
		t.Fatal("hidden boost indicator kept a stale click target under voice status")
	}
	model.voiceHint = ""
	model.voiceHintUntil = time.Time{}

	// A pending question outranks the tip even after boost cycles off.
	pressBoost(model) // pinned
	pressBoost(model) // off
	if model.boost != boostOff {
		t.Fatalf("boost cycle ended at %v, want off", model.boost)
	}
	model.cards = []jobCard{{ID: "q1", State: cardQuestion, QuestionKind: questionText, Question: "Which region?"}}
	now = now.Add(time.Hour)
	view = ansi.Strip(model.View())
	if !strings.Contains(view, "press a question's number") {
		t.Fatalf("pending-question context missing from footer:\n%s", view)
	}
	model.cards = nil

	// With boost off, no voice status, and no question, the waiting tip
	// finally lands after the ordinary idle stretch.
	now = now.Add(time.Hour)
	view = ansi.Strip(model.View())
	if !strings.Contains(view, curatedTips[1].text) {
		t.Fatalf("waiting tip did not resume after boost turned off:\n%s", view)
	}
}

func TestContextHelpLineFollowsFocusZone(t *testing.T) {
	model := New(&fakeBackend{}, "help")
	tests := []struct {
		name  string
		focus paneFocus
		want  []string
	}{
		{"input", focusInput, []string{"ctrl+v voice", "v receipts"}},
		{"rail", focusGraph, []string{"enter inspect", "ctrl+t hide"}},
		{"card", focusCards, []string{"enter details", "ctrl+t tasks"}},
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
