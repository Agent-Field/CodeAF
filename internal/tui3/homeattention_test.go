package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

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
	// AND THE CAP AND ITS DOOR HOLD IN THE ZONES' OWN COLUMN. At the columns tier
	// the strip is [homeAttentionCol] cells wide instead of the list's whole
	// width; the rows are the same rows, and a fold that only worked over the
	// list would leave the widest frame with five rows and no way to the rest.
	a.width, a.height = 140, 30
	wide := homeText(a)
	if !a.home.columns() {
		t.Fatalf("a %d-column frame is not the columns tier", a.width)
	}
	if names := zoneNames(a, attentionMovingWord); len(names) != attentionMovingShown {
		t.Fatalf("`moving` drew %d rows in its own column, want %d: %v\n%s",
			len(names), attentionMovingShown, names, wide)
	}
	door := false
	for _, line := range homeLines(a) {
		if strings.Contains(zoneColumnOf(line), "…2 more") {
			door = true
		}
	}
	if !door {
		t.Fatalf("the fold's door is not in the zones' own column:\n%s", wide)
	}
	fold = -1
	for at, line := range a.home.lines {
		if line.kind == homeAttentionMore {
			fold = at
		}
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

// zoneColumnOf is the part of a drawn frame row that belongs to the zones' own
// column at [homeTierColumns], which is what turns "the word is on the screen"
// into "the word is where the geography says it stands".
func zoneColumnOf(line string) string {
	if len(line) < homeAttentionCol {
		return line
	}
	return line[:homeAttentionCol]
}

// quietWideLab is a machine with two conversations and nothing at all happening
// to either, on a frame wide enough for three columns — which is the one shape
// where an empty zone has a column of its own to teach in.
func quietWideLab(t *testing.T) *app {
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

// AN EMPTY ZONE TEACHES INSTEAD OF STANDING MUTE, and only where it has a column
// to teach in. The line says what ARRIVES in the region — never that nothing has.
func TestAnEmptyZoneTeachesAtTheColumnsTierAndNowhereElse(t *testing.T) {
	a := quietWideLab(t)
	for _, zone := range homeZones {
		if names := zoneNames(a, zone.word); len(names) != 0 {
			t.Fatalf("the %q zone has %v in it, so it teaches nothing", zone.word, names)
		}
	}
	text := homeText(a)
	for _, zone := range homeZones {
		if !strings.Contains(text, zone.teach) {
			t.Fatalf("the empty %q zone said nothing about what lands in it:\n%s", zone.word, text)
		}
	}
	// AND IT IS A CAPTION ON THE LABEL: the very next line, with nothing between
	// them, in the zones' own column.
	lines := homeLines(a)
	for _, zone := range homeZones {
		at := -1
		for y, line := range lines {
			if strings.Contains(zoneColumnOf(line), zone.word) {
				at = y
				break
			}
		}
		if at < 0 || at+1 >= len(lines) {
			t.Fatalf("the %q label is not in the zones' column at all:\n%s", zone.word, text)
		}
		if head := zoneColumnOf(lines[at+1]); !strings.Contains(head, zone.teach) {
			t.Fatalf("the %q teaching line is not under its label: %q", zone.word, head)
		}
	}
	// AND THE TWO NARROWER TIERS SAY NOTHING. There the strips stand over the
	// list at its full width, where a sentence of explanation is prose across the
	// top of an index rather than a caption in a column.
	for _, width := range []int{homeMinColumns - 1, homeMinDetail - 1} {
		a.width = width
		narrow := homeText(a)
		for _, zone := range homeZones {
			if strings.Contains(narrow, zone.teach) {
				t.Fatalf("a %d-column frame taught under %q anyway:\n%s", width, zone.word, narrow)
			}
		}
	}
}

// AND A ZONE WITH ROWS IN IT TEACHES NOTHING. The rows are the teaching.
func TestAZoneWithRowsInItDrawsNoTeachingLine(t *testing.T) {
	a, _ := bridgeLab(t)
	for _, zone := range homeZones {
		if len(zoneNames(a, zone.word)) == 0 {
			t.Fatalf("the %q zone is empty on this machine, so it proves nothing", zone.word)
		}
	}
	text := homeText(a)
	for _, zone := range homeZones {
		if strings.Contains(text, zone.teach) {
			t.Fatalf("the %q zone drew its teaching line over its own rows:\n%s", zone.word, text)
		}
	}
}

// A TEACHING LINE IS NOT A ROW. It belongs to the label above it, so no key can
// leave the cursor standing on one and no click can select it.
func TestATeachingLineIsNotACursorStop(t *testing.T) {
	if (homeLine{kind: homeAttentionTeach}).stop() {
		t.Fatal("a teaching line says a cursor may rest on it")
	}
	a := quietWideLab(t)
	a.home.cursor = homeRest
	for i := 0; i < len(a.home.lines)+4; i++ {
		a.home.move(1)
		if at := a.home.cursor; at >= 0 && a.home.lines[at].kind == homeAttentionTeach {
			t.Fatalf("↓ %d times left the cursor on a teaching line:\n%s", i+1, homeText(a))
		}
	}
	for i := 0; i < len(a.home.lines)+4; i++ {
		a.home.move(-1)
		if at := a.home.cursor; at >= 0 && a.home.lines[at].kind == homeAttentionTeach {
			t.Fatalf("↑ %d times left the cursor on a teaching line:\n%s", i+1, homeText(a))
		}
	}
}

// THE WORDS TEACH AND NEVER ANNOUNCE ABSENCE, and they fit the column they are
// drawn in — which does not grow, so a sentence that outran it would arrive on
// the screen with an ellipsis through it.
func TestTheTeachingLinesFitTheZoneColumnAndNameNoAbsence(t *testing.T) {
	for _, zone := range homeZones {
		if zone.teach == "" {
			t.Fatalf("the %q zone has nothing to say when it is empty", zone.word)
		}
		if got := ansi.StringWidth(zone.teach); got > homeAttentionCol-2 {
			t.Fatalf("the %q teaching line is %d cells wide in a %d-cell column",
				zone.word, got, homeAttentionCol-2)
		}
		// THE EMPTINESS LAW IN WORDS. `no tasks yet` and every sentence like it
		// were taken off this surface on purpose; what replaces one may not be the
		// same announcement in a quieter voice.
		for _, banned := range []string{"nothing", "empty", "yet", "no "} {
			if strings.Contains(zone.teach, banned) {
				t.Fatalf("the %q teaching line announces absence with %q: %q",
					zone.word, banned, zone.teach)
			}
		}
	}
}

// THE TWO STRIPS ARE TWO BLOCKS, and the spacing ladder gives a block boundary
// exactly one blank row — never two, and never one above the first strip.
func TestOneBlankRowSeparatesTheTwoZones(t *testing.T) {
	for _, machine := range []struct {
		word  string
		build func(t *testing.T) *app
	}{
		{"busy", func(t *testing.T) *app { a, _ := bridgeLab(t); return a }},
		{"quiet", quietWideLab},
	} {
		a := machine.build(t)
		gaps := 0
		for at, line := range a.home.lines[:a.home.zoneSplit()] {
			if line.kind != homeAttentionGap {
				continue
			}
			gaps++
			if at == 0 {
				t.Fatalf("the %s machine opened its first strip with a blank row", machine.word)
			}
			if a.home.lines[at-1].kind == homeAttentionGap {
				t.Fatalf("the %s machine put two blank rows between two strips", machine.word)
			}
			if at+1 >= a.home.zoneSplit() || a.home.lines[at+1].kind != homeAttentionZone {
				t.Fatalf("the %s machine's blank row is not the head of a strip", machine.word)
			}
		}
		if gaps != len(homeZones)-1 {
			t.Fatalf("the %s machine put %d blank rows between %d strips, want %d",
				machine.word, gaps, len(homeZones), len(homeZones)-1)
		}
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

// zoneRowNamed is the index of the `needs you` row a zone gave a given name,
// which is the only handle a test has on ONE row of a strip.
func zoneRowNamed(t *testing.T, a *app, name string) int {
	t.Helper()
	for at, line := range a.home.lines {
		if attentionWordOf(line) == attentionNeedsWord && line.zone.name == name {
			return at
		}
	}
	t.Fatalf("`needs you` has no row named %q:\n%s", name, homeText(a))
	return -1
}

// A NEEDS-YOU ROW NAMED AFTER A PIECE OF WORK LANDS ON THAT PIECE OF WORK.
//
// This is the defect the door was fixed for. A conversation that has run
// forty-five tasks grows a `needs you` row per landing nobody has judged, each
// named after its task — and the door used to open the bare conversation, which
// put a person on the live edge of a transcript with no trace of the thing the
// row they pressed was about. The row knew which task it stood for; the line did
// not carry it, so the door could not aim.
func TestANeedsYouRowForLandedWorkOpensThatWorksRecord(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", lab.project("-tmp-alpha"), now)
	other := lab.session("-tmp-beta", "bbbb000000000001", "anthropic ipo insights",
		lab.project("-tmp-beta"), now.Add(-5*24*time.Hour))
	lab.task("-tmp-beta", session.TaskIndexEntry{
		ID: "7", SessionID: "bbbb000000000001", Title: "illustrate chapter two",
		Status: string(session.TaskUnverified), EndedAt: now.Add(-4 * 24 * time.Hour),
		Outcome: "four plates, one per scene", FilesChanged: 4,
	})
	// A SECOND LANDING IN THE SAME CONVERSATION, so that arriving on the right
	// one is a claim with something to be wrong about.
	lab.task("-tmp-beta", session.TaskIndexEntry{
		ID: "8", SessionID: "bbbb000000000001", Title: "generate the avengers quiz",
		Status: string(session.TaskUnverified), EndedAt: now.Add(-3 * 24 * time.Hour),
	})

	a := lab.app(mine)
	a.agent = &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	a.openHome()

	// THE ROW STILL READS HONESTLY AT A GLANCE: the lead says the work landed,
	// and the age tail says how long it has been standing there.
	a.home.cursor = zoneRowNamed(t, a, "illustrate chapter two")
	width, _ := a.size()
	row := ansi.Strip(a.attentionRow(a.home.lines[a.home.cursor], a.home.cursor, width, a.pal))
	for _, word := range []string{homeLandedWord, "4d"} {
		if !strings.Contains(row, word) {
			t.Fatalf("the landed row lost %q: %q", word, row)
		}
	}

	runCmd(a.homeEnter())

	if a.home.open {
		t.Fatalf("the row left home up saying %q", a.home.msg)
	}
	if a.file != other {
		t.Fatalf("the row opened %q, want the conversation that ran the task %q", a.file, other)
	}
	if !a.taskSheet.open || !a.taskSheet.detailOn {
		t.Fatalf("the row landed on the bare conversation: page open %v, card %v",
			a.taskSheet.open, a.taskSheet.detailOn)
	}
	if got := a.taskSheet.detail; got.ID != "7" || got.SessionID != "bbbb000000000001" {
		t.Fatalf("the card is standing on task %q of %q, want the row's own", got.ID, got.SessionID)
	}
	// AND THE CARD HAS THE WORK IN FRONT OF IT — what it was called, what came of
	// it, and what it wrote — off the record row the line carried, with no second
	// reading of anything.
	card := ansi.Strip(strings.Join(a.taskCardBody(a.taskSheet.detail, width-2), "\n"))
	for _, word := range []string{"four plates, one per scene", "4" + taskCardFilesMany} {
		if !strings.Contains(card, word) {
			t.Fatalf("the card does not say %q:\n%s", word, card)
		}
	}
	if title := ansi.Strip(a.taskCardTitle(width, a.taskSheet.detail)); !strings.Contains(title, "illustrate chapter two") {
		t.Fatalf("the card is not headed by the task the row named: %q", title)
	}
}

// ONE DOOR, BOTH HANDS. A click on the row arrives exactly where enter did,
// because the pointer's second press is [app.homeEnter] and not a second
// spelling of it.
func TestAClickOnALandedNeedsYouRowLandsOnTheSameRecord(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", lab.project("-tmp-alpha"), now)
	lab.session("-tmp-beta", "bbbb000000000001", "anthropic ipo insights",
		lab.project("-tmp-beta"), now.Add(-5*24*time.Hour))
	lab.task("-tmp-beta", session.TaskIndexEntry{
		ID: "7", SessionID: "bbbb000000000001", Title: "illustrate chapter two",
		Status: string(session.TaskUnverified), EndedAt: now.Add(-4 * 24 * time.Hour),
	})

	a := lab.app(mine)
	a.agent = &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	a.openHome()

	at := zoneRowNamed(t, a, "illustrate chapter two")
	// The first press puts the cursor on the row and the second opens it, which
	// is this column's two-step for every row it has ([app.homePress]).
	homeClickAt(t, a, at)
	homeClickAt(t, a, at)

	if !a.taskSheet.detailOn || a.taskSheet.detail.ID != "7" {
		t.Fatalf("a click landed on card %q (page open %v), want the row's own task",
			a.taskSheet.detail.ID, a.taskSheet.open)
	}
}

// AND A PLAIN NEEDS-YOU ROW IS THE DOOR IT ALWAYS WAS. A conversation stopped on
// a question stands for no one piece of work, so it opens the conversation and
// nothing is raised over it.
func TestARowThatNamesNoTaskRaisesNoRecordPage(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", lab.project("-tmp-alpha"), now)
	lab.session("-tmp-beta", "bbbb000000000001", "pricing research",
		lab.project("-tmp-beta"), now.Add(-time.Hour))
	lab.asking("-tmp-beta", "bbbb000000000001", consentQuestion(7, "needs your ok to run bash"), now)

	a := lab.app(mine)
	a.agent = &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	a.openHome()

	at := zoneRowNamed(t, a, "Pricing Research")
	if a.home.lines[at].task != nil {
		t.Fatal("a waiting conversation's row claims to stand for one piece of work")
	}
	a.home.cursor = at
	runCmd(a.homeEnter())

	// A CONVERSATION THAT IS WAITING ON A QUESTION IS A CONVERSATION ANOTHER
	// WINDOW IS SITTING ON, so the door answers here exactly what it answered
	// before this lane: the lock is announced and nothing is opened. What matters
	// for this change is that no record page was raised over a conversation
	// nobody walked into.
	if a.home.msg != sessionBusyWord {
		t.Fatalf("enter on the waiting row said %q, want the lock", a.home.msg)
	}
	if a.taskSheet.open {
		t.Fatal("a refused row raised the record page")
	}
	// AND AN ORDINARY ROW IS UNTOUCHED: the conversation opens, and nothing
	// stands in front of it.
	quiet := lab.session("-tmp-gamma", "cccc000000000001", "the quiet one",
		lab.project("-tmp-gamma"), now.Add(-2*time.Hour))
	a.refreshHome()
	a.home.point(quiet)
	runCmd(a.homeEnter())
	if a.file != quiet {
		t.Fatalf("an ordinary row opened %q, want %q (%q)", a.file, quiet, a.home.msg)
	}
	if a.taskSheet.open {
		t.Fatal("a row that stands for no one piece of work raised the record page")
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
	a := newTestApp(&fakeAgent{model: "m"})
	pal := a.pal
	// bridge lane: [homeRest] is a line number no row has, so both marks come
	// back still — the one row that turns is chosen per page (homespinner.go).
	ask := a.attentionMark(pal, attentionNeedsWord, homeRest)
	moving := a.attentionMark(pal, attentionMovingWord, homeRest)
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
