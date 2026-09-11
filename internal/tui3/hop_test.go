package tui3

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE SWITCHER IS A CARD OVER A DIMMED SURFACE, AND THESE ARE THE FOUR CLAIMS
// THAT MAKE IT ONE (hop.go): what is on it, what the keys do, that nothing
// underneath is left looking live, and that a key which cannot act is neither
// bound nor advertised.

// keepThree puts two more conversations in the keeper and hands back the app
// standing in front of the third. THE NAMES COME OFF THE SIDECAR because a fake
// agent has no title to give, which is also the real fallback: the switcher asks
// the agent, then what the surface was calling it when it was left, and never
// the transcript's file name (hop.go's [hopTitle]).
func keepThree(t *testing.T, a *app) (older, newer *fakeAgent) {
	t.Helper()
	// THE MACHINE IS PINNED EMPTY, and every test in this file that wants rows
	// below the fold hands its own closure. Without a seam the switcher's reading
	// is [session.ReadWorld] over whatever `~/.aforge` the suite is running as,
	// so a card asserted against the developer's own conversations would pass on
	// one machine and fail on the next (hop.go's [app.hopRest]).
	emptyMachine(a)
	older, newer = &fakeAgent{model: "m"}, &fakeAgent{model: "m"}
	a.stow(Conversation{
		Agent: older, SessionFile: "/tmp/lab/price-scrape.jsonl",
		Workspace: "/tmp/leadgen", Place: "leadgen",
	}, &aside{since: a.now().Add(-3 * time.Hour), title: "openrouter price scrape"})
	a.stow(Conversation{
		Agent: newer, SessionFile: "/tmp/lab/rail-scope.jsonl",
		Workspace: "/tmp/lab", Place: "lab",
	}, &aside{since: a.now().Add(-12 * time.Minute), title: "Refactor the rail scope model"})
	return older, newer
}

// emptyMachine pins the world seam to a machine with nothing else on it.
func emptyMachine(a *app) {
	a.world = func() (session.World, bool) { return session.World{}, true }
}

// TestTheSwitcherDrawsEveryOpenConversationWithHereLast is the reading, asserted
// row by row: the order, the seam, and where the cursor opens.
func TestTheSwitcherDrawsEveryOpenConversationWithHereLast(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)

	drive(t, a, key(hopOpenKey))
	if !a.hopShowing() {
		t.Fatal("ctrl+k did not raise the switcher")
	}
	rows := a.hop.rows
	if len(rows) != 3 {
		t.Fatalf("the card holds %d rows, and three conversations are open", len(rows))
	}
	// MOST RECENTLY IN FRONT FIRST, which is the order `tab` already walks.
	if !strings.Contains(rows[0].title, "rail scope") {
		t.Fatalf("the first row is %q, and the last conversation was rail-scope", rows[0].title)
	}
	if !strings.Contains(rows[1].title, "price scrape") {
		t.Fatalf("the second row is %q", rows[1].title)
	}
	// AND THE ONE ON SCREEN IS LAST, WITH THE SEAM ON IT.
	if !rows[2].here || rows[2].note != hopHereWord {
		t.Fatalf("the front conversation came out as %+v", rows[2])
	}
	// THE CURSOR OPENS ON THE ROW `tab` WOULD HAVE GONE TO, so the commonest
	// journey through this card is two keys.
	if a.hop.at != 0 {
		t.Fatalf("the cursor opened on row %d", a.hop.at)
	}
	// AND NEVER ON `you are here`, which is what makes `ctrl+k enter` land
	// somewhere on a session holding one conversation ([hopFirstStop]).
	if a.hop.rows[a.hop.at].here {
		t.Fatal("the cursor opened on the conversation the person is already in")
	}
	// The project and the age are the two things the tail says about a row it
	// did not have to ask the disk about.
	if rows[1].project != "leadgen" || rows[1].age == "" {
		t.Fatalf("the tail came out as project=%q age=%q", rows[1].project, rows[1].age)
	}
}

// TestTheSwitcherWalksWrapsAndGoes is the keyboard: down, up, round both ends,
// and the one key that actually moves the surface.
func TestTheSwitcherWalksWrapsAndGoes(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)

	drive(t, a, key(hopOpenKey))
	drive(t, a, key("tab"))
	if a.hop.at != 1 {
		t.Fatalf("tab left the cursor on %d", a.hop.at)
	}
	drive(t, a, key("shift+tab"))
	drive(t, a, key("shift+tab"))
	// A RING WRAPS AT BOTH ENDS. Walking up off the top lands on the bottom row
	// rather than stopping there, so an overshoot costs one key and not seven.
	if a.hop.at != len(a.hop.rows)-1 {
		t.Fatalf("walking up off the top left the cursor on %d of %d", a.hop.at, len(a.hop.rows))
	}
	// AND ANOTHER PRESS OF THE OPEN KEY IS ANOTHER STEP DOWN, which is what makes
	// this alt+tab rather than a menu: the gesture is one key, tapped.
	drive(t, a, key(hopOpenKey))
	if a.hop.at != 0 {
		t.Fatalf("a second ctrl+k left the cursor on %d", a.hop.at)
	}

	drive(t, a, key("tab"), key("enter"))
	if a.hopShowing() {
		t.Fatal("enter left the card up")
	}
	if a.file != "/tmp/lab/price-scrape.jsonl" {
		t.Fatalf("enter landed on %q", a.file)
	}
	// AND THE CONVERSATION THAT WAS IN FRONT IS STILL OPEN — a switch is not a
	// close (keeper.go), so the ring is the same size it was.
	if a.openCount() != 3 {
		t.Fatalf("%d conversations are open after the switch", a.openCount())
	}
}

// TestTheSwitcherPutsItselfAwayAndChangesNothing is `esc`: the whole point of a
// layer is that leaving it costs nothing.
func TestTheSwitcherPutsItselfAwayAndChangesNothing(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)

	drive(t, a, key(hopOpenKey), key("tab"), key("esc"))
	if a.hopShowing() {
		t.Fatal("esc left the card up")
	}
	if a.file != "/tmp/lab/this-one.jsonl" {
		t.Fatalf("esc moved the surface to %q", a.file)
	}
	// AND A KEY THAT MEANS NOTHING HERE PUTS IT AWAY WITHOUT REACHING THE DRAFT.
	drive(t, a, key(hopOpenKey), key("z"))
	if a.hopShowing() {
		t.Fatal("an unrelated key left the card up")
	}
	if !a.input.empty() {
		t.Fatalf("the key that closed the card also landed in the box as %q", a.input.String())
	}
}

// TestTheSurfaceUnderTheSwitcherIsDimmed is the depth claim, and it is the whole
// of what says `layer` on a surface that draws no borders (hop.go).
func TestTheSurfaceUnderTheSwitcherIsDimmed(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)
	if !a.pal.fading() {
		t.Skip("this palette has no depth ladder to fade down")
	}

	body := []string{"one", "two", "three", "four", "five", "six", "seven", "eight",
		"nine", "ten", "eleven", "twelve", "thirteen", "fourteen"}
	drive(t, a, key(hopOpenKey))
	out := a.hopOver(append([]string(nil), body...), 60, a.pal)
	if len(out) != len(body) {
		t.Fatalf("the layer changed the body's height from %d to %d", len(body), len(out))
	}
	dimmed, carded := 0, 0
	for i, line := range out {
		switch {
		case strings.Contains(line, body[i]):
			// The row survived as itself. It may only do that painted.
			if line == body[i] {
				t.Fatalf("row %d is still at full ink under the card: %q", i, line)
			}
			dimmed++
		default:
			carded++
		}
	}
	if dimmed == 0 {
		t.Fatal("nothing behind the card was dimmed")
	}
	if carded < 3 {
		t.Fatalf("the card wrote %d rows over the body", carded)
	}
	// AND THE CARD ITSELF IS NOT DRAWN AT THE FADED STOP. A card a person has to
	// squint at is a card that has taken the keyboard for nothing.
	head := ""
	for _, line := range out {
		if strings.Contains(plain(line), hopOpenWord) && strings.Contains(plain(line), "enter open") {
			head = line
		}
	}
	if head == "" {
		t.Fatalf("the card's head row is not on the frame:\n%s", strings.Join(out, "\n"))
	}
}

// TestASingleConversationHasNoSwitcherAndIsNeverToldAboutOne is the capability
// law: a key that cannot act says so by not being advertised, and the guard and
// the advertisement are one predicate so they cannot come apart.
func TestASingleConversationHasNoSwitcherAndIsNeverToldAboutOne(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	emptyMachine(a)
	// The legend slot is dropped whole under [hudTight], where the cells are
	// worth more to the conversation's name than to a reminder (render.go).
	a.width = 100

	drive(t, a, key(hopOpenKey))
	if a.hopShowing() || a.hopAvailable() {
		t.Fatal("the switcher opened over the only conversation this terminal holds")
	}
	if strings.Contains(a.legendRight(a.width), hopDoorWord) {
		t.Fatalf("the legend named the switcher with one conversation open: %q", a.legendRight(a.width))
	}

	// A SECOND CONVERSATION MAKES IT REAL, and the slot names BOTH doors: `tab`
	// is one key to the last one, and the card is every one of them.
	a.stow(Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: "/tmp/lab/other.jsonl"},
		&aside{since: a.now()})
	got := a.legendRight(a.width)
	if !strings.Contains(got, hopDoorWord) || !strings.Contains(got, lastDoorWord) {
		t.Fatalf("the legend names %q with two open", got)
	}

	// AND A MACHINE WITH OTHER CONVERSATIONS ON IT NAMES THE SWITCHER WITH NONE
	// OF THEM OPEN — which is the whole discoverability fix: on a fresh session
	// this key is the only thing that gets you anywhere, so it has to be on the
	// frame from the first one.
	fresh := newTestApp(&fakeAgent{model: "m"})
	emptyMachine(fresh)
	fresh.width, fresh.file = 100, "/tmp/lab/this-one.jsonl"
	if fresh.hopAvailable() {
		t.Fatal("the switcher is available before anything has counted the machine")
	}
	fresh.hopKnown = 6
	if !fresh.hopAvailable() {
		t.Fatal("the switcher is not available with six conversations on the machine")
	}
	if got := fresh.legendRight(fresh.width); !strings.Contains(got, hopDoorWord) {
		t.Fatalf("the legend does not name the switcher on a fresh session: %q", got)
	}
}

// TestTheSwitcherIsFrozenTheMomentItOpens guards the one race a list like this
// has: a conversation finishing a turn stirs the surface, and rows that re-ranked
// on that stir would move the row under the cursor mid-keystroke.
func TestTheSwitcherIsFrozenTheMomentItOpens(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)

	drive(t, a, key(hopOpenKey))
	first := a.hop.rows[0].title

	// Another conversation arrives in the keeper while the card is up.
	a.stow(Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: "/tmp/lab/late.jsonl"},
		&aside{since: a.now()})
	if len(a.hop.rows) != 3 || a.hop.rows[0].title != first {
		t.Fatalf("the card re-read itself under the cursor: %d rows, first %q",
			len(a.hop.rows), a.hop.rows[0].title)
	}
}

// TestTheSwitcherSaysWhatChangedWhileYouWereAway is the note column, which is
// what makes this a catch-up rather than a list of names.
func TestTheSwitcherSaysWhatChangedWhileYouWereAway(t *testing.T) {
	if got := hopNote(tabNeedsPerson, 4, 2); got != hopAskingWord {
		t.Fatalf("a conversation that wants a person says %q", got)
	}
	if got := hopNote(tabWorking, 3, 1); got != "3 tasks running" {
		t.Fatalf("three nodes turning says %q", got)
	}
	if got := hopNote(tabWorking, 1, 0); got != "1 task running" {
		t.Fatalf("one node turning says %q", got)
	}
	if got := hopNote(tabIdle, 0, 2); got != hopLandedWord {
		t.Fatalf("a turn that ended while away says %q", got)
	}
	if got := hopNote(tabIdle, 0, 0); got != hopNothingWord {
		t.Fatalf("a quiet conversation says %q", got)
	}
	// AND WORK NOTHING COUNTED STILL SAYS SO. A queued node and a background job
	// are both work the project index does not count, and the row that wears the
	// working mark for them must not fall through to the quiet words beside it —
	// which is the disagreement #708 was: `◐` on the tab, `nothing new` here.
	if got := hopNote(tabWorking, 0, 0); got != tabSignalWord(tabWorking) {
		t.Fatalf("work with no count says %q", got)
	}
	if got := hopNote(tabWorking, 0, 3); got != tabSignalWord(tabWorking) {
		t.Fatalf("work over a turn that landed while away says %q", got)
	}
}

// TestTheAliasIsBoundOnlyWhereTheTerminalCanSpellIt is the capability law again,
// on the one chord that has no legacy encoding (hop.go).
func TestTheAliasIsBoundOnlyWhereTheTerminalCanSpellIt(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)

	a.keysDisambiguated = false
	if a.hopOpens(hopAlias) {
		t.Fatal("ctrl+tab is bound on a terminal that cannot send it")
	}
	a.keysDisambiguated = true
	if !a.hopOpens(hopAlias) {
		t.Fatal("ctrl+tab is not bound on a terminal that answered the keyboard query")
	}
	drive(t, a, key(hopAlias))
	if !a.hopShowing() {
		t.Fatal("ctrl+tab did not raise the switcher where it can arrive")
	}
}

// TestTheCardHoldsTheWholeMachineAndOpensARowThatIsNotOpenYet is the half of the
// card that made the key worth binding: on a fresh session there is exactly one
// conversation open, and a switcher that listed only those would be a key that
// does nothing until somebody had already learned what it was for.
func TestTheCardHoldsTheWholeMachineAndOpensARowThatIsNotOpenYet(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = filepath.Join(dir, "this-one.jsonl")
	a.workspace = dir

	// A machine with two conversations on it, one of them the one on screen.
	other := filepath.Join(dir, "other.jsonl")
	a.world = func() (session.World, bool) {
		return session.World{Projects: []session.Project{{
			Name: "lab", Dir: dir,
			Sessions: []session.SessionRow{
				{ID: "a", Title: "this one", Transcript: a.file, ProjectDir: dir},
				{ID: "b", Title: "the other one", Transcript: other, ProjectDir: dir},
			},
		}}}, true
	}
	// The door the card opens a closed row through, standing in for cmd/aforge's.
	opened := ""
	a.open = func(workspace, transcript string) (Conversation, error) {
		opened = transcript
		return Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: transcript, Workspace: workspace}, nil
	}

	drive(t, a, key(hopOpenKey))
	if !a.hopShowing() {
		t.Fatal("the switcher did not open with one conversation open and another on the machine")
	}
	// THE CARD IS ABOUT WHAT IS OPEN. The one conversation this terminal holds is
	// the whole list, and the other is behind the fold with the count on it.
	if len(a.hop.rows) != 1 || a.hop.rest != 1 {
		t.Fatalf("the shut card holds %d rows and folds %d: %+v", len(a.hop.rows), a.hop.rest, a.hop.rows)
	}
	if !strings.Contains(plain(a.hopFoot()), "1 more on this machine") {
		t.Fatalf("the fold says %q", plain(a.hopFoot()))
	}
	// `→` REACHES THEM.
	drive(t, a, key("right"))
	if len(a.hop.rows) != 2 {
		t.Fatalf("the card holds %d rows once the fold is open: %+v", len(a.hop.rows), a.hop.rows)
	}
	// THE ONE ON SCREEN IS THE OPEN HALF, and the other is below the fold.
	if !a.hop.rows[0].open || !a.hop.rows[0].here {
		t.Fatalf("the first row came out as %+v", a.hop.rows[0])
	}
	if a.hop.rows[1].open || !strings.Contains(strings.ToLower(a.hop.rows[1].title), "other") {
		t.Fatalf("the closed row came out as %+v", a.hop.rows[1])
	}
	// AND A QUIET CLOSED ROW SAYS NOTHING. `not open yet` on nine rows is the
	// layout's own fact said nine times; the rule already draws that line.
	if a.hop.rows[1].note != "" {
		t.Fatalf("a quiet closed row says %q", a.hop.rows[1].note)
	}
	// OPENING THE FOLD MOVED THE CURSOR OFF `you are here`, which is what makes
	// the whole gesture `ctrl+k → enter` on a session holding one conversation.
	if a.hop.at != 1 {
		t.Fatalf("the cursor is on row %d after the fold opened", a.hop.at)
	}
	drive(t, a, key("enter"))
	if opened != other {
		t.Fatalf("enter opened %q", opened)
	}
	// AND THE CONVERSATION IT LEFT IS STILL RUNNING — the same bargain home's
	// own enter makes (keeper.go).
	if a.openCount() != 2 {
		t.Fatalf("%d conversations are open after taking a closed row", a.openCount())
	}
}

// TestTheFoldKeepsTheCardAboutWhatIsOpen is the shape the owner asked for: the
// ring is the conversations this terminal is holding, and everything else on the
// machine is behind one door with a count on it.
func TestTheFoldKeepsTheCardAboutWhatIsOpen(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(&fakeAgent{model: "m"})
	a.file, a.workspace = filepath.Join(dir, "this-one.jsonl"), dir
	rows := []session.SessionRow{{ID: "a", Title: "this one", Transcript: a.file, ProjectDir: dir}}
	for i := 0; i < 4; i++ {
		rows = append(rows, session.SessionRow{
			ID: itoa(i), Title: "shut chat " + itoa(i),
			Transcript: filepath.Join(dir, "shut"+itoa(i)+".jsonl"), ProjectDir: dir,
		})
	}
	a.world = func() (session.World, bool) {
		return session.World{Projects: []session.Project{{Name: "lab", Dir: dir, Sessions: rows}}}, true
	}

	drive(t, a, key(hopOpenKey))
	if len(a.hop.rows) != 1 || a.hop.rest != 4 {
		t.Fatalf("the shut card holds %d rows and folds %d", len(a.hop.rows), a.hop.rest)
	}
	// THE FOLD SAYS WHAT OPENING IT WOULD BE WORTH, and names the key.
	foot := plain(a.hopFoot())
	if !strings.Contains(foot, "4 more on this machine") || !strings.Contains(foot, hopFoldKeyWord) {
		t.Fatalf("the fold reads %q", foot)
	}
	// AND THE HEAD COUNTS ONLY WHAT IS OPEN, which is what the card is about.
	if head := plain(a.hopHead(80, a.pal)); !strings.Contains(head, "1 of 5") {
		t.Fatalf("the head reads %q", head)
	}

	drive(t, a, key(hopFoldKey))
	if len(a.hop.rows) != 5 || a.hop.rest != 0 {
		t.Fatalf("the open card holds %d rows and folds %d", len(a.hop.rows), a.hop.rest)
	}
	if foot := plain(a.hopFoot()); !strings.Contains(foot, hopShutKeyWord) {
		t.Fatalf("the open fold reads %q", foot)
	}
	drive(t, a, key(hopShutKey))
	if len(a.hop.rows) != 1 {
		t.Fatalf("`←` left %d rows on the card", len(a.hop.rows))
	}
}

// TestCtrlWDismissesATabAndKeepsItsConversation is the way out, and
// the one guard on it: closing ends the agent, so work turning inside it stops —
// which is what the quit door already warns about, said here on the row.
func TestCtrlWDismissesATabAndKeepsItsConversation(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	older, _ := keepThree(t, a)

	drive(t, a, key(hopOpenKey))
	if a.openCount() != 3 {
		t.Fatalf("%d conversations are open before anything is closed", a.openCount())
	}
	// The cursor opens on the most recent one behind this. Walk to the older.
	drive(t, a, key("down"))
	if !strings.Contains(a.hop.rows[a.hop.at].title, "price scrape") {
		t.Fatalf("the cursor is on %q", a.hop.rows[a.hop.at].title)
	}
	drive(t, a, key(hopAwayKey))
	if a.openCount() != 3 {
		t.Fatalf("ctrl+w stopped holding a conversation: %d open", a.openCount())
	}
	if older.closes != 0 || older.stops != 0 {
		t.Fatal("ctrl+w ended work instead of dismissing a tab")
	}
	// THE CARD STAYS UP AND SAYS SO, because tidying up is something people do
	// two or three of in a row.
	if !a.hopShowing() {
		t.Fatal("the card came down on a close")
	}
	if !strings.Contains(a.hop.say, hopClosedWord) {
		t.Fatalf("the card says %q", a.hop.say)
	}
}

// Repeated dismissal remains harmless even while work is running.
func TestDismissingARunningTabNeverStopsItsWork(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	emptyMachine(a)
	busy := &busyAgent{fakeAgent: &fakeAgent{model: "m"}}
	a.stow(Conversation{Agent: busy, SessionFile: "/tmp/lab/busy.jsonl", Place: "lab"},
		&aside{since: a.now(), title: "the busy one"})
	drive(t, a, key(hopOpenKey), key(hopAwayKey), key(hopAwayKey))
	if a.openCount() != 2 || busy.closes != 0 || busy.stops != 0 {
		t.Fatalf("dismissing stopped work: open=%d closes=%d stops=%d", a.openCount(), busy.closes, busy.stops)
	}
	if !a.tabShut[a.convKey("/tmp/lab/busy.jsonl")] {
		t.Fatal("dismissal did not hide the running tab")
	}
	if !strings.Contains(a.hop.say, hopAwayWord) {
		t.Fatalf("dismissal was not acknowledged: %q", a.hop.say)
	}
}

// busyAgent is a fake with work turning in it — the one door the switcher's own
// close guard reads ([runningTasks]).
type busyAgent struct{ *fakeAgent }

func (b *busyAgent) TaskIndex() []session.TaskIndexEntry {
	return []session.TaskIndexEntry{
		{ID: "1", Status: string(session.TaskRunning)},
		{ID: "2", Status: string(session.TaskRunning)},
		{ID: "3", Status: string(session.TaskDone)},
	}
}

// TestTheReverseChordIsBoundOnlyWhereTheTerminalCanSpellIt is `ctrl+shift+k`,
// which an ordinary terminal sends as a bare `ctrl+k` — so it is bound under the
// same law as `ctrl+tab`, and `shift+tab` is the spelling that always works.
func TestTheReverseChordIsBoundOnlyWhereTheTerminalCanSpellIt(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)

	a.keysDisambiguated = false
	if a.hopBacks(hopBackKey) || a.hopBacks(hopBackAlias) {
		t.Fatal("the reverse chords are bound on a terminal that cannot send them")
	}
	a.keysDisambiguated = true
	if !a.hopBacks(hopBackKey) || !a.hopBacks(hopBackAlias) {
		t.Fatal("the reverse chords are not bound where the terminal answered")
	}
	drive(t, a, key(hopOpenKey), key(hopBackKey))
	if a.hop.at != len(a.hop.rows)-1 {
		t.Fatalf("ctrl+shift+k left the cursor on %d of %d", a.hop.at, len(a.hop.rows))
	}
	// AND `shift+tab` DOES IT ON EVERY TERMINAL, which is why the chord above
	// being absent on half of them costs nothing.
	a.keysDisambiguated = false
	drive(t, a, key("shift+tab"))
	if a.hop.at != len(a.hop.rows)-2 {
		t.Fatalf("shift+tab left the cursor on %d", a.hop.at)
	}
}

// ── QUICK SWITCHING — the press is the switch (hop.go's second mode) ────────

// TestQuickSwitchingSwitchesOnThePressAndTheCardFades is the default gesture:
// each press of the chord lands you in the next conversation at once, the card
// is a receipt, and the pause after the last press is what puts it away.
func TestQuickSwitchingSwitchesOnThePressAndTheCardFades(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	a.hopQuick = true
	a.keysDisambiguated = true
	keepThree(t, a)

	drive(t, a, key(hopAlias))
	if a.file != "/tmp/lab/rail-scope.jsonl" {
		t.Fatalf("the first press left the surface on %q", a.file)
	}
	if !a.hopShowing() || !a.hop.live {
		t.Fatal("the receipt card is not up over the conversation just landed in")
	}
	// THE `you are here` MARK MOVED WITH THE SURFACE: the row slid to wears it,
	// and the row just left does not.
	if !a.hop.rows[a.hop.at].here || a.hop.rows[a.hop.at].note != hopHereWord {
		t.Fatalf("the mark did not move with the switch: %+v", a.hop.rows[a.hop.at])
	}
	for at, row := range a.hop.rows {
		if at != a.hop.at && row.here {
			t.Fatalf("row %d still claims to be where you are", at)
		}
	}

	drive(t, a, key(hopAlias))
	if a.file != "/tmp/lab/price-scrape.jsonl" {
		t.Fatalf("the second press left the surface on %q", a.file)
	}

	// A STALE FADE IS OUTRUN BY CONSTRUCTION: the timer an earlier press
	// scheduled carries an earlier pulse, and the card ignores it.
	drive(t, a, hopSettleMsg{pulse: a.hop.pulse - 1})
	if !a.hopShowing() {
		t.Fatal("a stale fade timer took the card down")
	}
	drive(t, a, hopSettleMsg{pulse: a.hop.pulse})
	if a.hopShowing() {
		t.Fatal("the fade left the card up")
	}
	if a.file != "/tmp/lab/price-scrape.jsonl" {
		t.Fatalf("the fade moved the surface to %q — it is a receipt, not a commit", a.file)
	}
	// AND THE BURST IS SEALED: `tab` goes back to where the burst STARTED, not
	// to the stepping stone it passed through (hop.go's [app.hopSeal]).
	if last, ok := a.lastBehind(); !ok || last != a.convKey("/tmp/lab/this-one.jsonl") {
		t.Fatalf("tab would go to %q after the burst", last)
	}
	if a.openCount() != 3 {
		t.Fatalf("%d conversations are open after the burst — switching is not closing", a.openCount())
	}
}

// TestQuickSwitchingEscTakesTheWholeBurstBack is the undo: the surface has
// already moved, and `esc` is the person saying they were only looking.
func TestQuickSwitchingEscTakesTheWholeBurstBack(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	a.hopQuick = true
	a.keysDisambiguated = true
	keepThree(t, a)

	drive(t, a, key(hopAlias), key(hopAlias))
	if a.file != "/tmp/lab/price-scrape.jsonl" {
		t.Fatalf("two presses landed on %q", a.file)
	}
	drive(t, a, key("esc"))
	if a.hopShowing() {
		t.Fatal("esc left the card up")
	}
	if a.file != "/tmp/lab/this-one.jsonl" {
		t.Fatalf("esc left the surface on %q rather than where the burst began", a.file)
	}
}

// TestTouchingAnythingButTheChordConvertsTheReceiptToTheBrowsingCard: an arrow
// is the person looking rather than switching, so the card stops fading, the
// cursor moves without the surface moving, and `enter` is the commit again.
func TestTouchingAnythingButTheChordConvertsTheReceiptToTheBrowsingCard(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	a.hopQuick = true
	a.keysDisambiguated = true
	keepThree(t, a)

	drive(t, a, key(hopAlias))
	landed := a.file
	drive(t, a, key("down"))
	if a.hop.live {
		t.Fatal("an arrow left the card fading")
	}
	if a.file != landed {
		t.Fatalf("an arrow moved the surface to %q", a.file)
	}
	// THE FADE TIMER THE SLIDE SCHEDULED IS DEAD NOW: a card being read must
	// never go down on its own.
	drive(t, a, hopSettleMsg{pulse: a.hop.pulse})
	if !a.hopShowing() {
		t.Fatal("the fade took down a card somebody was reading")
	}
	drive(t, a, key("enter"))
	if a.hopShowing() {
		t.Fatal("enter left the card up")
	}
}

// TestTypingRidesStraightThroughALiveCard: under quick switching the switch
// already happened, so a letter typed while the receipt lingers lands in the
// draft rather than being eaten by it. The browsing card swallows the same
// letter on purpose, and the pair of claims is the difference between the modes.
func TestTypingRidesStraightThroughALiveCard(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	a.hopQuick = true
	a.keysDisambiguated = true
	keepThree(t, a)

	drive(t, a, key(hopAlias), key("z"))
	if a.hopShowing() {
		t.Fatal("typing left the receipt up")
	}
	if a.input.String() != "z" {
		t.Fatalf("the letter typed over the receipt became %q rather than draft", a.input.String())
	}
}

// TestTheReverseChordEntersTheRingAtTheFarEnd: with the card down, the reverse
// chord opens the ring at the open conversation longest unlooked-at — and under
// quick switching it lands there at once.
func TestTheReverseChordEntersTheRingAtTheFarEnd(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	a.hopQuick = true
	a.keysDisambiguated = true
	keepThree(t, a)

	drive(t, a, key(hopBackAlias))
	if a.file != "/tmp/lab/price-scrape.jsonl" {
		t.Fatalf("the reverse chord landed on %q rather than the far end of the ring", a.file)
	}
	if !a.hopShowing() || !a.hop.live {
		t.Fatal("the receipt card is not up after a reverse entry")
	}
}

// TestQuickSwitchingOffIsTheBrowsingCardAlone: the setting turns the chord back
// into a menu — nothing moves until `enter`.
func TestQuickSwitchingOffIsTheBrowsingCardAlone(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	a.hopQuick = false
	a.keysDisambiguated = true
	keepThree(t, a)

	drive(t, a, key(hopAlias))
	if a.file != "/tmp/lab/this-one.jsonl" {
		t.Fatalf("with quick switching off the press moved the surface to %q", a.file)
	}
	if !a.hopShowing() || a.hop.live {
		t.Fatal("the card should be up, and browsing rather than fading")
	}
}

// Reading the list must never navigate, even with a profile saved while quick
// switch was the default. A timeout is not evidence that Ctrl was released.
func TestCtrlKPreviewsUntilAnExplicitChoiceEvenWithQuickSwitch(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	a.hopQuick = true
	keepThree(t, a)
	drive(t, a, key(hopOpenKey), key(hopOpenKey))
	if a.file != "/tmp/lab/this-one.jsonl" || a.hop.live || a.hop.at != 1 {
		t.Fatal("cycling navigated instead of previewing")
	}
	drive(t, a, hopSettleMsg{pulse: a.hop.pulse})
	if !a.hopShowing() {
		t.Fatal("the list disappeared while being read")
	}
	drive(t, a, key("esc"))
	if a.file != "/tmp/lab/this-one.jsonl" {
		t.Fatal("cancel changed chats")
	}
	drive(t, a, key(hopOpenKey), key(hopOpenKey), key("enter"))
	if a.file != "/tmp/lab/price-scrape.jsonl" || a.hopShowing() {
		t.Fatal("enter did not open the highlighted chat")
	}
}

func TestSwitcherMouseOpensOnlyVisibleRowsAndKeepsSelectionVisible(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)
	drive(t, a, key(hopOpenKey))
	a.hop.originY = 4
	body := make([]string, 16)
	a.hopOver(body, 60, a.pal)
	a.hopPress(0, 0)
	if a.file != "/tmp/lab/this-one.jsonl" {
		t.Fatal("backdrop click switched chats")
	}
	if a.hopShowing() {
		t.Fatal("backdrop click left the switcher open")
	}
	drive(t, a, key(hopOpenKey))
	a.hop.originY = 4
	a.hopOver(body, 60, a.pal)
	target := a.hop.spots[1]
	a.hopPress(a.hop.left+3, a.hop.top+target.row)
	if a.file != "/tmp/lab/price-scrape.jsonl" {
		t.Fatal("click did not open the rendered row")
	}
	drive(t, a, key(hopOpenKey))
	a.hop.at = len(a.hop.rows) - 1
	a.hopCardLines(40, 6, a.pal)
	found := false
	for _, spot := range a.hop.spots {
		found = found || spot.at == a.hop.at
	}
	if !found {
		t.Fatal("short switcher hid the highlighted choice")
	}
}

func TestSwitcherShowsLongSelectedTitleBelowTheList(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)
	drive(t, a, key(hopOpenKey))
	a.hop.rows[0].title = "Investigate the parsing regression in weekly reports"
	lines := a.hopCardLines(60, 18, a.pal)
	if !strings.Contains(plain(strings.Join(lines, "\n")), "weekly reports") {
		t.Fatal("selected title still clipped its distinguishing suffix")
	}
}
