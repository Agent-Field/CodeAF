package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// ── THE THREE COLUMNS ───────────────────────────────────────────────────────
//
// These tests are about the ARRANGEMENT and nothing else: which shape a width
// asks for, what stands in each column, where the cursor opens, which key moves
// between them, and the one cell on the whole page that is allowed to turn. What
// the columns HOLD is tested where it is built — homeattention.go's zones,
// homeband_machine_test.go's card, home_test.go's list.

// bridgeLab is a machine with something in every column: two projects, a
// conversation stopped on a question somewhere else, work running here, and two
// standing orders for the machine's own card.
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
	a.width, a.height = 140, 26
	a.openHome()
	return a, mine
}

// THE LADDER IS ONE DECISION. Every width belongs to exactly one shape, and the
// two floors are the ones the design names: the card at eighty, the third column
// at a hundred and ten — which is what the three columns and their gutters add
// up to at their floors and not a number chosen beside them.
func TestTheWidthLadderPicksOneShapePerTier(t *testing.T) {
	for _, want := range []struct {
		width int
		tier  homeTier
	}{
		{79, homeTierList}, {80, homeTierCard}, {109, homeTierCard},
		{110, homeTierColumns}, {140, homeTierColumns},
	} {
		if got := homeTierAt(want.width); got != want.tier {
			t.Fatalf("a %d-column frame is tier %v, want %v", want.width, got, want.tier)
		}
	}
	if homeMinColumns != homeAttentionCol+homeGutter+homePlacesCol+homeGutter+homeCardCol {
		t.Fatal("the tier's floor is not the sum of the columns it has to hold")
	}
	// AND THE CARD IS MEASURED OFF THE WIDTH AT EVERY TIER THAT HAS ONE, so the
	// three-column frame is the two-column one with the left half split in two.
	for _, width := range []int{110, 120, 140, 200} {
		zone, places, card := homeThreeColumns(width)
		left, right := homeColumns(width)
		if left+homeGutter+right != width {
			t.Fatalf("at %d the columns leave %d cells over", width, width-left-homeGutter-right)
		}
		if right != card || zone+homeGutter+places != left {
			t.Fatalf("at %d the split (%d %d %d) disagrees with the frame (%d %d)",
				width, zone, places, card, left, right)
		}
		// PLACES IS THE WIDEST COLUMN, at the tier's floor and at every width
		// above it. It is the one thing the ladder promises about the middle.
		if places < card || places <= zone {
			t.Fatalf("at %d places is %d beside a %d card and a %d zone column",
				width, places, card, zone)
		}
	}
}

// AND THE SHAPE ON THE SCREEN FOLLOWS IT. Below the floor the zones are strips
// standing OVER the list; above it they are a column standing BESIDE it, which
// is the same rows in a different place and is visible as one thing: a zone label
// and a project heading on the SAME screen row.
func TestTheZonesLeaveTheListOnlyAtTheColumnsTier(t *testing.T) {
	a, _ := bridgeLab(t)
	beside := func(width int) bool {
		a.width = width
		for _, line := range homeLines(a) {
			if strings.Contains(line, attentionNeedsWord) && strings.Contains(line, "alpha") {
				return true
			}
		}
		return false
	}
	if beside(109) {
		t.Fatalf("at 109 the zones already left the list:\n%s", homeText(a))
	}
	if !beside(110) {
		t.Fatalf("at 110 the zones did not take a column of their own:\n%s", homeText(a))
	}
	// AND THE KEY BETWEEN THEM IS NAMED THERE AND NOWHERE ELSE.
	a.width = 140
	if !strings.Contains(a.homeHint(), homeTabWord) {
		t.Fatalf("the wide tier does not name its own key: %q", a.homeHint())
	}
	a.width = 100
	a.homeFrame(a.width, a.height)
	if strings.Contains(a.homeHint(), homeTabWord) {
		t.Fatalf("a frame with no zone column names tab anyway: %q", a.homeHint())
	}
}

// THE RIGHT COLUMN IS ALWAYS A CARD: the row's own while the cursor is on one,
// and the MACHINE'S while it is on nothing. There is no state of this screen
// where the third column is a second list or an empty half.
func TestTheThirdColumnIsTheRowsCardAndTheMachinesAtRest(t *testing.T) {
	a, mine := bridgeLab(t)
	width, _ := a.size()
	_, right := homeColumns(width)

	// Rest is walked into now rather than opened onto ([homeView.openAt]).
	a.home.cursor = homeRest
	if got := plain(strings.Join(a.homeDetail(right, 20, a.pal), "\n")); !strings.Contains(got, machineWatchWord) {
		t.Fatalf("the third column at rest is not the machine's card:\n%s", got)
	}
	a.home.point(mine)
	got := plain(strings.Join(a.homeDetail(right, 20, a.pal), "\n"))
	if !strings.Contains(got, "Porting the Picker") {
		t.Fatalf("the third column on a row is not that row's card:\n%s", got)
	}
	if strings.Contains(got, machineWatchWord) {
		t.Fatalf("the row's card kept the machine's bands:\n%s", got)
	}
	// AND IT IS REALLY ON THE FRAME, across the second gutter from the list.
	if !strings.Contains(homeText(a), "Porting the Picker") {
		t.Fatalf("the card is not on the three-column frame:\n%s", homeText(a))
	}
}

// HOME OPENS ON THE CONVERSATION THIS WINDOW HOLDS, VISIBLY SELECTED — the row
// esc drops back into — so the first frame answers "where am I" before a key is
// pressed. Rest is still a place ([homeRest]), reached by walking up; and from
// rest, focus wakes at the center of mass: `↓` lands in the MIDDLE column and
// `tab`, the named triage key, enters `needs you`.
func TestHomeOpensOnItsOwnConversationAndRestStillWakesIntoTheList(t *testing.T) {
	a, mine := bridgeLab(t)
	if len(zoneNames(a, attentionNeedsWord)) == 0 {
		t.Fatal("this machine has nothing waiting, so the landing proves nothing")
	}
	for _, step := range []struct {
		word string
		key  func()
		zone string
	}{
		{"↓", func() { a.home.move(1) }, ""},
		{"tab", func() { a.home.tab() }, attentionNeedsWord},
	} {
		a.openHome()
		line, ok := a.home.focusedLine()
		if !ok || line.row.Transcript != mine {
			t.Fatalf("home opened on %q, want the conversation this window is holding:\n%s",
				homeName(a.home.focused()), homeText(a))
		}
		// Rest is one deliberate state away, and the first key from it still
		// lands where the old landing law promised.
		a.home.cursor = homeRest
		step.key()
		line, ok = a.home.focusedLine()
		if !ok || attentionWordOf(line) != step.zone {
			t.Fatalf("%s from rest landed in zone %q, want %q:\n%s",
				step.word, attentionWordOf(line), step.zone, homeText(a))
		}
		if step.zone == "" && a.home.cursor != a.home.placesTop() {
			t.Fatalf("%s from rest landed on line %d, want the top of the list at %d:\n%s",
				step.word, a.home.cursor, a.home.placesTop(), homeText(a))
		}
	}
}

// AND THE LANDING IS THE SAME LINE WHATEVER THE ZONES HOLD. A landing that moved
// with what the machine happens to be doing this morning is a landing nobody can
// build a habit on, so `↓` reaches the top of the list on a busy machine and on
// a quiet one alike — and `←` is the way across into the flank.
func TestTheFirstArrowLandsInTheListWhicheverWayTheZonesStand(t *testing.T) {
	quiet := func(t *testing.T) *app {
		t.Helper()
		lab := newHomeLab(t)
		now := time.Now()
		mine := lab.session("-alpha", "aaaa000000000001", "porting the picker", lab.workspace("alpha"), now.Add(-time.Hour))
		lab.session("-beta", "bbbb000000000001", "pricing research", lab.workspace("beta"), now.Add(-3*time.Hour))
		a := lab.app(mine)
		a.width, a.height = 140, 26
		a.openHome()
		return a
	}
	for _, machine := range []struct {
		word  string
		build func(t *testing.T) *app
		zoned bool
	}{
		{"busy", func(t *testing.T) *app { a, _ := bridgeLab(t); return a }, true},
		{"quiet", quiet, false},
	} {
		a := machine.build(t)
		if got := len(zoneNames(a, attentionNeedsWord)) > 0; got != machine.zoned {
			t.Fatalf("the %s machine has %d rows in %q, which is not the case this covers",
				machine.word, len(zoneNames(a, attentionNeedsWord)), attentionNeedsWord)
		}
		// Rest is walked into now rather than opened onto ([homeView.openAt]);
		// the landing law under test is about the first key FROM rest.
		a.home.cursor = homeRest
		a.home.move(1)
		line, ok := a.home.focusedLine()
		if !ok || attentionWordOf(line) != "" || a.home.cursor != a.home.placesTop() {
			t.Fatalf("↓ on the %s machine landed on line %d in zone %q, want the list's top at %d:\n%s",
				machine.word, a.home.cursor, attentionWordOf(line), a.home.placesTop(), homeText(a))
		}
		// AND THE ROW IT LANDS ON IS A REAL ONE, not a heading the cursor slid off.
		if !line.stop() {
			t.Fatalf("↓ on the %s machine landed on a line no cursor may rest on", machine.word)
		}
	}
}

// THE NARROWER TIERS ARE UNTOUCHED. There the zones are strips standing OVER the
// list, so the first `↓` walking into them is what the geometry promises.
func TestTheFirstArrowStillWalksIntoTheStripsBelowTheColumnsTier(t *testing.T) {
	a, _ := bridgeLab(t)
	a.width = homeMinColumns - 1
	a.homeFrame(a.width, a.height)
	a.home.cursor = homeRest
	if !a.home.resting() || a.home.columns() {
		t.Fatalf("the two-column tier was not set up (tier %v)", a.home.tier)
	}
	a.home.move(1)
	line, ok := a.home.focusedLine()
	if !ok || attentionWordOf(line) != attentionNeedsWord {
		t.Fatalf("↓ from rest at the strips tier landed in %q, want %q:\n%s",
			attentionWordOf(line), attentionNeedsWord, homeText(a))
	}
}

// ENTER AT REST STILL TAKES YOU IN. Rest is the screen's own furniture, and a
// person who opens home and presses enter is going back to work, not asking
// about the machine — so enter returns to the conversation this terminal is
// holding, instead of dying on a cursor that is on no row. It does not open
// the first list row: that can be another window's conversation, and enter at
// rest must never land on a refusal.
func TestEnterAtRestReturnsToTheConversationYouAreHolding(t *testing.T) {
	a, _ := bridgeLab(t)
	was := a.file
	a.openHome()
	// Rest is walked into now rather than opened onto ([homeView.openAt]).
	a.home.cursor = homeRest
	if !a.home.resting() {
		t.Fatal("rest is no longer a state this screen can hold")
	}
	a.homeEnter()
	if a.home.open {
		t.Fatalf("enter at rest left home up saying %q", a.home.msg)
	}
	if a.file != was {
		t.Fatalf("enter at rest landed in %q, want the held conversation %q", a.file, was)
	}
}

// AND TAB ON A MACHINE WITH NOTHING WAITING LANDS IN THE LIST TOO, because a
// zone with no rows is a label and a label is not a place a cursor can stand —
// which is the one way the triage key's landing depends on what is there, and
// the reason `↓` is not allowed to.
func TestTabLandsInTheListWhenTheZonesAreEmpty(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	alpha := lab.workspace("alpha")
	mine := lab.session("-alpha", "aaaa000000000001", "porting the picker", alpha, now.Add(-time.Hour))
	lab.session("-beta", "bbbb000000000001", "pricing research", lab.workspace("beta"), now.Add(-3*time.Hour))
	a := lab.app(mine)
	a.width, a.height = 140, 26
	a.openHome()
	a.home.tab()
	line, ok := a.home.focusedLine()
	if !ok || line.kind != homeSession || attentionWordOf(line) != "" {
		t.Fatalf("tab on a quiet machine landed on kind %v in zone %q:\n%s",
			line.kind, attentionWordOf(line), homeText(a))
	}
}

// STABLE GEOGRAPHY BEATS EMPTINESS, ON HOME ONLY. Both labels draw at the wide
// tier over nothing at all, in the zones' own column — an empty `needs you` is
// the good news, said with space, and a map that redraws itself is not a map.
func TestTheZoneLabelsHoldTheirGroundAtTheWideTierOverNothing(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-alpha", "aaaa000000000001", "porting the picker", lab.workspace("alpha"), now.Add(-time.Hour))
	lab.session("-beta", "bbbb000000000001", "pricing research", lab.workspace("beta"), now.Add(-3*time.Hour))
	a := lab.app(mine)
	a.width, a.height = 140, 26
	a.openHome()

	if names := zoneNames(a, attentionNeedsWord); len(names) != 0 {
		t.Fatalf("this machine has something waiting after all: %v", names)
	}
	lines := homeLines(a)
	for _, word := range []string{attentionNeedsWord, attentionMovingWord} {
		at := -1
		for y, line := range lines {
			if strings.Contains(line, word) {
				at = y
				break
			}
		}
		if at < 0 {
			t.Fatalf("the %q label vanished over nothing at the wide tier:\n%s", word, homeText(a))
		}
		// AND IT IS IN THE ZONES' OWN COLUMN, which is what makes the geography
		// stable rather than merely present.
		if head := strings.TrimRight(lines[at][:homeAttentionCol], " "); !strings.Contains(head, word) {
			t.Fatalf("the %q label is not in the first column: %q", word, lines[at])
		}
	}
}

// ONE SPINNER. However many things move, exactly one row turns and every other
// live row holds the still `●` — and the one that turns is the most recently
// active.
func TestExactlyOneRowSpinsHoweverManyAreMoving(t *testing.T) {
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
	a.width, a.height = 140, 30
	a.openHome()

	if names := zoneNames(a, attentionMovingWord); len(names) != 3 {
		t.Fatalf("the machine is not busy enough to prove anything: %v", names)
	}
	frame := homeText(a)
	spinning := 0
	for _, line := range strings.Split(frame, "\n") {
		if strings.ContainsAny(line, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
			spinning++
		}
	}
	if spinning != 1 {
		t.Fatalf("%d rows are turning at once, want exactly one:\n%s", spinning, frame)
	}
	// AND IT IS THE ONE THAT STARTED LAST, in the zone whose subject is what is
	// moving — the rest of that zone, sorted busiest-first, holds the still mark.
	var spun homeLine
	ok := false
	for at, line := range a.home.lines {
		if a.homeSpins(at) {
			spun, ok = line, true
		}
	}
	if !ok {
		t.Fatalf("nothing was given the spinner at all:\n%s", frame)
	}
	if attentionWordOf(spun) != attentionMovingWord {
		t.Fatalf("the spinner is on a %q row, want one in %q", attentionWordOf(spun), attentionMovingWord)
	}
	if spun.zone.name != "Wave C" {
		t.Fatalf("the spinner is on %q, want the most recently active", spun.zone.name)
	}
	// AND THE CLOCK IS WOKEN FOR THAT ONE ROW, however many are out.
	if !a.homeAnimating() {
		t.Fatal("a machine with three things running does not earn the paint clock")
	}
}

// AND NOTHING TURNS ON A STILL MACHINE, which is what lets the page fall back
// to its three-second beat.
func TestNothingTurnsWhenNothingIsMoving(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-alpha", "aaaa000000000001", "porting the picker", lab.workspace("alpha"), now.Add(-time.Hour))
	lab.session("-beta", "bbbb000000000001", "pricing research", lab.workspace("beta"), now.Add(-3*time.Hour))
	a := lab.app(mine)
	a.width, a.height = 140, 26
	a.openHome()
	if a.home.spin != homeRest || a.homeAnimating() {
		t.Fatalf("a still machine woke the paint clock for line %d", a.home.spin)
	}
}

// ← FROM REST IS THE NAMED WAY INTO WHAT NEEDS YOU, and it lands on a row and
// never on a label.
//
// This test used to be about `tab`, which cycled the zones in the order they are
// drawn and came back round from the last. `tab` is the way to the NEXT PLACE
// now, on every place (placekeys.go), so what it did here had to go somewhere —
// and both halves of it were already on the arrows: `←` and `→` cross the gutter
// ([homeView.crossColumns], which this file's own comment called "tab's circle,
// unrolled onto the two keys that already point the way"), and the one thing
// only the named key did — ENTERING the flank from rest, where there is no row
// to cross from — is what `←` picks up here ([app.homeZoneEntry]).
func TestTheNamedKeyEntersWhatNeedsYouFromRest(t *testing.T) {
	a, _ := bridgeLab(t)
	if len(zoneNames(a, attentionNeedsWord)) == 0 || len(zoneNames(a, attentionMovingWord)) == 0 {
		t.Fatal("this machine does not have both zones, so the key has nothing to prove")
	}
	// Home opens on this window's own conversation ([homeView.openAt]), so the
	// cursor is walked back up to rest first — which is the state the named entry
	// is for.
	a.home.cursor = homeRest
	a.homeKey(key("left"))
	if got := a.home.cursorZone(); got != attentionNeedsWord {
		t.Fatalf("← from rest landed in %q, want %q:\n%s", got, attentionNeedsWord, homeText(a))
	}
	if _, ok := a.home.focusedLine(); !ok {
		t.Fatal("← from rest landed on no row at all")
	}
	// AND ESC STILL MEANS WHAT IT MEANT: one layer at a time, and home closes.
	a.homeKey(key("esc"))
	if a.home.open {
		t.Fatal("esc from a zone did not close home")
	}
}

// THE ARROWS FOLLOW THE GEOGRAPHY AT THE COLUMNS TIER: the zones stand to the
// left of the list, so → off a zone row crosses into the list and ← crosses
// back — and both land on the SAME conversation when the far column holds it,
// so stepping across the gutter never loses the thing being read.
func TestTheArrowsCrossBetweenTheZonesAndTheList(t *testing.T) {
	a, _ := bridgeLab(t)
	// ← from rest lands in `needs you`, on the conversation stopped on a
	// question ([app.homeZoneEntry]).
	a.homeKey(key("left"))
	line, ok := a.home.focusedLine()
	if !ok || attentionWordOf(line) != attentionNeedsWord {
		t.Fatalf("← did not reach the needs-you row:\n%s", homeText(a))
	}
	held := line.row.Transcript

	a.homeKey(key("right"))
	after, ok := a.home.focusedLine()
	if !ok || attentionWordOf(after) != "" {
		t.Fatalf("→ did not leave the zones' column (zone %q):\n%s", attentionWordOf(after), homeText(a))
	}
	if after.row.Transcript != held {
		t.Fatalf("→ landed on %q, want the same conversation %q:\n%s", after.row.Transcript, held, homeText(a))
	}

	a.homeKey(key("left"))
	back, ok := a.home.focusedLine()
	if !ok || attentionWordOf(back) == "" {
		t.Fatalf("← did not cross back into the zones:\n%s", homeText(a))
	}
	if back.row.Transcript != held {
		t.Fatalf("← landed on %q, want the same conversation %q:\n%s", back.row.Transcript, held, homeText(a))
	}

	// AND A DRAFT KEEPS THE ARROWS. With something typed they are the caret's,
	// so the cursor stays where it is standing.
	a.homeKey(key("x"))
	was := a.home.cursor
	a.homeKey(key("right"))
	if a.home.cursor != was {
		t.Fatal("→ moved the cursor while something was typed")
	}
}

// AND THE KEYS ARE THE ROW'S OWN KEYS IN WHICHEVER COLUMN IT IS DRAWN. A digit
// over a conversation stopped on a question answers it from the zones' column
// exactly as it does from a strip over the list — the row is the same row, and
// the column it stands in is a fact about the frame and not about the door.
func TestADigitAnswersTheQuestionFromTheZonesColumn(t *testing.T) {
	a, _ := bridgeLab(t)
	sent := 0
	a.leaveAnswer = func(string, session.QuestionKind, uint64, string) error {
		sent++
		return nil
	}
	a.home.cursor = homeRest
	a.homeKey(key("left"))
	line, ok := a.home.focusedLine()
	if !ok || attentionWordOf(line) != attentionNeedsWord {
		t.Fatalf("← did not reach the needs-you row:\n%s", homeText(a))
	}
	a.homeKey(key("3"))
	if sent != 1 {
		t.Fatalf("a digit in the zones' column sent %d answers, want 1:\n%s", sent, homeText(a))
	}
	if typed := a.home.box.String(); typed != "" {
		t.Fatalf("the digit also typed %q into the box", typed)
	}
}

// A PRESS IN THE ZONES' COLUMN IS A PRESS ON THAT ZONE'S ROW. One screen row
// carries two lines now — a zone row and a places row — and the x is what tells
// them apart.
func TestThePointerTellsTheTwoLeftColumnsApart(t *testing.T) {
	a, _ := bridgeLab(t)
	width, height := a.size()
	lines, hits, _, _ := a.homeFrame(width, height)
	row := -1
	for y, line := range lines {
		if strings.HasPrefix(ansi.Strip(line), "  "+homeAskGlyph+" ") {
			row = y
			break
		}
	}
	if row < 0 {
		t.Fatalf("no needs-you row on the frame:\n%s", homeText(a))
	}
	if hits[row] < 0 || attentionWordOf(a.home.lines[hits[row]]) != "" {
		t.Fatalf("the screen row's own hit is a zone row, so this proves nothing")
	}
	a.homePress(3, row)
	line, ok := a.home.focusedLine()
	if !ok || attentionWordOf(line) != attentionNeedsWord {
		t.Fatalf("a press in the first column landed on %q:\n%s", attentionWordOf(line), homeText(a))
	}
	// AND A PRESS ACROSS THE GUTTER IS THE LIST'S, on the same screen row.
	a.homePress(homeAttentionCol+homeGutter+1, row)
	if line, ok := a.home.focusedLine(); !ok || attentionWordOf(line) != "" {
		t.Fatalf("a press in the middle column landed in %q:\n%s", attentionWordOf(line), homeText(a))
	}
}

// THE TWO COLUMNS SCROLL SEPARATELY, because they are two windows onto one list
// and only one of them holds the cursor.
func TestTheZoneColumnDoesNotScrollWithTheList(t *testing.T) {
	a, _ := bridgeLab(t)
	a.height = 12
	width, height := a.size()
	a.homeFrame(width, height)
	if a.home.zoneTop != 0 {
		t.Fatalf("the zones scrolled with the cursor in the list (top %d)", a.home.zoneTop)
	}
	for i := 0; i < 40; i++ {
		a.home.move(1)
	}
	a.homeFrame(width, height)
	if a.home.zoneTop != 0 {
		t.Fatalf("walking to the bottom of the list scrolled the zones (top %d)", a.home.zoneTop)
	}
	if a.home.top <= a.home.placesFrom() {
		t.Fatalf("the list never scrolled at all (top %d, first place %d)", a.home.top, a.home.placesFrom())
	}
}
