package tui3

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

// The start page is a selected view, never a synthetic conversation identity.
func TestNewChatTabOwnsTheSelectionAndCloseReturnsTheDraft(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	a.width = 100
	a.input.setText("old draft")
	_ = a.tabsRow(a.width)
	var plus tabHit
	for _, h := range a.chatTabHits {
		if h.kind == tabNew {
			plus = h
		}
	}
	if plus.span.to == 0 {
		t.Fatal("no plus control")
	}
	cmd, took := a.tabPress(plus.span.from, tabStripRow)
	if !took {
		t.Fatal("plus did not take click")
	}
	drain(t, a, cmd)
	a.input.setText("new draft")
	_ = a.tabsRow(a.width)
	active := 0
	var close tabHit
	for _, h := range a.chatTabHits {
		if h.kind == tabHere {
			active++
			if !h.tab.start {
				t.Fatal("old chat remains selected over new composer")
			}
		}
		if h.kind == tabClose && h.tab.start {
			close = h
		}
	}
	if active != 1 || close.span.to == 0 {
		t.Fatalf("active=%d close=%+v", active, close)
	}
	cmd, _ = a.tabPress(close.span.from, tabStripRow)
	drain(t, a, cmd)
	if a.startingChat() || a.input.String() != "old draft" {
		t.Fatalf("close lost old context: %q", a.input.String())
	}
	openStart(t, a)
	if a.input.String() != "new draft" || lab.made != 0 {
		t.Fatal("start draft or create boundary changed")
	}
}

func TestClickingAChatFromStartNeverMovesTheNewDraftIntoIt(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	a.width = 120
	keepThree(t, a)
	a.input.setText("front draft")
	openStart(t, a)
	a.input.setText("new page draft")
	_ = a.tabsRow(a.width)
	var target chatTab
	for _, tab := range a.chatTabs {
		if tab.key != a.frontTabKey() {
			target = tab
			break
		}
	}
	if target.file == "" {
		t.Fatal("fixture has no other chat")
	}
	drain(t, a, a.tabGo(target))
	if a.startingChat() || a.input.String() == "new page draft" {
		t.Fatal("new draft leaked on tab switch")
	}
	openStart(t, a)
	if a.input.String() != "new page draft" {
		t.Fatal("start draft was not parked")
	}
}

func TestNewChatHeaderTargetsFitAndStayStableAcrossStatusChanges(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	for _, width := range []int{30, 60, 80, 160} {
		a.width = width
		for _, starting := range []bool{false, true} {
			if starting {
				openStart(t, a)
			}
			line := a.tabsRow(width)
			if ansi.StringWidth(line) > width {
				t.Fatalf("width %d overflow: %q", width, plain(line))
			}
			prev := 0
			for _, h := range a.chatTabHits {
				if h.span.from < prev || h.span.to > width {
					t.Fatalf("bad targets: %+v", a.chatTabHits)
				}
				prev = h.span.to
			}
			if starting && strings.Contains(plain(line), "New chat") {
				t.Fatalf("empty start page added a tab at %d: %q", width, plain(line))
			}
			if starting {
				a.cancelChatStart()
			}
		}
	}
}

// The start page cannot label its composer with another conversation's costs.
func TestStartPageFooterBelongsToTheNewChat(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	openStart(t, a)
	for _, width := range []int{30, 80, 160} {
		rows := a.statusRows(width)
		if len(rows) != a.statusHeight(width) || strings.Contains(plain(strings.Join(rows, " ")), "Shipping") {
			t.Fatal("start footer exposes old identity")
		}
		if a.modelSpan.to != 0 || a.moneySpan.to != 0 || len(a.doors) != 0 {
			t.Fatal("hidden old footer retains click targets")
		}
	}
}

func TestTaskPlaceholderDoesNotOverwriteItsTrayWithASecondPrompt(t *testing.T) {
	a, _, _ := roomApp(t)
	railRun(a)
	clickRailNode(t, a, 1)
	width := 100
	lead := a.roomLead(width)
	rows := []string{"tray row", lead + a.pal.dim(prompt)}
	got := a.roomSteerLaneRows(rows, width)
	if got[0] != "tray row" || !strings.Contains(plain(got[1]), roomSteerHere) {
		t.Fatalf("placeholder is not on the composer: %q", got)
	}
}

// Editing a compact paste is a local editor action, not a first send.
func TestEditingAStartPagePasteCreatesNoConversation(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	openStart(t, a)
	a.pastes = []pasteChip{{n: 1, text: "log details"}}
	a.input.setText(pasteToken(1, 1))
	drive(t, a, key("enter"))
	if lab.made != 0 || !a.pasteEdit.open || !a.startingChat() {
		t.Fatal("editing a paste created a conversation")
	}
}

func TestNewChatHidesTheOldChatsCompactTaskStrip(t *testing.T) {
	a, _, _ := roomApp(t)
	railRun(a)
	a.width = 80
	a.closeRoom()
	made := 0
	startDoor(a, &made)
	openStart(t, a)
	if a.stripShowing() || a.stripHeight() != 0 || len(a.stripRows(a.width)) != 0 || len(a.stripSpans) != 0 {
		t.Fatal("new chat retained the old task strip")
	}
}
