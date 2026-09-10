package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/charmbracelet/x/ansi"
)

// NO ANSWER IS EVER CUT, at any width this surface draws at. The card is the
// question block's now (question.go), which puts every answer on a row of its
// own and degrades by giving up its TAIL — so the three answers survive a
// twenty-six-column frame that the old one-row layout had to shorten them for.
func TestClosingAWorkingTabKeepsEveryAnswerVisibleOnANarrowTerminal(t *testing.T) {
	for _, width := range []int{26, 38, 60, 120} {
		a, _, first := asyncApp(t)
		a.width = width
		turning(a, first)
		a.askTabClose(a.frontChatTab())
		rows := ansi.Strip(strings.Join(a.questionRows(width), "\n"))
		for _, word := range []string{"keep running", "stop work", "cancel"} {
			if !strings.Contains(rows, word) {
				t.Fatalf("width %d lost %q:\n%s", width, word, rows)
			}
		}
		for _, band := range a.questionBands {
			if band.span.from < 0 || band.span.to > width {
				t.Fatalf("width %d has an unreachable answer: %+v", width, band)
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
	// The typed answer goes into the MESSAGE BOX now, which is what the question
	// owning the box means ([questionOwnsBox]) — so this is the half-typed key,
	// where it actually lives.
	a.input.setText("unsent fixture")
	drive(t, a, key("ctrl+w"))
	if !a.closingTab() {
		t.Fatal("typed input swallowed the close-tab chord")
	}
	drive(t, a, key("esc"))
	if a.closingTab() || !a.entering() || a.input.String() != "unsent fixture" {
		t.Fatal("cancel changed the pending input")
	}
}

// Home can cover the last tab without starting a keeper; its attention is local.
func TestAccountInputKeepsItsAttentionWhenHomeCoversTheLastTab(t *testing.T) {
	a, _, first := asyncApp(t)
	turning(a, first)
	a.askConnect(session.Event{Kind: session.EventConnectAsk, ConnectID: "typed", Service: "stripe", NeedsKey: true})
	if a.frontSignal() != tabNeedsPerson {
		t.Fatal("account input was labelled working instead of needing a person")
	}
}
