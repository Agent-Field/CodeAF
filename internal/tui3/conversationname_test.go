package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/charmbracelet/x/ansi"
)

func TestConversationTabFollowsDraftThenOpeningPromptThenTitle(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.file = "/tmp/lab/unnamed.jsonl"
	a.width, a.height = 100, 35
	if tabs := a.tabList(); len(tabs) != 0 {
		t.Fatalf("empty session has tabs: %+v", tabs)
	}
	prompt := "explain how the parser handles unicode identifiers"
	a.input.setText(prompt)
	a.tabsRow(a.width)
	if tabs := a.tabList(); len(tabs) != 1 || tabs[0].word != prompt {
		t.Fatalf("draft tabs: %+v", tabs)
	}
	a.input.setText("")
	if tabs := a.tabList(); len(tabs) != 0 {
		t.Fatalf("erased draft retained a tab: %+v", tabs)
	}
	// Submit through the real input door, then type a different follow-up draft.
	a.input.setText(prompt)
	drive(t, a, key("enter"))
	a.input.setText("a different question")
	if tabs := a.tabList(); len(tabs) != 1 || tabs[0].word != prompt {
		t.Fatalf("first prompt was lost: %+v", tabs)
	}
	title := "understanding unicode identifiers in the parser"
	a.applyEvent(session.Event{Kind: session.EventTitleChanged, Text: title, ShortTitle: "legacy label"}, false)
	if tabs := a.tabList(); len(tabs) != 1 || tabs[0].word != title || tabs[0].full != title {
		t.Fatalf("generated title: %+v", tabs)
	}
}

func TestNewConversationPageNamesOnlyANonemptyDraft(t *testing.T) {
	lab := newStartLab(t)
	a := lab.a
	a.width, a.height = 100, 35
	openStart(t, a)
	assertStartTab := func(want string) {
		t.Helper()
		a.tabsRow(a.width)
		got := ""
		for _, h := range a.chatTabHits {
			if h.tab.start && h.kind != tabClose {
				got = h.tab.word
			}
		}
		if got != want {
			t.Fatalf("start tab = %q, want %q", got, want)
		}
	}
	assertStartTab("")
	prompt := "help me investigate this parser failure"
	a.input.setText(prompt)
	assertStartTab(prompt)
	a.input.setText("")
	assertStartTab("")
	a.input.setText(prompt)
	drive(t, a, key("enter"))
	if lab.made != 1 || a.conversationName() != prompt {
		t.Fatalf("submitted name = %q, creates = %d", a.conversationName(), lab.made)
	}
	side := a.detachConversation()
	if got := hopRawTitle(lab.next, side); got != prompt {
		t.Fatalf("held unnamed conversation = %q", got)
	}
	drive(t, a, runCmd(a.attachConversation(Conversation{Agent: lab.next, SessionFile: "/tmp/lab/two.jsonl", Workspace: "/tmp/lab"}, side))...)
	if got := a.conversationName(); got != prompt {
		t.Fatalf("reattached name = %q", got)
	}
}

func TestHomeAndTabsShareFullTitleAndTruncateWithThreeDots(t *testing.T) {
	a, _ := homeTabsFixture(t)
	title := "understanding unicode identifiers throughout the entire parser"
	a.setTitleEvent(title, "legacy label")
	open, _ := homeConversationLines(a)
	found := false
	for _, row := range open {
		if row.cell.title != title {
			continue
		}
		found = true
		line := plain(a.homeCellRow(row, -1, 28, a.pal, false)[0])
		if !strings.Contains(line, "...") || !strings.HasPrefix(line, "understanding") || ansi.StringWidth(line) > 28 {
			t.Fatalf("Home title = %q", line)
		}
	}
	if !found {
		t.Fatalf("full title absent from Home: %+v", open)
	}
	for _, width := range []int{1, 2, 3, 12, 25, 100} {
		label := tabLabel(chatTab{word: title}, width)
		if ansi.StringWidth(label) > width {
			t.Fatalf("tab exceeds %d cells: %q", width, label)
		}
		if width >= 12 && width < len(title) && !strings.Contains(label, "...") {
			t.Fatalf("missing three-dot suffix: %q", label)
		}
	}
}

func TestResumedUnnamedConversationUsesFirstPromptOutsideReplayTail(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	history := []session.DisplayEntry{{Role: "user", Text: "the original opening question about parsing"}}
	for i := 0; i < replayTail+20; i++ {
		history = append(history, session.DisplayEntry{Role: "assistant", Text: "later answer"})
	}
	a.replayList(history)
	if got := a.conversationName(); got != history[0].Text {
		t.Fatalf("opening prompt = %q", got)
	}
}
