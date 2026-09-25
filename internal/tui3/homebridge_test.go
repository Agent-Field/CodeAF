package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/standing"
)

// ── THE TWO COLUMNS ─────────────────────────────────────────────────────────
//
// These tests are about the ARRANGEMENT and nothing else: which shape a width
// asks for, what stands in each column, where the cursor opens, and the one cell
// on the whole page that is allowed to turn. What the columns HOLD is tested
// where it is built — switcher_test.go's reading, place_home_test.go's wiring,
// homeband_machine_test.go's card.
//
// THE LADDER USED TO HAVE THREE RUNGS AND HAS TWO. The zones' column and the
// everyday card tier went with the strips, so every test here that asked "which
// of three shapes is this width" now asks "is the width genuinely spare", and
// every test that asked "which column did that row leave the list for" now asks
// "does the list keep the whole frame".

// bridgeLab is a machine with something in every column: two projects, a
// conversation stopped on a question somewhere else, work running here, and a
// standing order for the machine's own card — on a frame past [homeCardMin], so
// there is a second column to have opinions about.
func bridgeLab(t *testing.T) (*app, string) {
	t.Helper()
	lab := newHomeLab(t)
	now := time.Now()
	alpha, beta := lab.workspace("alpha"), lab.workspace("beta")
	mine := lab.session("-alpha", "aaaa000000000001", "porting the picker", alpha, now.Add(-2*time.Minute))
	lab.session("-alpha", "aaaa000000000002", "odysseys wave 4", alpha, now.Add(-8*time.Minute))
	lab.session("-beta", "bbbb000000000001", "pricing research", beta, now.Add(-3*time.Hour))
	question := consentQuestion(7, "needs your ok to run bash")
	question.Asked = now.Add(-3 * time.Hour)
	lab.asking("-beta", "bbbb000000000001", question, now)
	lab.presence("-alpha", "aaaa000000000002", session.PresenceWorking, "", now,
		session.PresenceTask{ID: "1", Title: "Sweep", State: "running", StartedAt: now.Add(-8 * time.Minute)})
	lab.task("-alpha", session.TaskIndexEntry{
		ID: "1", Name: "sweep", Label: "Sweep", Title: "Sweep",
		Status: string(session.TaskRunning), SessionID: "aaaa000000000002",
	})
	band := &standBand{items: []standing.Item{
		bandItem("one", "check the deploy", alpha, standing.WhenEvery, "every 20 minutes"),
	}}
	a := lab.app(mine)
	band.wire(a)
	a.width, a.height = 200, 30
	a.openHome()
	return a, mine
}

// THE LADDER IS ONE DECISION AND ITS FLOOR IS AN ADDITION. Every width belongs
// to exactly one of two shapes, and the one floor there is left is the sum of
// what the two columns and the gutter ask for rather than a number chosen beside
// them — which is what lets it move by itself the day one of the parts changes.
func TestTheWidthLadderIsTwoRungsAndItsFloorIsTheSumOfTheColumns(t *testing.T) {
	for _, want := range []struct {
		width int
		tier  homeTier
	}{
		{60, homeTierList}, {120, homeTierList}, {homeCardMin - 1, homeTierList},
		{homeCardMin, homeTierCard}, {240, homeTierCard},
	} {
		if got := homeTierAt(want.width); got != want.tier {
			t.Fatalf("a %d-column frame is tier %v, want %v", want.width, got, want.tier)
		}
	}
	if homeCardMin != homeSwitchFull+homeGutter+homeCardCol {
		t.Fatal("the card tier's floor is not the sum of the columns it has to hold")
	}
	// BELOW THE FLOOR THE LIST IS THE WHOLE FRAME. There is no half-card and no
	// reserved gutter: the row's own note carries the one fact the card was for.
	for _, width := range []int{60, 100, homeCardMin - 1} {
		if left, right := homeColumns(width); left != width || right != 0 {
			t.Fatalf("a %d-column frame drew a card: left %d right %d", width, left, right)
		}
	}
	// AND ABOVE IT THE SPARE CELLS ARE SPLIT rather than handed to the card
	// whole: the card takes what it needs plus half of what is over, up to
	// [homeCardCap], and the list is never pushed below the width it draws every
	// fact at ([homeSwitchFull]).
	for _, width := range []int{homeCardMin, 180, 200, 400} {
		left, right := homeColumns(width)
		if left+homeGutter+right != width {
			t.Fatalf("at %d the two columns leave %d cells over", width, width-left-homeGutter-right)
		}
		if right < homeCardCol || right > homeCardCap {
			t.Fatalf("at %d the card is %d cells wide, want between %d and %d", width, right, homeCardCol, homeCardCap)
		}
		if left < homeSwitchFull {
			t.Fatalf("at %d the card was paid for out of the list: %d cells left", width, left)
		}
		if spare := width - homeCardMin; right < homeCardCap && right != homeCardCol+spare/2 {
			t.Fatalf("at %d the card took %d of the %d spare cells, want half of them", width, right-homeCardCol, spare)
		}
	}
}

// THE SECOND COLUMN IS ALWAYS THE CARD OF THE ROW UNDER THE CURSOR. There is no
// state of this screen where it is a second list, an empty half, or a card about
// something no row on the left names.
//
// THE LAW THAT DIED IS "AND THE MACHINE'S WHILE THE CURSOR IS ON NOTHING". The
// cursor could stand on no row at all — `↑` off the top row put it there — and
// the column became a card about the machine. `↑` reaches the TAB BAR now
// (pages.go's [barCursor]), so the cursor is on a row of this list at every
// moment home is up, and the column has one subject rather than two.
func TestTheSecondColumnIsTheCardOfTheRowUnderTheCursor(t *testing.T) {
	a, mine := bridgeLab(t)
	width, _ := a.size()
	_, right := homeColumns(width)
	if right == 0 {
		t.Fatalf("a %d-column frame has no second column to ask about", width)
	}

	a.home.point(mine)
	got := plain(strings.Join(a.homeDetail(right, 20, a.pal), "\n"))
	if !strings.Contains(got, "Porting the Picker") {
		t.Fatalf("the card on a row is not that row's:\n%s", got)
	}
	if !strings.Contains(homeText(a), "Porting the Picker") {
		t.Fatalf("the card is not on the frame at all:\n%s", got)
	}

	// AND RAISING THE BAR KEEPS IT. The cursor leaves the BODY for the tab bar
	// and not the list (raised as a press would — `↑` on home stays in the
	// field), so the card underneath still answers for the row it stood on.
	a.frame()
	a.barRaise()
	if !a.bar.on {
		t.Fatalf("the bar did not rise:\n%s", homeText(a))
	}
	if card := a.homeDetail(right, 20, a.pal); len(card) == 0 {
		t.Fatal("the column went blank with the cursor on the bar")
	}
	if line, ok := a.home.previewLine(); !ok || !line.stop() {
		t.Fatalf("the card is about no row of the list: %+v", line)
	}
}

// HOME OPENS ON THE CONVERSATION THIS WINDOW HOLDS, VISIBLY SELECTED — the row
// esc drops back into — so the first frame answers "where am I" before a key is
// pressed. `↑` OFF THE TOP ROW STAYS THERE: the field's column has no way up
// onto the tab bar (owner, 2026-09-17: nothing outside the left column is
// walked, and the tabs are reached by a press, `tab` or a chord), where every
// other place's `↑` still climbs onto the bar (pages.go's [app.barReach]).
func TestHomeOpensOnItsOwnConversationAndUpOffTheTopStaysInTheColumn(t *testing.T) {
	a, mine := bridgeLab(t)
	line, ok := a.home.focusedLine()
	if !ok || line.row.Transcript != mine {
		t.Fatalf("home opened on %q, want the conversation this window is holding:\n%s",
			homeName(a.home.focused()), homeText(a))
	}
	a.frame()
	for i := 0; i < len(a.home.lines)+2; i++ {
		drive(t, a, key("up"))
	}
	if a.bar.on {
		t.Fatalf("↑ off the top row climbed onto the bar:\n%s", homeText(a))
	}
	if a.home.cursor != a.home.placesTop() {
		t.Fatalf("↑ off the top row left the cursor on line %d, want the top of the list at %d:\n%s",
			a.home.cursor, a.home.placesTop(), homeText(a))
	}
	if line, ok := a.home.focusedLine(); !ok || !line.stop() {
		t.Fatalf("the top of the list is a line no cursor may rest on:\n%s", homeText(a))
	}
}

// AND THE TOP IS THE SAME LINE AT BOTH RUNGS OF THE LADDER.
//
// It used to differ: below the columns tier the strips stood over the list and
// the first `↓` walked into them, above it they had a column of their own. With
// one list the top cannot depend on the width at all — it is
// [homeView.placesTop] at every width — and a top that moved with the frame
// would be a landing nobody can build a habit on.
//
// IT IS ALSO NOT LINE ZERO, which is why it is a function and not a constant:
// the `since you left` heading and the claim over the ranked rows both stand
// above the first row a cursor may rest on.
func TestTheTopIsTheSameLineAtBothRungsOfTheLadder(t *testing.T) {
	lab := newSwitchLab(t)
	tops := map[int]int{}
	for _, width := range []int{120, homeCardMin, 200} {
		a := lab.open(width, 40)
		a.home.seen = lab.now.Add(-30 * time.Minute)
		a.home.build()
		a.frame()
		for i := 0; i < len(a.home.lines)+2; i++ {
			drive(t, a, key("up"))
		}
		if a.home.cursor != a.home.placesTop() {
			t.Fatalf("at %d columns ↑ off the top landed on %d, want %d", width, a.home.cursor, a.home.placesTop())
		}
		if a.home.cursor == 0 {
			t.Fatalf("at %d columns the list has no heading above its first row:\n%s", width, homeText(a))
		}
		tops[width] = a.home.cursor
	}
	if tops[120] != tops[homeCardMin] || tops[homeCardMin] != tops[200] {
		t.Fatalf("the top of the list moves with the width: %v", tops)
	}
}

// ENTER ON THE BAR OPENS THE PLACE UNDER THE CURSOR.
//
// THE LAW THAT DIED IS "ENTER AT REST RETURNS TO THE CONVERSATION YOU ARE
// HOLDING". Rest was the screen's own furniture — the cursor on no row at all —
// and `enter` there had nothing to open, so it was spent going back to work
// rather than being a dead key. The cursor reaches the TAB BAR now instead
// (pages.go's [barCursor]), and the bar is not furniture: every word on it is a
// room, so `enter` opens the one under the cursor. That is the same principle —
// the key is never dead — with somewhere real to go.
func TestEnterOnTheBarOpensThePlaceUnderTheCursor(t *testing.T) {
	a, _ := bridgeLab(t)
	a.frame()
	// Raised as a press on it would — `↑` on home stays in the field.
	a.barRaise()
	if !a.bar.on {
		t.Fatal("the bar did not rise")
	}
	// Two words along the bar, past the way back to the chats, which opens
	// nothing by itself...
	drive(t, a, key("right"))
	if a.bar.at != pageChats {
		t.Fatalf("→ landed the bar cursor on %q, want chats", a.bar.at.word())
	}
	drive(t, a, key("right"))
	if !a.at(pageHome) {
		t.Fatalf("walking the bar opened %q by itself", a.page.word())
	}
	if a.bar.at != pageTasks {
		t.Fatalf("→ landed the bar cursor on %q, want the next word along", a.bar.at.word())
	}
	// ...and `enter` is what goes in, with the cursor coming down into the body.
	drive(t, a, key("enter"))
	if !a.at(pageTasks) {
		t.Fatalf("enter on the bar left the person on %q", a.page.word())
	}
	if a.bar.on {
		t.Fatal("enter left the cursor standing on the bar of the room it opened")
	}
}

// A DRAFT KEEPS THE ARROWS FOR THE CARET. `←` and `→` are the fold's and the
// card's while the box is empty; the moment there is something typed they belong
// to the text, and a key that moved the selection out from under a person
// mid-word would be the list arguing with the box.
func TestADraftKeepsTheArrowsForTheCaret(t *testing.T) {
	a, _ := bridgeLab(t)
	a.homeKey(key("x"))
	if !a.home.searching() {
		t.Fatal("typing a letter did not put anything in the box")
	}
	was := a.home.cursor
	a.homeKey(key("right"))
	a.homeKey(key("left"))
	if a.home.cursor != was {
		t.Fatalf("the arrows moved the cursor from %d to %d while something was typed", was, a.home.cursor)
	}
}

// AND THE ROW'S OWN KEYS ARE THE SAME AT BOTH RUNGS. A digit over a conversation
// stopped on a question answers it whether or not there is a card beside the
// row: the tier is a fact about the FRAME and never about the door.
func TestADigitAnswersTheQuestionAtBothRungsOfTheLadder(t *testing.T) {
	for _, width := range []int{120, 200} {
		lab := newAnswerLab(t, consentQuestion(7, "needs your ok to run bash"), time.Now())
		a := lab.a
		a.width = width
		a.openHome()
		a.home.point(lab.row)
		if line, ok := a.home.focusedLine(); !ok || line.kind != homeSession {
			t.Fatalf("at %d columns the waiting conversation has no row:\n%s", width, homeText(a))
		}
		a.homeKey(key("3"))
		if len(*lab.sent) != 1 {
			t.Fatalf("at %d columns a digit sent %d answers, want 1:\n%s", width, len(*lab.sent), homeText(a))
		}
		if typed := a.home.box.String(); typed != "" {
			t.Fatalf("at %d columns the digit also typed %q into the box", width, typed)
		}
	}
}

// ONE SPINNER. However many things move, exactly one row is given the moving
// cell and every other live row holds the still mark — and the one that gets it
// is the most recently active, which is the one order a person can verify: the
// thing that started last is the thing they just did.
//
// THE CHOICE IS WHAT IS PINNED HERE rather than the cell on the screen. The
// resting list is painted by the reading itself ([switcherReading.paint]), which
// draws a conversation's state mark and does not ask [app.homeSpins] — so today
// no row of the switcher actually turns. The law that survives, and the one this
// guards, is that ONE line is chosen and the paint clock is earned for that one.
func TestExactlyOneRowIsGivenTheSpinnerHoweverManyAreMoving(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	alpha := lab.workspace("alpha")
	mine := lab.session("-alpha", "aaaa000000000001", "porting the picker", alpha, now.Add(-2*time.Minute))
	for i, when := range []time.Duration{40 * time.Minute, 20 * time.Minute, 5 * time.Minute} {
		id := "bbbb00000000000" + string(rune('1'+i))
		lab.session("-alpha", id, "wave "+string(rune('a'+i)), alpha, now.Add(-when))
		lab.presence("-alpha", id, session.PresenceWorking, "", now,
			session.PresenceTask{ID: "1", Title: "Sweep", State: "running", StartedAt: now.Add(-when)})
		lab.task("-alpha", session.TaskIndexEntry{
			ID: "1", Name: "sweep", Label: "Sweep", Title: "Sweep",
			Status: string(session.TaskRunning), SessionID: id,
		})
	}
	a := lab.app(mine)
	a.width, a.height = 200, 30
	a.openHome()

	moving := 0
	for _, line := range a.home.lines {
		if _, ok := homeMovingAt(line); ok {
			moving++
		}
	}
	// AT LEAST THREE: a conversation mid-turn stands on `threads` as
	// well as having its work on `running`, and the law is about the spinner.
	if moving < 3 {
		t.Fatalf("the machine is not busy enough to prove anything: %d moving rows\n%s", moving, homeText(a))
	}
	spun := 0
	var line homeLine
	for at := range a.home.lines {
		if a.homeSpins(at) {
			spun, line = spun+1, a.home.lines[at]
		}
	}
	if spun != 1 {
		t.Fatalf("%d rows were given the spinner at once, want exactly one:\n%s", spun, homeText(a))
	}
	if line.cell == nil || line.cell.row == nil {
		t.Fatal("the moving task has no owning conversation")
	}
	if got := homeName(line.cell.row.session); got != "Wave C" {
		t.Fatalf("the spinner is on %q, want the most recently active", got)
	}
	// AND THE CLOCK IS WOKEN FOR THAT ONE ROW, however many are out.
	if !a.homeAnimating() {
		t.Fatal("a machine with three things running does not earn the paint clock")
	}
	// AND NO ROW OF THE FRAME TURNS TWICE. However the paint changes, two turning
	// cells on one page is what this law exists to stop.
	turning := 0
	for _, drawn := range strings.Split(homeText(a), "\n") {
		if strings.ContainsAny(drawn, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
			turning++
		}
	}
	if turning > 1 {
		t.Fatalf("%d rows are turning at once:\n%s", turning, homeText(a))
	}
}

// AND NOTHING TURNS ON A STILL MACHINE, which is what lets the page fall back to
// its three-second beat.
func TestNothingTurnsWhenNothingIsMoving(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-alpha", "aaaa000000000001", "porting the picker", lab.workspace("alpha"), now.Add(-time.Hour))
	lab.session("-beta", "bbbb000000000001", "pricing research", lab.workspace("beta"), now.Add(-3*time.Hour))
	a := lab.app(mine)
	a.width, a.height = 200, 30
	a.openHome()
	if a.home.spin != homeNoLine || a.homeAnimating() {
		t.Fatalf("a still machine woke the paint clock for line %d", a.home.spin)
	}
}
