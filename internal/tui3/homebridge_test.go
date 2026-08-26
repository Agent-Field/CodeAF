package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
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

// AND THE SHAPE ON THE SCREEN FOLLOWS IT. Below the floor the list is alone on
// every screen row; above it the card stands beside it, which is visible as one
// thing — a fact only the card knows on the SAME screen row as a row of the list.
func TestTheCardJoinsTheListOnlyWhereTheWidthIsSpare(t *testing.T) {
	a, mine := bridgeLab(t)
	a.home.point(mine)
	// The card names the strip in words and never in letters, with a colon after
	// it ([app.homeCardVerbs]); the foot's own hint spells the same clause without
	// one. So this is the phrase only a card ever draws.
	only := homeVerbsWord + ":"
	// AND BESIDE IS THE WHOLE CLAIM. The card's title stands on the same screen
	// row as the first row of the list, which is what makes it a column rather
	// than something further down the page.
	beside := func(width int) bool {
		a.width = width
		for _, line := range homeLines(a) {
			if strings.Contains(line, "what wants you first") && strings.Contains(line, "Porting the Picker") {
				return true
			}
		}
		return false
	}
	a.width = homeCardMin - 1
	if strings.Contains(homeText(a), only) || beside(homeCardMin-1) {
		t.Fatalf("a %d-column frame drew a card anyway:\n%s", homeCardMin-1, homeText(a))
	}
	a.width = homeCardMin
	if !strings.Contains(homeText(a), only) {
		t.Fatalf("a %d-column frame drew no card:\n%s", homeCardMin, homeText(a))
	}
	if !beside(homeCardMin) {
		t.Fatalf("a %d-column frame put the card somewhere other than beside the list:\n%s",
			homeCardMin, homeText(a))
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

	// AND WALKING TO THE TOP AND ON UP KEEPS IT. The cursor leaves the BODY for
	// the tab bar and not the list, so the card underneath still answers for the
	// row a person walked up off.
	a.frame()
	for i := 0; i < len(a.home.lines)+2; i++ {
		drive(t, a, key("up"))
	}
	if !a.bar.on {
		t.Fatalf("walking up off the top row did not reach the bar:\n%s", homeText(a))
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
// pressed. `↑` off the top row reaches the TAB BAR, and the first `↓` back off
// the bar lands on the first stop of the body.
//
// THE LAW THAT DIED IS "REST IS STILL A PLACE, ONE `↑` ABOVE THE LIST". The
// cursor used to leave the list at the top row and stand on no row at all. It
// leaves the BODY now and stands on the bar (pages.go's [barCursor]), which is a
// row a person can walk along and open a room from — so what this pins is the
// same journey with a destination that does something.
func TestHomeOpensOnItsOwnConversationAndUpOffTheTopReachesTheBar(t *testing.T) {
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
	if !a.bar.on {
		t.Fatalf("↑ off the top row did not reach the bar:\n%s", homeText(a))
	}
	if a.bar.at != pageHome {
		t.Fatalf("the bar cursor landed on %q, want the room it was standing in", a.bar.at.word())
	}
	// AND THE FIRST `↓` OFF THE BAR LANDS ON THE FIRST STOP OF THE BODY, which is
	// where the walk up left home's own cursor: the router claims `↑` at the top
	// row before home ever sees it, so nothing moved on the way up.
	drive(t, a, key("down"))
	if a.bar.on {
		t.Fatalf("↓ left the cursor on the bar:\n%s", homeText(a))
	}
	if a.home.cursor != a.home.placesTop() {
		t.Fatalf("↓ off the bar landed on line %d, want the top of the list at %d:\n%s",
			a.home.cursor, a.home.placesTop(), homeText(a))
	}
	if line, ok := a.home.focusedLine(); !ok || !line.stop() {
		t.Fatalf("↓ off the bar landed on a line no cursor may rest on:\n%s", homeText(a))
	}
}

// AND THE LANDING IS THE SAME LINE AT BOTH RUNGS OF THE LADDER.
//
// It used to differ: below the columns tier the strips stood over the list and
// the first `↓` walked into them, above it they had a column of their own. With
// one list the landing cannot depend on the width at all — it is
// [homeView.placesTop] at every width — and a landing that moved with the frame
// would be a landing nobody can build a habit on.
//
// IT IS ALSO NOT LINE ZERO, which is why it is a function and not a constant:
// the `since you left` heading and the claim over the ranked rows both stand
// above the first row a cursor may rest on.
func TestTheLandingIsTheSameLineAtBothRungsOfTheLadder(t *testing.T) {
	lab := newSwitchLab(t)
	tops := map[int]int{}
	for _, width := range []int{120, homeCardMin, 200} {
		a := lab.open(width, 40)
		a.home.seen = lab.now.Add(-30 * time.Minute)
		a.home.build()
		a.frame()
		// UP OFF THE TOP AND STRAIGHT BACK DOWN, which is the whole journey the
		// bar added and the one this landing is about.
		for i := 0; i < len(a.home.lines)+2; i++ {
			drive(t, a, key("up"))
		}
		drive(t, a, key("down"))
		if a.home.cursor != a.home.placesTop() {
			t.Fatalf("at %d columns ↓ off the bar landed on %d, want %d", width, a.home.cursor, a.home.placesTop())
		}
		if a.home.cursor == 0 {
			t.Fatalf("at %d columns the list has no heading above its first row:\n%s", width, homeText(a))
		}
		tops[width] = a.home.cursor
	}
	for width, at := range tops {
		if at != tops[120] {
			t.Fatalf("the landing moved with the frame: %d at 120 columns, %d at %d", tops[120], at, width)
		}
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
	for i := 0; i < len(a.home.lines)+2; i++ {
		drive(t, a, key("up"))
	}
	if !a.bar.on {
		t.Fatal("↑ off the top row did not reach the bar")
	}
	// One word along the bar, which opens nothing by itself...
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

// ONE SCREEN ROW IS ONE LINE OF THE LIST.
//
// This used to be a test about telling two LEFT columns apart by x: a zone row
// and a list row shared a screen row, and the pointer had to know which half it
// was aimed at. There is one column of rows now, so the law inverts — every x
// inside the list resolves to the SAME line — and a pointer that resolved
// differently at the two ends of a row would be inventing a column that is not
// there.
func TestOneScreenRowIsOneLineOfTheList(t *testing.T) {
	a, _ := bridgeLab(t)
	width, height := a.size()
	left, _ := homeColumns(width)
	lines, hits, _, _ := a.homeFrame(width, height)
	row := -1
	for y := range lines {
		if y < len(hits) && hits[y] >= 0 && a.home.lines[hits[y]].kind == homeSession &&
			a.home.lines[hits[y]].row.Transcript != a.file {
			row = y
			break
		}
	}
	if row < 0 {
		t.Fatalf("no other conversation's row on the frame:\n%s", homeText(a))
	}
	want := hits[row]
	for _, x := range []int{0, 1, left / 2, left - 1} {
		a.home.cursor = homeNoLine
		a.homePress(x, row)
		if a.home.cursor != want {
			t.Fatalf("a press at x=%d on screen row %d landed on line %d, want %d",
				x, row, a.home.cursor, want)
		}
	}
}

// THE LIST IS THE ONLY THING THAT SCROLLS. It used to be two windows onto one
// line list — the zones' own top and the list's — and only one of them held the
// cursor. There is one window now, and the card beside it is not a window at all:
// it is redrawn for whatever row the cursor reached, from its own first line.
func TestTheListScrollsAndTheCardBesideItDoesNot(t *testing.T) {
	// A machine with more rows than the frame can hold, with the fold standing
	// open so that all of them are on the column at once.
	lab := newSwitchLab(t)
	a := lab.open(200, 12)
	a.home.foldSwitch(true)
	if len(a.home.lines) < 12 {
		t.Fatalf("this column fits in the frame, so there is no scroll to test (%d lines)", len(a.home.lines))
	}
	width, height := a.size()
	a.home.cursor = a.home.placesTop()
	a.homeFrame(width, height)
	if a.home.top != 0 {
		t.Fatalf("the list started scrolled (top %d)", a.home.top)
	}
	for i := 0; i < 40; i++ {
		a.home.move(1)
	}
	a.homeFrame(width, height)
	if a.home.top == 0 {
		t.Fatalf("walking to the bottom never scrolled the list at all (%d lines)", len(a.home.lines))
	}
	// AND THE CARD IS STILL THE CURSOR'S ROW, HEADED BY ITS NAME. A column that
	// had scrolled with the list would have its title somewhere off the top. The
	// bottom row of this list is the fold, which is a door and not a thing with a
	// card, so the walk steps back onto the last conversation.
	for i := 0; i < len(a.home.lines); i++ {
		if line, ok := a.home.focusedLine(); ok && line.kind == homeSession {
			break
		}
		a.home.move(-1)
	}
	a.homeFrame(width, height)
	line, ok := a.home.focusedLine()
	if !ok || line.kind != homeSession {
		t.Fatalf("the bottom of the list holds no conversation to have a card:\n%s", homeText(a))
	}
	_, right := homeColumns(width)
	card := a.homeDetail(right, height, a.pal)
	if len(card) == 0 {
		t.Fatalf("the bottom of the list has no card beside it:\n%s", homeText(a))
	}
	if !strings.Contains(plain(card[0]), homeName(line.row)) {
		t.Fatalf("the card's first line is %q, want the cursor's row %q", plain(card[0]), homeName(line.row))
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
	if moving != 3 {
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
	if got := homeName(line.row); got != "Wave C" {
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
