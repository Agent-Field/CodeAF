package tui3

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/history"
)

// The kill ring: what a whole-box clear keeps, and how the ↑ walk visits it.
// The ring's own arithmetic is proven against a bare draftRing; the doors that
// feed it and the walk that browses it are proven on an app, because that is
// where the mistakes they guard against happen.

func TestTheRingKeepsTheTenNewestKillsNewestFirst(t *testing.T) {
	var ring draftRing
	for i := range 12 {
		ring.push([]rune(fmt.Sprintf("kill %d", i)))
	}
	got := ring.list()
	if len(got) != draftRingSize {
		t.Fatalf("twelve kills left %d entries, want the cap of %d", len(got), draftRingSize)
	}
	if string(got[0].text) != "kill 11" || string(got[9].text) != "kill 2" {
		t.Fatalf("the ring reads %q … %q, want the ten newest, newest first",
			got[0].text, got[9].text)
	}
	if got[0].at.IsZero() {
		t.Fatal("a kill landed without the moment it happened")
	}
}

func TestABlankKillNeverLandsOnTheRing(t *testing.T) {
	var ring draftRing
	ring.push(nil)
	ring.push([]rune(""))
	ring.push([]rune("  \n "))
	if got := ring.list(); len(got) != 0 {
		t.Fatalf("blank boxes left %d entries — there were no words to keep", len(got))
	}
}

func TestTheRingRefreshesRatherThanRepeatsTheNewestKill(t *testing.T) {
	var ring draftRing
	ring.push([]rune("same words"))
	ring.push([]rune("other words"))
	ring.push([]rune("same words")) // not the newest: a loss of its own
	ring.push([]rune("same words")) // the newest again: a refresh, not a copy
	if got := ring.list(); len(got) != 3 {
		t.Fatalf("a repeated kill grew the ring to %d entries", len(got))
	}
}

func TestARingDropTakesTheNewestFirstPosition(t *testing.T) {
	var ring draftRing
	ring.push([]rune("one"))
	ring.push([]rune("two")) // newest first: two, one
	ring.drop(0)
	got := ring.list()
	if len(got) != 1 || string(got[0].text) != "one" {
		t.Fatalf("drop(0) left %+v, want only the older kill", got)
	}
	// Off the ends is a no-op: a drop answers a position somebody browsed to,
	// and a position that does not exist drops nothing.
	ring.drop(-1)
	ring.drop(5)
	if got := ring.list(); len(got) != 1 {
		t.Fatalf("an out-of-range drop lost entries: %d left", len(got))
	}
}

func TestAConsumeForgetsTheNewestMatchingWords(t *testing.T) {
	var ring draftRing
	ring.push([]rune("a"))
	ring.push([]rune("b"))
	ring.push([]rune("a")) // a second, newer loss of the same words
	if !ring.consume("a") {
		t.Fatal("a sent draft was not consumed")
	}
	got := ring.list()
	if len(got) != 2 || string(got[0].text) != "b" || string(got[1].text) != "a" {
		t.Fatalf("the consume took the wrong entry: %+v", got)
	}
	if !ring.consume(" a ") || ring.consume("a") {
		// The match is on the trimmed words, and only the one newest copy goes.
		t.Fatal("the consume did not trim, or took more than one copy")
	}
	if got := ring.list(); len(got) != 1 || string(got[0].text) != "b" {
		t.Fatalf("the consumes left %+v", got)
	}
}

// boxText is the draft box as painted rows, ANSI and all: dim is the walk's
// one signal, and it lives in the paint rather than in the words.
func boxText(a *app) string {
	block, _, _ := a.inputBlock(a.width)
	return strings.Join(block, "\n")
}

func TestAWholeBoxFlushLandsOnTheRing(t *testing.T) {
	a, _ := recallApp(t)
	typeInto(t, a, "half a sentence")
	drive(t, a, key("ctrl+u"))
	if got := a.input.String(); got != "" {
		t.Fatalf("ctrl+u left %q in the box", got)
	}
	entries := a.drafts.list()
	if len(entries) != 1 || string(entries[0].text) != "half a sentence" {
		t.Fatalf("the flush landed on the ring as %+v", entries)
	}

	// A kill that takes only the head of a line leaves the rest of the draft
	// standing, and a draft still standing is nothing the ring is about.
	typeInto(t, a, "hello")
	drive(t, a, key("left"), key("left"))
	drive(t, a, key("ctrl+u"))
	if got := a.input.String(); got != "lo" {
		t.Fatalf("a line-head kill left %+v", got)
	}
	if got := len(a.drafts.list()); got != 1 {
		t.Fatalf("a partial kill grew the ring to %d entries", got)
	}

	// And one line of two is the same gesture, not a flush.
	a, _ = recallApp(t)
	typeInto(t, a, "one")
	drive(t, a, keyed("alt+enter"))
	typeInto(t, a, "two")
	drive(t, a, key("ctrl+u"))
	if got := a.input.String(); got != "one\n" {
		t.Fatalf("ctrl+u on the second line left %q", got)
	}
	if got := len(a.drafts.list()); got != 0 {
		t.Fatalf("a line kill on a multi-line draft grew the ring to %d entries", got)
	}
}

func TestTheWalkVisitsKilledDraftsDimInFrontOfSentHistory(t *testing.T) {
	a, _ := recallApp(t,
		history.Entry{Text: "somewhere else", Cwd: "/tmp/other"},
		history.Entry{Text: "older here", Cwd: "/tmp/lab"},
		history.Entry{Text: "newer here", Cwd: "/tmp/lab"},
	)
	typeInto(t, a, "half a sentence")
	drive(t, a, key("ctrl+u")) // the kill: the words go on the ring
	typeInto(t, a, "a live draft")

	// The killed sentence leads the walk, in front of the newest SENT line.
	for _, want := range []string{"half a sentence", "newer here", "older here", "somewhere else"} {
		drive(t, a, key("up"))
		if a.input.String() != want {
			t.Fatalf("the walk shows %q, want %q", a.input.String(), want)
		}
		if want == "half a sentence" && !strings.Contains(boxText(a), a.pal.dim(want)) {
			t.Fatalf("a killed draft did not draw dim:\n%s", boxText(a))
		}
		if want == "newer here" && !strings.Contains(boxText(a), a.pal.ink(want)) {
			t.Fatalf("a sent line drew as anything but the body's ink:\n%s", boxText(a))
		}
	}

	// Back down past the newest entry is the end of the walk, and the live
	// draft is exactly where it was — the kill's entry stays on the ring.
	drive(t, a, key("down"), key("down"), key("down"))
	if a.input.String() != "half a sentence" {
		t.Fatalf("walking back shows %q", a.input.String())
	}
	drive(t, a, key("down"))
	if a.input.String() != "a live draft" || a.recalling() {
		t.Fatalf("the walk did not end on the live draft: %q", a.input.String())
	}
	if got := len(a.drafts.list()); got != 1 {
		t.Fatalf("browsing consumed the entry: %d left", got)
	}
}

func TestSendingAKilledDraftConsumesIt(t *testing.T) {
	a, store := recallApp(t)
	// The first conversation's greeting owns ↑/↓ over an empty box
	// (welcome.go's [app.welcomeStarterKey]) — the walk's own reach, unchanged
	// by the ring — so it is put away the way a first send would put it.
	a.dismissWelcome()
	typeInto(t, a, "the words I lost")
	drive(t, a, key("ctrl+u"))
	drive(t, a, key("up"))
	if a.input.String() != "the words I lost" {
		t.Fatalf("the walk did not open on the killed draft: %q", a.input.String())
	}
	drive(t, a, key("enter"))
	if got := len(a.drafts.list()); got != 0 {
		t.Fatalf("a sent draft stayed on the ring: %d entries left", got)
	}
	if got := store.RecentFor("/tmp/lab", 5); len(got) != 1 || got[0].Text != "the words I lost" {
		t.Fatalf("the history holds %+v", got)
	}

	// And the next walk finds the sentence once, from the history, in the
	// body's own ink — a line that went is not drawn like one that did not.
	drive(t, a, key("up"))
	if a.input.String() != "the words I lost" {
		t.Fatalf("the walk lost the sent line: %q", a.input.String())
	}
	if strings.Contains(boxText(a), a.pal.dim("the words I lost")) {
		t.Fatalf("a sent line drew dim:\n%s", boxText(a))
	}
}

func TestEscOnAKilledDraftLeavesItOnTheRing(t *testing.T) {
	a, _ := recallApp(t)
	typeInto(t, a, "killed words")
	drive(t, a, key("ctrl+u"))
	typeInto(t, a, "mine")
	drive(t, a, key("up"))
	drive(t, a, key("esc"))
	if a.input.String() != "mine" || a.recalling() {
		t.Fatalf("esc left %q (recalling=%v)", a.input.String(), a.recalling())
	}
	if got := len(a.drafts.list()); got != 1 {
		t.Fatalf("esc consumed the entry: %d left", got)
	}
	drive(t, a, key("up"))
	if a.input.String() != "killed words" {
		t.Fatalf("the next walk did not lead with the killed draft: %q", a.input.String())
	}
}

func TestASwitchRingsTheSentenceItPutsDown(t *testing.T) {
	a, _ := recallApp(t)
	typeInto(t, a, "half a sentence")
	side := a.detachConversation()
	if side.draft != "half a sentence" {
		t.Fatalf("the sidecar kept %q", side.draft)
	}
	entries := a.drafts.list()
	if len(entries) != 1 || string(entries[0].text) != "half a sentence" {
		t.Fatalf("the switch rang %+v", entries)
	}

	// Away and back is one entry, not two: the ring refreshes a repeat of the
	// newest kill rather than filling the walk with copies of one sentence.
	a.input.setText(side.draft)
	_ = a.detachConversation()
	if got := len(a.drafts.list()); got != 1 {
		t.Fatalf("the same sentence switched twice is %d entries", got)
	}
}
