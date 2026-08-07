package tui

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestArrivalBriefRendersAsOneCollapsibleTailCard(t *testing.T) {
	model := New(&fakeBackend{}, "arrival")
	model.messages = []store.Message{
		{Seq: 2, SessionID: "arrival", Role: store.RoleUser, Body: "Earlier conversation."},
		{
			Seq: 9, SessionID: "arrival", Role: store.RoleAgent,
			Body: "While you were away: three things landed, one choice is waiting, and $1.40 was spent.",
			Brief: &store.Brief{
				SinceSeq: 3, ThroughSeq: 8, Done: 3, Questions: 1, CostUSD: 1.4,
				Items: []store.BriefItem{
					{Kind: store.BriefDone, Body: "The release notes landed."},
					{Kind: store.BriefQuestion, Body: "The rollout is waiting for a region."},
					{Kind: store.BriefSpend, Body: "$1.40 spent."},
				},
			},
		},
	}

	collapsed := ansi.Strip(model.renderMessages())
	if !strings.Contains(collapsed, "▸ While you were away:") {
		t.Fatalf("collapsed brief missing fold line:\n%s", collapsed)
	}
	if strings.Contains(collapsed, "release notes landed") || strings.Count(collapsed, "While you were away:") != 1 {
		t.Fatalf("closed brief was not one calm line:\n%s", collapsed)
	}
	if strings.LastIndex(collapsed, "While you were away:") < strings.LastIndex(collapsed, "Earlier conversation") {
		t.Fatalf("brief did not render at the thread tail:\n%s", collapsed)
	}

	model.focus = focusChat
	model.chatFocusIndex = len(model.chatFocusLines()) - 1
	if !model.activateChatFocus() {
		t.Fatal("enter target did not activate the brief")
	}
	expanded := ansi.Strip(model.renderMessages())
	for _, want := range []string{"▾ While you were away:", "✓ The release notes landed.", "? The rollout is waiting for a region.", "$ $1.40 spent."} {
		if !strings.Contains(expanded, want) {
			t.Fatalf("expanded brief missing %q:\n%s", want, expanded)
		}
	}

	if _, handled := model.updateKey(tea.KeyMsg{Type: tea.KeyEsc}); !handled {
		t.Fatal("esc did not handle the expanded brief")
	}
	closedAgain := ansi.Strip(model.renderMessages())
	if strings.Contains(closedAgain, "release notes landed") || !strings.Contains(closedAgain, "▸ While you were away:") {
		t.Fatalf("esc did not collapse the brief:\n%s", closedAgain)
	}
}
