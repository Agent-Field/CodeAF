package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/manual"
)

// ── HOME'S ROW: THE SAME TIPS, THE OTHER BOX ─────────────────────────────────
//
// The conversation's foot has carried earned hints since notice.go was written;
// home, the other box a person types into, said nothing. These pin the row
// above home's rule: it says a tip over an idle box, says nothing while the box
// is being typed into, moves on every visit and every [homeHintEvery] at rest,
// and a tip spent on either box is spent on both.

// The frame's row directly above the rule is the tip, and only over an empty
// box with nothing else up.
func TestHomeRowSaysATipOverAnEmptyBox(t *testing.T) {
	lab := newHomeLab(t)
	a := lab.door("")
	a.key(key("esc"))
	if !a.at(pageHome) {
		t.Fatal("esc did not open home")
	}
	tip := a.noticeHomeHint()
	if tip == "" {
		t.Fatalf("home opened with nothing on its row; the slot holds %q", a.notices.current[slotHome])
	}
	if !strings.Contains(homeText(a), tip) {
		t.Fatalf("the tip is not on the frame:\n%s", homeText(a))
	}
	if !strings.HasPrefix(tip, "/") && !strings.HasPrefix(tip, "ctrl+") && !strings.HasPrefix(tip, "alt+") &&
		!strings.HasPrefix(tip, "opt+") && !strings.HasPrefix(tip, "@") && !strings.HasPrefix(tip, "ask ") {
		t.Fatalf("the tip does not open with the key or the command: %q", tip)
	}

	// A BOX WITH WORDS IN IT IS THE SENTENCE'S. The row goes blank and comes
	// back when the box is empty again.
	a.key(key("x"))
	if got := a.noticeHomeHint(); got != "" {
		t.Fatalf("a tip drew over a box with words in it: %q", got)
	}
	if strings.Contains(homeText(a), tip) {
		t.Fatalf("the tip is still on the frame over a typed box:\n%s", homeText(a))
	}
	a.key(key("backspace"))
	if got := a.noticeHomeHint(); got != tip {
		t.Fatalf("the row reads %q after the box emptied, want %q", got, tip)
	}

	// AND THE COMMAND LIST OUTRANKS IT, the way every list does.
	a.key(key("/"))
	if got := a.noticeHomeHint(); got != "" {
		t.Fatalf("a tip drew under the command list: %q", got)
	}
	a.key(key("backspace"))

	// OFF IS OFF. The Display tab's row silences this slot with the other.
	a.notices.enabled = false
	if got := a.noticeHomeHint(); got != "" {
		t.Fatalf("a silenced profile still says %q on home", got)
	}
	if strings.Contains(homeText(a), tip) {
		t.Fatal("a silenced tip is still drawn")
	}
}

// Every road home moves the row on; so does the beat once a tip has stood
// [homeHintEvery] at rest — and neither moves it while the box is being typed
// into, because a tip nobody could read has not been shown.
func TestHomeRowMovesOnEveryVisitAndAtRest(t *testing.T) {
	lab := newHomeLab(t)
	a := lab.door("")
	now := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	a.clock = func() time.Time { return now }

	a.showPage(pageHome)
	first := a.notices.current[slotHome]
	if first == "" {
		t.Fatal("the first visit put nothing on the row")
	}
	a.showPage(pageHome)
	second := a.notices.current[slotHome]
	if second == first || second == "" {
		t.Fatalf("a second visit left %q standing", second)
	}

	// AT REST THE BEAT MOVES IT, but not before its time.
	now = now.Add(homeHintEvery - time.Second)
	a.noticeHomeBeat()
	if got := a.notices.current[slotHome]; got != second {
		t.Fatalf("the beat moved the row early, to %q", got)
	}
	now = now.Add(2 * time.Second)
	a.noticeHomeBeat()
	third := a.notices.current[slotHome]
	if third == second || third == "" {
		t.Fatalf("the beat left %q standing past its time", third)
	}

	// A BOX BEING TYPED INTO DOES NOT AGE THE ROW.
	a.key(key("x"))
	now = now.Add(2 * homeHintEvery)
	a.noticeHomeBeat()
	if got := a.notices.current[slotHome]; got != third {
		t.Fatalf("the beat moved the row under a typed box, to %q", got)
	}
	a.key(key("backspace"))

	// AND THE RING COMES ROUND: every eligible tip has its turn before any
	// repeats, in the table's order.
	seen := map[string]bool{first: true, second: true, third: true}
	eligible := 0
	for _, n := range notices {
		if n.draws(slotHome) && n.armed(a) {
			eligible++
		}
	}
	for i := 3; i < eligible; i++ {
		a.showPage(pageHome)
		id := a.notices.current[slotHome]
		if seen[id] {
			t.Fatalf("visit %d repeated %q before the ring came round (%d eligible)", i+1, id, eligible)
		}
		seen[id] = true
	}
	a.showPage(pageHome)
	if got := a.notices.current[slotHome]; got != first {
		t.Fatalf("after the whole ring the row holds %q, want %q again", got, first)
	}
}

// A tip retired from home is retired from the conversation's foot as well, and
// the row moves on at once rather than standing empty.
func TestATipSpentOnHomeIsSpentEverywhere(t *testing.T) {
	lab := newHomeLab(t)
	a := lab.door("")
	a.showPage(pageHome)
	id := a.notices.current[slotHome]
	var row notice
	for _, n := range notices {
		if n.id == id {
			row = n
		}
	}
	if row.id == "" || row.retire == "" {
		t.Fatalf("home holds %q, which has no gesture to retire it", id)
	}
	a.noticeEvent(row.retire)
	if !a.notices.retired(id) {
		t.Fatalf("%q was not retired by %q", id, row.retire)
	}
	if got := a.notices.current[slotHome]; got == id || got == "" {
		t.Fatalf("home's row holds %q after the gesture", got)
	}
	if got := a.notices.current[slotHint]; got == id {
		t.Fatalf("the conversation's slot still holds %q after the gesture", id)
	}
	a.showPage(pageHome)
	a.showPage(pageHome)
	a.showPage(pageHome)
	for i := 0; i < len(notices); i++ {
		if a.notices.current[slotHome] == id {
			t.Fatal("a retired tip came back round on home")
		}
		a.showPage(pageHome)
	}
}

// Home counts every turn of its rotation as a showing, and a tip that has come
// round [homeShownDefault] times is taken as read.
func TestHomeCountsEveryTurnOfItsRotation(t *testing.T) {
	b := bareNoticeBoard()
	cands := []noticeCandidate{{id: "a", armed: true}, {id: "b", armed: true}}
	turns := map[string]int{}
	for i := 0; i < 2*homeShownDefault; i++ {
		b.homeAdvance = true
		id := b.pick(slotHome, cands, 0)
		if id == "" {
			t.Fatalf("turn %d put nothing on the row", i)
		}
		b.take(slotHome, id, homeShownDefault, 0)
		turns[id]++
	}
	if turns["a"] != homeShownDefault || turns["b"] != homeShownDefault {
		t.Fatalf("the ring did not share the turns evenly: %v", turns)
	}
	if !b.retired("a") || !b.retired("b") {
		t.Fatalf("after %d turns each the tips are not retired: %+v", homeShownDefault, b.ledger)
	}
	b.homeAdvance = true
	if got := b.pick(slotHome, cands, 0); got != "" {
		t.Fatalf("a retired tip came back: %q", got)
	}
}

// A tip that stops being eligible stands down at once and the next takes over,
// without waiting for a visit — and an event between visits otherwise leaves
// the row alone.
func TestHomeRowHoldsBetweenVisitsAndYieldsWhenSpent(t *testing.T) {
	b := bareNoticeBoard()
	cands := []noticeCandidate{{id: "a", armed: true}, {id: "b", armed: true}, {id: "c", armed: true}}
	b.homeAdvance = true
	if got := b.pick(slotHome, cands, 0); got != "a" {
		t.Fatalf("the ring did not start at the top: %q", got)
	}
	b.take(slotHome, "a", homeShownDefault, 0)
	// An event with nothing advancing keeps the one standing.
	if got := b.pick(slotHome, cands, 0); got != "a" {
		t.Fatalf("an event moved the row without a visit, to %q", got)
	}
	// The one standing retiring hands the row to the next in the ring.
	b.retire("a")
	if got := b.pick(slotHome, cands, 0); got != "b" {
		t.Fatalf("a spent tip did not yield to the next: %q", got)
	}
	// And with nothing eligible the row is empty rather than stale.
	for _, c := range cands {
		b.retire(c.id)
	}
	if got := b.pick(slotHome, cands, 0); got != "" {
		t.Fatalf("an empty ring still says %q", got)
	}
}

// THE MANUAL LAW, said for the tips: every line the table can draw is on the
// hints page word for word, so a person who asks the chat what a tip meant is
// answered from the page rather than improvised at.
func TestEveryTipIsOnTheManualPage(t *testing.T) {
	for _, n := range notices {
		if n.slot != slotHint || n.text == "" {
			continue
		}
		if !manual.Chat().Mentions(n.text) {
			t.Errorf("the hints page does not carry the tip %q (notice %q)", n.text, n.id)
		}
	}
}

// The cut was thirty, and the row above home's rule draws only rows that name
// it: a note row that names a box, or a row filed under home's slot, does not
// build.
func TestTheTableIsThirtyHintsAndEachNamesItsBoxes(t *testing.T) {
	hints, home, chat := 0, 0, 0
	for _, n := range notices {
		if n.slot != slotHint {
			continue
		}
		hints++
		if n.draws(slotHome) {
			home++
		}
		if n.draws(slotHint) {
			chat++
		}
	}
	if hints != 30 {
		t.Fatalf("the table holds %d hints, want 30 — the cut is deliberate, and the manual page counts them", hints)
	}
	if home == 0 || chat == 0 {
		t.Fatalf("%d hints draw on home and %d in a conversation; both boxes need some", home, chat)
	}
	// A hint that names no box is the conversation's, which is what every row
	// meant before home had a row.
	plain := notice{id: "boxless", slot: slotHint, armed: ready, text: "x"}
	if !plain.draws(slotHint) || plain.draws(slotHome) {
		t.Fatal("a hint naming no box is not the conversation's alone")
	}
	if err := checkNotices([]notice{{id: "noted", slot: slotNote, place: onHome, armed: ready, text: "x"}}); err == nil {
		t.Fatal("a note naming a box was accepted")
	}
	if err := checkNotices([]notice{{id: "filed", slot: slotHome, armed: ready, text: "x"}}); err == nil {
		t.Fatal("a row filed under home's slot was accepted")
	}
}
