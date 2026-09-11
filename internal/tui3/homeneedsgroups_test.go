package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── `needs you`, IN TWO GROUPS (#884, the spec of record §1) ────────────────
//
// The panel holds the live questions that have stopped something, then — under
// a dim line of their own — the landings nobody has checked. These tests pin
// the rules that are easy to break from the other side of the column: what the
// group line is, which row takes a key, and what `since you left` may say about
// a landing this panel is already showing.

// openAt is the live lab at a frame of a chosen size, which is what the squeeze
// needs.
func (l *liveLab) openAt(width, height int) *app {
	l.t.Helper()
	a := l.app(l.mine)
	a.width, a.height = width, height
	a.leaveAnswer = func(string, session.QuestionKind, uint64, string) error { return nil }
	a.openHome()
	homeText(a)
	return a
}

// landed writes one landing whose call is the person's.
func (l *liveLab) landed(id, label string, ago time.Duration) {
	l.t.Helper()
	l.task("-alpha", session.TaskIndexEntry{ID: id, SessionID: "aaaa000000000002", Label: label, Title: label,
		Status: string(session.TaskUnverified), EndedAt: l.now.Add(-ago)})
}

// THE GROUP LINE IS FURNITURE AND NOT A STOP: the cursor walks from the last
// question straight onto the first landing, and the blank over the group line
// is not a stop either.
func TestTheToCheckGroupLineIsNotACursorStop(t *testing.T) {
	l := newLiveLab(t)
	l.live("-beta", "bbbb000000000001", session.SessionPresence{State: session.PresenceWaiting,
		Question: consentQuestionAt(7, "needs your ok to run bash", l.now.Add(-2*time.Hour))})
	l.landed("4", "fix the flaky sieve", 30*time.Minute)
	a := l.open()
	at := -1
	for i, line := range a.home.lines {
		if line.cell != nil && line.cell.title == "Pricing Site" {
			at = i
		}
	}
	if at < 0 {
		t.Fatal("the question is not a line of the column")
	}
	a.home.cursor = at
	a.home.move(1)
	line := a.home.lines[a.home.cursor]
	if line.task == nil || line.task.ID != "4" {
		t.Fatalf("the cursor did not step from the question onto the landing: %+v", line.cell)
	}
	for _, line := range a.home.lines {
		if line.cell != nil && line.cell.kind == cellGroup && line.stop() {
			t.Fatal("the group line is a cursor stop")
		}
	}
}

// THE CHIPS ARE ON EXACTLY ONE ROW OF THE FRAME (law 7). Two questions and a
// landing, and only one answer clause is painted anywhere.
func TestTheAnswerChipsAreOnOneRowOfTheFrame(t *testing.T) {
	l := newLiveLab(t)
	l.live("-beta", "bbbb000000000001", session.SessionPresence{State: session.PresenceWaiting,
		Question: consentQuestionAt(7, "needs your ok to run bash", l.now.Add(-2*time.Hour))})
	l.live("-alpha", "aaaa000000000002", session.SessionPresence{State: session.PresenceWaiting,
		Question: consentQuestionAt(9, "needs your ok to run write", l.now.Add(-5*time.Minute))})
	l.landed("4", "fix the flaky sieve", 30*time.Minute)
	a := l.open()
	frame := homeText(a)
	if got := strings.Count(frame, "1 allow once"); got != 1 {
		t.Fatalf("the consent's chips are drawn on %d rows:\n%s", got, frame)
	}
	if got := strings.Count(frame, needsYesKey+" accept"); got != 0 {
		t.Fatalf("a landing nobody is standing on drew its answers:\n%s", frame)
	}
	// AND THE CURSOR OUTRANKS THE TOP ROW. Walked onto the landing, the chips
	// move with it and the question above says `enter` again.
	for i, line := range a.home.lines {
		if line.task != nil && line.task.ID == "4" {
			a.home.cursor = i
		}
	}
	frame = homeText(a)
	if strings.Count(frame, "1 allow once") != 0 {
		t.Fatalf("the chips stayed on the top question with the cursor on a landing:\n%s", frame)
	}
	if got := strings.Count(frame, needsYesKey+" accept"); got != 1 {
		t.Fatalf("the landing under the cursor drew its answers %d times:\n%s", got, frame)
	}
}

// A DIGIT WITH THE CURSOR ON NOTHING ANSWERABLE STILL REACHES THE TOP QUESTION,
// which is law 7 and did not change.
func TestADigitStillReachesTheTopQuestionFromALandingThatCannotAnswer(t *testing.T) {
	l := newLiveLab(t)
	l.live("-beta", "bbbb000000000001", session.SessionPresence{State: session.PresenceWaiting,
		Question: consentQuestionAt(7, "needs your ok to run bash", l.now.Add(-2*time.Hour))})
	a := l.open()
	var left []string
	a.leaveAnswer = func(dir string, kind session.QuestionKind, id uint64, key string) error {
		left = append(left, string(kind)+"/"+key)
		return nil
	}
	a.home.point(a.file)
	homeText(a)
	if _, took := a.homeGridAnswer("1"); !took {
		t.Fatal("the digit did not reach the top question")
	}
	if len(left) != 1 || left[0] != string(session.QuestionConsent)+"/1" {
		t.Fatalf("the digit went somewhere else: %v", left)
	}
}

// SAID ONCE ACROSS THE COLUMNS: `since you left` does not repeat a landing that
// `to check` is showing, and shows it again once the landing has aged out of the
// group.
func TestSinceYouLeftOmitsALandingToCheckIsShowing(t *testing.T) {
	l := newLiveLab(t)
	l.landed("4", "fix the flaky sieve", 30*time.Minute)
	a := l.open()
	a.home.seen = l.now.Add(-4 * time.Hour)
	a.home.build()
	frame := homeText(a)
	if got := strings.Count(frame, "fix the flaky sieve"); got != 1 {
		t.Fatalf("the landing is drawn on %d rows of the column:\n%s", got, frame)
	}
	if rows := panelRows(a, panelLeft); len(rows) != 0 {
		t.Fatalf("since you left repeated the landing: %+v", rows)
	}
	// AND ONCE IT IS HISTORY IT IS AN ORDINARY LINE OF `since you left` AGAIN.
	l2 := newLiveLab(t)
	l2.landed("4", "fix the flaky sieve", homeNeedsTaskFresh+time.Hour)
	b := l2.open()
	b.home.seen = l2.now.Add(-homeNeedsTaskFresh - 2*time.Hour)
	b.home.build()
	found := false
	for _, row := range panelRows(b, panelLeft) {
		if strings.Contains(row.title, "fix the flaky sieve") {
			found = true
		}
	}
	if !found {
		t.Fatalf("an aged-out landing never came back to since you left:\n%s", homeText(b))
	}
}

// THE GROUP FOLDS BEFORE A `needs you` ROW GOES. At a hundred and twenty by
// fourteen the panel keeps its question and says the landings on its fold.
func TestToCheckFoldsBeforeANeedsYouRowGoes(t *testing.T) {
	l := newLiveLab(t)
	l.live("-beta", "bbbb000000000001", session.SessionPresence{State: session.PresenceWaiting,
		Question: consentQuestionAt(7, "needs your ok to run bash", l.now.Add(-2*time.Hour))})
	for i, label := range []string{"one", "two", "three"} {
		l.landed(itoa(i+1), "landed "+label, time.Duration(i+1)*time.Hour)
	}
	a := l.openAt(120, 14)
	frame := homeText(a)
	if !strings.Contains(frame, "Pricing Site") {
		t.Fatalf("the question gave way before the landings did:\n%s", frame)
	}
	if strings.Contains(frame, needsCheckClause) {
		t.Fatalf("the group line survived the squeeze:\n%s", frame)
	}
	if !strings.Contains(frame, "3 "+needsCheckWord+" · tasks") {
		t.Fatalf("the fold does not say what the folded group is:\n%s", frame)
	}
}

// THE PULSE'S COUNT IS THE SUM OF THE TWO GROUPS, read through the panel's own
// reading of a landing rather than through a second one ([machineChecks]).
func TestThePulseCountsBothGroupsOfNeedsYou(t *testing.T) {
	l := newLiveLab(t)
	l.live("-beta", "bbbb000000000001", session.SessionPresence{State: session.PresenceWaiting,
		Question: consentQuestionAt(7, "needs your ok to run bash", l.now.Add(-2*time.Hour))})
	l.landed("4", "fix the flaky sieve", 30*time.Minute)
	l.landed("5", "port the parser", 5*time.Hour)
	// AND A LANDING THAT AGED OUT IS NOT COUNTED, because it is not a row.
	l.landed("6", "old business", homeNeedsTaskFresh+time.Hour)
	a := l.open()
	if rows := panelRows(a, panelNeeds); len(rows) != 3 || a.machine.wants != len(rows) {
		t.Fatalf("the pulse says %d want you over %d rows", a.machine.wants, len(panelRows(a, panelNeeds)))
	}
}

// SAID ONCE INSIDE THE PANEL TOO. A live conversation waiting on nothing but its
// own landing writes `waiting on you · your call on <title>` with no question
// object ([session.Agent.waitingOnPerson] reads a pending decision last), and
// drawing it as a question put the same piece of work on two rows of one panel.
// The landing's row is the one that survives — it has the work's own name, its
// files and its two answers.
func TestAConversationWaitingOnItsOwnLandingIsOnlyTheLandingsRow(t *testing.T) {
	l := newLiveLab(t)
	l.live("-alpha", "aaaa000000000002", session.SessionPresence{State: session.PresenceWaiting,
		Reason: "your call on fix the flaky sieve"})
	l.landed("4", "fix the flaky sieve", 30*time.Minute)
	a := l.open()
	rows := panelRows(a, panelNeeds)
	if len(rows) != 1 || rows[0].title != "fix the flaky sieve" {
		t.Fatalf("the landing is on the panel twice, or not at all: %+v", rows)
	}
	// AND A CONVERSATION WITH A QUESTION OF ITS OWN KEEPS ITS ROW, landing or no
	// landing: the question is a different thing, and it blocks.
	l2 := newLiveLab(t)
	l2.live("-alpha", "aaaa000000000002", session.SessionPresence{State: session.PresenceWaiting,
		Question: consentQuestionAt(7, "needs your ok to run bash", l2.now.Add(-time.Minute))})
	l2.landed("4", "fix the flaky sieve", 30*time.Minute)
	b := l2.open()
	if rows := panelRows(b, panelNeeds); len(rows) != 2 {
		t.Fatalf("a conversation with a question of its own lost its row: %+v", rows)
	}
}
