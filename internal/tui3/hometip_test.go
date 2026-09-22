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
// is being typed into, moves on every visit and every [hintEvery] at rest,
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
		!strings.HasPrefix(tip, "opt+") && !strings.HasPrefix(tip, "@") && !strings.HasPrefix(tip, "enter ") {
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

	// OFF IS OFF. The Workspace tab's row silences this slot with the other.
	a.notices.enabled = false
	if got := a.noticeHomeHint(); got != "" {
		t.Fatalf("a silenced profile still says %q on home", got)
	}
	if strings.Contains(homeText(a), tip) {
		t.Fatal("a silenced tip is still drawn")
	}
}

// Every road home moves the row on; so does the beat once a tip has stood
// [hintEvery] at rest — and neither moves it while the box is being typed
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
	now = now.Add(hintEvery - time.Second)
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
	now = now.Add(2 * hintEvery)
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

// A tip retired from home is retired from the conversation's row as well, and
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

// Every turn of a row's rotation that stood long enough to be read is a
// showing, on either box, and a tip that has come round [noticeShownDefault]
// times that way is taken as read.
func TestEveryTurnOfTheRotationThatStoodIsAShowing(t *testing.T) {
	for _, slot := range []noticeSlot{slotHome, slotHint} {
		b := bareNoticeBoard()
		now := time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC)
		limit := func(string) int { return noticeShownDefault }
		cands := []noticeCandidate{{id: "a", armed: true}, {id: "b", armed: true}}
		turns := map[string]int{}
		for i := 0; i < 2*noticeShownDefault; i++ {
			b.advance[slot] = true
			id := b.pick(slot, cands)
			if id == "" {
				t.Fatalf("turn %d put nothing on the row", i)
			}
			b.take(slot, id, true, now, limit)
			turns[id]++
			now = now.Add(noticeReadTime)
		}
		b.settle(slot, now, limit)
		if turns["a"] != noticeShownDefault || turns["b"] != noticeShownDefault {
			t.Fatalf("the ring did not share the turns evenly: %v", turns)
		}
		if !b.retired("a") || !b.retired("b") {
			t.Fatalf("after %d turns each the tips are not retired: %+v", noticeShownDefault, b.ledger)
		}
		b.advance[slot] = true
		if got := b.pick(slot, cands); got != "" {
			t.Fatalf("a retired tip came back: %q", got)
		}
	}
}

// A tip that stops being eligible stands down at once and the next takes over,
// without waiting for a visit — and an event between visits otherwise leaves
// the row alone.
func TestARowHoldsBetweenVisitsAndYieldsWhenSpent(t *testing.T) {
	b := bareNoticeBoard()
	cands := []noticeCandidate{{id: "a", armed: true}, {id: "b", armed: true}, {id: "c", armed: true}}
	b.advance[slotHome] = true
	if got := b.pick(slotHome, cands); got != "a" {
		t.Fatalf("the ring did not start at the top: %q", got)
	}
	b.take(slotHome, "a", true, time.Now(), func(string) int { return noticeShownDefault })
	// An event with nothing advancing keeps the one standing.
	if got := b.pick(slotHome, cands); got != "a" {
		t.Fatalf("an event moved the row without a visit, to %q", got)
	}
	// The one standing retiring hands the row to the next in the ring.
	b.retire("a")
	if got := b.pick(slotHome, cands); got != "b" {
		t.Fatalf("a spent tip did not yield to the next: %q", got)
	}
	// And with nothing eligible the row is empty rather than stale.
	for _, c := range cands {
		b.retire(c.id)
	}
	if got := b.pick(slotHome, cands); got != "" {
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

// The cut was thirty and /project made it thirty-one, and there is ONE set:
// every hint draws on both boxes, a news row on neither, and a row filed under
// home's slot does not build.
func TestTheTableIsThirtyOneHintsAndEveryOneDrawsOnBothBoxes(t *testing.T) {
	hints := 0
	for _, n := range notices {
		if n.slot != slotHint {
			continue
		}
		hints++
		if !n.draws(slotHome) || !n.draws(slotHint) {
			t.Errorf("hint %q does not draw on both boxes", n.id)
		}
		if n.draws(slotNote) {
			t.Errorf("hint %q draws in the transcript", n.id)
		}
	}
	if hints != 31 {
		t.Fatalf("the table holds %d hints, want 31 — the cut is deliberate, and the manual page counts them", hints)
	}
	news := notice{id: "noted", slot: slotNote, armed: ready, text: "x"}
	if news.draws(slotHint) || news.draws(slotHome) || !news.draws(slotNote) {
		t.Fatal("a news row draws beside a box")
	}
	if err := checkNotices([]notice{{id: "filed", slot: slotHome, armed: ready, text: "x"}}); err == nil {
		t.Fatal("a row filed under home's slot was accepted")
	}
}

// ── the cross ───────────────────────────────────────────────────────────────

// THE CROSS MEANS "NOT THIS ONE, SAY SOMETHING ELSE": the row answers with the
// next tip in the rotation, and the tip put away is charged NOTHING — however
// long it had been standing when the cross was pressed. It used to be charged
// a showing and the row went blank, so six presses retired a tip nobody had
// read and the next visit to home brought the same sentence straight back.
func TestTheCrossMovesTheRowOnAndSpendsNothingOfTheTipItPutAway(t *testing.T) {
	lab := newHomeLab(t)
	a := lab.door("")
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	a.clock = func() time.Time { return now }
	a.showPage(pageHome)

	was := a.notices.current[slotHome]
	if was == "" {
		t.Fatal("home opened with no tip")
	}
	// LONG ENOUGH TO HAVE BEEN READ, which is the case that used to cost a
	// showing: the gesture says the opposite of "I have read this".
	now = now.Add(noticeReadTime * 3)
	a.noticeDismiss(slotHome)

	if got := a.notices.current[slotHome]; got == was {
		t.Fatalf("the cross left %q standing", got)
	}
	if a.noticeHomeHint() == "" {
		t.Fatal("the cross left the row blank with other tips still to say")
	}
	if got := a.notices.ledger.shown(was); got != 0 {
		t.Fatalf("the cross spent %d showings of the tip it put away", got)
	}
	if a.notices.retired(was) {
		t.Fatalf("the cross retired %q", was)
	}

	// AND IT COMES ROUND AGAIN. The ring is a ring: walk it and the tip that
	// was put away takes its turn like every other row.
	seen := false
	for i := 0; i <= len(notices); i++ {
		a.noticeHomeRotate()
		if a.notices.current[slotHome] == was {
			seen = true
			break
		}
	}
	if !seen {
		t.Fatalf("%q never came back round after its cross was pressed", was)
	}
}

// AND WITH NOTHING ELSE TRUE TO SAY THE ROW GOES BLANK. Drawing the same
// sentence again under the cross somebody just pressed would be the surface
// arguing, so the one-eligible-tip case keeps the old behaviour: hidden until
// the slot next changes hands.
func TestTheCrossBlanksTheRowWhenItIsTheLastTipStanding(t *testing.T) {
	lab := newHomeLab(t)
	a := lab.door("")
	a.showPage(pageHome)

	// Retire every tip but the one standing, so the ring is that one tip.
	last := a.notices.current[slotHome]
	if last == "" {
		t.Fatal("home opened with no tip")
	}
	for _, n := range notices {
		if n.id != last {
			a.notices.retire(n.id)
		}
	}
	a.noticeHomeRotate()
	if got := a.notices.current[slotHome]; got != last {
		t.Fatalf("the last tip standing is %q, want %q", got, last)
	}

	a.noticeDismiss(slotHome)
	if got := a.noticeHomeHint(); got != "" {
		t.Fatalf("the cross redrew the only tip there was: %q", got)
	}
	if a.notices.retired(last) {
		t.Fatal("the cross retired the last tip standing")
	}
}

// A TIP ABOUT A COMMAND ONLY HOME HAS IS ONLY ARMED ON HOME. One list feeds
// both boxes, so `/project sets the folder the next conversation opens in`
// over a conversation's box would be teaching a command that answers there by
// pointing back at home.
func TestTheProjectTipStandsOnHomeAndNowhereElse(t *testing.T) {
	lab := newHomeLab(t)
	a := lab.door("")

	a.showPage(pageHome)
	if !onHome(a) {
		t.Fatal("the home-only rule is not armed on home")
	}
	seen := false
	for i := 0; i <= len(notices); i++ {
		if a.notices.current[slotHome] == "pick-a-project" {
			seen = true
			break
		}
		a.noticeHomeRotate()
	}
	if !seen {
		t.Fatal("the /project tip never came round on home")
	}

	// AND IT STANDS DOWN THE MOMENT HOME IS NOT IN FRONT. The conversation's
	// row is decided again at the next event, and the rule is false there.
	runCmd(a.showPage(pageNone))
	if a.at(pageHome) {
		t.Fatal("the conversation did not come back to the frame")
	}
	if onHome(a) {
		t.Fatal("the home-only rule is armed in a conversation")
	}
	a.noticeEvent(eventTurnEnded)
	for slot, id := range a.notices.current {
		if id == "pick-a-project" {
			t.Fatalf("the /project tip is standing in slot %d off home", slot)
		}
	}
}

// AND TAKING A PROJECT RETIRES IT, by either form of the command.
func TestTakingAProjectRetiresItsTip(t *testing.T) {
	for _, take := range []struct {
		name string
		do   func(*app, string)
	}{
		{"a path after it", func(a *app, dir string) { runCmd(a.homeSlash("/project " + dir)) }},
		{"the browser it opens", func(a *app, _ string) { runCmd(a.homeSlash("/project")) }},
	} {
		t.Run(take.name, func(t *testing.T) {
			lab := newHomeLab(t)
			a := lab.door("")
			a.showPage(pageHome)
			if a.notices.retired("pick-a-project") {
				t.Fatal("the tip was retired before the command ran")
			}
			take.do(a, t.TempDir())
			if !a.notices.retired("pick-a-project") {
				t.Fatal("taking a project did not retire its tip")
			}
		})
	}
}

// A TIP BEHIND A BLANK ROW IS NOT BEING SHOWN. Once the cross has hidden a
// row — the one-eligible-tip case — the events that go on re-deciding the slot
// may not start a standing for a sentence nobody can read, or the tip left
// there would spend its six showings on a row that draws nothing.
func TestATipBehindAHiddenRowStandsForNothing(t *testing.T) {
	lab := newHomeLab(t)
	a := lab.door("")
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	a.clock = func() time.Time { return now }
	a.showPage(pageHome)

	last := a.notices.current[slotHome]
	if last == "" {
		t.Fatal("home opened with no tip")
	}
	for _, n := range notices {
		if n.id != last {
			a.notices.retire(n.id)
		}
	}
	a.noticeHomeRotate()
	a.noticeDismiss(slotHome)
	if a.noticeHomeHint() != "" {
		t.Fatal("the cross did not blank the row")
	}

	// AN HOUR OF EVENTS OVER A BLANK ROW. Each one re-decides the slot.
	for i := 0; i < noticeShownDefault*3; i++ {
		now = now.Add(noticeReadTime * 2)
		a.noticeEvent(eventTurnEnded)
	}
	if got := a.notices.ledger.shown(last); got != 0 {
		t.Fatalf("a tip behind a blank row was shown %d times", got)
	}
	if a.notices.retired(last) {
		t.Fatal("a tip behind a blank row retired itself")
	}
}
