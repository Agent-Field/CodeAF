package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── THE ATTENTION RULES, WHICH ARE THE SORT ORDER NOW ───────────────────────
//
// This file used to be about TWO STRIPS standing over the list — `needs you`
// and `moving` — and about the four things that made them worth their rows:
// everything blocked gathered in one place whatever kind of thing it was, the
// order being how long each had been standing still, a busy machine folding
// rather than filling the screen, and a row in a strip being the same live
// object as the row under its project.
//
// THE STRIPS ARE GONE AND EVERY ONE OF THOSE FOUR IS STILL TRUE — of the one
// flat ranked list itself (homeattention.go states the change). So each test
// here asks the same question of the new screen: the longest wait is the FIRST
// ROW rather than the first row of a strip, one thing has ONE row rather than
// two, the cap and its door belong to the whole list rather than to a zone, and
// a row of the list answers a digit and walks a door exactly as a strip row did.

// switchRowLine is the drawn frame row that carries a name, which is what turns
// "the word is somewhere on the screen" into "the row for that thing looks like
// this". It answers the empty string when nothing on the frame names it.
func switchRowLine(a *app, name string) string {
	for _, line := range homeLines(a) {
		if strings.Contains(line, name) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// switchNames is every conversation and standing item on the built column, in
// the order the list draws them — the flat list's answer to what the two strips
// used to be asked for.
func switchNames(a *app) []string {
	var out []string
	for _, line := range a.home.lines {
		if line.sw == nil || line.sw.row == nil {
			continue
		}
		switch line.sw.row.kind {
		case switcherConversation, switcherStanding:
			out = append(out, line.sw.row.title)
		}
	}
	return out
}

// EVERY BLOCKED THING, ANY PROJECT, ANY KIND, AND THE LONGEST WAIT FIRST — and
// now it is the TOP OF THE LIST rather than the top of a strip.
//
// The strip earned its rows because a project tree could not answer "what needs
// me"; a list already sorted by that answers it by existing, so what this test
// pins is the sort itself: the thing that has been stopped longest has cost the
// most already, so it is row one, and a watch that is asking counts as the same
// kind of blocked as a conversation that is asking.
func TestTheThingThatWaitedLongestIsTheFirstRowOfTheList(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	alpha, beta := lab.workspace("alpha"), lab.workspace("beta")
	mine := lab.session("-alpha", "aaaa000000000001", "the newest chat", alpha, now)
	lab.session("-beta", "bbbb000000000001", "pricing research", beta, now.Add(-4*time.Hour))
	// Another window, stopped on a card three hours ago — the oldest wait here.
	question := consentQuestion(7, "needs your ok to run bash")
	question.Asked = now.Add(-3 * time.Hour)
	lab.asking("-beta", "bbbb000000000001", question, now)

	// And a standing order that has been asking for an hour, which is the second
	// KIND of blocked thing and belongs in the same ranking rather than beside it.
	band := &standBand{}
	band.items = []standing.Item{bandItem("ask", "keep main green", alpha, standing.WhenProbe, "when CI goes red")}
	band.items[0].NeedsPerson = "the fix touches migrations"
	band.items[0].Updated = now.Add(-time.Hour)

	a := lab.app(mine)
	band.wire(a)
	a.width, a.height = 120, 30
	a.openHome()

	names := switchNames(a)
	if len(names) < 2 || names[0] != "Pricing Research" || names[1] != "keep main green" {
		t.Fatalf("the list reads %v, want the three-hour wait over the one-hour wait:\n%s", names, homeText(a))
	}
	// AND THE TWO BLOCKED ROWS STAND OVER EVERYTHING ELSE, whatever their kind.
	// This is the whole of what the two strips were gathering.
	if at := switchNameAt(names, "The Newest Chat"); at >= 0 && at < 2 {
		t.Fatalf("a quiet conversation sorted above something that is waiting: %v", names)
	}
	// AND A STAMP NOBODY RECORDED GOES LAST rather than to the top of a list
	// ordered by how long something has been standing still ([attentionOlder] is
	// the one comparison this rank sorts with, and the unknown is the one case a
	// screen cannot show because a lab cannot write a row without an age).
	if !attentionOlder(now.Add(-time.Hour), time.Time{}) || attentionOlder(time.Time{}, now) {
		t.Fatal("an unrecorded stamp claims to be the oldest wait on the machine")
	}
}

// switchNameAt is where a name stands in the list, and -1 when it is not drawn.
func switchNameAt(names []string, want string) int {
	for i, name := range names {
		if name == want {
			return i
		}
	}
	return -1
}

// ONE THING HAS ONE ROW. A conversation used to be a strip row AND a row under
// its project at the same moment, which is what forced the cursor restore to
// prefer one of them; with one flat list there is nothing left to prefer
// ([homeView.pointAt]), and a rescan three seconds later must leave the cursor
// exactly where a person put it.
func TestOneConversationHasOneRowAndARescanLeavesTheCursorOnIt(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(120, 40)
	row := ""
	for _, line := range a.home.lines {
		if line.kind == homeSession && strings.Contains(homeName(line.row), "Bounty") {
			row = line.row.Transcript
		}
	}
	if row == "" {
		t.Fatalf("the moving conversation has no row at all:\n%s", homeText(a))
	}
	rows := 0
	for _, line := range a.home.lines {
		if line.kind == homeSession && line.row.Transcript == row {
			rows++
		}
	}
	if rows != 1 {
		t.Fatalf("one conversation has %d rows on the flat list:\n%s", rows, homeText(a))
	}
	a.home.point(row)
	stood := a.home.cursor
	a.refreshHome()
	if a.home.cursor != stood {
		t.Fatalf("a rescan moved the cursor from line %d to %d", stood, a.home.cursor)
	}
	if line, ok := a.home.focusedLine(); !ok || line.row.Transcript != row {
		t.Fatalf("a rescan left the cursor on something else:\n%s", homeText(a))
	}
}

// THE CAP AND ITS DOOR BELONG TO THE WHOLE LIST NOW. The `moving` strip capped
// at five and folded; the one list caps at [switcherShown] and folds once, and
// the fold is the same two-arrow gesture every other fold on this surface has —
// `→` opens what is closed and `←` closes what is open.
//
// That enter opens and closes it is place_home_test.go's; what is here is the
// ARROWS, which are the gesture a hand already knows from the task column and
// from a project's own tail.
func TestTheOneFoldOpensAndClosesOnTheArrows(t *testing.T) {
	lab := newSwitchLab(t)
	// A FRAME THE LIST CANNOT FILL, because the list now grows to the frame it is
	// given (switcher.go's [switcherView.room]) and a forty-row window over twelve
	// conversations has nothing left to fold.
	a := lab.open(120, 19)
	foldAt := func() int {
		for at, line := range a.home.lines {
			if line.kind == homeSwitchFold {
				return at
			}
		}
		t.Fatalf("the list drew no fold at all:\n%s", homeText(a))
		return homeNoLine
	}
	// AND THE ROW THE FOLD STANDS OVER IS LOOKED FOR ON THE LIST RATHER THAN ON
	// THE FRAME. What a fold hides is now exactly what the window could not have
	// shown anyway, so opening it puts the rows on the list — where `↓` reaches
	// them — and not necessarily on the visible frame.
	onTheList := func(title string) bool {
		for _, line := range a.home.lines {
			if line.kind == homeSession && strings.Contains(homeName(line.row), title) {
				return true
			}
		}
		return false
	}
	a.home.cursor = foldAt()
	if !strings.Contains(homeText(a), "more, quiet since") {
		t.Fatalf("the fold does not say what it stands for:\n%s", homeText(a))
	}
	if onTheList("Quiet Chat I") {
		t.Fatalf("the shut fold is standing over a row that is on the list anyway:\n%s", homeText(a))
	}
	a.homeKey(key("right"))
	if !onTheList("Quiet Chat I") {
		t.Fatalf("→ did not open the fold:\n%s", homeText(a))
	}
	a.home.cursor = foldAt()
	a.homeKey(key("left"))
	if onTheList("Quiet Chat I") {
		t.Fatalf("← did not fold the tail back away:\n%s", homeText(a))
	}
}

// NOTHING IS DRAWN FOR A STATE THE MACHINE IS NOT IN, AT EVERY WIDTH.
//
// This is where the strips' one exception died. `needs you` and `moving` used to
// keep their labels over nothing at the widest tier — stable geography beating
// emptiness, because a map that redraws itself is not a map — and the labels
// were the map. With one list there is no map to keep still: the claim over the
// list, the `since you left` heading and the fold are each a sentence about
// something that has happened, and a quiet morning has none of them to say. So
// the emptiness law applies here with no exception left in it.
func TestAQuietMachineDrawsNothingForAStateItIsNotIn(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	alpha := lab.workspace("alpha")
	mine := lab.session("-alpha", "aaaa000000000001", "porting the picker", alpha, now.Add(-time.Hour))
	lab.session("-beta", "bbbb000000000001", "pricing research", lab.workspace("beta"), now.Add(-3*time.Hour))
	a := lab.app(mine)
	a.height = 30
	a.openHome()
	// The look stamp is now, so nothing at all happened while nobody was looking.
	a.home.seen = now
	a.home.build()

	// Both rungs of the ladder, because the exception this replaces lived at the
	// wide one (homebridge.go).
	for _, width := range []int{80, 120, homeCardMin, 200} {
		a.width = width
		text := homeText(a)
		for _, claim := range []string{"what wants you first", "since you left", "more, quiet"} {
			if strings.Contains(text, claim) {
				t.Fatalf("a %d-column quiet machine claimed %q:\n%s", width, claim, text)
			}
		}
		// AND THE ROWS ARE STILL THERE. What emptiness takes off the screen is a
		// claim about a state, never a thing that exists.
		if !strings.Contains(text, "Porting the Picker") || !strings.Contains(text, "Pricing Research") {
			t.Fatalf("a %d-column quiet machine dropped its conversations:\n%s", width, text)
		}
	}
}

// A LINE THAT NAMES ROWS IS NOT A ROW. The teaching line under an empty zone
// was the first of these; the switcher's headings are the ones left
// ([homeSwitchHead] — the claim over the list, the `since you left` heading and
// a project's name while `alt+g` groups). No key may leave the cursor standing
// on one, in either direction, which is the whole of what "not a stop" means to
// a person's hands.
func TestNoArrowLeavesTheCursorOnAHeadingOrABlank(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(120, 40)
	// A ledger and a grouping, so that every kind of heading this list has is on
	// the column while the walk goes over it.
	a.home.seen = lab.now.Add(-30 * time.Minute)
	if !a.placeAlt('g') {
		t.Fatal("alt+g did nothing, so the project headings are not on the column")
	}
	if (homeLine{kind: homeSwitchHead}).stop() {
		t.Fatal("a heading of the list says a cursor may rest on it")
	}
	for _, step := range []struct {
		word string
		by   int
	}{{"↓", 1}, {"↑", -1}} {
		a.home.cursor = homeNoLine
		for i := 0; i < len(a.home.lines)+4; i++ {
			a.home.move(step.by)
			at := a.home.cursor
			if at < 0 {
				continue
			}
			if kind := a.home.lines[at].kind; kind == homeSwitchHead || kind == homeBlank {
				t.Fatalf("%s %d times left the cursor on line %d, which names rows rather than being one:\n%s",
					step.word, i+1, at, homeText(a))
			}
		}
	}
}

// THE WORDS THIS LIST STANDS ON SAY WHAT IS THERE AND NEVER WHAT IS NOT.
//
// The teaching lines under the empty zones were held to this and they are gone;
// the law is not. `no tasks yet` and every sentence like it were taken off this
// surface on purpose, and the four sentences the switcher owns — the claim over
// the list and the two views it names, and the fold at the foot — may not
// smuggle one back in a quieter voice.
func TestTheSwitchersOwnWordsNeverAnnounceAbsence(t *testing.T) {
	for _, word := range []string{switcherGroupWord, switcherQuietWord, foldLine(15, "quiet since aug 21")} {
		for _, banned := range []string{"nothing", "empty", " yet", "no "} {
			if strings.Contains(word, banned) {
				t.Fatalf("%q announces absence with %q", word, banned)
			}
		}
	}
	lab := newSwitchLab(t)
	a := lab.open(120, 40)
	if !strings.Contains(homeText(a), "what wants you first") {
		t.Fatalf("the list's own claim is not on the screen:\n%s", homeText(a))
	}
}

// ONE BLANK ROW BETWEEN TWO BLOCKS, NEVER TWO AND NEVER ONE AT THE TOP.
//
// The two strips were two blocks and THE SPACING LADDER gave their boundary
// exactly one blank row. The list has more blocks than that now — the errands,
// the `since you left` ledger, the claim over the ranked rows, a heading per
// project under `alt+g` — and they are all separated by the same one row
// ([switcherReading.addSectionLine] holds the rule), so the ladder is asked of
// the whole column rather than of one seam in it.
func TestOneBlankRowSeparatesTheBlocksOfTheList(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(120, 40)
	a.home.seen = lab.now.Add(-30 * time.Minute)
	if !a.placeAlt('g') {
		t.Fatal("alt+g did nothing, so this column has only one block in it")
	}
	blanks := 0
	for at, line := range a.home.lines {
		if line.kind != homeBlank {
			continue
		}
		blanks++
		if at == 0 {
			t.Fatalf("the column opened with a blank row:\n%s", homeText(a))
		}
		if a.home.lines[at-1].kind == homeBlank {
			t.Fatalf("two blank rows stand between two blocks at line %d:\n%s", at, homeText(a))
		}
		if at+1 >= len(a.home.lines) {
			t.Fatalf("the column ends on a blank row:\n%s", homeText(a))
		}
	}
	if blanks == 0 {
		t.Fatalf("this column has no block boundary in it at all, so it proves nothing:\n%s", homeText(a))
	}
}

// A ROW OF THE LIST IS A DOOR OF THE KIND IT ALWAYS WAS. The card beside it is
// that conversation's, a digit over it answers the window it belongs to through
// the road the answer band already rides, and enter walks the conversation's own
// door — which in this lab offers to MOVE the conversation, because the one it
// names is the one another window is sitting on ([app.homeOpenLine]'s third
// check, takeover.go). That offer is the proof: a row that was not a
// conversation's door would have folded something, or done nothing at all.
func TestARowThatIsWaitingIsADoorOfTheKindItAlwaysWas(t *testing.T) {
	lab := newAnswerLab(t, consentQuestion(7, "needs your ok to run bash"), time.Now())
	a := lab.a
	// The card only exists where the width is genuinely spare (homebridge.go), and
	// the question's own chips are on it.
	a.width = 200
	a.openHome()
	a.home.point(lab.row)
	line, ok := a.home.focusedLine()
	if !ok || line.kind != homeSession {
		t.Fatalf("the waiting conversation has no row of its own:\n%s", homeText(a))
	}
	if !strings.Contains(homeText(a), "1 allow once") {
		t.Fatalf("the card beside the row is not the question's:\n%s", homeText(a))
	}
	a.homeKey(key("3"))
	if len(*lab.sent) != 1 {
		t.Fatalf("a digit over the row sent %d answers, want 1", len(*lab.sent))
	}
	if answer := (*lab.sent)[0]; answer.kind != session.QuestionConsent || answer.id != 7 || answer.key != "3" {
		t.Fatalf("the row answered %+v", answer)
	}
	a.homeEnter()
	if !strings.Contains(a.home.msg, "enter again to move it here") {
		t.Fatalf("enter on the row did not walk the conversation's door: %q", a.home.msg)
	}
}

// WAITING OUTRANKS WORKING, once more: a conversation stopped on a question is
// a needs-you row and nothing else, however much work it has out. The two states
// used to be two strips and are two RANKS now ([switcherRank]), so the fact that
// a thing can only be in one of them shows up as the mark on its one row.
func TestAWaitingConversationIsNotAlsoAMovingOne(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-alpha", "aaaa000000000001", "the newest chat", lab.workspace("alpha"), now)
	lab.session("-beta", "bbbb000000000001", "pricing research", lab.workspace("beta"), now.Add(-time.Hour))
	lab.asking("-beta", "bbbb000000000001", consentQuestion(7, "needs your ok to run bash"), now)
	lab.task("-beta", session.TaskIndexEntry{
		ID: "1", SessionID: "bbbb000000000001", Title: "port the parser",
		Status: string(session.TaskRunning),
	})

	a := lab.app(mine)
	a.width, a.height = 120, 30
	a.openHome()

	rows := 0
	for _, line := range a.home.lines {
		if line.kind == homeSession && strings.Contains(homeName(line.row), "Pricing") {
			rows++
		}
	}
	if rows != 1 {
		t.Fatalf("a conversation that is both waiting and working has %d rows:\n%s", rows, homeText(a))
	}
	row := switchRowLine(a, "Pricing Research")
	if !strings.HasPrefix(row, tokens.GlyphNeedsHuman) {
		t.Fatalf("the row wears %q, want the needs-you mark %q:\n%s", row, tokens.GlyphNeedsHuman, homeText(a))
	}
	if strings.Contains(row, tokens.GlyphWorking) {
		t.Fatalf("one row claims both states at once: %q", row)
	}
}

// movingMark reports that a row's first cell says "this is happening right now":
// the still `◐` that every live row holds, or the turning cell the ONE row this
// frame animates wears instead (homespinner.go).
func movingMark(a *app, row string) bool {
	return strings.HasPrefix(row, tokens.GlyphWorking) || strings.HasPrefix(row, a.homeSpinGlyph())
}

// A CONVERSATION MID-TURN IS MOVING WITH NOTHING COMMISSIONED. The model
// thinking is work in flight, and it is the fact the presence file writes off
// the turn rather than off the task graph — so the row wears the moving mark
// with an empty task rollup behind it.
func TestAConversationMidTurnIsMovingWithNoTasksAtAll(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-alpha", "aaaa000000000001", "the newest chat", lab.workspace("alpha"), now)
	lab.session("-beta", "bbbb000000000001", "pricing research", lab.workspace("beta"), now.Add(-3*time.Minute))
	lab.presence("-beta", "bbbb000000000001", session.PresenceWorking, "", now)

	a := lab.app(mine)
	a.width, a.height = 120, 30
	a.openHome()

	// THE ONE MOVING ROW IS ALSO THE ONE THE PAGE ANIMATES, so the mark it wears
	// is the turning cell rather than the still `◐` (homespinner.go). They are the
	// same claim at two tiers, and the still one is what every OTHER live row
	// holds — which is what [TestExactlyOneRowIsGivenTheSpinnerHoweverManyAreMoving]
	// pins one file over.
	if row := switchRowLine(a, "Pricing Research"); !movingMark(a, row) {
		t.Fatalf("the mid-turn row reads %q, want a moving mark:\n%s", row, homeText(a))
	}
	if names := switchNames(a); len(names) == 0 || names[0] != "Pricing Research" {
		t.Fatalf("the mid-turn conversation is not what is moving: %v\n%s", names, homeText(a))
	}
}

// A LINE ABOUT A PIECE OF WORK LANDS ON THAT WORK'S PLACE.
//
// This is the defect the old door was fixed for, asked of the screen that
// replaced it. A `needs you` row named after a landing used to open the bare
// conversation, which put a person on the live edge of a transcript with no
// trace of the thing they had pressed. There is no such row now — work that
// landed is a LINE OF THE LEDGER at the top of the list — and the same law holds
// over it: the line says how many tasks landed, and enter goes to the place that
// holds them rather than to a conversation that happens to have run one.
func TestTheLedgerLineAboutLandedWorkOpensTheTasksPlace(t *testing.T) {
	lab := newSwitchLab(t)
	now := lab.now
	lab.task("-alpha", session.TaskIndexEntry{ID: "t9", SessionID: "aaaa000000000001",
		Title: "toy-scale validation", Label: "toy-scale validation",
		Status: string(session.TaskDone), EndedAt: now.Add(-time.Minute), FilesChanged: 1})
	a := lab.open(120, 40)
	// A look stamp is what makes anything "since you left" at all.
	a.home.seen = now.Add(-30 * time.Minute)
	a.home.build()

	at := homeNoLine
	for i, line := range a.home.lines {
		if line.kind == homeLedger && line.project == "tasks" {
			at = i
		}
	}
	if at == homeNoLine {
		t.Fatalf("nothing on the ledger is about work that landed:\n%s", homeText(a))
	}
	// AND IT COUNTS IN A PERSON'S WORDS: one task landed, never `1 tasks`.
	if !strings.Contains(homeText(a), "1 task landed") {
		t.Fatalf("the ledger does not say what landed:\n%s", homeText(a))
	}
	// THE WORD IN THE MARGIN IS THE DOOR, which is why the two are one field
	// ([app.homeLedgerEnter]).
	if id, ok := parsePageWord(a.home.lines[at].project); !ok || id != pageTasks {
		t.Fatalf("the line about landed work names %q, which is not the place that holds it",
			a.home.lines[at].project)
	}
	a.home.cursor = at
	runCmd(a.homeEnter())
	// AND ENTER ASKS THAT PLACE. The conversation this window is holding has run
	// nothing of its own, so the tasks place answers with its own refusal — which
	// is the proof the door went THERE, rather than opening a conversation that
	// happened to have run one of the tasks the line counted.
	//
	// THE REFUSAL IS SAID ON THE FRAME AND NO LONGER IN THE TRANSCRIPT. It used
	// to be a note under a screen drawn over the top of it, which is a sentence
	// written where nobody can read it (pages.go's [app.refusePage]); the law
	// this line has always pinned — the door reached the place, and the place
	// answered — is unchanged.
	if a.page != pageTasks && a.pageMsg != taskSheetEmpty {
		t.Fatalf("enter on the line left page %v with nothing said: %q", a.page, a.pageMsg)
	}
}

// ONE DOOR, BOTH HANDS. A click on a row arrives exactly where enter did,
// because the pointer's second press is [app.homeEnter] and not a second
// spelling of it — the first press puts the cursor on the row and the second
// opens it ([app.homePress]).
func TestAClickOnARowArrivesWhereEnterDoes(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-alpha", "aaaa000000000001", "the one I am in", lab.workspace("alpha"), now)
	other := lab.session("-beta", "bbbb000000000001", "anthropic ipo insights",
		lab.workspace("beta"), now.Add(-5*24*time.Hour))

	a := lab.app(mine)
	a.width, a.height = 120, 30
	a.agent = &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	a.openHome()

	at := homeNoLine
	for i, line := range a.home.lines {
		if line.kind == homeSession && line.row.Transcript == other {
			at = i
		}
	}
	if at == homeNoLine {
		t.Fatalf("the other conversation has no row to press:\n%s", homeText(a))
	}
	homeClickAt(t, a, at)
	homeClickAt(t, a, at)
	if a.at(pageHome) {
		t.Fatalf("two presses left home up saying %q", a.home.msg)
	}
	if a.file != other {
		t.Fatalf("a click opened %q, want the row's own conversation %q", a.file, other)
	}
}

// AND THE ROW OPENS ITS CONVERSATION WITH NOTHING RAISED OVER IT. A row of this
// list stands for a whole conversation and never for one piece of work inside
// it, so there is no record page to put in front of the transcript.
func TestARowOpensItsConversationAndRaisesNothingOverIt(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-alpha", "aaaa000000000001", "the one I am in", lab.workspace("alpha"), now)
	quiet := lab.session("-gamma", "cccc000000000001", "the quiet one", lab.workspace("gamma"), now.Add(-2*time.Hour))
	lab.task("-gamma", session.TaskIndexEntry{
		ID: "7", SessionID: "cccc000000000001", Title: "illustrate chapter two",
		Status: string(session.TaskUnverified), EndedAt: now.Add(-4 * 24 * time.Hour),
	})

	a := lab.app(mine)
	a.width, a.height = 120, 30
	a.agent = &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	a.openHome()
	a.home.point(quiet)
	runCmd(a.homeEnter())

	if a.file != quiet {
		t.Fatalf("the row opened %q, want %q (%q)", a.file, quiet, a.home.msg)
	}
	if a.at(pageTasks) {
		t.Fatal("a row that stands for a whole conversation raised a record page over it")
	}
}

// A SEARCH HAS NO SWITCHER. The drop-up is the matches and the two action rows,
// and a ranked reading of the whole machine standing over a filter would be
// answering a question the person had stopped asking — which is also why the two
// views the switcher offers refuse to toggle while something is typed
// ([app.homeSwitchAlt]: a key that silently changed a list nobody can see is the
// worst kind of chord).
func TestTypingTakesTheSwitcherAway(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(120, 40)
	a.home.seen = lab.now.Add(-30 * time.Minute)
	a.home.build()
	if !strings.Contains(homeText(a), "what wants you first") {
		t.Fatalf("the switcher was not there to begin with:\n%s", homeText(a))
	}
	typeHome(a, "quiet")
	if !a.home.searching() {
		t.Fatal("typing into the box did not put home into a search")
	}
	text := homeText(a)
	for _, gone := range []string{"what wants you first", "since you left", "more, quiet", switcherGroupWord} {
		if strings.Contains(text, gone) {
			t.Fatalf("a search kept the switcher's %q:\n%s", gone, text)
		}
	}
	for _, line := range a.home.lines {
		if line.sw != nil {
			t.Fatalf("a search kept a line of the reading:\n%s", text)
		}
	}
	if a.placeAlt('g') {
		t.Fatal("alt+g regrouped a list that is not on the screen")
	}
}

// THE MARK CARRIES THE MEANING AND THE TEXT STAYS CALM. Four states, four marks
// in the first cell of a row, and only the two that are about RIGHT NOW spend
// ink: amber on the row waiting for a hand, the live hue on the row that is
// moving, and the dim on everything simply sitting there.
//
// The strips wore their hue on a label; the list wears it on the one cell that
// says what a row is doing, which is the same budget spent one scale down.
func TestEachStateHasItsOwnMarkAndOnlyTheTwoThatWantYouSpendInk(t *testing.T) {
	marks := []string{tokens.GlyphNeedsHuman, tokens.GlyphWorking, tokens.GlyphPaused, tokens.GlyphQueued}
	for i, mark := range marks {
		for j, other := range marks {
			if i != j && mark == other {
				t.Fatalf("two states wear the same mark %q", mark)
			}
		}
	}
	lab := newHomeLab(t)
	now := time.Now()
	alpha := lab.workspace("alpha")
	mine := lab.session("-alpha", "aaaa000000000001", "the quiet one", alpha, now.Add(-time.Hour))
	lab.session("-alpha", "aaaa000000000002", "the asking one", alpha, now.Add(-2*time.Hour))
	lab.asking("-alpha", "aaaa000000000002", consentQuestion(7, "needs your ok to run bash"), now)
	lab.session("-beta", "bbbb000000000001", "the moving one", lab.workspace("beta"), now.Add(-3*time.Minute))
	lab.presence("-beta", "bbbb000000000001", session.PresenceWorking, "", now)

	a := lab.app(mine)
	a.width, a.height = 120, 30
	a.openHome()
	for _, want := range []struct {
		name string
		mark string
	}{
		{"The Asking One", tokens.GlyphNeedsHuman},
		{"The Quiet One", tokens.GlyphQueued},
	} {
		if row := switchRowLine(a, want.name); !strings.HasPrefix(row, want.mark) {
			t.Fatalf("%q reads %q, want the %q mark:\n%s", want.name, row, want.mark, homeText(a))
		}
	}
	// AND THE MOVING ONE WEARS A MOVING MARK — the still `◐` on every live row
	// but the one this frame gave the spinner to, and the turning cell on that one
	// (homespinner.go). With a single thing in flight it is always that one.
	if row := switchRowLine(a, "The Moving One"); !movingMark(a, row) {
		t.Fatalf("%q reads %q, want a moving mark:\n%s", "The Moving One", row, homeText(a))
	}
	// AND THE STILL ROW IS STILL IN EVERY CHANNEL. A quiet conversation spends
	// neither the amber that means a hand is wanted nor the accent that means
	// something is happening this instant.
	pal := newPalette(tokens.TrueColor, false)
	for _, line := range a.home.reading.rows(120, pal) {
		if !strings.Contains(plain(line), "The Quiet One") {
			continue
		}
		for word, paint := range map[string]func(string) string{"amber": pal.warn, "the accent": pal.accent} {
			if strings.Contains(line, paint(tokens.GlyphNeedsHuman)) || strings.Contains(line, paint(tokens.GlyphWorking)) {
				t.Fatalf("a quiet row spent %s: %q", word, line)
			}
		}
	}
}
