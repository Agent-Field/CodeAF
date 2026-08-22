package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// ── THE TWO ZONES ABOVE THE LIST ────────────────────────────────────────────
//
// What needs a person, and what is running everywhere. These tests are about
// the four things that make the strips worth their rows: everything blocked is
// in one place whatever KIND of thing it is, the order is how long each has
// been standing still, a busy machine folds rather than fills the screen, and a
// row in a zone is the same live object as the row under its project — which is
// what lets a digit answer it from up here.

// zoneNames is what one zone's rows are called, in the order they are drawn.
func zoneNames(a *app, word string) []string {
	var out []string
	for _, line := range a.home.lines {
		if attentionWordOf(line) == word {
			out = append(out, line.zone.name)
		}
	}
	return out
}

// EVERY BLOCKED THING, ANY PROJECT, ANY KIND — AND THE LONGEST WAIT FIRST.
func TestTheNeedsYouZoneGathersEveryKindWaitedLongestFirst(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the newest chat", "/tmp/alpha", now)
	lab.session("-tmp-beta", "bbbb000000000001", "pricing research", "/tmp/beta", now.Add(-4*time.Hour))
	// Another window, stopped on a card three hours ago — the oldest wait here.
	question := consentQuestion(7, "needs your ok to run bash")
	question.Asked = now.Add(-3 * time.Hour)
	lab.asking("-tmp-beta", "bbbb000000000001", question, now)
	// Work that landed and cannot say whether it holds, half an hour ago.
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "1", SessionID: "aaaa000000000001", Title: "port the parser",
		Status: string(session.TaskUnverified), EndedAt: now.Add(-30 * time.Minute),
	})

	band := &standBand{}
	band.items = []standing.Item{bandItem("ask", "keep main green", "/tmp/alpha", standing.WhenProbe, "when CI goes red")}
	band.items[0].NeedsPerson = "the fix touches migrations"
	band.items[0].Updated = now.Add(-time.Hour)

	a := lab.app(mine)
	band.wire(a)
	a.openHome()

	names := zoneNames(a, attentionNeedsWord)
	want := []string{"Pricing Research", "keep main green", "port the parser"}
	if strings.Join(names, "|") != strings.Join(want, "|") {
		t.Fatalf("`needs you` reads %v, want %v:\n%s", names, want, homeText(a))
	}
	// And the strip is drawn above the first project heading.
	text := homeText(a)
	if at, heading := strings.Index(text, attentionNeedsWord), strings.Index(text, "alpha\n"); at < 0 || at > heading {
		t.Fatalf("the zone is not above the list (zone %d, heading %d):\n%s", at, heading, text)
	}
	// THE WORK THAT LANDED SAYS SO. Both rows wear `▲`, and only one of them is
	// a thing that finished.
	if !strings.Contains(text, homeLandedWord) {
		t.Fatalf("the landed task does not say it landed:\n%s", text)
	}
}

// A ROW MAY LIVE IN TWO ZONES. The waiting conversation is a `needs you` row and
// a row under its own project at the same moment, and neither is a copy: both
// carry the one [session.SessionRow] the reading holds.
func TestAZoneRowAndItsProjectRowAreTwoViewsOfOneThing(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the newest chat", "/tmp/alpha", now)
	row := lab.session("-tmp-beta", "bbbb000000000001", "pricing research", "/tmp/beta", now.Add(-time.Hour))
	lab.asking("-tmp-beta", "bbbb000000000001", consentQuestion(7, "needs your ok to run bash"), now)

	a := lab.app(mine)
	a.openHome()

	var zoned, listed int
	for _, line := range a.home.lines {
		if line.kind != homeSession || line.row.Transcript != row {
			continue
		}
		if line.zone != nil {
			zoned++
		} else {
			listed++
		}
	}
	if zoned != 1 || listed != 1 {
		t.Fatalf("the conversation has %d zone rows and %d list rows, want one of each:\n%s",
			zoned, listed, homeText(a))
	}
	// AND THE CURSOR STAYS WHERE IT WAS PUT. A rescan re-sorts the column under
	// it; a cursor standing in the list must not be lifted into the zone by the
	// restore, and one standing in the zone must not be dropped out of it.
	a.home.point(row)
	list := a.home.cursor
	if a.home.lines[list].zone != nil {
		t.Fatalf("pointing at a conversation landed in a zone rather than in the list")
	}
	a.refreshHome()
	if a.home.cursor != list {
		t.Fatalf("a rescan moved the cursor from the list row %d to %d", list, a.home.cursor)
	}
	a.home.cursor = list - 0
	for at, line := range a.home.lines {
		if line.zone != nil && line.kind == homeSession {
			a.home.cursor = at
			break
		}
	}
	stood := a.home.cursor
	a.refreshHome()
	if a.home.cursor != stood {
		t.Fatalf("a rescan moved the cursor out of the zone: %d became %d", stood, a.home.cursor)
	}
}

// FIVE AND A DOOR. A busy machine folds rather than spending the whole column
// on what is moving, and the fold is the same gesture as every other fold here.
func TestTheMovingZoneFoldsPastFiveAndOpensInPlace(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the newest chat", "/tmp/alpha", now)
	for i := 0; i < 7; i++ {
		id := "bbbb00000000000" + itoa(i+1)
		lab.session("-tmp-beta", id, "runner "+itoa(i+1), "/tmp/beta", now.Add(-time.Duration(i)*time.Minute))
		lab.presence("-tmp-beta", id, session.PresenceWorking, "", now)
	}
	a := lab.app(mine)
	a.openHome()

	if names := zoneNames(a, attentionMovingWord); len(names) != attentionMovingShown {
		t.Fatalf("`moving` drew %d rows, want %d: %v\n%s", len(names), attentionMovingShown, names, homeText(a))
	}
	fold := -1
	for at, line := range a.home.lines {
		if line.kind == homeAttentionMore {
			fold = at
		}
	}
	if fold < 0 {
		t.Fatalf("the moving zone hid rows without a door:\n%s", homeText(a))
	}
	if !strings.Contains(homeText(a), "…2 more") {
		t.Fatalf("the door does not say how many it stands for:\n%s", homeText(a))
	}
	a.home.cursor = fold
	a.homeEnter()
	if names := zoneNames(a, attentionMovingWord); len(names) != 7 {
		t.Fatalf("opening the fold drew %d rows, want 7: %v", len(names), names)
	}
	if !strings.Contains(homeText(a), "…2 fewer") {
		t.Fatalf("the open door is not the way back:\n%s", homeText(a))
	}
}

// STABLE GEOGRAPHY BEATS EMPTINESS, ON HOME ONLY. At the wide tier the two
// labels are always there — an empty `needs you` is the good news, said with
// space — and below it an empty zone vanishes whole.
func TestTheZoneLabelsHoldTheirGroundOverNothingAndVanishWhenNarrow(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the newest chat", "/tmp/alpha", now)
	a := lab.app(mine)
	a.width, a.height = 100, 24
	a.openHome()

	text := homeText(a)
	for _, word := range []string{attentionNeedsWord, attentionMovingWord} {
		if !strings.Contains(text, word) {
			t.Fatalf("a quiet machine dropped the %q label at the wide tier:\n%s", word, text)
		}
	}
	a.width = homeMinDetail - 1
	narrow := homeText(a)
	for _, word := range []string{attentionNeedsWord, attentionMovingWord} {
		if strings.Contains(narrow, word) {
			t.Fatalf("an empty %q zone still draws under %d columns:\n%s", word, homeMinDetail, narrow)
		}
	}
	// AND A ZONE WITH SOMETHING IN IT IS DRAWN AT EVERY WIDTH. What the narrow
	// frame drops is emptiness, never a blocked thing.
	lab.session("-tmp-beta", "bbbb000000000001", "pricing research", "/tmp/beta", now.Add(-time.Hour))
	lab.asking("-tmp-beta", "bbbb000000000001", consentQuestion(7, "needs your ok to run bash"), now)
	a.refreshHome()
	if narrow := homeText(a); !strings.Contains(narrow, attentionNeedsWord) {
		t.Fatalf("a narrow frame hid a zone that had something in it:\n%s", narrow)
	}
}

// A ZONE ROW IS A DOOR OF THE KIND IT ALWAYS WAS: the card beside it is that
// conversation's, and a digit over it answers the window it belongs to through
// the road the answer band already rides.
func TestADigitOnAZoneRowAnswersTheWindowItBelongsTo(t *testing.T) {
	lab := newAnswerLab(t, consentQuestion(7, "needs your ok to run bash"), time.Now())
	a := lab.a
	at := -1
	for i, line := range a.home.lines {
		if attentionWordOf(line) == attentionNeedsWord && line.kind == homeSession {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("the waiting conversation has no `needs you` row:\n%s", homeText(a))
	}
	a.home.cursor = at
	if !strings.Contains(homeText(a), "1 allow once") {
		t.Fatalf("the card beside the zone row is not the question's:\n%s", homeText(a))
	}
	a.homeKey(key("3"))
	if len(*lab.sent) != 1 {
		t.Fatalf("a digit over the zone row sent %d answers, want 1", len(*lab.sent))
	}
	if answer := (*lab.sent)[0]; answer.kind != session.QuestionConsent || answer.id != 7 || answer.key != "3" {
		t.Fatalf("the zone row answered %+v", answer)
	}
	// AND ENTER GOES WHERE ENTER WENT. The row's kind is a conversation's, so
	// enter walks the session door — which in this lab refuses, because the
	// conversation it names is the one another window is sitting on
	// ([app.homeOpenLine]'s third check). The refusal is the proof: a row that
	// was not a conversation's door would have folded something, or done nothing
	// at all.
	a.homeEnter()
	if a.home.msg != sessionBusyWord {
		t.Fatalf("enter on the zone row did not walk the conversation's door: %q", a.home.msg)
	}
}

// WAITING OUTRANKS WORKING, once more: a conversation stopped on a question is
// in `needs you` and nowhere else, however much work it has out.
func TestAWaitingConversationIsNotAlsoMoving(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the newest chat", "/tmp/alpha", now)
	lab.session("-tmp-beta", "bbbb000000000001", "pricing research", "/tmp/beta", now.Add(-time.Hour))
	lab.asking("-tmp-beta", "bbbb000000000001", consentQuestion(7, "needs your ok to run bash"), now)
	lab.task("-tmp-beta", session.TaskIndexEntry{
		ID: "1", SessionID: "bbbb000000000001", Title: "port the parser",
		Status: string(session.TaskRunning),
	})

	a := lab.app(mine)
	a.openHome()
	if names := zoneNames(a, attentionMovingWord); len(names) != 0 {
		t.Fatalf("a waiting conversation is also moving: %v\n%s", names, homeText(a))
	}
	if names := zoneNames(a, attentionNeedsWord); len(names) != 1 {
		t.Fatalf("`needs you` reads %v, want the one waiting conversation", names)
	}
}

// A CONVERSATION MID-TURN IS MOVING WITH NOTHING COMMISSIONED. The model
// thinking is work in flight, and it is the fact the presence file writes off
// the turn rather than off the task graph.
func TestAConversationMidTurnIsMovingWithNoTasksAtAll(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the newest chat", "/tmp/alpha", now)
	lab.session("-tmp-beta", "bbbb000000000001", "pricing research", "/tmp/beta", now.Add(-3*time.Minute))
	lab.presence("-tmp-beta", "bbbb000000000001", session.PresenceWorking, "", now)

	a := lab.app(mine)
	a.openHome()
	if names := zoneNames(a, attentionMovingWord); len(names) != 1 || names[0] != "Pricing Research" {
		t.Fatalf("`moving` reads %v, want the conversation mid-turn:\n%s", names, homeText(a))
	}
}

// A SEARCH HAS NO ZONES: the drop-up is the matches and the two action rows,
// and two strips of triage standing over a filter would be answering a question
// the person had stopped asking.
func TestTypingTakesTheZonesAway(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the newest chat", "/tmp/alpha", now)
	lab.session("-tmp-beta", "bbbb000000000001", "pricing research", "/tmp/beta", now.Add(-time.Hour))
	lab.asking("-tmp-beta", "bbbb000000000001", consentQuestion(7, "needs your ok to run bash"), now)

	a := lab.app(mine)
	a.openHome()
	if len(zoneNames(a, attentionNeedsWord)) != 1 {
		t.Fatal("the zone was not there to begin with")
	}
	a.homeKey(key("p"))
	if names := zoneNames(a, attentionNeedsWord); len(names) != 0 {
		t.Fatalf("a search kept its zones: %v\n%s", names, homeText(a))
	}
	if strings.Contains(homeText(a), attentionMovingWord) {
		t.Fatalf("a search kept a zone label:\n%s", homeText(a))
	}
}

// THE MARK CARRIES THE MEANING AND THE TEXT STAYS CALM: `▲` in the question hue,
// `●` in the still blue under it, and the name in ink either way.
func TestTheZoneMarksAreTheOnlyColouredCells(t *testing.T) {
	pal := newTestApp(&fakeAgent{model: "m"}).pal
	ask := attentionMark(pal, attentionNeedsWord)
	moving := attentionMark(pal, attentionMovingWord)
	if ask == homeAskGlyph || moving == homeLiveGlyph {
		t.Fatalf("a zone mark is unpainted: %q %q", ask, moving)
	}
	if ask == moving {
		t.Fatalf("the two zones wear the same paint: %q", ask)
	}
	if ask != pal.askBold(homeAskGlyph) {
		t.Fatalf("the needs-you mark is not in the question hue: %q", ask)
	}
	if moving != pal.muted(homeLiveGlyph) {
		t.Fatalf("the moving mark is not in the still hue: %q", moving)
	}
	// AND EVERY STRIP IN THE TABLE HAS ONE. A zone added later cannot ship
	// unpainted or unnamed, because the paint and the label are fields of the
	// thing rather than branches somewhere else.
	for _, zone := range homeZones {
		if zone.word == "" || zone.mark == nil || zone.ink == nil ||
			zone.gather == nil || zone.order == nil {
			t.Fatalf("the zone %q is not described whole", zone.word)
		}
		if zone.mark(true) == zone.mark(false) {
			t.Fatalf("the zone %q has no stand-in where box drawing cannot go", zone.word)
		}
	}
}
