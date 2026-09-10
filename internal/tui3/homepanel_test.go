package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE PANELS (docs/design/home-mission-control/DESIGN.md §1, §3 G2–G6) ────

// homeLineAfter is the frame line under the first one holding a word.
func homeLineAfter(frame, word string) string {
	lines := strings.Split(frame, "\n")
	for y, line := range lines {
		if y >= placeHeadRows && strings.Contains(line, word) && y+1 < len(lines) {
			return lines[y+1]
		}
	}
	return ""
}

// A QUESTION IS ON ITS ROW WITH ITS ANSWERS: the row names the conversation, the
// line under it is what it asked, and the keys that answer it are drawn there.
func TestNeedsYouCarriesTheQuestionAndItsAnswersOnTheRow(t *testing.T) {
	lab := newAnswerLab(t, consentQuestion(7, "needs your ok to run bash"), time.Now())
	frame := homeText(lab.a)
	under := homeLineAfter(frame, "Pricing Research")
	if !strings.Contains(under, "needs your ok to run bash") || !strings.Contains(under, "1 allow once") {
		t.Fatalf("the row does not carry its question and answers:\n%s", frame)
	}
	if !strings.Contains(frame, "needs you · 1") {
		t.Fatalf("the heading does not count what is waiting:\n%s", frame)
	}
}

// A DIGIT ANSWERS THE TOP QUESTION FROM ANYWHERE ON HOME (law 7): the cursor is
// on this window's own conversation, and `3` still reaches the other one.
func TestADigitAnswersTheTopQuestionWithTheCursorElsewhere(t *testing.T) {
	lab := newAnswerLab(t, consentQuestion(7, "needs your ok to run bash"), time.Now())
	lab.a.home.point(lab.a.file)
	homeText(lab.a)
	lab.a.homeKey(key("3"))
	if len(*lab.sent) != 1 || (*lab.sent)[0].dir != lab.dir || (*lab.sent)[0].key != "3" {
		t.Fatalf("the digit did not answer the top question: %+v", *lab.sent)
	}
	if typed := lab.a.home.box.String(); typed != "" {
		t.Fatalf("the digit also typed %q", typed)
	}
}

// RUNNING DRAWS ITS ROWS WITH ONE MOVING CELL, on the first, and the line under
// each row says what it is doing.
func TestRunningDrawsTheWorkAndWhatItIsDoing(t *testing.T) {
	a := newSwitchLab(t).open(120, 45)
	frame := homeText(a)
	if !strings.Contains(frame, "running · 1") || !strings.Contains(frame, "Bounty Reward Companies") {
		t.Fatalf("running does not draw the moving conversation:\n%s", frame)
	}
	if under := homeLineAfter(frame, "Bounty Reward Companies"); !strings.Contains(under, "1 task running") {
		t.Fatalf("the running row does not say what it is doing:\n%s", frame)
	}
	if spin := a.home.spinAt(); spin < 0 || a.home.lines[spin].cell.panel != panelRunning {
		t.Fatalf("the one moving cell is not on the running panel's first row")
	}
}

// NO ROW WEARS A MARK BUT THE TWO (law 8): the question mark on a waiting row and
// the one moving cell. The quiet rows' circles are gone.
func TestNothingOnTheGridWearsAGlyphButTheTwoMarks(t *testing.T) {
	a := newSwitchLab(t).open(120, 45)
	frame := homeText(a)
	for _, banned := range []string{"○", "✓", "▸", "◦"} {
		body := strings.Join(strings.Split(frame, "\n")[placeHeadRows:], "\n")
		if strings.Contains(body, banned) {
			t.Fatalf("a row on the grid wears %q:\n%s", banned, frame)
		}
	}
}

// A `since you left` LINE IS A DOOR INTO ITS PLACE, on its own panel.
func TestSinceYouLeftIsItsOwnPanelOfDoors(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(120, 45)
	a.home.seen = lab.now.Add(-4 * time.Hour)
	a.home.ledger = switcherLedgerInput{learned: 2}
	a.home.build()
	frame := homeText(a)
	if !strings.Contains(frame, "since you left · 4h") || !strings.Contains(frame, "learned 2 things") {
		t.Fatalf("the ledger is not on its panel:\n%s", frame)
	}
	for at, line := range a.home.lines {
		if line.cell != nil && line.cell.title == "learned 2 things" {
			a.home.cursor = at
		}
	}
	a.homeKey(key("enter"))
	if !a.at(pageMemory) {
		t.Fatal("enter on the memory line did not open the memory place")
	}
}

// WHERE YOU WERE: this window's own conversation first with `here`, the last
// thing said in it under it, then the most recent quiet ones, then the fold.
func TestWhereYouWereLeadsWithThisWindowsOwnConversation(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(120, 45)
	a.home.last[lab.mine] = session.Summary{LastUser: "explain open addressing vs chaining"}
	a.home.build()
	frame := homeText(a)
	head, _ := homeRowOf(frame, "where you were")
	own, _ := homeRowOf(frame, "Porting the Resume Picker")
	if head < 0 || own != head+1 {
		t.Fatalf("this window's own conversation is not the first row of where you were:\n%s", frame)
	}
	lines := strings.Split(frame, "\n")
	if !strings.Contains(lines[own], homeHereWord) || !strings.Contains(lines[own+1], "explain open addressing") {
		t.Fatalf("the own row does not say here with its last words under it:\n%s", frame)
	}
	if !strings.Contains(frame, "5 more · "+homeFindWord) {
		t.Fatalf("the quiet tail is not folded behind one line:\n%s", frame)
	}
	// AND A ROW FROM ANOTHER FOLDER SAYS WHICH, where one from this folder does
	// not.
	if quiet := lines[own+2]; !strings.Contains(quiet, "Quiet Chat a") || !strings.Contains(quiet, "beta") {
		t.Fatalf("a row from another folder does not carry its project:\n%s", frame)
	}
	// AND THE TWO ROWS THAT ARE ON OTHER PANELS ARE NOT HERE A SECOND TIME.
	if strings.Count(frame, "Swarm Task Splitting") != 1 || strings.Count(frame, "Bounty Reward Companies") != 1 {
		t.Fatalf("a conversation is drawn on two panels:\n%s", frame)
	}
}

// PRESELECT THE PREVIOUS THING (law 6): home opened from a conversation puts the
// cursor on the one this window was in before it, so enter is a switch in two
// keys.
func TestHomePreselectsTheConversationThisWindowWasInBefore(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.app(lab.mine)
	a.width, a.height = 120, 45
	var before string
	for _, row := range a.readWorld().Sessions() {
		if row.Title == "quiet chat c" {
			before = row.Transcript
		}
	}
	a.prev = []string{before}
	a.openHome()
	homeText(a)
	if got := a.home.focused(); got.Transcript != before {
		t.Fatalf("the cursor opened on %q, want the previous conversation %q", got.Title, before)
	}
}

// AND TYPING IS UNTOUCHED: with anything in the box the body is the search's
// drop-up, exactly as it was, and not a panel.
func TestTypingOnHomeStillRaisesTheSearch(t *testing.T) {
	a := newSwitchLab(t).open(120, 45)
	for _, r := range "quiet" {
		a.homeKey(key(string(r)))
	}
	if a.home.gridOn() {
		t.Fatal("the grid is still up under a query")
	}
	for _, line := range a.home.lines {
		if line.cell != nil {
			t.Fatal("a line of the drop-up carries a panel's cell")
		}
	}
	if frame := homeText(a); !strings.Contains(frame, "Quiet Chat a") {
		t.Fatalf("the query did not find its rows:\n%s", frame)
	}
}
