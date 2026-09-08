package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/charmbracelet/x/ansi"
)

func TestClosingAWorkingTabKeepsEveryAnswerVisibleOnANarrowTerminal(t *testing.T) {
	for _, width := range []int{26, 38, 60, 120} {
		a, _, first := asyncApp(t)
		turning(a, first)
		a.askTabClose(a.frontChatTab())
		rows := a.tabCloseRows(width)
		plain := ansi.Strip(rows[1])
		for _, word := range []string{"keep", "stop", "cancel"} {
			if !strings.Contains(plain, word) {
				t.Fatalf("width %d lost %q: %q", width, word, plain)
			}
		}
		for _, span := range a.tabClose.spans {
			if span.from < 0 || span.to > width {
				t.Fatalf("width %d has an invisible answer: %+v", width, span)
			}
		}
	}
}

func TestHomeNamesAHiddenConversationHeldByThisWindowAsOpenHere(t *testing.T) {
	a, _, _ := asyncApp(t)
	file := "/tmp/lab/hidden.jsonl"
	a.behind = map[string]*kept{a.convKey(file): {conv: Conversation{Agent: newAsyncAgent(), SessionFile: file}}}
	row := session.SessionRow{Transcript: file, Open: true}
	if got := a.homeHolding(row); got != "open here" {
		t.Fatalf("held conversation says %q", got)
	}
	if a.homeHeld(row) {
		t.Fatal("the held conversation offers takeover from another window")
	}
}

func TestChatsNamesAHiddenRunningReplyAndRetainsItsCompletion(t *testing.T) {
	a, _, agent := asyncApp(t)
	watch := &behindWatch{}
	watch.turning.Store(true)
	held := &kept{conv: Conversation{Agent: agent, SessionFile: "/tmp/lab/hidden.jsonl"}, watch: watch}
	row := a.hopKept(held, a.now())
	if !row.moving || row.note != "working" {
		t.Fatalf("running reply: %+v", row)
	}
	watch.turning.Store(false)
	watch.finished.Add(1)
	for i := 0; i < 2; i++ {
		row = a.hopKept(held, a.now())
		if row.moving || row.note != hopLandedWord {
			t.Fatalf("completed reply: %+v", row)
		}
	}
}

// A typed account question holds one conversation, not navigation between them.
func TestTypedConnectionInputAllowsSafeTabCloseAndCancel(t *testing.T) {
	a, _, first := asyncApp(t)
	turning(a, first)
	a.askConnect(session.Event{Kind: session.EventConnectAsk, ConnectID: "typed", Service: "stripe", ServiceName: "Stripe", NeedsKey: true})
	a.connAsks[0].key = &editor{}
	a.connAsks[0].key.setText("unsent fixture")
	drive(t, a, key("ctrl+w"))
	if !a.closingTab() {
		t.Fatal("typed input swallowed the close-tab chord")
	}
	drive(t, a, key("esc"))
	if a.closingTab() || !a.entering() || a.connAsks[0].key.String() != "unsent fixture" {
		t.Fatal("cancel changed the pending input")
	}
}
